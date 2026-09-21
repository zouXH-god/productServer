package main

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
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
func itoa[T ~uint](v T) string { return strconv.FormatUint(uint64(v), 10) }
