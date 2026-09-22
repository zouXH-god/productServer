package main

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func testApp(t *testing.T) (*App, Config) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	cfg := Config{DBDriver: "sqlite", DBDSN: filepath.Join(root, "test.db"), StorageDir: filepath.Join(root, "artifacts"), JWTSecret: "test-secret", JWTExpiry: time.Hour, MaxFileBytes: 1024, MaxBatchBytes: 4096, AdminUsername: "admin", AdminPassword: "password123"}
	db, err := gorm.Open(sqlite.Open(cfg.DBDSN), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err = migrateAndBootstrap(db, cfg); err != nil {
		t.Fatal(err)
	}
	app, err := newApp(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return app, cfg
}
func request(t *testing.T, a *App, method, path string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, body)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	a.router.ServeHTTP(w, r)
	return w
}
func loginToken(t *testing.T, a *App) string {
	t.Helper()
	w := request(t, a, "POST", "/api/auth/login", bytes.NewBufferString(`{"username":"admin","password":"password123"}`), map[string]string{"Content-Type": "application/json"})
	if w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return out.Token
}
func loginAs(t *testing.T, a *App, username, password string) string {
	t.Helper()
	w := request(t, a, "POST", "/api/auth/login", bytes.NewBufferString(fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)), map[string]string{"Content-Type": "application/json"})
	if w.Code != 200 {
		t.Fatalf("login %s: %d %s", username, w.Code, w.Body.String())
	}
	var out struct {
		Token string `json:"token"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	return out.Token
}

func TestRegistrationJWTRevocationAndProjectMembership(t *testing.T) {
	a, _ := testApp(t)
	w := request(t, a, "POST", "/api/auth/register", bytes.NewBufferString(`{"username":"developer","email":"dev@example.com","password":"password123"}`), map[string]string{"Content-Type": "application/json"})
	if w.Code != 201 {
		t.Fatalf("register: %d %s", w.Code, w.Body.String())
	}
	developerToken := loginAs(t, a, "developer", "password123")
	adminToken := loginToken(t, a)
	w = request(t, a, "POST", "/api/projects", bytes.NewBufferString(`{"name":"timer","type":"scheduled"}`), map[string]string{"Content-Type": "application/json", "Authorization": "Bearer " + adminToken})
	if w.Code != 201 {
		t.Fatalf("project: %d %s", w.Code, w.Body.String())
	}
	var project struct {
		ID    uint   `json:"id"`
		Token string `json:"token"`
	}
	json.Unmarshal(w.Body.Bytes(), &project)
	if project.Token != "" {
		t.Fatal("scheduled project returned a token")
	}
	w = request(t, a, "POST", fmt.Sprintf("/api/projects/%d/members", project.ID), bytes.NewBufferString(`{"username":"developer","role":"developer"}`), map[string]string{"Content-Type": "application/json", "Authorization": "Bearer " + adminToken})
	if w.Code != 201 {
		t.Fatalf("member: %d %s", w.Code, w.Body.String())
	}
	w = request(t, a, "GET", fmt.Sprintf("/api/projects/%d", project.ID), nil, map[string]string{"Authorization": "Bearer " + developerToken})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"role":"developer"`) {
		t.Fatalf("member access: %d %s", w.Code, w.Body.String())
	}
	w = request(t, a, "DELETE", fmt.Sprintf("/api/projects/%d", project.ID), nil, map[string]string{"Authorization": "Bearer " + developerToken})
	if w.Code != 403 {
		t.Fatalf("developer deleted project: %d", w.Code)
	}
	var developer User
	a.db.Where("username=?", "developer").First(&developer)
	a.db.Model(&developer).Update("token_version", gorm.Expr("token_version + 1"))
	w = request(t, a, "GET", "/api/auth/me", nil, map[string]string{"Authorization": "Bearer " + developerToken})
	if w.Code != 401 {
		t.Fatalf("revoked JWT status=%d", w.Code)
	}
}

func TestCopyWorkflowToAnotherProject(t *testing.T) {
	a, _ := testApp(t)
	token := loginToken(t, a)
	createProject := func(name string) uint {
		w := request(t, a, "POST", "/api/projects", bytes.NewBufferString(fmt.Sprintf(`{"name":%q,"type":"scheduled"}`, name)), map[string]string{"Content-Type": "application/json", "Authorization": "Bearer " + token})
		if w.Code != 201 {
			t.Fatalf("create project %s: %d %s", name, w.Code, w.Body.String())
		}
		var project Project
		json.Unmarshal(w.Body.Bytes(), &project)
		return project.ID
	}
	sourceProjectID, targetProjectID := createProject("source"), createProject("target")
	definition, _ := json.Marshal(WorkflowDefinition{Nodes: []WorkflowNode{{ID: "split", Type: "string_split", Config: map[string]any{"value": "a,b", "delimiter": ","}}}})
	source := Workflow{ProjectID: sourceProjectID, Name: "deploy", Enabled: true, TriggerType: "any", Definition: string(definition)}
	if err := a.db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"target_project_id":%d,"name":"deploy copy"}`, targetProjectID)
	w := request(t, a, "POST", fmt.Sprintf("/api/projects/%d/workflows/%d/copy", sourceProjectID, source.ID), bytes.NewBufferString(body), map[string]string{"Content-Type": "application/json", "Authorization": "Bearer " + token})
	if w.Code != 201 {
		t.Fatalf("copy workflow: %d %s", w.Code, w.Body.String())
	}
	var copied Workflow
	if err := a.db.Where("project_id=? AND name=?", targetProjectID, "deploy copy").First(&copied).Error; err != nil {
		t.Fatal(err)
	}
	if copied.Enabled || copied.Definition != source.Definition || copied.TriggerType != source.TriggerType {
		t.Fatalf("unexpected copied workflow: %#v", copied)
	}
	w = request(t, a, "POST", fmt.Sprintf("/api/projects/%d/workflows/%d/copy", sourceProjectID, source.ID), bytes.NewBufferString(body), map[string]string{"Content-Type": "application/json", "Authorization": "Bearer " + token})
	if w.Code != 409 {
		t.Fatalf("duplicate name status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAIProviderEncryptionAndCanvasTools(t *testing.T) {
	a, _ := testApp(t)
	a.cfg.SecretEncryptionKey = "12345678901234567890123456789012"
	token := loginToken(t, a)
	body := `{"name":"local","base_url":"http://127.0.0.1:11434","model":"test","api_key":"secret-key","timeout_seconds":30,"enabled":true}`
	w := request(t, a, "POST", "/api/ai/providers", bytes.NewBufferString(body), map[string]string{"Content-Type": "application/json", "Authorization": "Bearer " + token})
	if w.Code != 201 {
		t.Fatalf("provider status=%d body=%s", w.Code, w.Body.String())
	}
	var provider AIProvider
	if err := a.db.First(&provider).Error; err != nil {
		t.Fatal(err)
	}
	if provider.APIKeyEncrypted == "secret-key" || provider.APIKeyEncrypted == "" {
		t.Fatal("API key was not encrypted")
	}
	canvas := WorkflowDefinition{Nodes: []WorkflowNode{{ID: "a", Type: "ssh_command", Config: map[string]any{}}}}
	_, ops, err := a.toolContext(1, 1, "add_node", map[string]any{"node": map[string]any{"id": "b", "type": "http_webhook", "config": map[string]any{"url": "https://example.com/hook"}}}, &canvas)
	if err != nil || len(ops) != 1 || len(canvas.Nodes) != 2 {
		t.Fatalf("ops=%v canvas=%v err=%v", ops, canvas, err)
	}
	_, _, err = a.toolContext(1, 1, "add_edge", map[string]any{"edge": map[string]any{"from": "a", "to": "b", "condition": "success"}}, &canvas)
	if err != nil || len(canvas.Edges) != 1 {
		t.Fatalf("edge err=%v canvas=%v", err, canvas)
	}
	_, _, err = a.toolContext(1, 1, "add_edge", map[string]any{"edge": map[string]any{"from": "b", "to": "a", "condition": "success"}}, &canvas)
	if err == nil {
		t.Fatal("AI tool accepted a cyclic edge")
	}
}

func TestOpenAIToolCallRoundTripUsesCompatibleFieldNames(t *testing.T) {
	var response chatResponse
	raw := `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_canvas","arguments":"{}"}}]}}]}`
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(map[string]any{"role": "assistant", "tool_calls": response.Choices[0].Message.ToolCalls})
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if !strings.Contains(text, `"id":"call_1"`) || !strings.Contains(text, `"function":{"name":"get_canvas"`) || strings.Contains(text, `"ID"`) {
		t.Fatalf("incompatible tool call JSON: %s", text)
	}
}

func TestNormalizeAIGeneratedWorkflow(t *testing.T) {
	d := WorkflowDefinition{Nodes: []WorkflowNode{
		{ID: "upload", Type: "sftp_upload", Config: map[string]any{"connection_id": 1, "local_path": "${release.artifact}", "remote_dir": "/root/test"}},
		{ID: "extract", Type: "extract", Config: map[string]any{"connection_id": 1, "archive_dir": "/root/test", "target_dir": "/root/test"}},
		{ID: "run", Type: "ssh_command", Config: map[string]any{"connection_id": 1, "command": "bash start.sh"}},
	}, Edges: []WorkflowEdge{{From: "upload", To: "extract"}, {From: "extract", To: "run"}}}
	if !normalizeWorkflowDefinition(&d) {
		t.Fatal("generated workflow was not normalized")
	}
	if len(d.Nodes) != 2 || d.Nodes[0].Type != "sftp_extract" || d.Nodes[0].Config["destination"] != "/root/test" {
		t.Fatalf("nodes=%#v", d.Nodes)
	}
	commands, ok := d.Nodes[1].Config["commands"].([]any)
	if !ok || len(commands) != 1 {
		t.Fatalf("commands=%#v", d.Nodes[1].Config["commands"])
	}
	if len(d.Edges) != 1 || d.Edges[0].From != "upload" || d.Edges[0].To != "run" {
		t.Fatalf("edges=%#v", d.Edges)
	}
}
func createProjectAndToken(t *testing.T, a *App, jwt string) (uint, string) {
	t.Helper()
	w := request(t, a, "POST", "/api/projects", bytes.NewBufferString(`{"name":"demo"}`), map[string]string{"Content-Type": "application/json", "Authorization": "Bearer " + jwt})
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	var result struct {
		Project
		Token string `json:"token"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	return result.ID, result.Token
}

func TestSSHPasswordConnectionIsEncrypted(t *testing.T) {
	a, _ := testApp(t)
	a.cfg.SecretEncryptionKey = "12345678901234567890123456789012"
	jwt := loginToken(t, a)
	body := `{"name":"password-server","host":"example.internal","port":22,"username":"deploy","auth_type":"password","password":"very-secret","timeout_seconds":10}`
	w := request(t, a, "POST", "/api/ssh-connections", bytes.NewBufferString(body), map[string]string{"Content-Type": "application/json", "Authorization": "Bearer " + jwt})
	if w.Code != 201 {
		t.Fatalf("create password connection: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "very-secret") {
		t.Fatal("password leaked in API response")
	}
	var connection SSHConnection
	if err := a.db.Where("name = ?", "password-server").First(&connection).Error; err != nil {
		t.Fatal(err)
	}
	if connection.AuthType != "password" || connection.PasswordEncrypted == "" || connection.PasswordEncrypted == "very-secret" {
		t.Fatalf("password was not encrypted: %#v", connection)
	}
	plain, err := decryptSecret(a.cfg.SecretEncryptionKey, connection.PasswordEncrypted)
	if err != nil || plain != "very-secret" {
		t.Fatalf("stored password cannot be decrypted: %q %v", plain, err)
	}
}

func TestAllRunsFiltersByProjectAndWorkflow(t *testing.T) {
	a, _ := testApp(t)
	jwt := loginToken(t, a)
	projectID, _ := createProjectAndToken(t, a, jwt)
	first := Workflow{ProjectID: projectID, Name: "first", Enabled: true, TriggerType: "any", Definition: `{"nodes":[],"edges":[]}`}
	second := Workflow{ProjectID: projectID, Name: "second", Enabled: true, TriggerType: "any", Definition: `{"nodes":[],"edges":[]}`}
	if err := a.db.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.db.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.db.Create(&WorkflowRun{ProjectID: projectID, WorkflowID: first.ID, Status: "queued"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.db.Create(&WorkflowRun{ProjectID: projectID, WorkflowID: second.ID, Status: "failed"}).Error; err != nil {
		t.Fatal(err)
	}

	w := request(t, a, "GET", fmt.Sprintf("/api/runs?project_id=%d&workflow_id=%d", projectID, second.ID), nil, map[string]string{"Authorization": "Bearer " + jwt})
	if w.Code != 200 {
		t.Fatalf("filter runs: %d %s", w.Code, w.Body.String())
	}
	var runs []dashboardRun
	if err := json.Unmarshal(w.Body.Bytes(), &runs); err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].WorkflowID != second.ID || runs[0].ProjectID != projectID {
		t.Fatalf("unexpected filtered runs: %#v", runs)
	}
}

func TestCreateSSHCredentialAcceptsSnakeCasePrivateKey(t *testing.T) {
	a, _ := testApp(t)
	a.cfg.SecretEncryptionKey = "12345678901234567890123456789012"
	jwt := loginToken(t, a)
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	privatePEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	body, err := json.Marshal(map[string]string{"name": "deploy-key", "private_key": privatePEM})
	if err != nil {
		t.Fatal(err)
	}

	w := request(t, a, "POST", "/api/ssh-credentials", bytes.NewReader(body), map[string]string{"Content-Type": "application/json", "Authorization": "Bearer " + jwt})
	if w.Code != 201 {
		t.Fatalf("create SSH credential: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), privatePEM) {
		t.Fatal("private key leaked in API response")
	}
	var credential SSHCredential
	if err := a.db.Where("name = ?", "deploy-key").First(&credential).Error; err != nil {
		t.Fatal(err)
	}
	if credential.PrivateKeyEncrypted == "" || credential.PrivateKeyEncrypted == privatePEM {
		t.Fatal("private key was not encrypted")
	}
	plain, err := decryptSecret(a.cfg.SecretEncryptionKey, credential.PrivateKeyEncrypted)
	if err != nil || plain != strings.TrimSpace(privatePEM) {
		t.Fatalf("stored private key cannot be decrypted: %v", err)
	}
}
func uploadBody(t *testing.T, version string, files map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	b := new(bytes.Buffer)
	m := multipart.NewWriter(b)
	_ = m.WriteField("version", version)
	for name, data := range files {
		w, _ := m.CreateFormFile("files", name)
		_, _ = w.Write([]byte(data))
	}
	_ = m.Close()
	return b, m.FormDataContentType()
}

func zipData(t *testing.T, files map[string]string) string {
	t.Helper()
	var data bytes.Buffer
	w := zip.NewWriter(&data)
	for name, content := range files {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return data.String()
}

func TestUploadAutomaticallyExtractsZIPAndKeepsArchive(t *testing.T) {
	a, cfg := testApp(t)
	jwt := loginToken(t, a)
	pid, token := createProjectAndToken(t, a, jwt)
	archive := zipData(t, map[string]string{"dist/index.html": "hello", "checksums.txt": "abc123"})
	body, contentType := uploadBody(t, "zip-release", map[string]string{"artifact.zip": archive})
	w := request(t, a, "POST", "/api/upload", body, map[string]string{"Content-Type": contentType, "Authorization": "Bearer " + token})
	if w.Code != 201 {
		t.Fatalf("upload zip: %d %s", w.Code, w.Body.String())
	}
	var release Release
	if err := json.Unmarshal(w.Body.Bytes(), &release); err != nil {
		t.Fatal(err)
	}
	if len(release.Files) != 3 {
		t.Fatalf("expected archive and 2 extracted files, got %d", len(release.Files))
	}
	kinds := map[string]string{}
	for _, file := range release.Files {
		kinds[file.OriginalName] = file.Kind
		if _, err := os.Stat(filepath.Join(cfg.StorageDir, itoa(pid), itoa(release.ID), file.StoredName)); err != nil {
			t.Fatal(err)
		}
	}
	if kinds["artifact.zip"] != "uploaded" || kinds["dist/index.html"] != "extracted" || kinds["checksums.txt"] != "extracted" {
		t.Fatalf("unexpected artifact list: %#v", kinds)
	}
	body, contentType = uploadBody(t, "zip-release", map[string]string{"artifact.zip": archive})
	w = request(t, a, "POST", "/api/upload", body, map[string]string{"Content-Type": contentType, "Authorization": "Bearer " + token})
	if w.Code != 200 {
		t.Fatalf("idempotent zip upload: %d %s", w.Code, w.Body.String())
	}
}

func TestUploadRejectsUnsafeZIPEntry(t *testing.T) {
	a, _ := testApp(t)
	jwt := loginToken(t, a)
	_, token := createProjectAndToken(t, a, jwt)
	body, contentType := uploadBody(t, "unsafe-zip", map[string]string{"artifact.zip": zipData(t, map[string]string{"../escape.txt": "bad"})})
	w := request(t, a, "POST", "/api/upload", body, map[string]string{"Content-Type": contentType, "Authorization": "Bearer " + token})
	if w.Code != 400 {
		t.Fatalf("unsafe zip status: %d %s", w.Code, w.Body.String())
	}
	var count int64
	a.db.Model(&Release{}).Count(&count)
	if count != 0 {
		t.Fatal("unsafe archive created a release")
	}
}

func TestBootstrapOnlyOnce(t *testing.T) {
	a, cfg := testApp(t)
	var count int64
	a.db.Model(&User{}).Count(&count)
	if count != 1 {
		t.Fatalf("count=%d", count)
	}
	cfg.AdminUsername = "other"
	if err := migrateAndBootstrap(a.db, cfg); err != nil {
		t.Fatal(err)
	}
	a.db.Model(&User{}).Count(&count)
	if count != 1 {
		t.Fatalf("bootstrap duplicated user")
	}
}
func TestAuthProjectsAndIsolation(t *testing.T) {
	a, _ := testApp(t)
	if w := request(t, a, "POST", "/api/auth/login", bytes.NewBufferString(`{"username":"admin","password":"wrong"}`), map[string]string{"Content-Type": "application/json"}); w.Code != 401 {
		t.Fatalf("wrong password status=%d", w.Code)
	}
	jwt := loginToken(t, a)
	pid, _ := createProjectAndToken(t, a, jwt)
	hash, _ := bcryptHash("password123")
	other := User{Username: "other", PasswordHash: hash}
	a.db.Create(&other)
	otherJWT := signTestJWT(t, a, other.ID, time.Now().Add(time.Hour))
	w := request(t, a, "GET", "/api/projects/"+itoa(pid), nil, map[string]string{"Authorization": "Bearer " + otherJWT})
	if w.Code != 404 {
		t.Fatalf("isolation status=%d", w.Code)
	}
}
func TestTokenUploadDuplicateAndDownload(t *testing.T) {
	a, cfg := testApp(t)
	jwt := loginToken(t, a)
	pid, token := createProjectAndToken(t, a, jwt)
	b, ct := uploadBody(t, "v1", map[string]string{"app.zip": "hello", "sum.txt": "world"})
	w := request(t, a, "POST", "/api/upload", b, map[string]string{"Content-Type": ct, "Authorization": "Bearer " + token})
	if w.Code != 201 {
		t.Fatalf("upload %d: %s", w.Code, w.Body.String())
	}
	var rel Release
	_ = json.Unmarshal(w.Body.Bytes(), &rel)
	if len(rel.Files) != 2 {
		t.Fatalf("files=%d", len(rel.Files))
	}
	for _, f := range rel.Files {
		if f.SHA256 == "" {
			t.Fatal("missing hash")
		}
		if _, err := os.Stat(filepath.Join(cfg.StorageDir, itoa(pid), itoa(rel.ID), f.StoredName)); err != nil {
			t.Fatal(err)
		}
	}
	b, ct = uploadBody(t, "v1", map[string]string{"again": "x"})
	w = request(t, a, "POST", "/api/upload", b, map[string]string{"Content-Type": ct, "Authorization": "Bearer " + token})
	if w.Code != 409 {
		t.Fatalf("duplicate=%d", w.Code)
	}
	b, ct = uploadBody(t, "v1", map[string]string{"app.zip": "hello", "sum.txt": "world"})
	w = request(t, a, "POST", "/api/upload", b, map[string]string{"Content-Type": ct, "Authorization": "Bearer " + token})
	if w.Code != 200 {
		t.Fatalf("idempotent upload=%d %s", w.Code, w.Body.String())
	}
	f := rel.Files[0]
	w = request(t, a, "GET", "/api/projects/1/releases/"+itoa(rel.ID)+"/files/"+itoa(f.ID)+"/download", nil, map[string]string{"Authorization": "Bearer " + jwt})
	if w.Code != 200 {
		t.Fatalf("download=%d %s", w.Code, w.Body.String())
	}
	w = request(t, a, "GET", "/api/download?token="+token+"&version=v1&file="+f.OriginalName, nil, nil)
	if w.Code != 200 {
		t.Fatalf("token download=%d %s", w.Code, w.Body.String())
	}
}
func TestUniqueTokenRotation(t *testing.T) {
	a, _ := testApp(t)
	jwt := loginToken(t, a)
	_, old := createProjectAndToken(t, a, jwt)
	w := request(t, a, "GET", "/api/projects/1/token", nil, map[string]string{"Authorization": "Bearer " + jwt})
	if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte(old)) {
		t.Fatalf("token metadata leaked or unavailable: %d %s", w.Code, w.Body.String())
	}
	w = request(t, a, "POST", "/api/projects/1/token/rotate", nil, map[string]string{"Authorization": "Bearer " + jwt})
	if w.Code != 201 {
		t.Fatalf("rotate=%d %s", w.Code, w.Body.String())
	}
	var result struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	if result.Token == "" || result.Token == old {
		t.Fatal("rotation did not issue a new token")
	}
	b, ct := uploadBody(t, "old", map[string]string{"a": "a"})
	if w = request(t, a, "POST", "/api/upload", b, map[string]string{"Content-Type": ct, "Authorization": "Bearer " + old}); w.Code != 401 {
		t.Fatalf("old token status=%d", w.Code)
	}
}
func TestTokenDownloadValidationAndEncoding(t *testing.T) {
	a, _ := testApp(t)
	jwt := loginToken(t, a)
	_, token := createProjectAndToken(t, a, jwt)
	b, ct := uploadBody(t, "release/tag", map[string]string{"web build.zip": "content"})
	w := request(t, a, "POST", "/api/upload", b, map[string]string{"Content-Type": ct, "Authorization": "Bearer " + token})
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	query := url.Values{"token": {token}, "version": {"release/tag"}, "file": {"web build.zip"}}
	w = request(t, a, "GET", "/api/download?"+query.Encode(), nil, nil)
	if w.Code != 200 || w.Body.String() != "content" {
		t.Fatalf("encoded download=%d %q", w.Code, w.Body.String())
	}
	if w = request(t, a, "GET", "/api/download?token=bad&version=x&file=y", nil, nil); w.Code != 401 {
		t.Fatalf("invalid token=%d", w.Code)
	}
	if w = request(t, a, "GET", "/api/download", nil, nil); w.Code != 400 {
		t.Fatalf("missing query=%d", w.Code)
	}
}
func TestMigrationKeepsNewestTokenAndSeedsMissing(t *testing.T) {
	root := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(root, "legacy.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err = db.AutoMigrate(&User{}, &Project{}, &Release{}, &ArtifactFile{}); err != nil {
		t.Fatal(err)
	}
	if err = db.Exec(`CREATE TABLE project_tokens (id integer primary key autoincrement, project_id integer not null, name text, prefix text, token_hash text, created_at datetime)`).Error; err != nil {
		t.Fatal(err)
	}
	user := User{Username: "legacy", PasswordHash: "x"}
	db.Create(&user)
	p1 := Project{UserID: user.ID, Name: "one"}
	p2 := Project{UserID: user.ID, Name: "two"}
	db.Create(&p1)
	db.Create(&p2)
	db.Exec(`INSERT INTO project_tokens(project_id,name,prefix,token_hash,created_at) VALUES (?, 'old', 'old', ?, CURRENT_TIMESTAMP), (?, 'new', 'new', ?, CURRENT_TIMESTAMP)`, p1.ID, strings.Repeat("a", 64), p1.ID, strings.Repeat("b", 64))
	cfg := Config{AdminUsername: "admin", AdminPassword: "password123"}
	if err = migrateAndBootstrap(db, cfg); err != nil {
		t.Fatal(err)
	}
	var tokens []ProjectToken
	if err = db.Order("project_id").Find(&tokens).Error; err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 2 || tokens[0].ProjectID != p1.ID || tokens[0].Prefix != "new" || tokens[1].ProjectID != p2.ID {
		t.Fatalf("unexpected migrated tokens: %+v", tokens)
	}
}
func TestUploadValidationAndRollback(t *testing.T) {
	a, cfg := testApp(t)
	jwt := loginToken(t, a)
	_, token := createProjectAndToken(t, a, jwt)
	b, ct := uploadBody(t, "empty", map[string]string{})
	w := request(t, a, "POST", "/api/upload", b, map[string]string{"Content-Type": ct, "Authorization": "Bearer " + token})
	if w.Code != 400 {
		t.Fatalf("empty=%d", w.Code)
	}
	if validFilename("../bad") || validFilename(`dir\\bad`) {
		t.Fatal("dangerous filename accepted")
	}
	b, ct = uploadBody(t, "large", map[string]string{"large.bin": string(make([]byte, 2048))})
	w = request(t, a, "POST", "/api/upload", b, map[string]string{"Content-Type": ct, "Authorization": "Bearer " + token})
	if w.Code != 413 {
		t.Fatalf("large=%d", w.Code)
	}
	var count int64
	a.db.Model(&Release{}).Count(&count)
	if count != 0 {
		t.Fatal("failed uploads created release")
	}
	entries, _ := os.ReadDir(cfg.StorageDir)
	for _, e := range entries {
		if len(e.Name()) >= 8 && e.Name()[:8] == ".upload-" {
			t.Fatal("staging directory leaked")
		}
	}
}
func TestRevokeAndDeleteProject(t *testing.T) {
	a, cfg := testApp(t)
	jwt := loginToken(t, a)
	pid, token := createProjectAndToken(t, a, jwt)
	b, ct := uploadBody(t, "v1", map[string]string{"a.txt": "a"})
	w := request(t, a, "POST", "/api/upload", b, map[string]string{"Content-Type": ct, "Authorization": "Bearer " + token})
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	w = request(t, a, "DELETE", "/api/projects/1/tokens/1", nil, map[string]string{"Authorization": "Bearer " + jwt})
	if w.Code != 204 {
		t.Fatalf("revoke=%d", w.Code)
	}
	b, ct = uploadBody(t, "v2", map[string]string{"a.txt": "a"})
	w = request(t, a, "POST", "/api/upload", b, map[string]string{"Content-Type": ct, "Authorization": "Bearer " + token})
	if w.Code != 401 {
		t.Fatalf("revoked token=%d", w.Code)
	}
	w = request(t, a, "DELETE", "/api/projects/1", nil, map[string]string{"Authorization": "Bearer " + jwt})
	if w.Code != 204 {
		t.Fatalf("delete=%d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(cfg.StorageDir, itoa(pid))); !os.IsNotExist(err) {
		t.Fatal("project files remain")
	}
}

func bcryptHash(s string) (string, error) {
	b, e := bcrypt.GenerateFromPassword([]byte(s), bcrypt.DefaultCost)
	return string(b), e
}
func signTestJWT(t *testing.T, a *App, id uint, exp time.Time) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": itoa(id), "exp": exp.Unix()})
	s, e := tok.SignedString([]byte(a.cfg.JWTSecret))
	if e != nil {
		t.Fatal(e)
	}
	return s
}
