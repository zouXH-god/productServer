package main

import (
	"archive/tar"
	"archive/zip"
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
	repl := map[string]string{"{{project.id}}": itoa(p.ID), "{{project.name}}": p.Name, "{{release.id}}": itoa(r.ID), "{{release.version}}": r.Version, "{{release.ref_type}}": r.RefType, "{{release.commit_sha}}": r.CommitSHA, "{{workspace.input}}": input, "{{workspace.work}}": work}
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
			if re.MatchString(rel) {
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
func stringSplitModule(c map[string]any) (map[string]any, *bool, error) {
	value, separator := str(c, "value"), str(c, "separator")
	if separator == "" {
		separator = ","
	}
	var parts []string
	if useRegex, _ := c["regex"].(bool); useRegex {
		re, err := regexp.Compile(separator)
		if err != nil {
			return nil, nil, err
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
		return nil, nil, fmt.Errorf("split result exceeds 100 items")
	}
	return map[string]any{"items": items, "count": len(items)}, nil, nil
}
func valueMatchModule(c map[string]any) (map[string]any, *bool, error) {
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
			return nil, nil, err
		}
		matched = re.MatchString(actual)
	default:
		return nil, nil, fmt.Errorf("unsupported match operator")
	}
	return map[string]any{"matched": matched, "actual": actual}, &matched, nil
}
func remoteFileExistsModule(ctx context.Context, db *gorm.DB, cfg Config, run WorkflowRun, c map[string]any) (map[string]any, *bool, error) {
	client, err := sshForRun(db, cfg, run, num(c, "connection_id"))
	if err != nil {
		return nil, nil, err
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return nil, nil, err
	}
	defer session.Close()
	remote := str(c, "path")
	if remote == "" {
		return nil, nil, fmt.Errorf("remote path required")
	}
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
			return nil, nil, err
		}
	}
	return map[string]any{"matched": exists, "exists": exists, "path": remote}, &exists, nil
}
func executeModule(ctx context.Context, db *gorm.DB, cfg Config, run WorkflowRun, p Project, r Release, n WorkflowNode, input, work string, log *runLogger) (map[string]any, *bool, error) {
	switch n.Type {
	case "archive":
		err := archiveModule(n.Config, input, work)
		return map[string]any{"path": str(n.Config, "output")}, nil, err
	case "extract":
		err := extractModule(n.Config, input, work)
		return map[string]any{"path": str(n.Config, "output")}, nil, err
	case "checksum_verify":
		err := checksumModule(n.Config, input, work)
		return map[string]any{"verified": err == nil}, nil, err
	case "http_webhook":
		return webhookModule(ctx, cfg, n.Config, p, r, input, work)
	case "ssh_command":
		return sshCommandModule(ctx, db, cfg, run, n.Config, p, r, input, work, log)
	case "sftp_upload":
		err := sftpModule(ctx, db, cfg, run, n.Config, input, work)
		return map[string]any{"destination": str(n.Config, "destination")}, nil, err
	case "sftp_extract":
		err := sftpExtractModule(ctx, db, cfg, run, n.Config, p, r, input, log)
		return map[string]any{"destination": str(n.Config, "destination")}, nil, err
	case "string_split":
		return stringSplitModule(n.Config)
	case "value_match":
		return valueMatchModule(n.Config)
	case "remote_file_exists":
		return remoteFileExistsModule(ctx, db, cfg, run, n.Config)
	case "foreach", "loop_end", "server_list", "server_list_end":
		return map[string]any{}, nil, nil
	}
	return nil, nil, fmt.Errorf("unknown module %s", n.Type)
}
func archiveModule(c map[string]any, input, work string) error {
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
		return fmt.Errorf("file selection matched no files")
	}
	out, e := safePath(work, str(c, "output"))
	if e != nil {
		return e
	}
	if out == work {
		return fmt.Errorf("output required")
	}
	os.MkdirAll(filepath.Dir(out), 0755)
	format := str(c, "format")
	if format == "tar.gz" {
		f, e := os.Create(out)
		if e != nil {
			return e
		}
		gz := gzip.NewWriter(f)
		tw := tar.NewWriter(gz)
		for _, match := range matches {
			x := match.Path
			info, _ := os.Stat(x)
			if info.IsDir() {
				continue
			}
			h, _ := tar.FileInfoHeader(info, "")
			h.Name = match.Name
			tw.WriteHeader(h)
			in, _ := os.Open(x)
			io.Copy(tw, in)
			in.Close()
		}
		tw.Close()
		gz.Close()
		return f.Close()
	}
	f, e := os.Create(out)
	if e != nil {
		return e
	}
	zw := zip.NewWriter(f)
	for _, match := range matches {
		x := match.Path
		info, _ := os.Stat(x)
		if info.IsDir() {
			continue
		}
		h, _ := zip.FileInfoHeader(info)
		h.Name = match.Name
		h.SetModTime(time.Unix(0, 0))
		w, _ := zw.CreateHeader(h)
		in, _ := os.Open(x)
		io.Copy(w, in)
		in.Close()
	}
	zw.Close()
	return f.Close()
}
func extractModule(c map[string]any, input, work string) error {
	var src string
	var e error
	if str(c, "file_pattern") != "" {
		src, e = oneMatchedFile(input, work, c)
	} else {
		src, e = safePath(input, str(c, "source"))
	}
	if e != nil {
		return e
	}
	if _, e = os.Stat(src); e != nil {
		src, e = safePath(work, str(c, "source"))
		if e != nil {
			return e
		}
	}
	dst, e := safePath(work, str(c, "output"))
	if e != nil {
		return e
	}
	os.MkdirAll(dst, 0755)
	if strings.HasSuffix(strings.ToLower(src), ".zip") {
		z, e := zip.OpenReader(src)
		if e != nil {
			return e
		}
		defer z.Close()
		for _, f := range z.File {
			target, e := safePath(dst, f.Name)
			if e != nil {
				return e
			}
			if f.FileInfo().IsDir() {
				os.MkdirAll(target, 0755)
				continue
			}
			os.MkdirAll(filepath.Dir(target), 0755)
			in, e := f.Open()
			if e != nil {
				return e
			}
			out, e := os.Create(target)
			if e == nil {
				_, e = io.Copy(out, in)
				out.Close()
			}
			in.Close()
			if e != nil {
				return e
			}
		}
		return nil
	}
	f, e := os.Open(src)
	if e != nil {
		return e
	}
	defer f.Close()
	gz, e := gzip.NewReader(f)
	if e != nil {
		return e
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir {
			return fmt.Errorf("unsupported archive entry")
		}
		target, e := safePath(dst, h.Name)
		if e != nil {
			return e
		}
		if h.Typeflag == tar.TypeDir {
			os.MkdirAll(target, 0755)
			continue
		}
		os.MkdirAll(filepath.Dir(target), 0755)
		out, e := os.Create(target)
		if e != nil {
			return e
		}
		_, e = io.Copy(out, tr)
		out.Close()
		if e != nil {
			return e
		}
	}
	return nil
}
func checksumModule(c map[string]any, input, work string) error {
	var file string
	var e error
	if str(c, "file_pattern") != "" {
		file, e = oneMatchedFile(input, work, c)
	} else {
		file, e = safePath(input, str(c, "file"))
	}
	if e != nil {
		return e
	}
	if _, e = os.Stat(file); e != nil {
		file, e = safePath(work, str(c, "file"))
		if e != nil {
			return e
		}
	}
	f, e := os.Open(file)
	if e != nil {
		return e
	}
	defer f.Close()
	var actual string
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
		return fmt.Errorf("checksum mismatch: got %s", actual)
	}
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
func webhookModule(ctx context.Context, cfg Config, c map[string]any, p Project, r Release, input, work string) (map[string]any, *bool, error) {
	raw := render(str(c, "url"), p, r, input, work)
	if !webhookAllowed(raw, cfg.WebhookAllowlist) {
		return nil, nil, fmt.Errorf("webhook URL is not allowed")
	}
	body := render(str(c, "body"), p, r, input, work)
	req, e := http.NewRequestWithContext(ctx, strings.ToUpper(str(c, "method")), raw, strings.NewReader(body))
	if e != nil {
		return nil, nil, e
	}
	if req.Method == "" {
		req.Method = "POST"
	}
	if secret := str(c, "secret"); secret != "" {
		secret = strings.TrimPrefix(secret, "enc:")
		plain, e := decryptSecret(cfg.SecretEncryptionKey, secret)
		if e != nil {
			return nil, nil, e
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
	resp, e := client.Do(req)
	if e != nil {
		return nil, nil, e
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return map[string]any{"status_code": resp.StatusCode}, nil, fmt.Errorf("webhook returned %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return nil, nil, err
	}
	if len(data) > 1<<20 {
		return nil, nil, fmt.Errorf("HTTP response exceeds 1 MiB")
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
func sshCommandModule(ctx context.Context, db *gorm.DB, cfg Config, run WorkflowRun, c map[string]any, p Project, r Release, input, work string, log *runLogger) (map[string]any, *bool, error) {
	client, e := sshForRun(db, cfg, run, num(c, "connection_id"))
	if e != nil {
		return nil, nil, e
	}
	defer client.Close()
	session, e := client.NewSession()
	if e != nil {
		return nil, nil, e
	}
	defer session.Close()
	commands := []string{}
	if raw, ok := c["commands"].([]any); ok {
		for _, v := range raw {
			commands = append(commands, render(fmt.Sprint(v), p, r, input, work))
		}
	}
	cmd := strings.Join(commands, "\n")
	wd := str(c, "work_dir")
	if wd == "" {
		wd = str(c, "workdir")
	}
	if wd != "" {
		cmd = "cd " + shellQuote(render(wd, p, r, input, work)) + "\n" + cmd
	}
	type commandResult struct {
		output []byte
		err    error
	}
	done := make(chan commandResult, 1)
	go func() {
		out, e := session.CombinedOutput(cmd)
		if len(out) > 1<<20 {
			out = out[:1<<20]
			if e == nil {
				e = fmt.Errorf("SSH output exceeds 1 MiB")
			}
		}
		done <- commandResult{out, e}
	}()
	select {
	case <-ctx.Done():
		client.Close()
		return nil, nil, ctx.Err()
	case result := <-done:
		outputs := map[string]any{"stdout": string(result.output), "stderr": "", "exit_code": 0}
		if result.err != nil {
			if exit, ok := result.err.(*ssh.ExitError); ok {
				outputs["exit_code"] = exit.ExitStatus()
			}
		}
		return outputs, nil, result.err
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
	commands := []string{
		"set -e",
		"mkdir -p " + shellQuote(destination),
		"rm -rf " + shellQuote(staging),
		"mkdir -p " + shellQuote(staging),
		"trap " + shellQuote("rm -rf "+shellQuote(staging)) + " EXIT",
		extract,
		"chmod -R " + permission + " " + shellQuote(staging),
		"cp -a " + shellQuote(staging) + "/. " + shellQuote(destination) + "/",
		"rm -rf " + shellQuote(staging),
		"trap - EXIT",
	}
	if !keep {
		commands = append(commands, "rm -f "+shellQuote(archive))
	}
	return strings.Join(commands, "\n"), nil
}

func sftpExtractModule(ctx context.Context, db *gorm.DB, cfg Config, run WorkflowRun, c map[string]any, p Project, r Release, input string, log *runLogger) error {
	archive, err := actionArchive(r)
	if err != nil {
		return err
	}
	local, err := safePath(input, archive.OriginalName)
	if err != nil {
		return err
	}
	destination := render(str(c, "destination"), p, r, input, "")
	remoteArchive := pathJoinRemote(destination, fmt.Sprintf(".productserver-%d-%s", run.ID, filepath.Base(archive.OriginalName)))
	keep, _ := c["keep_archive"].(bool)
	command, err := remoteExtractCommand(remoteArchive, destination, str(c, "permission"), keep)
	if err != nil {
		return err
	}
	client, err := sshForRun(db, cfg, run, num(c, "connection_id"))
	if err != nil {
		return err
	}
	defer client.Close()
	sf, err := sftp.NewClient(client)
	if err != nil {
		return err
	}
	if err = sftpFile(sf, local, remoteArchive); err != nil {
		sf.Close()
		return fmt.Errorf("upload Action archive: %w", err)
	}
	sf.Close()
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	done := make(chan error, 1)
	go func() {
		output, runErr := session.CombinedOutput(command)
		log.write("sftp_extract", "stdout", string(output))
		done <- runErr
	}()
	select {
	case <-ctx.Done():
		client.Close()
		return ctx.Err()
	case err = <-done:
		return err
	}
}

func sftpModule(ctx context.Context, db *gorm.DB, cfg Config, run WorkflowRun, c map[string]any, input, work string) error {
	client, e := sshForRun(db, cfg, run, num(c, "connection_id"))
	if e != nil {
		return e
	}
	defer client.Close()
	sf, e := sftp.NewClient(client)
	if e != nil {
		return e
	}
	defer sf.Close()
	if expression := str(c, "file_pattern"); expression != "" {
		matches, err := matchWorkspaceFiles(input, work, expression)
		if err != nil {
			return err
		}
		if len(matches) == 0 {
			return fmt.Errorf("file selection matched no files")
		}
		remote := str(c, "destination")
		for _, match := range matches {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			if err = sftpFile(sf, match.Path, pathJoinRemote(remote, match.Name)); err != nil {
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
		return sftpFile(sf, local, remote)
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
		return sftpFile(sf, p, target)
	})
}
func pathJoinRemote(a, b string) string {
	return strings.TrimRight(a, "/") + "/" + strings.ReplaceAll(b, "\\", "/")
}
func sftpFile(sf *sftp.Client, local, remote string) error {
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
	_, e = io.Copy(out, in)
	ce := out.Close()
	if e != nil {
		return e
	}
	return ce
}
func pathDirRemote(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return "."
}
