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
func executeModule(ctx context.Context, db *gorm.DB, cfg Config, run WorkflowRun, p Project, r Release, n WorkflowNode, input, work string, log *runLogger) error {
	switch n.Type {
	case "archive":
		return archiveModule(n.Config, input, work)
	case "extract":
		return extractModule(n.Config, input, work)
	case "checksum_verify":
		return checksumModule(n.Config, input, work)
	case "http_webhook":
		return webhookModule(ctx, cfg, n.Config, p, r, input, work)
	case "ssh_command":
		return sshCommandModule(ctx, db, cfg, run, n.Config, p, r, input, work, log)
	case "sftp_upload":
		return sftpModule(ctx, db, cfg, run, n.Config, input, work)
	}
	return fmt.Errorf("unknown module %s", n.Type)
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
func webhookModule(ctx context.Context, cfg Config, c map[string]any, p Project, r Release, input, work string) error {
	raw := render(str(c, "url"), p, r, input, work)
	if !webhookAllowed(raw, cfg.WebhookAllowlist) {
		return fmt.Errorf("webhook URL is not allowed")
	}
	body := render(str(c, "body"), p, r, input, work)
	req, e := http.NewRequestWithContext(ctx, strings.ToUpper(str(c, "method")), raw, strings.NewReader(body))
	if e != nil {
		return e
	}
	if req.Method == "" {
		req.Method = "POST"
	}
	if secret := str(c, "secret"); secret != "" {
		secret = strings.TrimPrefix(secret, "enc:")
		plain, e := decryptSecret(cfg.SecretEncryptionKey, secret)
		if e != nil {
			return e
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
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned %s", resp.Status)
	}
	return nil
}
func sshForRun(db *gorm.DB, cfg Config, run WorkflowRun, id uint) (*ssh.Client, error) {
	var project Project
	if e := db.First(&project, run.ProjectID).Error; e != nil {
		return nil, e
	}
	a := &App{db: db, cfg: cfg}
	return a.sshClient(id, project.UserID)
}
func sshCommandModule(ctx context.Context, db *gorm.DB, cfg Config, run WorkflowRun, c map[string]any, p Project, r Release, input, work string, log *runLogger) error {
	client, e := sshForRun(db, cfg, run, num(c, "connection_id"))
	if e != nil {
		return e
	}
	defer client.Close()
	session, e := client.NewSession()
	if e != nil {
		return e
	}
	defer session.Close()
	commands := []string{}
	if raw, ok := c["commands"].([]any); ok {
		for _, v := range raw {
			commands = append(commands, render(fmt.Sprint(v), p, r, input, work))
		}
	}
	cmd := strings.Join(commands, "\n")
	if wd := str(c, "workdir"); wd != "" {
		cmd = "cd " + shellQuote(render(wd, p, r, input, work)) + "\n" + cmd
	}
	done := make(chan error, 1)
	go func() {
		out, e := session.CombinedOutput(cmd)
		log.write("ssh_command", "stdout", string(out))
		done <- e
	}()
	select {
	case <-ctx.Done():
		client.Close()
		return ctx.Err()
	case e := <-done:
		return e
	}
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
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
