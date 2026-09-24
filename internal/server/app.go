package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

//go:embed frontend/dist
var frontendFS embed.FS

type App struct {
	db           *gorm.DB
	cfg          Config
	router       *gin.Engine
	releaseHooks []func(Release)
	aiMu         sync.Mutex
	aiStreams    map[uint][]chan []byte
	aiCancels    map[uint]context.CancelFunc
}
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func fail(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": apiError{code, message}})
}
func newApp(db *gorm.DB, cfg Config) (*App, error) {
	if cfg.WorkflowLogDir == "" {
		cfg.WorkflowLogDir = filepath.Join(filepath.Dir(cfg.StorageDir), "logs")
	}
	if cfg.WorkflowWorkDir == "" {
		cfg.WorkflowWorkDir = filepath.Join(filepath.Dir(cfg.StorageDir), "work")
	}
	if err := os.MkdirAll(cfg.StorageDir, 0755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.WorkflowLogDir, 0755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.WorkflowWorkDir, 0755); err != nil {
		return nil, err
	}
	a := &App{db: db, cfg: cfg, router: gin.New(), aiStreams: map[uint][]chan []byte{}, aiCancels: map[uint]context.CancelFunc{}}
	a.router.Use(gin.Logger(), gin.Recovery())
	a.routes()
	return a, nil
}
func (a *App) routes() {
	r := a.router
	r.GET("/healthz", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	r.GET("/readyz", a.ready)
	r.POST("/api/auth/login", a.login)
	r.POST("/api/auth/register", a.register)
	r.POST("/api/upload", a.tokenAuth(), a.upload)
	r.GET("/api/download", a.tokenDownload)
	api := r.Group("/api", a.jwtAuth())
	api.GET("/auth/me", a.me)
	api.PUT("/auth/profile", a.updateProfile)
	api.POST("/auth/password", a.changePassword)
	api.GET("/admin/users", a.listUsers)
	api.POST("/admin/users", a.createUser)
	api.PUT("/admin/users/:userId", a.updateUser)
	api.POST("/admin/users/:userId/reset-password", a.resetUserPassword)
	api.GET("/admin/settings", a.getSystemSettings)
	api.PUT("/admin/settings", a.updateSystemSettings)
	api.GET("/environment-variables", a.listGlobalEnvironmentVariables)
	api.POST("/environment-variables", a.saveGlobalEnvironmentVariable)
	api.PUT("/environment-variables/:variableId", a.saveGlobalEnvironmentVariable)
	api.DELETE("/environment-variables/:variableId", a.deleteGlobalEnvironmentVariable)
	api.GET("/dashboard", a.dashboard)
	api.GET("/runs", a.allRuns)
	a.aiRoutes(api)
	api.GET("/projects", a.listProjects)
	api.POST("/projects", a.createProject)
	api.GET("/projects/:id", a.getProject)
	api.PUT("/projects/:id", a.updateProject)
	api.GET("/projects/:id/members", a.listProjectMembers)
	api.POST("/projects/:id/members", a.addProjectMember)
	api.PUT("/projects/:id/members/:userId", a.updateProjectMember)
	api.DELETE("/projects/:id/members/:userId", a.deleteProjectMember)
	api.POST("/projects/:id/transfer-owner", a.transferProjectOwner)
	api.GET("/projects/:id/environment-variables", a.listProjectEnvironmentVariables)
	api.GET("/projects/:id/environment-variables/available", a.listAvailableEnvironmentVariables)
	api.POST("/projects/:id/environment-variables", a.saveProjectEnvironmentVariable)
	api.PUT("/projects/:id/environment-variables/:variableId", a.saveProjectEnvironmentVariable)
	api.DELETE("/projects/:id/environment-variables/:variableId", a.deleteProjectEnvironmentVariable)
	api.DELETE("/projects/:id", a.deleteProject)
	api.GET("/projects/:id/tokens", a.listTokens)
	api.POST("/projects/:id/tokens", a.createToken)
	api.DELETE("/projects/:id/tokens/:tokenId", a.deleteToken)
	api.GET("/projects/:id/token", a.getToken)
	api.POST("/projects/:id/token/rotate", a.rotateToken)
	api.GET("/projects/:id/releases", a.listReleases)
	api.GET("/projects/:id/releases/:releaseId", a.getRelease)
	api.GET("/projects/:id/releases/:releaseId/accesses", a.listArtifactAccesses)
	api.GET("/projects/:id/releases/:releaseId/files/:fileId/download", a.download)
	a.workflowRoutes(api)
	a.scheduleRoutes(api)
	assets, _ := fs.Sub(frontendFS, "frontend/dist")
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			fail(c, 404, "not_found", "route not found")
			return
		}
		p := strings.TrimPrefix(filepath.ToSlash(c.Request.URL.Path), "/")
		if p != "" {
			if f, err := assets.Open(p); err == nil {
				_ = f.Close()
				c.FileFromFS(p, http.FS(assets))
				return
			}
		}
		data, err := fs.ReadFile(assets, "index.html")
		if err != nil {
			fail(c, 404, "frontend_unavailable", "frontend is not built")
			return
		}
		c.Data(200, "text/html; charset=utf-8", data)
	})
}
func parseID(c *gin.Context, key string) (uint, bool) {
	n, err := strconv.ParseUint(c.Param(key), 10, 64)
	if err != nil || n == 0 {
		fail(c, 400, "invalid_id", "invalid identifier")
		return 0, false
	}
	return uint(n), true
}
func (a *App) ready(c *gin.Context) {
	sqlDB, err := a.db.DB()
	if err != nil || sqlDB.Ping() != nil {
		fail(c, 503, "database_unavailable", "database unavailable")
		return
	}
	f, err := os.CreateTemp(a.cfg.StorageDir, ".ready-")
	if err != nil {
		fail(c, 503, "storage_unavailable", "storage unavailable")
		return
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	c.JSON(200, gin.H{"status": "ready"})
}
func (a *App) login(c *gin.Context) {
	var in struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "invalid_request", "username and password are required")
		return
	}
	var u User
	if a.db.Where("username = ?", in.Username).First(&u).Error != nil || !u.Enabled || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.Password)) != nil {
		fail(c, 401, "invalid_credentials", "invalid username or password")
		return
	}
	now := time.Now()
	claims := jwt.MapClaims{"sub": strconv.FormatUint(uint64(u.ID), 10), "ver": u.TokenVersion, "iat": now.Unix(), "exp": now.Add(a.cfg.JWTExpiry).Unix()}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, _ := t.SignedString([]byte(a.cfg.JWTSecret))
	c.JSON(200, gin.H{"token": signed, "expires_at": now.Add(a.cfg.JWTExpiry), "must_change_password": u.MustChangePassword})
}
func (a *App) jwtAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := bearer(c)
		token, err := jwt.Parse(raw, func(t *jwt.Token) (any, error) {
			if t.Method != jwt.SigningMethodHS256 {
				return nil, errors.New("invalid signing method")
			}
			return []byte(a.cfg.JWTSecret), nil
		})
		if err != nil || !token.Valid {
			fail(c, 401, "unauthorized", "valid JWT required")
			return
		}
		sub, err := token.Claims.GetSubject()
		if err != nil {
			fail(c, 401, "unauthorized", "invalid JWT subject")
			return
		}
		id, err := strconv.ParseUint(sub, 10, 64)
		if err != nil {
			fail(c, 401, "unauthorized", "invalid JWT subject")
			return
		}
		var user User
		if a.db.First(&user, uint(id)).Error != nil || !user.Enabled {
			fail(c, 401, "unauthorized", "user is disabled or unavailable")
			return
		}
		version := 0
		if claims, ok := token.Claims.(jwt.MapClaims); ok {
			switch v := claims["ver"].(type) {
			case float64:
				version = int(v)
			case int:
				version = v
			}
		}
		if version != user.TokenVersion {
			fail(c, 401, "token_revoked", "JWT has been revoked")
			return
		}
		c.Set("userID", uint(id))
		c.Set("user", user)
		c.Next()
	}
}
func bearer(c *gin.Context) string {
	h := c.GetHeader("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return ""
}
func (a *App) tokenAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := bearer(c)
		t, ok := a.findProjectToken(raw)
		if !ok {
			fail(c, 401, "invalid_project_token", "valid project token required")
			return
		}
		now := time.Now()
		_ = a.db.Model(&t).Update("last_used_at", now).Error
		c.Set("projectID", t.ProjectID)
		c.Next()
	}
}
func (a *App) findProjectToken(raw string) (ProjectToken, bool) {
	if raw == "" {
		return ProjectToken{}, false
	}
	sum := sha256.Sum256([]byte(raw))
	var token ProjectToken
	if a.db.Where("token_hash = ?", hex.EncodeToString(sum[:])).First(&token).Error != nil {
		return token, false
	}
	return token, true
}
func (a *App) me(c *gin.Context) {
	var u User
	if a.db.First(&u, c.MustGet("userID")).Error != nil {
		fail(c, 404, "not_found", "user not found")
		return
	}
	c.JSON(200, u)
}
func (a *App) listProjects(c *gin.Context) {
	uid := c.MustGet("userID").(uint)
	var members []ProjectMember
	a.db.Preload("User").Where("user_id=?", uid).Find(&members)
	ids := []uint{}
	roles := map[uint]string{}
	for _, member := range members {
		ids = append(ids, member.ProjectID)
		roles[member.ProjectID] = member.Role
	}
	var projects []Project
	if len(ids) > 0 {
		a.db.Where("id IN ?", ids).Order("id desc").Find(&projects)
	}
	type aggregateRow struct {
		ProjectID uint
		Count     int64
		Bytes     int64
		LatestID  uint
	}
	workflowCounts := map[uint]int64{}
	releaseCounts := map[uint]int64{}
	artifactBytes := map[uint]int64{}
	latestReleases := map[uint]Release{}
	if len(ids) > 0 {
		var workflowRows, releaseRows, byteRows []aggregateRow
		a.db.Model(&Workflow{}).Select("project_id, COUNT(*) AS count").Where("project_id IN ?", ids).Group("project_id").Scan(&workflowRows)
		a.db.Model(&Release{}).Select("project_id, COUNT(*) AS count, MAX(id) AS latest_id").Where("project_id IN ?", ids).Group("project_id").Scan(&releaseRows)
		a.db.Model(&ArtifactFile{}).Select("releases.project_id, COALESCE(SUM(artifact_files.size), 0) AS bytes").Joins("JOIN releases ON releases.id = artifact_files.release_id").Where("releases.project_id IN ?", ids).Group("releases.project_id").Scan(&byteRows)
		latestIDs := make([]uint, 0, len(releaseRows))
		for _, row := range workflowRows {
			workflowCounts[row.ProjectID] = row.Count
		}
		for _, row := range releaseRows {
			releaseCounts[row.ProjectID] = row.Count
			latestIDs = append(latestIDs, row.LatestID)
		}
		for _, row := range byteRows {
			artifactBytes[row.ProjectID] = row.Bytes
		}
		if len(latestIDs) > 0 {
			var latest []Release
			a.db.Where("id IN ?", latestIDs).Find(&latest)
			for _, release := range latest {
				latestReleases[release.ProjectID] = release
			}
		}
	}
	items := make([]gin.H, 0, len(projects))
	for _, p := range projects {
		item := gin.H{"id": p.ID, "name": p.Name, "type": p.Type, "max_artifact_bytes": p.MaxArtifactBytes, "artifact_bytes": int64(0), "release_count": int64(0), "workflow_count": workflowCounts[p.ID], "latest_version": "", "latest_release_at": nil, "created_at": p.CreatedAt, "role": roles[p.ID]}
		if p.Type == "artifact" {
			item["release_count"] = releaseCounts[p.ID]
			item["artifact_bytes"] = artifactBytes[p.ID]
			if latest, exists := latestReleases[p.ID]; exists {
				item["latest_version"] = latest.Version
				item["latest_release_at"] = latest.CreatedAt
			}
		}
		items = append(items, item)
	}
	c.JSON(200, items)
}
func (a *App) createProject(c *gin.Context) {
	var in struct {
		Name             string `json:"name" binding:"required"`
		Type             string `json:"type"`
		MaxArtifactBytes int64  `json:"max_artifact_bytes"`
	}
	if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Name) == "" {
		fail(c, 400, "invalid_request", "name is required")
		return
	}
	if in.Type == "" {
		in.Type = "artifact"
	}
	if in.Type != "artifact" && in.Type != "scheduled" {
		fail(c, 400, "invalid_project_type", "type must be artifact or scheduled")
		return
	}
	if in.MaxArtifactBytes < 0 {
		fail(c, 400, "invalid_capacity", "max_artifact_bytes cannot be negative")
		return
	}
	if in.Type == "scheduled" {
		in.MaxArtifactBytes = 0
	}
	p := Project{UserID: c.MustGet("userID").(uint), Name: strings.TrimSpace(in.Name), Type: in.Type, MaxArtifactBytes: in.MaxArtifactBytes}
	var raw string
	err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&p).Error; err != nil {
			return err
		}
		if err := tx.Create(&ProjectMember{ProjectID: p.ID, UserID: p.UserID, Role: "owner"}).Error; err != nil {
			return err
		}
		if p.Type == "scheduled" {
			return nil
		}
		_, generated, err := generateProjectToken(tx, p.ID)
		raw = generated
		return err
	})
	if err != nil {
		fail(c, 409, "project_exists", "project name already exists")
		return
	}
	c.JSON(201, gin.H{"id": p.ID, "name": p.Name, "type": p.Type, "max_artifact_bytes": p.MaxArtifactBytes, "artifact_bytes": int64(0), "role": "owner", "created_at": p.CreatedAt, "token": raw})
}
func (a *App) ownedProject(c *gin.Context, id uint) (Project, bool) {
	p, _, ok := a.projectAccess(c, id, projectView)
	return p, ok
}
func (a *App) getProject(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	p, member, ok := a.projectAccess(c, id, projectView)
	if ok {
		c.JSON(200, gin.H{"id": p.ID, "name": p.Name, "type": p.Type, "max_artifact_bytes": p.MaxArtifactBytes, "artifact_bytes": a.projectArtifactBytes(id), "created_at": p.CreatedAt, "role": member.Role})
	}
}

func (a *App) updateProject(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	project, _, ok := a.projectAccess(c, id, projectAdmin)
	if !ok {
		return
	}
	if project.Type != "artifact" {
		fail(c, 400, "invalid_project_type", "scheduled projects do not store artifacts")
		return
	}
	var in struct {
		MaxArtifactBytes int64 `json:"max_artifact_bytes"`
	}
	if c.ShouldBindJSON(&in) != nil || in.MaxArtifactBytes < 0 {
		fail(c, 400, "invalid_capacity", "max_artifact_bytes must be zero or a positive byte count")
		return
	}
	if err := a.db.Model(&project).Update("max_artifact_bytes", in.MaxArtifactBytes).Error; err != nil {
		fail(c, 500, "database_error", "unable to update project capacity")
		return
	}
	project.MaxArtifactBytes = in.MaxArtifactBytes
	a.enforceProjectArtifactCapacity(project.ID, 0)
	c.JSON(200, gin.H{"max_artifact_bytes": project.MaxArtifactBytes, "artifact_bytes": a.projectArtifactBytes(project.ID)})
}
func (a *App) deleteProject(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	_, _, ok = a.projectAccess(c, id, projectOwn)
	if !ok {
		return
	}
	root := filepath.Join(a.cfg.StorageDir, strconv.FormatUint(uint64(id), 10))
	trash := root + fmt.Sprintf(".deleting-%d", time.Now().UnixNano())
	moved := false
	if _, err := os.Stat(root); err == nil {
		if err = os.Rename(root, trash); err != nil {
			fail(c, 500, "storage_error", "unable to prepare artifact deletion")
			return
		}
		moved = true
	}
	err := a.db.Transaction(func(tx *gorm.DB) error {
		var schedules []WorkflowSchedule
		tx.Where("project_id=?", id).Find(&schedules)
		for _, schedule := range schedules {
			if err := tx.Where("schedule_id=?", schedule.ID).Delete(&ScheduleEvent{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("project_id=?", id).Delete(&WorkflowSchedule{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id=?", id).Delete(&WorkflowRevision{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id=?", id).Delete(&ProjectMember{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&ProjectToken{}).Error; err != nil {
			return err
		}
		var releases []Release
		if err := tx.Where("project_id = ?", id).Find(&releases).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&ArtifactAccessLog{}).Error; err != nil {
			return err
		}
		for _, r := range releases {
			if err := tx.Where("release_id = ?", r.ID).Delete(&ArtifactFile{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("project_id = ?", id).Delete(&Release{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&Project{}, id).Error; err != nil {
			return err
		}
		if moved {
			if err := os.RemoveAll(trash); err != nil {
				return fmt.Errorf("remove artifact files: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		if moved {
			_ = os.Rename(trash, root)
		}
		fail(c, 500, "database_error", "unable to delete project")
		return
	}
	c.Status(204)
}
func (a *App) listTokens(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	project, _, ok := a.projectAccess(c, id, projectAdmin)
	if !ok {
		return
	}
	if project.Type != "artifact" {
		fail(c, 400, "invalid_project_type", "scheduled projects do not have tokens")
		return
	}
	var items []ProjectToken
	a.db.Where("project_id = ?", id).Order("id desc").Find(&items)
	c.JSON(200, items)
}
func (a *App) createToken(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	project, _, ok := a.projectAccess(c, id, projectAdmin)
	if !ok {
		return
	}
	if project.Type != "artifact" {
		fail(c, 400, "invalid_project_type", "scheduled projects do not have tokens")
		return
	}
	var t ProjectToken
	var raw string
	err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("project_id = ?", id).Delete(&ProjectToken{}).Error; err != nil {
			return err
		}
		var err error
		t, raw, err = generateProjectToken(tx, id)
		return err
	})
	if err != nil {
		fail(c, 500, "database_error", "unable to create token")
		return
	}
	c.JSON(201, gin.H{"id": t.ID, "name": t.Name, "prefix": t.Prefix, "created_at": t.CreatedAt, "token": raw})
}
func generateProjectToken(db *gorm.DB, projectID uint) (ProjectToken, string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return ProjectToken{}, "", err
	}
	raw := "ps_" + base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(raw))
	t := ProjectToken{ProjectID: projectID, Name: "project", Prefix: raw[:11], TokenHash: hex.EncodeToString(sum[:])}
	return t, raw, db.Create(&t).Error
}
func (a *App) getToken(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	project, _, ok := a.projectAccess(c, id, projectAdmin)
	if !ok {
		return
	}
	if project.Type != "artifact" {
		fail(c, 400, "invalid_project_type", "scheduled projects do not have tokens")
		return
	}
	var token ProjectToken
	if a.db.Where("project_id = ?", id).First(&token).Error != nil {
		fail(c, 404, "not_found", "project token not found")
		return
	}
	c.JSON(200, token)
}
func (a *App) rotateToken(c *gin.Context) { a.createToken(c) }
func (a *App) deleteToken(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	project, _, ok := a.projectAccess(c, id, projectAdmin)
	if !ok {
		return
	}
	if project.Type != "artifact" {
		fail(c, 400, "invalid_project_type", "scheduled projects do not have tokens")
		return
	}
	tid, ok := parseID(c, "tokenId")
	if !ok {
		return
	}
	r := a.db.Where("id = ? AND project_id = ?", tid, id).Delete(&ProjectToken{})
	if r.RowsAffected == 0 {
		fail(c, 404, "not_found", "token not found")
		return
	}
	c.Status(204)
}
func (a *App) listReleases(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	project, _, ok := a.projectAccess(c, id, projectView)
	if !ok {
		return
	}
	if project.Type != "artifact" {
		fail(c, 400, "invalid_project_type", "scheduled projects do not have releases")
		return
	}
	var items []Release
	a.db.Model(&Release{}).
		Select("releases.*, (SELECT COUNT(*) FROM artifact_access_logs WHERE artifact_access_logs.release_id = releases.id) AS access_count").
		Where("releases.project_id = ?", id).
		Order("releases.id desc").
		Scan(&items)
	c.JSON(200, items)
}
func (a *App) getRelease(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	project, _, ok := a.projectAccess(c, id, projectView)
	if !ok {
		return
	}
	if project.Type != "artifact" {
		fail(c, 400, "invalid_project_type", "scheduled projects do not have releases")
		return
	}
	rid, ok := parseID(c, "releaseId")
	if !ok {
		return
	}
	var r Release
	if a.db.Preload("Files").
		Select("releases.*, (SELECT COUNT(*) FROM artifact_access_logs WHERE artifact_access_logs.release_id = releases.id) AS access_count").
		Where("releases.id = ? AND releases.project_id = ?", rid, id).
		First(&r).Error != nil {
		fail(c, 404, "not_found", "release not found")
		return
	}
	c.JSON(200, r)
}

type artifactAccessResponse struct {
	ID           uint      `json:"id"`
	IPAddress    string    `json:"ip_address"`
	AccessMethod string    `json:"access_method"`
	HTTPMethod   string    `json:"http_method"`
	FileID       uint      `json:"file_id"`
	FileName     string    `json:"file_name"`
	CreatedAt    time.Time `json:"created_at"`
}

func (a *App) listArtifactAccesses(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	if _, _, ok = a.projectAccess(c, id, projectView); !ok {
		return
	}
	rid, ok := parseID(c, "releaseId")
	if !ok {
		return
	}
	var releaseCount int64
	if a.db.Model(&Release{}).Where("id = ? AND project_id = ?", rid, id).Count(&releaseCount).Error != nil || releaseCount == 0 {
		fail(c, 404, "not_found", "release not found")
		return
	}
	items := make([]artifactAccessResponse, 0)
	a.db.Table("artifact_access_logs").
		Select("artifact_access_logs.id, artifact_access_logs.ip_address, artifact_access_logs.access_method, artifact_access_logs.http_method, artifact_access_logs.artifact_file_id AS file_id, artifact_files.original_name AS file_name, artifact_access_logs.created_at").
		Joins("JOIN artifact_files ON artifact_files.id = artifact_access_logs.artifact_file_id").
		Where("artifact_access_logs.project_id = ? AND artifact_access_logs.release_id = ?", id, rid).
		Order("artifact_access_logs.id DESC").
		Limit(200).
		Scan(&items)
	c.JSON(200, items)
}
func (a *App) download(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	if _, ok = a.ownedProject(c, id); !ok {
		return
	}
	rid, ok := parseID(c, "releaseId")
	if !ok {
		return
	}
	fid, ok := parseID(c, "fileId")
	if !ok {
		return
	}
	var f ArtifactFile
	if a.db.Joins("JOIN releases ON releases.id = artifact_files.release_id").Where("artifact_files.id = ? AND artifact_files.release_id = ? AND releases.project_id = ?", fid, rid, id).First(&f).Error != nil {
		fail(c, 404, "not_found", "file not found")
		return
	}
	uid := c.MustGet("userID").(uint)
	a.serveArtifact(c, id, rid, f, "jwt", &uid)
}
func (a *App) tokenDownload(c *gin.Context) {
	raw, version, filename := c.Query("token"), strings.TrimSpace(c.Query("version")), c.Query("file")
	if raw == "" {
		fail(c, 400, "invalid_request", "token is required")
		return
	}
	if version == "" {
		version = "latest"
	}
	token, ok := a.findProjectToken(raw)
	if !ok {
		fail(c, 401, "invalid_project_token", "valid project token required")
		return
	}
	var release Release
	releaseQuery := a.db.Where("project_id = ?", token.ProjectID)
	if version == "latest" {
		releaseQuery = releaseQuery.Order("created_at DESC").Order("id DESC")
	} else {
		releaseQuery = releaseQuery.Where("version = ?", version)
	}
	if releaseQuery.First(&release).Error != nil {
		fail(c, 404, "not_found", "release not found")
		return
	}
	var file ArtifactFile
	if filename == "" {
		var files []ArtifactFile
		a.db.Where("release_id = ?", release.ID).Find(&files)
		archives := make([]ArtifactFile, 0, 1)
		for _, candidate := range files {
			if candidate.Kind == "uploaded" && strings.EqualFold(filepath.Ext(candidate.OriginalName), ".zip") {
				archives = append(archives, candidate)
			}
		}
		if len(archives) != 1 {
			fail(c, 400, "ambiguous_file", "file is required unless the release contains exactly one uploaded ZIP archive")
			return
		}
		file = archives[0]
	} else if a.db.Where("release_id = ? AND original_name = ?", release.ID, filename).First(&file).Error != nil {
		fail(c, 404, "not_found", "file not found")
		return
	}
	now := time.Now()
	_ = a.db.Model(&token).Update("last_used_at", now).Error
	a.serveArtifact(c, token.ProjectID, release.ID, file, "project_token", nil)
}
func (a *App) serveArtifact(c *gin.Context, projectID, releaseID uint, f ArtifactFile, accessMethod string, userID *uint) {
	path := filepath.Join(a.cfg.StorageDir, strconv.FormatUint(uint64(projectID), 10), strconv.FormatUint(uint64(releaseID), 10), f.StoredName)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		fail(c, 404, "not_found", "artifact file not found on storage")
		return
	}
	_ = a.db.Create(&ArtifactAccessLog{
		ProjectID: projectID, ReleaseID: releaseID, ArtifactFileID: f.ID,
		UserID: userID, IPAddress: c.ClientIP(), AccessMethod: accessMethod,
		HTTPMethod: c.Request.Method,
	}).Error
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(f.OriginalName)))
	c.File(path)
}

func (a *App) upload(c *gin.Context) {
	projectID := c.MustGet("projectID").(uint)
	var project Project
	if a.db.First(&project, projectID).Error != nil || project.Type != "artifact" {
		fail(c, 400, "invalid_project_type", "only artifact projects accept uploads")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, a.cfg.MaxBatchBytes+(10<<20))
	if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
		fail(c, 413, "batch_too_large", "invalid or oversized multipart request")
		return
	}
	version := strings.TrimSpace(c.PostForm("version"))
	if version == "" || len(version) > 255 || version == "latest" {
		fail(c, 400, "invalid_version", "version is required, must not exceed 255 characters, and must not be the reserved value latest")
		return
	}
	headers := c.Request.MultipartForm.File["files"]
	if len(headers) == 0 {
		fail(c, 400, "files_required", "at least one file is required")
		return
	}
	tmp, err := os.MkdirTemp(a.cfg.StorageDir, ".upload-")
	if err != nil {
		fail(c, 500, "storage_error", "unable to create upload staging area")
		return
	}
	defer os.RemoveAll(tmp)
	seen := map[string]bool{}
	files := make([]ArtifactFile, 0, len(headers))
	var total int64
	for i, h := range headers {
		name := h.Filename
		if !validFilename(name) || seen[name] {
			fail(c, 400, "invalid_filename", "file names must be unique, plain file names")
			return
		}
		seen[name] = true
		if h.Size > a.cfg.MaxFileBytes {
			fail(c, 413, "file_too_large", name+" exceeds the file size limit")
			return
		}
		total += h.Size
		if total > a.cfg.MaxBatchBytes {
			fail(c, 413, "batch_too_large", "files exceed the batch size limit")
			return
		}
		src, err := h.Open()
		if err != nil {
			fail(c, 400, "file_read_error", "unable to read "+name)
			return
		}
		stored := fmt.Sprintf("%04d-%x", i, sha256.Sum256([]byte(name)))
		dst, err := os.OpenFile(filepath.Join(tmp, stored), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			src.Close()
			fail(c, 500, "storage_error", "unable to stage file")
			return
		}
		hash := sha256.New()
		n, copyErr := io.Copy(io.MultiWriter(dst, hash), io.LimitReader(src, a.cfg.MaxFileBytes+1))
		closeErr := dst.Close()
		src.Close()
		if copyErr != nil || closeErr != nil || n > a.cfg.MaxFileBytes {
			fail(c, 413, "file_too_large", "unable to save "+name)
			return
		}
		files = append(files, ArtifactFile{OriginalName: name, StoredName: stored, Size: n, SHA256: hex.EncodeToString(hash.Sum(nil)), MIMEType: h.Header.Get("Content-Type"), Kind: "uploaded"})
	}
	files, total, err = expandUploadedZIPs(tmp, files, total, a.cfg.MaxFileBytes, a.cfg.MaxBatchBytes)
	if err != nil {
		if errors.Is(err, errExtractTooLarge) {
			fail(c, 413, "extracted_files_too_large", err.Error())
		} else {
			fail(c, 400, "invalid_archive", err.Error())
		}
		return
	}
	var existing Release
	if err := a.db.Preload("Files").Where("project_id = ? AND version = ?", projectID, version).First(&existing).Error; err == nil {
		if artifactFilesEqual(existing.Files, files) {
			c.JSON(200, existing)
		} else {
			fail(c, 409, "release_exists", "release version already exists with different files")
		}
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		fail(c, 500, "database_error", "unable to check release")
		return
	}
	refType := c.PostForm("ref_type")
	if refType == "" && c.PostForm("commit_sha") != "" {
		refType = "commit"
	}
	release := Release{ProjectID: projectID, Version: version, CommitSHA: c.PostForm("commit_sha"), Branch: c.PostForm("branch"), JobURL: c.PostForm("job_url"), PipelineID: c.PostForm("pipeline_id"), RefType: refType}
	final := ""
	err = a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&release).Error; err != nil {
			return err
		}
		final = filepath.Join(a.cfg.StorageDir, strconv.FormatUint(uint64(projectID), 10), strconv.FormatUint(uint64(release.ID), 10))
		if err := os.MkdirAll(filepath.Dir(final), 0755); err != nil {
			return err
		}
		if err := os.Rename(tmp, final); err != nil {
			return err
		}
		for i := range files {
			files[i].ReleaseID = release.ID
		}
		if err := tx.Create(&files).Error; err != nil {
			_ = os.RemoveAll(final)
			return err
		}
		if err := tx.Create(&ReleaseEvent{ReleaseID: release.ID}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		if final != "" {
			_ = os.RemoveAll(final)
		}
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			if fetchErr := a.db.Preload("Files").Where("project_id = ? AND version = ?", projectID, version).First(&existing).Error; fetchErr == nil && artifactFilesEqual(existing.Files, files) {
				c.JSON(200, existing)
			} else {
				fail(c, 409, "release_exists", "release version already exists with different files")
			}
		} else {
			fail(c, 500, "upload_failed", "unable to save release")
		}
		return
	}
	release.Files = files
	for _, hook := range a.releaseHooks {
		hook(release)
	}
	a.enforceProjectArtifactCapacity(projectID, release.ID)
	c.JSON(201, release)
}
func artifactFilesEqual(existing, incoming []ArtifactFile) bool {
	existing = uploadedArtifactFiles(existing)
	incoming = uploadedArtifactFiles(incoming)
	if len(existing) != len(incoming) {
		return false
	}
	byName := make(map[string]ArtifactFile, len(existing))
	for _, file := range existing {
		byName[file.OriginalName] = file
	}
	for _, file := range incoming {
		old, ok := byName[file.OriginalName]
		if !ok || old.Size != file.Size || old.SHA256 != file.SHA256 {
			return false
		}
	}
	return true
}

func uploadedArtifactFiles(files []ArtifactFile) []ArtifactFile {
	result := make([]ArtifactFile, 0, len(files))
	for _, file := range files {
		if file.Kind == "" || file.Kind == "uploaded" {
			result = append(result, file)
		}
	}
	return result
}
func validFilename(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name && !strings.ContainsAny(name, "/\\\x00")
}
