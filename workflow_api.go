package main

import (
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/ssh"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
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
	api.DELETE("/projects/:id/workflows/:workflowId", a.deleteWorkflow)
	api.POST("/projects/:id/workflows/:workflowId/runs", a.manualRun)
	api.GET("/projects/:id/runs", a.listRuns)
	api.GET("/projects/:id/runs/:runId", a.getRun)
	api.POST("/projects/:id/runs/:runId/cancel", a.cancelRun)
	api.GET("/projects/:id/runs/:runId/logs/stream", a.streamLogs)
}
func (a *App) listCredentials(c *gin.Context) {
	var x []SSHCredential
	a.db.Where("user_id=?", c.MustGet("userID")).Find(&x)
	c.JSON(200, x)
}
func (a *App) createCredential(c *gin.Context) {
	var in struct{ Name, PrivateKey, Passphrase string }
	if c.ShouldBindJSON(&in) != nil || in.Name == "" || in.PrivateKey == "" {
		fail(c, 400, "invalid_request", "name and private_key required")
		return
	}
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
	a.db.Where("user_id=?", c.MustGet("userID")).Find(&x)
	c.JSON(200, x)
}
func bindConnection(c *gin.Context, x *SSHConnection) error { return c.ShouldBindJSON(x) }
func (a *App) createConnection(c *gin.Context) {
	x := SSHConnection{UserID: c.MustGet("userID").(uint), Port: 22, TimeoutSeconds: 10}
	if bindConnection(c, &x) != nil || x.Name == "" || x.Host == "" || x.Username == "" {
		fail(c, 400, "invalid_request", "name, host and username required")
		return
	}
	if !a.ownsCredential(x.CredentialID, x.UserID) {
		fail(c, 400, "invalid_credential", "credential not found")
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
	uid := x.UserID
	if bindConnection(c, &x) != nil {
		return
	}
	x.ID = uint(id)
	x.UserID = uid
	if !a.ownsCredential(x.CredentialID, uid) {
		fail(c, 400, "invalid_credential", "credential not found")
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
	timeout := time.Duration(conn.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return ssh.Dial("tcp", net.JoinHostPort(conn.Host, strconv.Itoa(conn.Port)), &ssh.ClientConfig{User: conn.Username, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: timeout})
}

type workflowPayload struct {
	Name        string             `json:"name"`
	Enabled     bool               `json:"enabled"`
	TriggerType string             `json:"trigger_type"`
	TriggerGlob string             `json:"trigger_glob"`
	Definition  WorkflowDefinition `json:"definition"`
}

func (a *App) listWorkflows(c *gin.Context) {
	pid, ok := parseID(c, "id")
	if !ok {
		return
	}
	if _, ok = a.ownedProject(c, pid); !ok {
		return
	}
	var x []Workflow
	a.db.Where("project_id=?", pid).Find(&x)
	c.JSON(200, x)
}
func (a *App) saveWorkflow(c *gin.Context) {
	pid, ok := parseID(c, "id")
	if !ok {
		return
	}
	if _, ok = a.ownedProject(c, pid); !ok {
		return
	}
	var p workflowPayload
	if c.ShouldBindJSON(&p) != nil || p.Name == "" || !validateDefinition(p.Definition) {
		fail(c, 400, "invalid_workflow", "valid name and acyclic DAG required")
		return
	}
	if p.TriggerType != "any" && p.TriggerType != "tag" && p.TriggerType != "commit" && p.TriggerType != "tag_glob" {
		fail(c, 400, "invalid_trigger", "invalid trigger type")
		return
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
	a.db.Save(&w)
	redactWorkflowSecrets(&p.Definition)
	c.JSON(status, gin.H{"id": w.ID, "project_id": w.ProjectID, "name": w.Name, "enabled": w.Enabled, "trigger_type": w.TriggerType, "trigger_glob": w.TriggerGlob, "definition": p.Definition, "created_at": w.CreatedAt, "updated_at": w.UpdatedAt})
}
func (a *App) getWorkflow(c *gin.Context) {
	pid, ok := parseID(c, "id")
	if !ok {
		return
	}
	if _, ok = a.ownedProject(c, pid); !ok {
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
func redactWorkflowSecrets(d *WorkflowDefinition) {
	for i := range d.Nodes {
		if d.Nodes[i].Type == "http_webhook" && str(d.Nodes[i].Config, "secret") != "" {
			d.Nodes[i].Config["secret"] = ""
			d.Nodes[i].Config["secret_set"] = true
		}
	}
}
func (a *App) deleteWorkflow(c *gin.Context) {
	pid, ok := parseID(c, "id")
	if !ok {
		return
	}
	if _, ok = a.ownedProject(c, pid); !ok {
		return
	}
	a.db.Where("id=? AND project_id=?", c.Param("workflowId"), pid).Delete(&Workflow{})
	c.Status(204)
}
func (a *App) manualRun(c *gin.Context) {
	pid, ok := parseID(c, "id")
	if !ok {
		return
	}
	if _, ok = a.ownedProject(c, pid); !ok {
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
	if a.db.Where("id=? AND project_id=?", c.Param("workflowId"), pid).First(&w).Error != nil || a.db.Where("id=? AND project_id=?", in.ReleaseID, pid).First(&r).Error != nil {
		fail(c, 404, "not_found", "workflow or release not found")
		return
	}
	run, e := createRun(a.db, a.cfg, w, r)
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
	if _, ok = a.ownedProject(c, pid); !ok {
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
	if _, ok = a.ownedProject(c, pid); !ok {
		return
	}
	var x WorkflowRun
	if a.db.Preload("Nodes").Where("id=? AND project_id=?", c.Param("runId"), pid).First(&x).Error != nil {
		fail(c, 404, "not_found", "run not found")
		return
	}
	c.JSON(200, x)
}
func (a *App) cancelRun(c *gin.Context) {
	pid, ok := parseID(c, "id")
	if !ok {
		return
	}
	if _, ok = a.ownedProject(c, pid); !ok {
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
	if _, ok = a.ownedProject(c, pid); !ok {
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
