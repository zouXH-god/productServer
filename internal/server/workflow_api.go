package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/ssh"
	"io"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

func (a *App) workflowRoutes(api *gin.RouterGroup) {
	api.GET("/ssh-credentials", a.listCredentials)
	api.POST("/ssh-credentials", a.createCredential)
	api.DELETE("/ssh-credentials/:credentialId", a.deleteCredential)
	api.GET("/ssh-connections", a.listConnections)
	api.POST("/ssh-connections", a.createConnection)
	api.PUT("/ssh-connections/:connectionId", a.updateConnection)
	api.DELETE("/ssh-connections/:connectionId", a.deleteConnection)
	api.POST("/ssh-connections/:connectionId/test", a.testConnection)
	api.GET("/projects/:id/workflows", a.listWorkflows)
	api.POST("/projects/:id/workflows", a.saveWorkflow)
	api.GET("/projects/:id/workflows/:workflowId", a.getWorkflow)
	api.PUT("/projects/:id/workflows/:workflowId", a.saveWorkflow)
	api.GET("/projects/:id/workflows/:workflowId/history", a.listWorkflowHistory)
	api.GET("/projects/:id/workflows/:workflowId/history/:revisionId", a.getWorkflowRevision)
	api.POST("/projects/:id/workflows/:workflowId/copy", a.copyWorkflow)
	api.DELETE("/projects/:id/workflows/:workflowId", a.deleteWorkflow)
	api.POST("/projects/:id/workflows/:workflowId/runs", a.manualRun)
	api.GET("/projects/:id/runs", a.listRuns)
	api.GET("/projects/:id/runs/:runId", a.getRun)
	api.GET("/projects/:id/runs/:runId/logs", a.getRunLogs)
	api.POST("/projects/:id/runs/:runId/cancel", a.cancelRun)
	api.GET("/projects/:id/runs/:runId/logs/stream", a.streamLogs)
}
func (a *App) listCredentials(c *gin.Context) {
	var x []SSHCredential
	a.db.Where("user_id=?", c.MustGet("userID")).Find(&x)
	c.JSON(200, x)
}
func (a *App) createCredential(c *gin.Context) {
	var in struct {
		Name       string `json:"name"`
		PrivateKey string `json:"private_key"`
		Passphrase string `json:"passphrase"`
	}
	if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.PrivateKey) == "" {
		fail(c, 400, "invalid_request", "name and private_key required")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.PrivateKey = strings.TrimSpace(in.PrivateKey)
	enc, e := encryptSecret(a.cfg.SecretEncryptionKey, in.PrivateKey)
	if e != nil {
		fail(c, 400, "encryption_unavailable", e.Error())
		return
	}
	pass, e := encryptSecret(a.cfg.SecretEncryptionKey, in.Passphrase)
	if e != nil {
		fail(c, 400, "encryption_unavailable", e.Error())
		return
	}
	var signer ssh.Signer
	if in.Passphrase != "" {
		signer, e = ssh.ParsePrivateKeyWithPassphrase([]byte(in.PrivateKey), []byte(in.Passphrase))
	} else {
		signer, e = ssh.ParsePrivateKey([]byte(in.PrivateKey))
	}
	if e != nil {
		fail(c, 400, "invalid_private_key", e.Error())
		return
	}
	cred := SSHCredential{UserID: c.MustGet("userID").(uint), Name: in.Name, PrivateKeyEncrypted: enc, PassphraseEncrypted: pass, PublicKey: string(ssh.MarshalAuthorizedKey(signer.PublicKey())), Fingerprint: ssh.FingerprintSHA256(signer.PublicKey())}
	a.db.Create(&cred)
	c.JSON(201, cred)
}
func (a *App) deleteCredential(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("credentialId"))
	var n int64
	a.db.Model(&SSHConnection{}).Where("credential_id=?", id).Count(&n)
	if n > 0 {
		fail(c, 409, "credential_in_use", "credential is used by a connection")
		return
	}
	a.db.Where("id=? AND user_id=?", id, c.MustGet("userID")).Delete(&SSHCredential{})
	c.Status(204)
}
func (a *App) listConnections(c *gin.Context) {
	var x []SSHConnection
	ownerID := c.MustGet("userID").(uint)
	if raw := c.Query("project_id"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			fail(c, 400, "invalid_id", "invalid project_id")
			return
		}
		project, _, ok := a.projectAccess(c, uint(id), projectView)
		if !ok {
			return
		}
		ownerID = project.UserID
	}
	a.db.Where("user_id=?", ownerID).Find(&x)
	c.JSON(200, x)
}

type connectionPayload struct {
	Name           string `json:"name"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Username       string `json:"username"`
	CredentialID   uint   `json:"credential_id"`
	AuthType       string `json:"auth_type"`
	Password       string `json:"password"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func (a *App) applyConnection(c *gin.Context, x *SSHConnection, creating bool) bool {
	var in connectionPayload
	if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Host) == "" || strings.TrimSpace(in.Username) == "" {
		fail(c, 400, "invalid_request", "name, host and username required")
		return false
	}
	if in.AuthType == "" {
		in.AuthType = "key"
	}
	if in.AuthType != "key" && in.AuthType != "password" {
		fail(c, 400, "invalid_auth_type", "auth_type must be key or password")
		return false
	}
	if in.Port <= 0 {
		in.Port = 22
	}
	if in.TimeoutSeconds <= 0 {
		in.TimeoutSeconds = 10
	}
	x.Name, x.Host, x.Port, x.Username, x.TimeoutSeconds, x.AuthType = strings.TrimSpace(in.Name), strings.TrimSpace(in.Host), in.Port, strings.TrimSpace(in.Username), in.TimeoutSeconds, in.AuthType
	if in.AuthType == "key" {
		if !a.ownsCredential(in.CredentialID, x.UserID) {
			fail(c, 400, "invalid_credential", "credential not found")
			return false
		}
		x.CredentialID, x.PasswordEncrypted = in.CredentialID, ""
		return true
	}
	if creating && in.Password == "" {
		fail(c, 400, "invalid_request", "password is required for password authentication")
		return false
	}
	if in.Password != "" {
		enc, err := encryptSecret(a.cfg.SecretEncryptionKey, in.Password)
		if err != nil {
			fail(c, 400, "encryption_unavailable", err.Error())
			return false
		}
		x.PasswordEncrypted = enc
	}
	x.CredentialID = 0
	return true
}
func (a *App) createConnection(c *gin.Context) {
	x := SSHConnection{UserID: c.MustGet("userID").(uint), Port: 22, TimeoutSeconds: 10, AuthType: "key"}
	if !a.applyConnection(c, &x, true) {
		return
	}
	a.db.Create(&x)
	c.JSON(201, x)
}
func (a *App) updateConnection(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("connectionId"))
	var x SSHConnection
	if a.db.Where("id=? AND user_id=?", id, c.MustGet("userID")).First(&x).Error != nil {
		fail(c, 404, "not_found", "connection not found")
		return
	}
	if !a.applyConnection(c, &x, false) {
		return
	}
	a.db.Save(&x)
	c.JSON(200, x)
}
func (a *App) ownsCredential(id, uid uint) bool {
	var n int64
	a.db.Model(&SSHCredential{}).Where("id=? AND user_id=?", id, uid).Count(&n)
	return n == 1
}
func (a *App) deleteConnection(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("connectionId"))
	a.db.Where("id=? AND user_id=?", id, c.MustGet("userID")).Delete(&SSHConnection{})
	c.Status(204)
}
func (a *App) testConnection(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("connectionId"))
	client, e := a.sshClient(uint(id), c.MustGet("userID").(uint))
	if e != nil {
		fail(c, 400, "ssh_failed", e.Error())
		return
	}
	client.Close()
	c.JSON(200, gin.H{"status": "ok", "warning": "host key verification is disabled"})
}
func (a *App) sshClient(id, uid uint) (*ssh.Client, error) {
	var conn SSHConnection
	if e := a.db.Where("id=? AND user_id=?", id, uid).First(&conn).Error; e != nil {
		return nil, e
	}
	var auth ssh.AuthMethod
	if conn.AuthType == "password" {
		password, e := decryptSecret(a.cfg.SecretEncryptionKey, conn.PasswordEncrypted)
		if e != nil {
			return nil, e
		}
		if password == "" {
			return nil, fmt.Errorf("password authentication is not configured")
		}
		auth = ssh.Password(password)
	} else {
		var cred SSHCredential
		if e := a.db.First(&cred, conn.CredentialID).Error; e != nil {
			return nil, e
		}
		key, e := decryptSecret(a.cfg.SecretEncryptionKey, cred.PrivateKeyEncrypted)
		if e != nil {
			return nil, e
		}
		pass, e := decryptSecret(a.cfg.SecretEncryptionKey, cred.PassphraseEncrypted)
		if e != nil {
			return nil, e
		}
		var signer ssh.Signer
		if pass != "" {
			signer, e = ssh.ParsePrivateKeyWithPassphrase([]byte(key), []byte(pass))
		} else {
			signer, e = ssh.ParsePrivateKey([]byte(key))
		}
		if e != nil {
			return nil, e
		}
		auth = ssh.PublicKeys(signer)
	}
	timeout := time.Duration(conn.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return ssh.Dial("tcp", net.JoinHostPort(conn.Host, strconv.Itoa(conn.Port)), &ssh.ClientConfig{User: conn.Username, Auth: []ssh.AuthMethod{auth}, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: timeout})
}

type workflowPayload struct {
	Name           string             `json:"name"`
	Enabled        bool               `json:"enabled"`
	TriggerType    string             `json:"trigger_type"`
	TriggerGlob    string             `json:"trigger_glob"`
	BaseRevisionID uint               `json:"base_revision_id"`
	Definition     WorkflowDefinition `json:"definition"`
}

func (a *App) listWorkflows(c *gin.Context) {
	pid, ok := parseID(c, "id")
	if !ok {
		return
	}
	if _, _, ok = a.projectAccess(c, pid, projectView); !ok {
		return
	}
	var x []Workflow
	a.db.Where("project_id=?", pid).Find(&x)
	items := make([]gin.H, 0, len(x))
	for _, w := range x {
		item := gin.H{"id": w.ID, "project_id": w.ProjectID, "name": w.Name, "enabled": w.Enabled, "trigger_type": w.TriggerType, "trigger_glob": w.TriggerGlob, "created_at": w.CreatedAt, "updated_at": w.UpdatedAt}
		var schedule WorkflowSchedule
		if a.db.Where("workflow_id=?", w.ID).First(&schedule).Error == nil {
			item["schedule"] = schedule
			var event ScheduleEvent
			if a.db.Where("schedule_id=?", schedule.ID).Order("id desc").First(&event).Error == nil {
				item["last_schedule_event"] = event
			}
		}
		items = append(items, item)
	}
	c.JSON(200, items)
}
func (a *App) saveWorkflow(c *gin.Context) {
	pid, ok := parseID(c, "id")
	if !ok {
		return
	}
	project, _, ok := a.projectAccess(c, pid, projectDevelop)
	if !ok {
		return
	}
	var p workflowPayload
	if c.ShouldBindJSON(&p) != nil || p.Name == "" || !validateDefinition(p.Definition) {
		fail(c, 400, "invalid_workflow", "valid name and acyclic DAG required")
		return
	}
	if err := a.validateWorkflowConnections(project.UserID, p.Definition); err != nil {
		fail(c, 400, "invalid_server_list", err.Error())
		return
	}
	if p.TriggerType != "any" && p.TriggerType != "tag" && p.TriggerType != "commit" && p.TriggerType != "tag_glob" && p.TriggerType != "branch_glob" {
		fail(c, 400, "invalid_trigger", "invalid trigger type")
		return
	}
	p.TriggerGlob = strings.TrimSpace(p.TriggerGlob)
	if p.TriggerType == "tag_glob" || p.TriggerType == "branch_glob" {
		if p.TriggerGlob == "" {
			fail(c, 400, "invalid_trigger_glob", "trigger glob is required")
			return
		}
		if _, err := path.Match(p.TriggerGlob, "validation"); err != nil {
			fail(c, 400, "invalid_trigger_glob", "invalid trigger glob")
			return
		}
	}
	w := Workflow{ProjectID: pid, Name: p.Name, Enabled: p.Enabled, TriggerType: p.TriggerType, TriggerGlob: p.TriggerGlob}
	status := 201
	var old WorkflowDefinition
	if raw := c.Param("workflowId"); raw != "" {
		id, _ := strconv.Atoi(raw)
		if a.db.Where("id=? AND project_id=?", id, pid).First(&w).Error != nil {
			fail(c, 404, "not_found", "workflow not found")
			return
		}
		_ = json.Unmarshal([]byte(w.Definition), &old)
		w.Name = p.Name
		w.Enabled = p.Enabled
		w.TriggerType = p.TriggerType
		w.TriggerGlob = p.TriggerGlob
		status = 200
	}
	if p.BaseRevisionID != 0 && w.ID != 0 {
		var revision WorkflowRevision
		if a.db.Where("id=? AND workflow_id=? AND project_id=?", p.BaseRevisionID, w.ID, pid).First(&revision).Error != nil {
			fail(c, 400, "invalid_revision", "workflow revision not found")
			return
		}
		_ = json.Unmarshal([]byte(revision.Definition), &old)
	}
	oldSecrets := map[string]string{}
	for _, n := range old.Nodes {
		if n.Type == "http_webhook" {
			oldSecrets[n.ID] = str(n.Config, "secret")
		}
	}
	for i := range p.Definition.Nodes {
		n := &p.Definition.Nodes[i]
		if n.Type != "http_webhook" {
			continue
		}
		secret := str(n.Config, "secret")
		if secret == "" {
			n.Config["secret"] = oldSecrets[n.ID]
			continue
		}
		if !strings.HasPrefix(secret, "enc:") {
			encrypted, e := encryptSecret(a.cfg.SecretEncryptionKey, secret)
			if e != nil {
				fail(c, 400, "encryption_unavailable", e.Error())
				return
			}
			n.Config["secret"] = "enc:" + encrypted
		}
	}
	data, _ := json.Marshal(p.Definition)
	w.Definition = string(data)
	revision := WorkflowRevision{ProjectID: pid, UserID: c.MustGet("userID").(uint), Name: w.Name, Enabled: w.Enabled, TriggerType: w.TriggerType, TriggerGlob: w.TriggerGlob, Definition: w.Definition}
	if err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&w).Error; err != nil {
			return err
		}
		revision.WorkflowID = w.ID
		return tx.Create(&revision).Error
	}); err != nil {
		fail(c, 500, "save_failed", "unable to save workflow")
		return
	}
	redactWorkflowSecrets(&p.Definition)
	c.JSON(status, gin.H{"id": w.ID, "project_id": w.ProjectID, "name": w.Name, "enabled": w.Enabled, "trigger_type": w.TriggerType, "trigger_glob": w.TriggerGlob, "definition": p.Definition, "created_at": w.CreatedAt, "updated_at": w.UpdatedAt})
}

func (a *App) validateWorkflowConnections(userID uint, definition WorkflowDefinition) error {
	for _, node := range definition.Nodes {
		if node.Type != "server_list" {
			continue
		}
		ids, ok := uintList(node.Config["connection_ids"])
		if !ok || len(ids) == 0 {
			return fmt.Errorf("服务器列表至少需要选择一台服务器")
		}
		seen := map[uint]bool{}
		for _, id := range ids {
			if seen[id] {
				return fmt.Errorf("服务器列表包含重复连接 %d", id)
			}
			seen[id] = true
			var count int64
			a.db.Model(&SSHConnection{}).Where("id=? AND user_id=?", id, userID).Count(&count)
			if count != 1 {
				return fmt.Errorf("服务器连接 %d 不存在或无权访问", id)
			}
		}
	}
	return nil
}
func (a *App) getWorkflow(c *gin.Context) {
	pid, ok := parseID(c, "id")
	if !ok {
		return
	}
	if _, _, ok = a.projectAccess(c, pid, projectView); !ok {
		return
	}
	var w Workflow
	if a.db.Where("id=? AND project_id=?", c.Param("workflowId"), pid).First(&w).Error != nil {
		fail(c, 404, "not_found", "workflow not found")
		return
	}
	var d WorkflowDefinition
	json.Unmarshal([]byte(w.Definition), &d)
	redactWorkflowSecrets(&d)
	c.JSON(200, gin.H{"id": w.ID, "name": w.Name, "enabled": w.Enabled, "trigger_type": w.TriggerType, "trigger_glob": w.TriggerGlob, "definition": d})
}
func (a *App) workflowHistoryContext(c *gin.Context) (uint, Workflow, bool) {
	pid, ok := parseID(c, "id")
	if !ok {
		return 0, Workflow{}, false
	}
	if _, _, ok = a.projectAccess(c, pid, projectView); !ok {
		return pid, Workflow{}, false
	}
	var workflow Workflow
	if a.db.Where("id=? AND project_id=?", c.Param("workflowId"), pid).First(&workflow).Error != nil {
		fail(c, 404, "not_found", "workflow not found")
		return pid, workflow, false
	}
	return pid, workflow, true
}
func (a *App) listWorkflowHistory(c *gin.Context) {
	pid, workflow, ok := a.workflowHistoryContext(c)
	if !ok {
		return
	}
	var revisions []WorkflowRevision
	a.db.Preload("User").Where("project_id=? AND workflow_id=?", pid, workflow.ID).Order("id desc").Find(&revisions)
	items := make([]gin.H, 0, len(revisions))
	for _, revision := range revisions {
		var definition WorkflowDefinition
		_ = json.Unmarshal([]byte(revision.Definition), &definition)
		items = append(items, gin.H{"id": revision.ID, "name": revision.Name, "enabled": revision.Enabled, "trigger_type": revision.TriggerType, "trigger_glob": revision.TriggerGlob, "node_count": len(definition.Nodes), "edge_count": len(definition.Edges), "created_at": revision.CreatedAt, "created_by": revision.User.Username})
	}
	c.JSON(200, items)
}
func (a *App) getWorkflowRevision(c *gin.Context) {
	pid, workflow, ok := a.workflowHistoryContext(c)
	if !ok {
		return
	}
	var revision WorkflowRevision
	if a.db.Preload("User").Where("id=? AND project_id=? AND workflow_id=?", c.Param("revisionId"), pid, workflow.ID).First(&revision).Error != nil {
		fail(c, 404, "not_found", "workflow revision not found")
		return
	}
	var definition WorkflowDefinition
	if json.Unmarshal([]byte(revision.Definition), &definition) != nil {
		fail(c, 500, "invalid_revision", "workflow revision is invalid")
		return
	}
	redactWorkflowSecrets(&definition)
	c.JSON(200, gin.H{"id": revision.ID, "workflow_id": revision.WorkflowID, "name": revision.Name, "enabled": revision.Enabled, "trigger_type": revision.TriggerType, "trigger_glob": revision.TriggerGlob, "definition": definition, "created_at": revision.CreatedAt, "created_by": revision.User.Username})
}
func redactWorkflowSecrets(d *WorkflowDefinition) {
	for i := range d.Nodes {
		if d.Nodes[i].Type == "http_webhook" && str(d.Nodes[i].Config, "secret") != "" {
			d.Nodes[i].Config["secret"] = ""
			d.Nodes[i].Config["secret_set"] = true
		}
	}
}
func (a *App) copyWorkflow(c *gin.Context) {
	pid, ok := parseID(c, "id")
	if !ok {
		return
	}
	if _, _, ok = a.projectAccess(c, pid, projectDevelop); !ok {
		return
	}
	var source Workflow
	if a.db.Where("id=? AND project_id=?", c.Param("workflowId"), pid).First(&source).Error != nil {
		fail(c, 404, "not_found", "workflow not found")
		return
	}
	var in struct {
		TargetProjectID uint   `json:"target_project_id"`
		Name            string `json:"name"`
	}
	if c.ShouldBindJSON(&in) != nil || in.TargetProjectID == 0 || strings.TrimSpace(in.Name) == "" {
		fail(c, 400, "invalid_request", "target_project_id and name required")
		return
	}
	target, _, ok := a.projectAccess(c, in.TargetProjectID, projectDevelop)
	if !ok {
		return
	}
	name := strings.TrimSpace(in.Name)
	var duplicate int64
	a.db.Model(&Workflow{}).Where("project_id=? AND name=?", target.ID, name).Count(&duplicate)
	if duplicate > 0 {
		fail(c, 409, "workflow_name_exists", "workflow name already exists in target project")
		return
	}
	var definition WorkflowDefinition
	if json.Unmarshal([]byte(source.Definition), &definition) != nil || !validateDefinition(definition) {
		fail(c, 409, "invalid_source_workflow", "source workflow definition is invalid")
		return
	}
	if err := a.validateWorkflowConnections(target.UserID, definition); err != nil {
		fail(c, 400, "invalid_target_resources", err.Error())
		return
	}
	copy := Workflow{ProjectID: target.ID, Name: name, Enabled: false, TriggerType: source.TriggerType, TriggerGlob: source.TriggerGlob, Definition: source.Definition}
	if err := a.db.Create(&copy).Error; err != nil {
		fail(c, 500, "copy_failed", "failed to copy workflow")
		return
	}
	redactWorkflowSecrets(&definition)
	c.JSON(201, gin.H{"id": copy.ID, "project_id": copy.ProjectID, "name": copy.Name, "enabled": copy.Enabled, "trigger_type": copy.TriggerType, "trigger_glob": copy.TriggerGlob, "definition": definition, "created_at": copy.CreatedAt, "updated_at": copy.UpdatedAt})
}
func (a *App) deleteWorkflow(c *gin.Context) {
	pid, ok := parseID(c, "id")
	if !ok {
		return
	}
	if _, _, ok = a.projectAccess(c, pid, projectDevelop); !ok {
		return
	}
	var schedule WorkflowSchedule
	if a.db.Where("workflow_id=?", c.Param("workflowId")).First(&schedule).Error == nil {
		a.db.Where("schedule_id=?", schedule.ID).Delete(&ScheduleEvent{})
		a.db.Delete(&schedule)
	}
	a.db.Where("workflow_id=? AND project_id=?", c.Param("workflowId"), pid).Delete(&WorkflowRevision{})
	a.db.Where("id=? AND project_id=?", c.Param("workflowId"), pid).Delete(&Workflow{})
	c.Status(204)
}
func (a *App) manualRun(c *gin.Context) {
	pid, ok := parseID(c, "id")
	if !ok {
		return
	}
	project, _, ok := a.projectAccess(c, pid, projectDevelop)
	if !ok {
		return
	}
	var in struct {
		ReleaseID uint `json:"release_id"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "invalid_request", "release_id required")
		return
	}
	var w Workflow
	var r Release
	if a.db.Where("id=? AND project_id=?", c.Param("workflowId"), pid).First(&w).Error != nil {
		fail(c, 404, "not_found", "workflow not found")
		return
	}
	var run WorkflowRun
	var e error
	if project.Type == "scheduled" {
		run, e = createEmptyRun(a.db, a.cfg, w, project, "manual", nil, false)
	} else {
		if a.db.Where("id=? AND project_id=?", in.ReleaseID, pid).First(&r).Error != nil {
			fail(c, 404, "not_found", "release not found")
			return
		}
		run, e = createArtifactRun(a.db, a.cfg, w, r, "manual")
	}
	if e != nil {
		fail(c, 500, "run_failed", e.Error())
		return
	}
	c.JSON(201, run)
}
func (a *App) listRuns(c *gin.Context) {
	pid, ok := parseID(c, "id")
	if !ok {
		return
	}
	if _, _, ok = a.projectAccess(c, pid, projectView); !ok {
		return
	}
	var x []WorkflowRun
	a.db.Where("project_id=?", pid).Order("id desc").Find(&x)
	c.JSON(200, x)
}
func (a *App) getRun(c *gin.Context) {
	pid, ok := parseID(c, "id")
	if !ok {
		return
	}
	project, _, ok := a.projectAccess(c, pid, projectView)
	if !ok {
		return
	}
	var x WorkflowRun
	if a.db.Preload("Nodes").Where("id=? AND project_id=?", c.Param("runId"), pid).First(&x).Error != nil {
		fail(c, 404, "not_found", "run not found")
		return
	}
	var definition WorkflowDefinition
	_ = json.Unmarshal([]byte(x.Snapshot), &definition)
	var workflow Workflow
	var release Release
	var runProject Project
	a.db.First(&workflow, x.WorkflowID)
	a.db.First(&release, x.ReleaseID)
	a.db.First(&runProject, x.ProjectID)
	nodes := make([]gin.H, 0, len(x.Nodes))
	serverIDs := []uint{}
	for _, node := range x.Nodes {
		if node.ServerConnectionID != 0 {
			serverIDs = append(serverIDs, node.ServerConnectionID)
		}
	}
	serverViews := map[uint]SSHConnection{}
	if len(serverIDs) > 0 {
		var connections []SSHConnection
		a.db.Select("id,name,host,port,username,auth_type").Where("id IN ? AND user_id=?", serverIDs, project.UserID).Find(&connections)
		for _, connection := range connections {
			serverViews[connection.ID] = connection
		}
	}
	for _, node := range x.Nodes {
		var outputs any
		if !node.OutputsSensitive && node.Outputs != "" {
			_ = json.Unmarshal([]byte(node.Outputs), &outputs)
		}
		var server any
		if view, exists := serverViews[node.ServerConnectionID]; exists {
			server = gin.H{"id": view.ID, "name": view.Name, "host": view.Host, "port": view.Port, "username": view.Username, "auth_type": view.AuthType}
		}
		nodes = append(nodes, gin.H{"id": node.ID, "node_key": node.NodeKey, "module": node.Module, "status": node.Status, "attempts": node.Attempts, "error_summary": node.ErrorSummary, "outputs": outputs, "outputs_sensitive": node.OutputsSensitive, "loop_node_key": node.LoopNodeKey, "iteration_index": node.IterationIndex, "server_list_node_key": node.ServerListNodeKey, "server_connection_id": node.ServerConnectionID, "server_index": node.ServerIndex, "server": server, "started_at": node.StartedAt, "finished_at": node.FinishedAt})
	}
	version := release.Version
	if version == "" {
		version = x.DisplayVersion
	}
	c.JSON(200, gin.H{"id": x.ID, "project_id": x.ProjectID, "project_name": runProject.Name, "workflow_id": x.WorkflowID, "workflow_name": workflow.Name, "release_id": x.ReleaseID, "version": version, "ref_type": release.RefType, "commit_sha": release.CommitSHA, "branch": release.Branch, "trigger_source": x.TriggerSource, "scheduled_for": x.ScheduledFor, "status": x.Status, "error_summary": x.ErrorSummary, "created_at": x.CreatedAt, "started_at": x.StartedAt, "finished_at": x.FinishedAt, "log_bytes": x.LogBytes, "definition": definition, "nodes": nodes})
}
func (a *App) getRunLogs(c *gin.Context) {
	pid, ok := parseID(c, "id")
	if !ok {
		return
	}
	if _, _, ok = a.projectAccess(c, pid, projectView); !ok {
		return
	}
	var run WorkflowRun
	if a.db.Where("id=? AND project_id=?", c.Param("runId"), pid).First(&run).Error != nil {
		fail(c, 404, "not_found", "run not found")
		return
	}
	entries := []map[string]any{}
	f, err := os.Open(run.LogPath)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(200, entries)
			return
		}
		fail(c, 500, "log_unavailable", err.Error())
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(io.LimitReader(f, 16<<20))
	scanner.Buffer(make([]byte, 64*1024), 2<<20)
	nodeFilter := c.Query("node")
	for scanner.Scan() {
		var entry map[string]any
		if json.Unmarshal(scanner.Bytes(), &entry) == nil && (nodeFilter == "" || fmt.Sprint(entry["node"]) == nodeFilter) {
			entries = append(entries, entry)
		}
	}
	c.JSON(200, entries)
}
func (a *App) cancelRun(c *gin.Context) {
	pid, ok := parseID(c, "id")
	if !ok {
		return
	}
	if _, _, ok = a.projectAccess(c, pid, projectDevelop); !ok {
		return
	}
	a.db.Model(&WorkflowRun{}).Where("id=? AND project_id=?", c.Param("runId"), pid).Update("cancel_requested", true)
	c.Status(202)
}
func (a *App) streamLogs(c *gin.Context) {
	pid, ok := parseID(c, "id")
	if !ok {
		return
	}
	if _, _, ok = a.projectAccess(c, pid, projectView); !ok {
		return
	}
	var run WorkflowRun
	if a.db.Where("id=? AND project_id=?", c.Param("runId"), pid).First(&run).Error != nil {
		fail(c, 404, "not_found", "run not found")
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	offset, _ := strconv.ParseInt(c.GetHeader("Last-Event-ID"), 10, 64)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		f, e := os.Open(run.LogPath)
		if e == nil {
			f.Seek(offset, 0)
			buf := make([]byte, 32*1024)
			for {
				n, _ := f.Read(buf)
				if n == 0 {
					break
				}
				offset += int64(n)
				fmt.Fprintf(c.Writer, "id: %d\ndata: %s\n\n", offset, strings.ReplaceAll(string(buf[:n]), "\n", "\ndata: "))
				c.Writer.Flush()
			}
			f.Close()
		}
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprint(c.Writer, ": keepalive\n\n")
			c.Writer.Flush()
		}
	}
}
