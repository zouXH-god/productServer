package server

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

func str(c map[string]any, k string) string { v, _ := c[k].(string); return v }
func num(c map[string]any, k string) uint {
	switch v := c[k].(type) {
	case float64:
		return uint(v)
	case string:
		n, _ := strconv.Atoi(v)
		return uint(n)
	}
	return 0
}
func serverModule(module string) bool {
	switch module {
	case "sftp_upload", "sftp_extract", "ssh_command", "remote_file_exists":
		return true
	default:
		return false
	}
}

func moduleDisplayName(module string) string {
	names := map[string]string{
		"archive": "归档压缩", "extract": "解压文件", "sftp_upload": "SFTP 上传",
		"sftp_extract": "上传并解压", "ssh_command": "SSH 命令", "http_webhook": "HTTP 回调",
		"checksum_verify": "摘要校验", "remote_file_exists": "远端文件存在判断",
		"value_match": "值匹配", "string_split": "字符串分割", "foreach": "列表循环",
		"loop_end": "循环结束", "server_list": "服务器列表", "server_list_end": "服务器列表结束",
	}
	if name := names[module]; name != "" {
		return name
	}
	return module
}

func stageLog(log *runLogger, node, message string) {
	if log != nil {
		log.write(node, "stage", message)
	}
}

func moduleLogArgs(logging []any) (string, *runLogger) {
	var node string
	var log *runLogger
	if len(logging) > 0 {
		node, _ = logging[0].(string)
	}
	if len(logging) > 1 {
		log, _ = logging[1].(*runLogger)
	}
	return node, log
}
func safePath(root, p string) (string, error) {
	if filepath.IsAbs(p) {
		return "", fmt.Errorf("absolute paths are not allowed")
	}
	x := filepath.Clean(filepath.Join(root, p))
	rel, e := filepath.Rel(root, x)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes workspace")
	}
	return x, nil
}
func render(s string, p Project, r Release, input, work string) string {
	repl := map[string]string{"{{project.id}}": itoa(p.ID), "{{project.name}}": p.Name, "{{release.id}}": itoa(r.ID), "{{release.version}}": r.Version, "{{release.ref_type}}": r.RefType, "{{release.commit_sha}}": r.CommitSHA, "{{release.branch}}": r.Branch, "{{workspace.input}}": input, "{{workspace.work}}": work}
	for k, v := range repl {
		s = strings.ReplaceAll(s, k, v)
	}
	return s
}

type matchedFile struct {
	Path string
	Name string
}

func matchWorkspaceFiles(input, work, expression string) ([]matchedFile, error) {
	re, err := regexp.Compile(expression)
	if err != nil {
		return nil, fmt.Errorf("invalid file regular expression: %w", err)
	}
	result := []matchedFile{}
	for _, root := range []string{input, work} {
		err = filepath.Walk(root, func(current string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if info.IsDir() {
				return nil
			}
			rel, relErr := filepath.Rel(root, current)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			legacyName := strings.TrimPrefix(strings.TrimPrefix(rel, "archive/"), "files/")
			if re.MatchString(rel) || (legacyName != rel && re.MatchString(legacyName)) {
				result = append(result, matchedFile{Path: current, Name: rel})
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func oneMatchedFile(input, work string, c map[string]any) (string, error) {
	matches, err := matchWorkspaceFiles(input, work, str(c, "file_pattern"))
	if err != nil {
		return "", err
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("file regular expression must match exactly one file, matched %d", len(matches))
	}
	return matches[0].Path, nil
}
func stringSplitModule(c map[string]any, logging ...any) (map[string]any, *bool, error) {
	nodeKey, log := moduleLogArgs(logging)
	stageLog(log, nodeKey, "正在解析分隔规则并拆分字符串")
	value, separator := str(c, "value"), str(c, "separator")
	if separator == "" {
		separator = ","
	}
	var parts []string
	if useRegex, _ := c["regex"].(bool); useRegex {
		re, err := regexp.Compile(separator)
		if err != nil {
			return nil, nil, fmt.Errorf("编译分隔正则失败：%w", err)
		}
		parts = re.Split(value, -1)
	} else {
		parts = strings.Split(value, separator)
	}
	items := []string{}
	trim, _ := c["trim"].(bool)
	drop, _ := c["drop_empty"].(bool)
	for _, part := range parts {
		if trim {
			part = strings.TrimSpace(part)
		}
		if drop && part == "" {
			continue
		}
		items = append(items, part)
	}
	if len(items) > 100 {
		return nil, nil, fmt.Errorf("字符串拆分失败：结果超过 100 项")
	}
	stageLog(log, nodeKey, fmt.Sprintf("字符串拆分完成，共生成 %d 项", len(items)))
	return map[string]any{"items": items, "count": len(items)}, nil, nil
}
func valueMatchModule(c map[string]any, logging ...any) (map[string]any, *bool, error) {
	nodeKey, log := moduleLogArgs(logging)
	stageLog(log, nodeKey, fmt.Sprintf("正在执行值匹配：%s", str(c, "operator")))
	actual, expected, operator := str(c, "actual"), str(c, "expected"), str(c, "operator")
	ignore, _ := c["ignore_case"].(bool)
	a, b := actual, expected
	if ignore {
		a, b = strings.ToLower(a), strings.ToLower(b)
	}
	matched := false
	switch operator {
	case "exists":
		matched = a != ""
	case "equals":
		matched = a == b
	case "not_equals":
		matched = a != b
	case "contains":
		matched = strings.Contains(a, b)
	case "regex":
		re, err := regexp.Compile(expected)
		if err != nil {
			return nil, nil, fmt.Errorf("编译匹配正则失败：%w", err)
		}
		matched = re.MatchString(actual)
	default:
		return nil, nil, fmt.Errorf("值匹配失败：未知操作符 %q", operator)
	}
	stageLog(log, nodeKey, fmt.Sprintf("值匹配完成：matched=%t", matched))
	return map[string]any{"matched": matched, "actual": actual}, &matched, nil
}
func remoteFileExistsModule(ctx context.Context, db *gorm.DB, cfg Config, run WorkflowRun, c map[string]any, logging ...any) (map[string]any, *bool, error) {
	nodeKey, log := moduleLogArgs(logging)
	stageLog(log, nodeKey, "正在连接服务器")
	client, err := sshForRun(db, cfg, run, num(c, "connection_id"))
	if err != nil {
		return nil, nil, fmt.Errorf("连接服务器失败：%w", err)
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return nil, nil, fmt.Errorf("创建 SSH 会话失败：%w", err)
	}
	defer session.Close()
	remote := str(c, "path")
	if remote == "" {
		return nil, nil, fmt.Errorf("检查远端文件失败：未配置远端路径")
	}
	stageLog(log, nodeKey, fmt.Sprintf("正在检查远端路径：%s", remote))
	done := make(chan error, 1)
	go func() { done <- session.Run("test -e " + shellQuote(remote)) }()
	exists := false
	select {
	case <-ctx.Done():
		client.Close()
		return nil, nil, ctx.Err()
	case err = <-done:
		if err == nil {
			exists = true
		} else if _, ok := err.(*ssh.ExitError); !ok {
			return nil, nil, fmt.Errorf("执行远端文件检查失败：%w", err)
		}
	}
	stageLog(log, nodeKey, fmt.Sprintf("远端路径检查完成：存在=%t", exists))
	return map[string]any{"matched": exists, "exists": exists, "path": remote}, &exists, nil
}
func executeModule(ctx context.Context, db *gorm.DB, cfg Config, run WorkflowRun, p Project, r Release, n WorkflowNode, nodeKey, input, work string, log *runLogger) (map[string]any, *bool, error) {
	switch n.Type {
	case "archive":
		err := archiveModule(n.Config, input, work, nodeKey, log)
		return map[string]any{"path": str(n.Config, "output")}, nil, err
	case "extract":
		err := extractModule(n.Config, input, work, nodeKey, log)
		return map[string]any{"path": str(n.Config, "output")}, nil, err
	case "checksum_verify":
		err := checksumModule(n.Config, input, work, nodeKey, log)
		return map[string]any{"verified": err == nil}, nil, err
	case "http_webhook":
		return webhookModule(ctx, cfg, n.Config, p, r, input, work, nodeKey, log)
	case "ssh_command":
		return sshCommandModule(ctx, db, cfg, run, n.Config, p, r, input, work, nodeKey, log)
	case "sftp_upload":
		err := sftpModule(ctx, db, cfg, run, n.Config, input, work, nodeKey, log)
		return map[string]any{"destination": str(n.Config, "destination")}, nil, err
	case "sftp_extract":
		err := sftpExtractModule(ctx, db, cfg, run, n.Config, p, r, input, nodeKey, log)
		return map[string]any{"destination": str(n.Config, "destination")}, nil, err
	case "string_split":
		return stringSplitModule(n.Config, nodeKey, log)
	case "value_match":
		return valueMatchModule(n.Config, nodeKey, log)
	case "remote_file_exists":
		return remoteFileExistsModule(ctx, db, cfg, run, n.Config, nodeKey, log)
	case "foreach", "loop_end", "server_list", "server_list_end":
		return map[string]any{}, nil, nil
	}
	return nil, nil, fmt.Errorf("unknown module %s", n.Type)
}
func archiveModule(c map[string]any, input, work string, logging ...any) error {
	nodeKey, log := moduleLogArgs(logging)
	stageLog(log, nodeKey, "正在筛选需要归档的文件")
	pattern := str(c, "input")
	if pattern == "" {
		pattern = "*"
	}
	matches := []matchedFile{}
	if expression := str(c, "file_pattern"); expression != "" {
		var e error
		matches, e = matchWorkspaceFiles(input, work, expression)
		if e != nil {
			return e
		}
	} else {
		legacy, _ := filepath.Glob(filepath.Join(input, pattern))
		if len(legacy) == 0 {
			if x, e := safePath(work, pattern); e == nil {
				legacy, _ = filepath.Glob(x)
			}
		}
		for _, x := range legacy {
			matches = append(matches, matchedFile{Path: x, Name: filepath.Base(x)})
		}
	}
	if len(matches) == 0 {
		return fmt.Errorf("筛选输入文件失败：没有匹配到文件")
	}
	stageLog(log, nodeKey, fmt.Sprintf("已匹配 %d 个文件，准备创建归档", len(matches)))
	out, e := safePath(work, str(c, "output"))
	if e != nil {
		return fmt.Errorf("解析归档输出路径失败：%w", e)
	}
	if out == work {
		return fmt.Errorf("output required")
	}
	os.MkdirAll(filepath.Dir(out), 0755)
	format := str(c, "format")
	stageLog(log, nodeKey, fmt.Sprintf("正在生成 %s 归档：%s", format, str(c, "output")))
	if format == "tar.gz" {
		f, e := os.Create(out)
		if e != nil {
			return fmt.Errorf("创建 tar.gz 输出文件失败：%w", e)
		}
		gz := gzip.NewWriter(f)
		tw := tar.NewWriter(gz)
		for _, match := range matches {
			x := match.Path
			info, _ := os.Stat(x)
			if info.IsDir() {
				continue
			}
			h, headerErr := tar.FileInfoHeader(info, "")
			if headerErr != nil {
				return fmt.Errorf("生成归档条目 %q 失败：%w", match.Name, headerErr)
			}
			h.Name = match.Name
			if e = tw.WriteHeader(h); e != nil {
				return fmt.Errorf("写入归档条目 %q 失败：%w", match.Name, e)
			}
			in, openErr := os.Open(x)
			if openErr != nil {
				return fmt.Errorf("打开归档源文件 %q 失败：%w", match.Name, openErr)
			}
			_, e = io.Copy(tw, in)
			in.Close()
			if e != nil {
				return fmt.Errorf("复制归档源文件 %q 失败：%w", match.Name, e)
			}
		}
		if e = tw.Close(); e != nil {
			return fmt.Errorf("完成 tar 归档失败：%w", e)
		}
		if e = gz.Close(); e != nil {
			return fmt.Errorf("完成 gzip 压缩失败：%w", e)
		}
		if e = f.Close(); e != nil {
			return fmt.Errorf("关闭归档输出文件失败：%w", e)
		}
		return nil
	}
	f, e := os.Create(out)
	if e != nil {
		return fmt.Errorf("创建 ZIP 输出文件失败：%w", e)
	}
	zw := zip.NewWriter(f)
	for _, match := range matches {
		x := match.Path
		info, _ := os.Stat(x)
		if info.IsDir() {
			continue
		}
		h, headerErr := zip.FileInfoHeader(info)
		if headerErr != nil {
			return fmt.Errorf("生成 ZIP 条目 %q 失败：%w", match.Name, headerErr)
		}
		h.Name = match.Name
		h.SetModTime(time.Unix(0, 0))
		w, createErr := zw.CreateHeader(h)
		if createErr != nil {
			return fmt.Errorf("创建 ZIP 条目 %q 失败：%w", match.Name, createErr)
		}
		in, openErr := os.Open(x)
		if openErr != nil {
			return fmt.Errorf("打开归档源文件 %q 失败：%w", match.Name, openErr)
		}
		_, e = io.Copy(w, in)
		in.Close()
		if e != nil {
			return fmt.Errorf("复制归档源文件 %q 失败：%w", match.Name, e)
		}
	}
	if e = zw.Close(); e != nil {
		return fmt.Errorf("完成 ZIP 归档失败：%w", e)
	}
	if e = f.Close(); e != nil {
		return fmt.Errorf("关闭归档输出文件失败：%w", e)
	}
	return nil
}
func extractModule(c map[string]any, input, work string, logging ...any) error {
	nodeKey, log := moduleLogArgs(logging)
	stageLog(log, nodeKey, "正在定位待解压文件")
	var src string
	var e error
	if str(c, "file_pattern") != "" {
		src, e = oneMatchedFile(input, work, c)
	} else {
		src, e = safePath(input, str(c, "source"))
	}
	if e != nil {
		return fmt.Errorf("定位待解压文件失败：%w", e)
	}
	if _, e = os.Stat(src); e != nil {
		src, e = safePath(work, str(c, "source"))
		if e != nil {
			return e
		}
	}
	dst, e := safePath(work, str(c, "output"))
	if e != nil {
		return fmt.Errorf("解析解压目标目录失败：%w", e)
	}
	if e = os.MkdirAll(dst, 0755); e != nil {
		return fmt.Errorf("创建解压目标目录失败：%w", e)
	}
	stageLog(log, nodeKey, fmt.Sprintf("正在解压 %s 到 %s", filepath.Base(src), str(c, "output")))
	if strings.HasSuffix(strings.ToLower(src), ".zip") {
		z, e := zip.OpenReader(src)
		if e != nil {
			return fmt.Errorf("打开 ZIP 压缩包失败：%w", e)
		}
		defer z.Close()
		for _, f := range z.File {
			target, e := safePath(dst, f.Name)
			if e != nil {
				return fmt.Errorf("校验 ZIP 条目 %q 失败：%w", f.Name, e)
			}
			if f.FileInfo().IsDir() {
				os.MkdirAll(target, 0755)
				continue
			}
			os.MkdirAll(filepath.Dir(target), 0755)
			in, e := f.Open()
			if e != nil {
				return fmt.Errorf("读取 ZIP 条目 %q 失败：%w", f.Name, e)
			}
			out, e := os.Create(target)
			if e == nil {
				_, e = io.Copy(out, in)
				out.Close()
			}
			in.Close()
			if e != nil {
				return fmt.Errorf("写入解压文件 %q 失败：%w", f.Name, e)
			}
		}
		stageLog(log, nodeKey, fmt.Sprintf("ZIP 解压完成，共处理 %d 个条目", len(z.File)))
		return nil
	}
	f, e := os.Open(src)
	if e != nil {
		return fmt.Errorf("打开 tar.gz 压缩包失败：%w", e)
	}
	defer f.Close()
	gz, e := gzip.NewReader(f)
	if e != nil {
		return fmt.Errorf("读取 gzip 数据失败：%w", e)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	entries := 0
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return fmt.Errorf("读取 tar 条目失败：%w", e)
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir {
			return fmt.Errorf("解压失败：不支持的归档条目 %q", h.Name)
		}
		target, e := safePath(dst, h.Name)
		if e != nil {
			return fmt.Errorf("校验 tar 条目 %q 失败：%w", h.Name, e)
		}
		if h.Typeflag == tar.TypeDir {
			os.MkdirAll(target, 0755)
			continue
		}
		os.MkdirAll(filepath.Dir(target), 0755)
		out, e := os.Create(target)
		if e != nil {
			return fmt.Errorf("创建解压文件 %q 失败：%w", h.Name, e)
		}
		_, e = io.Copy(out, tr)
		out.Close()
		if e != nil {
			return fmt.Errorf("写入解压文件 %q 失败：%w", h.Name, e)
		}
		entries++
	}
	stageLog(log, nodeKey, fmt.Sprintf("tar.gz 解压完成，共处理 %d 个文件", entries))
	return nil
}
func checksumModule(c map[string]any, input, work string, logging ...any) error {
	nodeKey, log := moduleLogArgs(logging)
	stageLog(log, nodeKey, "正在定位待校验文件")
	var file string
	var e error
	if str(c, "file_pattern") != "" {
		file, e = oneMatchedFile(input, work, c)
	} else {
		file, e = safePath(input, str(c, "file"))
	}
	if e != nil {
		return fmt.Errorf("定位待校验文件失败：%w", e)
	}
	if _, e = os.Stat(file); e != nil {
		file, e = safePath(work, str(c, "file"))
		if e != nil {
			return e
		}
	}
	f, e := os.Open(file)
	if e != nil {
		return fmt.Errorf("打开待校验文件失败：%w", e)
	}
	defer f.Close()
	var actual string
	stageLog(log, nodeKey, fmt.Sprintf("正在使用 %s 校验 %s", str(c, "algorithm"), filepath.Base(file)))
	if str(c, "algorithm") == "sha512" {
		h := sha512.New()
		io.Copy(h, f)
		actual = hex.EncodeToString(h.Sum(nil))
	} else {
		h := sha256.New()
		io.Copy(h, f)
		actual = hex.EncodeToString(h.Sum(nil))
	}
	if !strings.EqualFold(actual, str(c, "expected")) {
		return fmt.Errorf("摘要校验失败：期望 %s，实际 %s", str(c, "expected"), actual)
	}
	stageLog(log, nodeKey, fmt.Sprintf("摘要校验通过：%s", actual))
	return nil
}
func webhookAllowed(raw, allow string) bool {
	u, e := url.Parse(raw)
	if e != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	for _, item := range strings.Split(allow, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, n, e := net.ParseCIDR(item); e == nil && ip != nil && n.Contains(ip) {
			return true
		}
		if strings.EqualFold(host, item) || strings.HasSuffix(host, "."+item) {
			return true
		}
	}
	return false
}
func webhookModule(ctx context.Context, cfg Config, c map[string]any, p Project, r Release, input, work string, logging ...any) (map[string]any, *bool, error) {
	nodeKey, log := moduleLogArgs(logging)
	raw := render(str(c, "url"), p, r, input, work)
	stageLog(log, nodeKey, "正在校验 HTTP 回调目标")
	if !webhookAllowed(raw, cfg.WebhookAllowlist) {
		return nil, nil, fmt.Errorf("校验回调地址失败：目标 URL 不在允许列表中")
	}
	body := render(str(c, "body"), p, r, input, work)
	req, e := http.NewRequestWithContext(ctx, strings.ToUpper(str(c, "method")), raw, strings.NewReader(body))
	if e != nil {
		return nil, nil, fmt.Errorf("创建 HTTP 请求失败：%w", e)
	}
	if req.Method == "" {
		req.Method = "POST"
	}
	if secret := str(c, "secret"); secret != "" {
		secret = strings.TrimPrefix(secret, "enc:")
		plain, e := decryptSecret(cfg.SecretEncryptionKey, secret)
		if e != nil {
			return nil, nil, fmt.Errorf("解密 HTTP 回调密钥失败：%w", e)
		}
		req.Header.Set("Authorization", "Bearer "+plain)
	}
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if !webhookAllowed(req.URL.String(), cfg.WebhookAllowlist) {
			return fmt.Errorf("redirect denied")
		}
		if len(via) > 3 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	}}
	stageLog(log, nodeKey, fmt.Sprintf("正在发送 %s 请求到 %s", req.Method, req.URL.Host))
	resp, e := client.Do(req)
	if e != nil {
		return nil, nil, fmt.Errorf("发送 HTTP 回调失败：%w", e)
	}
	defer resp.Body.Close()
	stageLog(log, nodeKey, fmt.Sprintf("收到 HTTP 响应：%s", resp.Status))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return map[string]any{"status_code": resp.StatusCode}, nil, fmt.Errorf("HTTP 回调状态校验失败：服务端返回 %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return nil, nil, fmt.Errorf("读取 HTTP 响应失败：%w", err)
	}
	if len(data) > 1<<20 {
		return nil, nil, fmt.Errorf("读取 HTTP 响应失败：正文超过 1 MiB")
	}
	return map[string]any{"status_code": resp.StatusCode, "body": string(data)}, nil, nil
}
func sshForRun(db *gorm.DB, cfg Config, run WorkflowRun, id uint) (*ssh.Client, error) {
	var project Project
	if e := db.First(&project, run.ProjectID).Error; e != nil {
		return nil, e
	}
	a := &App{db: db, cfg: cfg}
	return a.sshClient(id, project.UserID)
}

type liveLogWriter struct {
	mu        sync.Mutex
	log       *runLogger
	node      string
	stream    string
	buf       bytes.Buffer
	truncated bool
}

func (w *liveLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.log != nil {
		for _, line := range strings.Split(strings.TrimRight(string(p), "\r\n"), "\n") {
			if line != "" {
				w.log.write(w.node, w.stream, strings.TrimSuffix(line, "\r"))
			}
		}
	}
	remaining := (1 << 20) - w.buf.Len()
	if remaining > 0 {
		chunk := p
		if len(chunk) > remaining {
			chunk = chunk[:remaining]
		}
		_, _ = w.buf.Write(chunk)
	}
	if len(p) > remaining {
		w.truncated = true
	}
	return len(p), nil
}

func (w *liveLogWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func sshCommandModule(ctx context.Context, db *gorm.DB, cfg Config, run WorkflowRun, c map[string]any, p Project, r Release, input, work, nodeKey string, log *runLogger) (map[string]any, *bool, error) {
	stageLog(log, nodeKey, "正在连接 SSH 服务器")
	client, e := sshForRun(db, cfg, run, num(c, "connection_id"))
	if e != nil {
		return nil, nil, fmt.Errorf("连接 SSH 服务器失败：%w", e)
	}
	defer client.Close()
	session, e := client.NewSession()
	if e != nil {
		return nil, nil, fmt.Errorf("创建 SSH 会话失败：%w", e)
	}
	defer session.Close()
	commands := []string{}
	if raw, ok := c["commands"].([]any); ok {
		for _, v := range raw {
			commands = append(commands, render(fmt.Sprint(v), p, r, input, work))
		}
	}
	for i, command := range commands {
		log.write(nodeKey, "command", fmt.Sprintf("[%d/%d] $ %s", i+1, len(commands), command))
	}
	if len(commands) == 0 {
		return nil, nil, fmt.Errorf("准备 SSH 命令失败：命令列表为空")
	}
	cmd := "set -e\n" + strings.Join(commands, "\n")
	wd := str(c, "work_dir")
	if wd == "" {
		wd = str(c, "workdir")
	}
	if wd != "" {
		cmd = "cd " + shellQuote(render(wd, p, r, input, work)) + "\n" + cmd
	}
	stdout := &liveLogWriter{log: log, node: nodeKey, stream: "stdout"}
	stderr := &liveLogWriter{log: log, node: nodeKey, stream: "stderr"}
	session.Stdout = stdout
	session.Stderr = stderr
	done := make(chan error, 1)
	go func() {
		done <- session.Run(cmd)
	}()
	select {
	case <-ctx.Done():
		client.Close()
		return nil, nil, fmt.Errorf("执行 SSH 命令超时或已取消：%w", ctx.Err())
	case runErr := <-done:
		outputs := map[string]any{"stdout": stdout.String(), "stderr": stderr.String(), "exit_code": 0}
		if stdout.truncated || stderr.truncated {
			if runErr == nil {
				runErr = fmt.Errorf("SSH output exceeds 1 MiB")
			}
		}
		if runErr != nil {
			if exit, ok := runErr.(*ssh.ExitError); ok {
				outputs["exit_code"] = exit.ExitStatus()
				runErr = fmt.Errorf("执行 SSH 命令失败：远端进程退出码 %d", exit.ExitStatus())
			} else {
				runErr = fmt.Errorf("执行 SSH 命令失败：%w", runErr)
			}
		}
		return outputs, nil, runErr
	}
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func actionArchive(r Release) (ArtifactFile, error) {
	archives := []ArtifactFile{}
	for _, file := range r.Files {
		name := strings.ToLower(file.OriginalName)
		if (file.Kind == "" || file.Kind == "uploaded") && (strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".tgz")) {
			archives = append(archives, file)
		}
	}
	if len(archives) != 1 {
		return ArtifactFile{}, fmt.Errorf("release must contain exactly one Action upload archive, found %d", len(archives))
	}
	return archives[0], nil
}

func remoteExtractCommand(archive, destination, permission string, keep bool) (string, error) {
	if strings.TrimSpace(destination) == "" {
		return "", fmt.Errorf("remote destination is required")
	}
	if permission == "" {
		permission = "0755"
	}
	if ok, _ := regexp.MatchString(`^[0-7]{3,4}$`, permission); !ok {
		return "", fmt.Errorf("permission must be a 3 or 4 digit octal mode")
	}
	lower := strings.ToLower(archive)
	staging := archive + ".extracting"
	var extract string
	if strings.HasSuffix(lower, ".zip") {
		extract = "unzip -oq " + shellQuote(archive) + " -d " + shellQuote(staging)
	} else if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		extract = "tar -xzf " + shellQuote(archive) + " -C " + shellQuote(staging)
	} else {
		return "", fmt.Errorf("unsupported Action archive format")
	}
	step := func(name, command string) string {
		quotedName := shellQuote(name)
		return "printf '[阶段开始] %s\\n' " + quotedName + "; if " + command + "; then printf '[阶段成功] %s\\n' " + quotedName + "; else code=$?; printf '[阶段失败] %s（退出码 %s）\\n' " + quotedName + " \"$code\" >&2; exit \"$code\"; fi"
	}
	commands := []string{
		"set -e",
		step("准备目标目录", "mkdir -p "+shellQuote(destination)),
		step("清理临时目录", "rm -rf "+shellQuote(staging)),
		step("创建临时目录", "mkdir -p "+shellQuote(staging)),
		"trap " + shellQuote("rm -rf "+shellQuote(staging)) + " EXIT",
		step("解压产物", extract),
		step("设置文件权限", "chmod -R "+permission+" "+shellQuote(staging)),
		step("复制到目标目录", "cp -a "+shellQuote(staging)+"/. "+shellQuote(destination)+"/"),
		step("清理解压临时目录", "rm -rf "+shellQuote(staging)),
		"trap - EXIT",
	}
	if !keep {
		commands = append(commands, step("删除远端压缩包", "rm -f "+shellQuote(archive)))
	}
	return strings.Join(commands, "\n"), nil
}

func sftpExtractModule(ctx context.Context, db *gorm.DB, cfg Config, run WorkflowRun, c map[string]any, p Project, r Release, input, nodeKey string, log *runLogger) error {
	stageLog(log, nodeKey, "正在查找 Action 上传的唯一压缩包")
	archive, err := actionArchive(r)
	if err != nil {
		return fmt.Errorf("选择待上传压缩包失败：%w", err)
	}
	local, err := releaseWorkspacePath(input, archive)
	if err != nil {
		return fmt.Errorf("解析本地压缩包路径失败：%w", err)
	}
	destination := render(str(c, "destination"), p, r, input, "")
	remoteArchive := pathJoinRemote(destination, fmt.Sprintf(".productserver-%d-%s", run.ID, filepath.Base(archive.OriginalName)))
	keep, _ := c["keep_archive"].(bool)
	command, err := remoteExtractCommand(remoteArchive, destination, str(c, "permission"), keep)
	if err != nil {
		return fmt.Errorf("生成远端解压命令失败：%w", err)
	}
	stageLog(log, nodeKey, "正在连接目标服务器")
	client, err := sshForRun(db, cfg, run, num(c, "connection_id"))
	if err != nil {
		return fmt.Errorf("连接目标服务器失败：%w", err)
	}
	defer client.Close()
	sf, err := sftp.NewClient(client)
	if err != nil {
		return fmt.Errorf("创建 SFTP 会话失败：%w", err)
	}
	if err = sftpFile(ctx, sf, local, remoteArchive, nodeKey, log); err != nil {
		sf.Close()
		return fmt.Errorf("上传压缩包失败（%s → %s）：%w", filepath.Base(local), remoteArchive, err)
	}
	sf.Close()
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("创建远端解压 SSH 会话失败：%w", err)
	}
	defer session.Close()
	stdout := &liveLogWriter{log: log, node: nodeKey, stream: "stdout"}
	stderr := &liveLogWriter{log: log, node: nodeKey, stream: "stderr"}
	session.Stdout = stdout
	session.Stderr = stderr
	log.write(nodeKey, "command", fmt.Sprintf("在远端解压 %s 到 %s，并设置权限 %s", filepath.Base(remoteArchive), destination, str(c, "permission")))
	done := make(chan error, 1)
	go func() {
		done <- session.Run(command)
	}()
	select {
	case <-ctx.Done():
		client.Close()
		return fmt.Errorf("远端解压超时或已取消：%w", ctx.Err())
	case err = <-done:
		if err != nil {
			return fmt.Errorf("远端解压流程失败，请查看上方最后一个“阶段失败”日志：%w", err)
		}
		stageLog(log, nodeKey, "远端解压、赋权和部署全部完成")
		return nil
	}
}

func sftpModule(ctx context.Context, db *gorm.DB, cfg Config, run WorkflowRun, c map[string]any, input, work, nodeKey string, log *runLogger) error {
	stageLog(log, nodeKey, "正在连接目标服务器")
	client, e := sshForRun(db, cfg, run, num(c, "connection_id"))
	if e != nil {
		return fmt.Errorf("连接目标服务器失败：%w", e)
	}
	defer client.Close()
	sf, e := sftp.NewClient(client)
	if e != nil {
		return fmt.Errorf("创建 SFTP 会话失败：%w", e)
	}
	defer sf.Close()
	if expression := str(c, "file_pattern"); expression != "" {
		matches, err := matchWorkspaceFiles(input, work, expression)
		if err != nil {
			return fmt.Errorf("筛选上传文件失败：%w", err)
		}
		if len(matches) == 0 {
			return fmt.Errorf("筛选上传文件失败：没有匹配到文件")
		}
		stageLog(log, nodeKey, fmt.Sprintf("已匹配 %d 个文件，开始上传", len(matches)))
		remote := str(c, "destination")
		for _, match := range matches {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			if err = sftpFile(ctx, sf, match.Path, pathJoinRemote(remote, match.Name), nodeKey, log); err != nil {
				return fmt.Errorf("upload %s: %w", match.Name, err)
			}
		}
		return nil
	}
	local, e := safePath(input, str(c, "source"))
	if e != nil {
		return e
	}
	if _, e = os.Stat(local); e != nil {
		local, e = safePath(work, str(c, "source"))
		if e != nil {
			return e
		}
	}
	remote := str(c, "destination")
	info, e := os.Stat(local)
	if e != nil {
		return e
	}
	if !info.IsDir() {
		return sftpFile(ctx, sf, local, remote, nodeKey, log)
	}
	return filepath.Walk(local, func(p string, i os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		rel, _ := filepath.Rel(local, p)
		target := pathJoinRemote(remote, rel)
		if i.IsDir() {
			return sf.MkdirAll(target)
		}
		return sftpFile(ctx, sf, p, target, nodeKey, log)
	})
}
func pathJoinRemote(a, b string) string {
	return strings.TrimRight(a, "/") + "/" + strings.ReplaceAll(b, "\\", "/")
}
func sftpFile(ctx context.Context, sf *sftp.Client, local, remote, nodeKey string, log *runLogger) error {
	in, e := os.Open(local)
	if e != nil {
		return e
	}
	defer in.Close()
	sf.MkdirAll(pathDirRemote(remote))
	out, e := sf.Create(remote)
	if e != nil {
		return e
	}
	info, _ := in.Stat()
	total := int64(0)
	if info != nil {
		total = info.Size()
	}
	if log != nil {
		log.write(nodeKey, "progress", fmt.Sprintf("开始上传 %s → %s（%s）", filepath.Base(local), remote, humanBytes(total)))
	}
	var uploaded int64
	lastReport := time.Now()
	buffer := make([]byte, 256*1024)
	for {
		select {
		case <-ctx.Done():
			_ = out.Close()
			return ctx.Err()
		default:
		}
		n, readErr := in.Read(buffer)
		if n > 0 {
			written, writeErr := out.Write(buffer[:n])
			uploaded += int64(written)
			if writeErr != nil {
				e = writeErr
				break
			}
			if written != n {
				e = io.ErrShortWrite
				break
			}
			if log != nil && (time.Since(lastReport) >= 500*time.Millisecond || uploaded == total) {
				percent := float64(0)
				if total > 0 {
					percent = float64(uploaded) * 100 / float64(total)
				}
				log.write(nodeKey, "progress", fmt.Sprintf("上传进度 %.1f%%（%s / %s）", percent, humanBytes(uploaded), humanBytes(total)))
				lastReport = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			e = readErr
			break
		}
	}
	ce := out.Close()
	if e != nil {
		return e
	}
	if log != nil {
		log.write(nodeKey, "progress", fmt.Sprintf("上传完成：%s（%s）", remote, humanBytes(uploaded)))
	}
	return ce
}

func humanBytes(size int64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	value := float64(size)
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d %s", size, units[unit])
	}
	return fmt.Sprintf("%.1f %s", value, units[unit])
}
func pathDirRemote(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return "."
}
