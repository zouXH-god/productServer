package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type aiProviderInput struct {
	Name           string            `json:"name"`
	BaseURL        string            `json:"base_url"`
	Model          string            `json:"model"`
	APIKey         string            `json:"api_key"`
	Headers        map[string]string `json:"headers"`
	TimeoutSeconds int               `json:"timeout_seconds"`
	Enabled        bool              `json:"enabled"`
}
type aiCanvasInput struct {
	Content        string             `json:"content"`
	Canvas         WorkflowDefinition `json:"canvas"`
	CanvasRevision int64              `json:"canvas_revision"`
	WorkflowID     *uint              `json:"workflow_id"`
	WorkflowName   string             `json:"workflow_name"`
	TriggerType    string             `json:"trigger_type"`
	TriggerGlob    string             `json:"trigger_glob"`
}
type aiEvent struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

func (a *App) aiRoutes(api *gin.RouterGroup) {
	api.GET("/ai/providers", a.listAIProviders)
	api.POST("/ai/providers", a.saveAIProvider)
	api.PUT("/ai/providers/:providerId", a.saveAIProvider)
	api.DELETE("/ai/providers/:providerId", a.deleteAIProvider)
	api.POST("/ai/providers/:providerId/test", a.testAIProvider)
	api.GET("/projects/:id/ai/conversations", a.listAIConversations)
	api.POST("/projects/:id/ai/conversations", a.createAIConversation)
	api.GET("/projects/:id/ai/conversations/:conversationId", a.getAIConversation)
	api.PATCH("/projects/:id/ai/conversations/:conversationId", a.patchAIConversation)
	api.DELETE("/projects/:id/ai/conversations/:conversationId", a.deleteAIConversation)
	api.POST("/projects/:id/ai/conversations/:conversationId/messages", a.sendAIMessage)
	api.POST("/projects/:id/ai/conversations/:conversationId/stop", a.stopAIConversation)
	api.GET("/projects/:id/ai/conversations/:conversationId/stream", a.streamAIConversation)
}
func providerView(p AIProvider) gin.H {
	return gin.H{"id": p.ID, "name": p.Name, "base_url": p.BaseURL, "model": p.Model, "timeout_seconds": p.TimeoutSeconds, "enabled": p.Enabled, "api_key_set": p.APIKeyEncrypted != "", "headers_set": p.HeadersEncrypted != "", "created_at": p.CreatedAt, "updated_at": p.UpdatedAt}
}
func (a *App) listAIProviders(c *gin.Context) {
	var x []AIProvider
	ownerID := c.MustGet("userID").(uint)
	if raw := c.Query("project_id"); raw != "" {
		var id uint64
		fmt.Sscan(raw, &id)
		project, _, ok := a.projectAccess(c, uint(id), projectDevelop)
		if !ok {
			return
		}
		ownerID = project.UserID
	}
	a.db.Where("user_id=?", ownerID).Order("id desc").Find(&x)
	out := make([]gin.H, len(x))
	for i := range x {
		out[i] = providerView(x[i])
	}
	c.JSON(200, out)
}
func normalizeAIURL(raw string) (string, error) {
	u, e := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("valid HTTP or HTTPS base URL required")
	}
	if !strings.HasSuffix(u.Path, "/chat/completions") {
		if !strings.HasSuffix(u.Path, "/v1") {
			u.Path = strings.TrimRight(u.Path, "/") + "/v1"
		}
		u.Path += "/chat/completions"
	}
	return u.String(), nil
}
func (a *App) saveAIProvider(c *gin.Context) {
	if a.cfg.SecretEncryptionKey == "" {
		fail(c, 400, "encryption_unavailable", "SECRET_ENCRYPTION_KEY is required")
		return
	}
	var in aiProviderInput
	if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Model) == "" {
		fail(c, 400, "invalid_provider", "name, base_url and model are required")
		return
	}
	endpoint, e := normalizeAIURL(in.BaseURL)
	if e != nil {
		fail(c, 400, "invalid_provider", e.Error())
		return
	}
	uid := c.MustGet("userID").(uint)
	var p AIProvider
	status := 201
	if id := c.Param("providerId"); id != "" {
		if a.db.Where("id=? AND user_id=?", id, uid).First(&p).Error != nil {
			fail(c, 404, "not_found", "provider not found")
			return
		}
		status = 200
	}
	p.UserID = uid
	p.Name = strings.TrimSpace(in.Name)
	p.BaseURL = endpoint
	p.Model = strings.TrimSpace(in.Model)
	p.Enabled = in.Enabled
	p.TimeoutSeconds = in.TimeoutSeconds
	if p.TimeoutSeconds <= 0 {
		p.TimeoutSeconds = 120
	}
	if in.APIKey != "" {
		p.APIKeyEncrypted, e = encryptSecret(a.cfg.SecretEncryptionKey, in.APIKey)
		if e != nil {
			fail(c, 400, "encryption_unavailable", e.Error())
			return
		}
	}
	if in.Headers != nil {
		raw, _ := json.Marshal(in.Headers)
		p.HeadersEncrypted, e = encryptSecret(a.cfg.SecretEncryptionKey, string(raw))
		if e != nil {
			fail(c, 400, "encryption_unavailable", e.Error())
			return
		}
	}
	if p.APIKeyEncrypted == "" {
		fail(c, 400, "api_key_required", "API key is required")
		return
	}
	if status == 201 {
		e = a.db.Create(&p).Error
	} else {
		e = a.db.Save(&p).Error
	}
	if e != nil {
		fail(c, 409, "provider_exists", e.Error())
		return
	}
	c.JSON(status, providerView(p))
}
func (a *App) deleteAIProvider(c *gin.Context) {
	uid := c.MustGet("userID")
	var count int64
	a.db.Model(&AIConversation{}).Where("provider_id=? AND user_id=?", c.Param("providerId"), uid).Count(&count)
	if count > 0 {
		fail(c, 409, "provider_in_use", "provider is used by conversations")
		return
	}
	a.db.Where("id=? AND user_id=?", c.Param("providerId"), uid).Delete(&AIProvider{})
	c.Status(204)
}
func (a *App) ownedAIProvider(uid uint, id any) (AIProvider, error) {
	var p AIProvider
	e := a.db.Where("id=? AND user_id=?", id, uid).First(&p).Error
	return p, e
}
func (a *App) testAIProvider(c *gin.Context) {
	uid := c.MustGet("userID").(uint)
	p, e := a.ownedAIProvider(uid, c.Param("providerId"))
	if e != nil {
		fail(c, 404, "not_found", "provider not found")
		return
	}
	_, e = a.callAI(c.Request.Context(), p, []map[string]any{{"role": "user", "content": "Reply with OK."}}, nil)
	if e != nil {
		fail(c, 502, "provider_error", e.Error())
		return
	}
	c.JSON(200, gin.H{"status": "ok"})
}

func (a *App) ownedConversation(c *gin.Context) (AIConversation, bool) {
	pid, ok := parseID(c, "id")
	if !ok {
		return AIConversation{}, false
	}
	project, _, ok := a.projectAccess(c, pid, projectDevelop)
	if !ok {
		return AIConversation{}, false
	}
	var x AIConversation
	if a.db.Where("id=? AND project_id=? AND user_id=?", c.Param("conversationId"), pid, project.UserID).First(&x).Error != nil {
		fail(c, 404, "not_found", "conversation not found")
		return x, false
	}
	return x, true
}
func (a *App) listAIConversations(c *gin.Context) {
	pid, ok := parseID(c, "id")
	if !ok {
		return
	}
	project, _, ok := a.projectAccess(c, pid, projectDevelop)
	if !ok {
		return
	}
	q := a.db.Where("project_id=? AND user_id=?", pid, project.UserID)
	if wid := c.Query("workflow_id"); wid != "" {
		q = q.Where("workflow_id=?", wid)
	}
	var x []AIConversation
	q.Order("last_active_at desc").Find(&x)
	c.JSON(200, x)
}
func (a *App) createAIConversation(c *gin.Context) {
	pid, ok := parseID(c, "id")
	if !ok {
		return
	}
	project, _, ok := a.projectAccess(c, pid, projectDevelop)
	if !ok {
		return
	}
	var in struct {
		Title      string             `json:"title"`
		ProviderID uint               `json:"provider_id"`
		WorkflowID *uint              `json:"workflow_id"`
		Canvas     WorkflowDefinition `json:"canvas"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "invalid_request", "invalid conversation")
		return
	}
	if _, e := a.ownedAIProvider(project.UserID, in.ProviderID); e != nil {
		fail(c, 400, "invalid_provider", "provider not found")
		return
	}
	if in.Title == "" {
		in.Title = "新会话"
	}
	raw, _ := json.Marshal(sanitizeAICanvas(in.Canvas))
	x := AIConversation{UserID: project.UserID, ProjectID: pid, WorkflowID: in.WorkflowID, ProviderID: in.ProviderID, Title: in.Title, CanvasSnapshot: string(raw), LastActiveAt: time.Now()}
	a.db.Create(&x)
	c.JSON(201, x)
}
func (a *App) getAIConversation(c *gin.Context) {
	x, ok := a.ownedConversation(c)
	if !ok {
		return
	}
	a.db.Order("id").Model(&x).Association("Messages").Find(&x.Messages)
	c.JSON(200, x)
}
func (a *App) patchAIConversation(c *gin.Context) {
	x, ok := a.ownedConversation(c)
	if !ok {
		return
	}
	var in struct {
		Title      *string `json:"title"`
		WorkflowID *uint   `json:"workflow_id"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "invalid_request", "invalid update")
		return
	}
	updates := map[string]any{}
	if in.Title != nil {
		updates["title"] = strings.TrimSpace(*in.Title)
	}
	if in.WorkflowID != nil {
		updates["workflow_id"] = *in.WorkflowID
	}
	a.db.Model(&x).Updates(updates)
	a.db.First(&x, x.ID)
	c.JSON(200, x)
}
func (a *App) deleteAIConversation(c *gin.Context) {
	x, ok := a.ownedConversation(c)
	if !ok {
		return
	}
	if x.Generating {
		fail(c, 409, "conversation_busy", "conversation is generating")
		return
	}
	a.db.Where("conversation_id=?", x.ID).Delete(&AIMessage{})
	a.db.Delete(&x)
	c.Status(204)
}

func (a *App) emitAI(id uint, event aiEvent) {
	raw, _ := json.Marshal(event)
	a.aiMu.Lock()
	subs := append([]chan []byte{}, a.aiStreams[id]...)
	a.aiMu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- raw:
		default:
		}
	}
}
func (a *App) streamAIConversation(c *gin.Context) {
	x, ok := a.ownedConversation(c)
	if !ok {
		return
	}
	ch := make(chan []byte, 64)
	a.aiMu.Lock()
	a.aiStreams[x.ID] = append(a.aiStreams[x.ID], ch)
	a.aiMu.Unlock()
	defer func() {
		a.aiMu.Lock()
		list := a.aiStreams[x.ID]
		for i, v := range list {
			if v == ch {
				a.aiStreams[x.ID] = append(list[:i], list[i+1:]...)
				break
			}
		}
		a.aiMu.Unlock()
	}()
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Stream(func(w io.Writer) bool {
		select {
		case raw := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", raw)
			return true
		case <-time.After(15 * time.Second):
			fmt.Fprint(w, ": keepalive\n\n")
			return true
		case <-c.Request.Context().Done():
			return false
		}
	})
}
func (a *App) sendAIMessage(c *gin.Context) {
	x, ok := a.ownedConversation(c)
	if !ok {
		return
	}
	var in aiCanvasInput
	if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Content) == "" {
		fail(c, 400, "invalid_message", "content and canvas required")
		return
	}
	if x.Generating {
		fail(c, 409, "conversation_busy", "conversation is generating")
		return
	}
	if x.CanvasRevision != in.CanvasRevision {
		fail(c, 409, "canvas_conflict", "canvas revision is stale")
		return
	}
	if a.cfg.SecretEncryptionKey == "" {
		fail(c, 400, "encryption_unavailable", "SECRET_ENCRYPTION_KEY is required")
		return
	}
	raw, _ := json.Marshal(sanitizeAICanvas(in.Canvas))
	a.db.Model(&x).Updates(map[string]any{"generating": true, "canvas_snapshot": string(raw), "workflow_id": in.WorkflowID, "last_active_at": time.Now()})
	msg := AIMessage{ConversationID: x.ID, Role: "user", Content: in.Content, Status: "complete"}
	a.db.Create(&msg)
	go a.runAIConversation(x, in)
	c.JSON(202, gin.H{"message_id": msg.ID})
}
func (a *App) stopAIConversation(c *gin.Context) {
	x, ok := a.ownedConversation(c)
	if !ok {
		return
	}
	a.aiMu.Lock()
	cancel := a.aiCancels[x.ID]
	a.aiMu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.db.Model(&x).Update("generating", false)
	c.JSON(200, gin.H{"status": "stopped"})
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
}

func (a *App) callAI(ctx context.Context, p AIProvider, messages []map[string]any, tools []map[string]any) (chatResponse, error) {
	if !p.Enabled {
		return chatResponse{}, fmt.Errorf("AI provider is disabled")
	}
	key, e := decryptSecret(a.cfg.SecretEncryptionKey, p.APIKeyEncrypted)
	if e != nil {
		return chatResponse{}, e
	}
	body, _ := json.Marshal(map[string]any{"model": p.Model, "messages": messages, "tools": tools, "tool_choice": "auto"})
	timeout := a.cfg.AIRequestTimeout
	if p.TimeoutSeconds > 0 {
		timeout = time.Duration(p.TimeoutSeconds) * time.Second
	}
	client := &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, e := http.NewRequestWithContext(ctx, "POST", p.BaseURL, bytes.NewReader(body))
	if e != nil {
		return chatResponse{}, e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	if p.HeadersEncrypted != "" {
		plain, _ := decryptSecret(a.cfg.SecretEncryptionKey, p.HeadersEncrypted)
		var h map[string]string
		_ = json.Unmarshal([]byte(plain), &h)
		for k, v := range h {
			req.Header.Set(k, v)
		}
	}
	resp, e := client.Do(req)
	if e != nil {
		return chatResponse{}, e
	}
	defer resp.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(resp.Body, a.cfg.AIMaxResponseBytes+1))
	if e != nil {
		return chatResponse{}, e
	}
	if int64(len(raw)) > a.cfg.AIMaxResponseBytes {
		return chatResponse{}, fmt.Errorf("AI response exceeds limit")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return chatResponse{}, fmt.Errorf("AI provider returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out chatResponse
	e = json.Unmarshal(raw, &out)
	if e != nil || len(out.Choices) == 0 {
		return out, fmt.Errorf("invalid AI response")
	}
	return out, nil
}

func aiTools() []map[string]any {
	names := []string{"get_canvas", "get_module_catalog", "validate_canvas", "list_ssh_connections", "list_releases", "list_release_files", "list_environment_variables", "add_node", "update_node", "delete_node", "add_edge", "update_edge", "delete_edge", "auto_layout"}
	descriptions := map[string]string{"add_node": "Add node: node={id,type,config,timeout_seconds,retries,position:{x,y}}.", "update_node": "Replace node: id,node.", "delete_node": "Delete node and connected edges: id.", "add_edge": "Add edge: edge={from,to,condition}.", "update_edge": "Update edge: from,to,condition.", "delete_edge": "Delete edge: from,to.", "list_release_files": "List files: release_id.", "auto_layout": "Apply deterministic DAG layout."}
	out := make([]map[string]any, 0, len(names))
	for _, n := range names {
		desc := descriptions[n]
		if desc == "" {
			desc = "Read Product Server workflow context: " + n
		}
		out = append(out, map[string]any{"type": "function", "function": map[string]any{"name": n, "description": desc, "parameters": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": true}}})
	}
	return out
}
func moduleCatalog() map[string]any {
	return map[string]any{
		"archive":            map[string]any{"name": "归档压缩", "config": map[string]any{"file_pattern": "正则，必填", "output": "工作区相对输出路径，必填", "format": "zip|tar.gz"}},
		"extract":            map[string]any{"name": "本地解压文件", "config": map[string]any{"file_pattern": "选择一个本地产物的正则，必填", "output": "工作区相对目录，必填"}},
		"sftp_upload":        map[string]any{"name": "SFTP 上传文件或目录", "config": map[string]any{"connection_id": "服务器连接 ID，必填", "file_pattern": "本地文件正则，必填", "destination": "远端目录，必填"}},
		"sftp_extract":       map[string]any{"name": "上传 Action 唯一压缩包并在远端解压", "usage": "需要上传发布中的唯一 Action 压缩包时必须优先使用本模块，不要组合 sftp_upload 和 extract", "config": map[string]any{"connection_id": "服务器连接 ID，必填", "destination": "远端解压目录，必填", "permission": "3或4位八进制权限，默认0755", "keep_archive": "是否保留远端压缩包"}},
		"checksum_verify":    map[string]any{"name": "摘要校验", "config": map[string]any{"file_pattern": "文件正则，必填", "algorithm": "sha256|sha512", "expected": "期望摘要，必填"}},
		"foreach":            map[string]any{"name": "列表循环", "config": map[string]any{"items_from": "steps.<id>.outputs.items", "mode": "parallel|sequential", "concurrency": "1-100", "end_node_id": "配对 loop_end ID"}},
		"loop_end":           map[string]any{"name": "循环结束", "config": map[string]any{}},
		"server_list":        map[string]any{"name": "服务器列表", "outputs": []string{"servers", "count"}, "config": map[string]any{"connection_ids": "服务器连接 ID 数组，至少一项", "mode": "parallel|sequential", "concurrency": "并发上限", "end_node_id": "配对 server_list_end ID"}, "templates": []string{"server.id", "server.index", "server.name", "server.host", "server.port", "server.username", "server.auth_type"}},
		"server_list_end":    map[string]any{"name": "服务器列表结束", "outputs": []string{"items", "count"}, "config": map[string]any{}},
		"remote_file_exists": map[string]any{"name": "远端文件存在", "outputs": []string{"matched", "exists", "path"}, "config": map[string]any{"connection_id": "服务器连接 ID，必填", "path": "远端绝对路径，必填"}},
		"value_match":        map[string]any{"name": "值匹配", "config": map[string]any{"actual": "待判断模板值", "operator": "exists|equals|not_equals|contains|regex", "expected": "期望值", "ignore_case": "布尔值"}},
		"string_split":       map[string]any{"name": "字符串分割", "outputs": []string{"items", "count"}, "config": map[string]any{"value": "输入字符串或模板", "separator": "分隔符", "regex": "是否正则", "trim": "去空格", "drop_empty": "忽略空项"}},
		"ssh_command":        map[string]any{"name": "SSH 命令", "outputs": []string{"stdout", "stderr", "exit_code"}, "config": map[string]any{"connection_id": "服务器连接 ID，必填", "commands": "字符串数组，必填；每项一条命令", "work_dir": "可选远端工作目录", "sensitive_output": "布尔值"}},
		"http_webhook":       map[string]any{"name": "HTTP 回调", "outputs": []string{"status_code", "body", "headers"}, "config": map[string]any{"method": "HTTP 方法", "url": "URL，必填", "body": "请求正文", "sensitive_output": "布尔值"}},
	}
}

func normalizeAINode(n *WorkflowNode) {
	if n.Config == nil {
		n.Config = map[string]any{}
	}
	c := n.Config
	if n.Type == "sftp_upload" {
		if v, ok := c["remote_dir"]; ok {
			c["destination"] = v
			delete(c, "remote_dir")
		}
		if v, ok := c["local_path"]; ok {
			c["file_pattern"] = v
			delete(c, "local_path")
		}
	}
	if n.Type == "extract" {
		if v, ok := c["target_dir"]; ok {
			c["output"] = v
			delete(c, "target_dir")
		}
		if v, ok := c["archive_dir"]; ok {
			c["file_pattern"] = v
			delete(c, "archive_dir")
		}
	}
	if n.Type == "ssh_command" {
		if v, ok := c["command"].(string); ok {
			c["commands"] = []any{v}
			delete(c, "command")
		}
		if v, ok := c["workdir"]; ok {
			c["work_dir"] = v
			delete(c, "workdir")
		}
	}
	if n.Type == "sftp_extract" {
		if _, ok := c["permission"]; !ok {
			c["permission"] = "0755"
		}
		if _, ok := c["keep_archive"]; !ok {
			c["keep_archive"] = false
		}
	}
}
func validateAINode(n WorkflowNode) error {
	if _, ok := moduleCatalog()[n.Type]; !ok {
		return fmt.Errorf("unknown module %s", n.Type)
	}
	required := map[string][]string{"archive": {"file_pattern", "output", "format"}, "extract": {"file_pattern", "output"}, "sftp_upload": {"connection_id", "file_pattern", "destination"}, "sftp_extract": {"connection_id", "destination"}, "checksum_verify": {"file_pattern", "algorithm", "expected"}, "remote_file_exists": {"connection_id", "path"}, "ssh_command": {"connection_id", "commands"}, "http_webhook": {"url"}, "string_split": {"value"}, "foreach": {"items_from", "end_node_id"}, "server_list": {"connection_ids", "end_node_id"}}
	for _, key := range required[n.Type] {
		v, ok := n.Config[key]
		if !ok || v == nil || fmt.Sprint(v) == "" {
			return fmt.Errorf("%s requires config.%s", n.Type, key)
		}
	}
	if n.Type == "ssh_command" {
		if _, ok := n.Config["commands"].([]any); !ok {
			return fmt.Errorf("ssh_command config.commands must be an array")
		}
	}
	if n.Type == "server_list" {
		if ids, ok := uintList(n.Config["connection_ids"]); !ok || len(ids) == 0 {
			return fmt.Errorf("server_list config.connection_ids must be a non-empty array")
		}
	}
	return nil
}

func normalizeWorkflowDefinition(d *WorkflowDefinition) bool {
	changed := false
	for i := range d.Nodes {
		before, _ := json.Marshal(d.Nodes[i])
		normalizeAINode(&d.Nodes[i])
		after, _ := json.Marshal(d.Nodes[i])
		if !bytes.Equal(before, after) {
			changed = true
		}
	}
	remove := map[string]bool{}
	for i := range d.Nodes {
		n := &d.Nodes[i]
		if n.Type != "sftp_upload" {
			continue
		}
		pattern := fmt.Sprint(n.Config["file_pattern"])
		if pattern != "${release.artifact}" {
			continue
		}
		n.Type = "sftp_extract"
		n.Config = map[string]any{"connection_id": n.Config["connection_id"], "destination": n.Config["destination"], "permission": "0755", "keep_archive": false}
		changed = true
		for _, e := range d.Edges {
			if e.From != n.ID {
				continue
			}
			for _, candidate := range d.Nodes {
				if candidate.ID == e.To && candidate.Type == "extract" {
					remove[candidate.ID] = true
				}
			}
		}
	}
	if len(remove) > 0 {
		nn := d.Nodes[:0]
		for _, n := range d.Nodes {
			if !remove[n.ID] {
				nn = append(nn, n)
			}
		}
		d.Nodes = nn
		ee := []WorkflowEdge{}
		for _, e := range d.Edges {
			if remove[e.To] {
				for _, out := range d.Edges {
					if out.From == e.To {
						ee = append(ee, WorkflowEdge{From: e.From, To: out.To, Condition: out.Condition})
					}
				}
				continue
			}
			if remove[e.From] {
				continue
			}
			ee = append(ee, e)
		}
		d.Edges = ee
		changed = true
	}
	return changed
}

func normalizeStoredWorkflowCanvases(db *gorm.DB) error {
	var workflows []Workflow
	if err := db.Find(&workflows).Error; err != nil {
		return err
	}
	for _, w := range workflows {
		var d WorkflowDefinition
		if json.Unmarshal([]byte(w.Definition), &d) == nil && normalizeWorkflowDefinition(&d) {
			raw, _ := json.Marshal(d)
			if err := db.Model(&w).Update("definition", string(raw)).Error; err != nil {
				return err
			}
		}
	}
	var conversations []AIConversation
	if err := db.Find(&conversations).Error; err != nil {
		return err
	}
	for _, c := range conversations {
		var d WorkflowDefinition
		if json.Unmarshal([]byte(c.CanvasSnapshot), &d) == nil && normalizeWorkflowDefinition(&d) {
			raw, _ := json.Marshal(d)
			if err := db.Model(&c).Update("canvas_snapshot", string(raw)).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
func sanitizeAICanvas(source WorkflowDefinition) WorkflowDefinition {
	raw, _ := json.Marshal(source)
	var safe WorkflowDefinition
	_ = json.Unmarshal(raw, &safe)
	for i := range safe.Nodes {
		if safe.Nodes[i].Type == "http_webhook" {
			delete(safe.Nodes[i].Config, "secret")
		}
	}
	return safe
}
func (a *App) toolContext(uid, pid uint, name string, args map[string]any, canvas *WorkflowDefinition) (any, []map[string]any, error) {
	ops := []map[string]any{}
	switch name {
	case "get_canvas":
		return canvas, ops, nil
	case "get_module_catalog":
		return moduleCatalog(), ops, nil
	case "validate_canvas":
		if !validateDefinition(*canvas) {
			return map[string]any{"valid": false, "error": "画布结构、范围或 DAG 无效"}, ops, nil
		}
		if err := a.validateWorkflowConnections(uid, *canvas); err != nil {
			return map[string]any{"valid": false, "error": err.Error()}, ops, nil
		}
		return map[string]any{"valid": true}, ops, nil
	case "list_ssh_connections":
		var x []SSHConnection
		a.db.Select("id,name,host,port,username,auth_type").Where("user_id=?", uid).Find(&x)
		return x, ops, nil
	case "list_releases":
		var x []Release
		a.db.Select("id,project_id,version,commit_sha,branch,ref_type,created_at").Where("project_id=?", pid).Order("id desc").Limit(30).Find(&x)
		return x, ops, nil
	case "list_release_files":
		var x []ArtifactFile
		var release Release
		if a.db.Where("id=? AND project_id=?", uintArg(args, "release_id"), pid).First(&release).Error != nil {
			return nil, nil, fmt.Errorf("release not found")
		}
		a.db.Where("release_id=?", release.ID).Find(&x)
		return x, ops, nil
	case "list_environment_variables":
		var x []EnvironmentVariable
		a.db.Select("id,project_id,name,sensitive,updated_at").Where("user_id=? AND project_id IN ?", uid, []uint{0, pid}).Find(&x)
		return x, ops, nil
	}
	switch name {
	case "add_node":
		var n WorkflowNode
		if json.Unmarshal(rawField(args, "node"), &n) != nil || n.ID == "" {
			return nil, nil, fmt.Errorf("valid node required")
		}
		for _, v := range canvas.Nodes {
			if v.ID == n.ID {
				return nil, nil, fmt.Errorf("node exists")
			}
		}
		normalizeAINode(&n)
		if err := validateAINode(n); err != nil {
			return nil, nil, err
		}
		if n.Type == "server_list" {
			if err := a.validateWorkflowConnections(uid, WorkflowDefinition{Nodes: []WorkflowNode{n}}); err != nil {
				return nil, nil, err
			}
		}
		canvas.Nodes = append(canvas.Nodes, n)
		ops = append(ops, map[string]any{"op": "add_node", "node": n})
	case "update_node":
		id := strAny(args, "id")
		for i := range canvas.Nodes {
			if canvas.Nodes[i].ID == id {
				var n WorkflowNode
				if json.Unmarshal(rawField(args, "node"), &n) != nil {
					return nil, nil, fmt.Errorf("node required")
				}
				n.ID = id
				normalizeAINode(&n)
				if err := validateAINode(n); err != nil {
					return nil, nil, err
				}
				if n.Type == "server_list" {
					if err := a.validateWorkflowConnections(uid, WorkflowDefinition{Nodes: []WorkflowNode{n}}); err != nil {
						return nil, nil, err
					}
				}
				canvas.Nodes[i] = n
				ops = append(ops, map[string]any{"op": "update_node", "node": n})
				return map[string]any{"ok": true}, ops, nil
			}
		}
		return nil, nil, fmt.Errorf("node not found")
	case "delete_node":
		id := strAny(args, "id")
		remove := map[string]bool{id: true}
		for _, n := range canvas.Nodes {
			if n.ID == id && (n.Type == "foreach" || n.Type == "server_list") {
				if end, ok := n.Config["end_node_id"].(string); ok {
					remove[end] = true
				}
			}
			if (n.Type == "loop_end" || n.Type == "server_list_end") && n.ID == id {
				if start, ok := n.Config["start_node_id"].(string); ok {
					remove[start] = true
				}
			}
		}
		nn := canvas.Nodes[:0]
		for _, n := range canvas.Nodes {
			if !remove[n.ID] {
				nn = append(nn, n)
			}
		}
		canvas.Nodes = nn
		ee := canvas.Edges[:0]
		for _, e := range canvas.Edges {
			if !remove[e.From] && !remove[e.To] {
				ee = append(ee, e)
			}
		}
		canvas.Edges = ee
		ops = append(ops, map[string]any{"op": "delete_node", "id": id})
	case "add_edge":
		var e WorkflowEdge
		if json.Unmarshal(rawField(args, "edge"), &e) != nil {
			return nil, nil, fmt.Errorf("edge required")
		}
		canvas.Edges = append(canvas.Edges, e)
		if !validateDefinition(*canvas) {
			canvas.Edges = canvas.Edges[:len(canvas.Edges)-1]
			return nil, nil, fmt.Errorf("edge makes canvas invalid")
		}
		ops = append(ops, map[string]any{"op": "add_edge", "edge": e})
	case "delete_edge":
		from, to := strAny(args, "from"), strAny(args, "to")
		ee := canvas.Edges[:0]
		for _, e := range canvas.Edges {
			if e.From != from || e.To != to {
				ee = append(ee, e)
			}
		}
		canvas.Edges = ee
		ops = append(ops, map[string]any{"op": "delete_edge", "from": from, "to": to})
	case "update_edge":
		from, to := strAny(args, "from"), strAny(args, "to")
		condition := strAny(args, "condition")
		for i := range canvas.Edges {
			if canvas.Edges[i].From == from && canvas.Edges[i].To == to {
				canvas.Edges[i].Condition = condition
			}
		}
		if !validateDefinition(*canvas) {
			return nil, nil, fmt.Errorf("invalid edge")
		}
		ops = append(ops, map[string]any{"op": "update_edge", "from": from, "to": to, "condition": condition})
	case "auto_layout":
		layoutCanvas(canvas)
		ops = append(ops, map[string]any{"op": "replace_canvas", "canvas": canvas})
	default:
		return nil, nil, fmt.Errorf("unknown tool")
	}
	return map[string]any{"ok": true}, ops, nil
}
func rawField(m map[string]any, k string) []byte { b, _ := json.Marshal(m[k]); return b }
func strAny(m map[string]any, k string) string   { v, _ := m[k].(string); return v }
func uintArg(m map[string]any, k string) uint {
	switch v := m[k].(type) {
	case float64:
		return uint(v)
	case int:
		return uint(v)
	}
	return 0
}
func layoutCanvas(d *WorkflowDefinition) {
	level := map[string]int{}
	for pass, changed := 0, true; changed && pass <= len(d.Nodes); pass++ {
		changed = false
		for _, e := range d.Edges {
			if level[e.To] <= level[e.From] {
				level[e.To] = level[e.From] + 1
				changed = true
			}
		}
	}
	groups := map[int][]int{}
	for i, n := range d.Nodes {
		groups[level[n.ID]] = append(groups[level[n.ID]], i)
	}
	for l, ids := range groups {
		sort.Slice(ids, func(i, j int) bool { return d.Nodes[ids[i]].ID < d.Nodes[ids[j]].ID })
		for row, index := range ids {
			d.Nodes[index].Position = map[string]float64{"x": float64(100 + l*280), "y": float64(80 + row*130)}
		}
	}
}

func (a *App) runAIConversation(conv AIConversation, in aiCanvasInput) {
	defer a.db.Model(&AIConversation{}).Where("id=?", conv.ID).Update("generating", false)
	p, e := a.ownedAIProvider(conv.UserID, conv.ProviderID)
	if e != nil {
		a.emitAI(conv.ID, aiEvent{"error", e.Error()})
		return
	}
	var history []AIMessage
	a.db.Where("conversation_id=?", conv.ID).Order("id").Find(&history)
	messages := []map[string]any{{"role": "system", "content": "你是 Product Server 工作流画布助手。必须使用工具读取和修改画布。只能编辑草稿，不能保存或运行。服务器与变量均为脱敏元数据。完成后用中文简述修改。工作流：" + in.WorkflowName + "，触发：" + in.TriggerType + " " + in.TriggerGlob}}
	total := 0
	for i := len(history) - 1; i >= 0; i-- {
		total += len(history[i].Content)
		if total > a.cfg.AIMaxContextChars {
			break
		}
		messages = append(messages, map[string]any{"role": history[i].Role, "content": history[i].Content})
	}
	if len(messages) > 2 {
		for i, j := 1, len(messages)-1; i < j; i, j = i+1, j-1 {
			messages[i], messages[j] = messages[j], messages[i]
		}
	}
	canvas := in.Canvas
	canvasSecrets := map[string]any{}
	for i := range canvas.Nodes {
		if canvas.Nodes[i].Type == "http_webhook" {
			if secret, ok := canvas.Nodes[i].Config["secret"]; ok && fmt.Sprint(secret) != "" {
				canvasSecrets[canvas.Nodes[i].ID] = secret
			}
			delete(canvas.Nodes[i].Config, "secret")
		}
	}
	allOps := []map[string]any{}
	final := ""
	ctx, cancel := context.WithTimeout(context.Background(), a.cfg.AIRequestTimeout)
	a.aiMu.Lock()
	a.aiCancels[conv.ID] = cancel
	a.aiMu.Unlock()
	defer func() { a.aiMu.Lock(); delete(a.aiCancels, conv.ID); a.aiMu.Unlock() }()
	defer cancel()
	for round := 0; round < a.cfg.AIMaxToolRounds; round++ {
		resp, err := a.callAI(ctx, p, messages, aiTools())
		if err != nil {
			a.emitAI(conv.ID, aiEvent{"error", err.Error()})
			a.db.Create(&AIMessage{ConversationID: conv.ID, Role: "assistant", Status: "error", ErrorSummary: err.Error()})
			return
		}
		m := resp.Choices[0].Message
		if m.Content != "" {
			final += m.Content
			a.emitAI(conv.ID, aiEvent{"delta", m.Content})
		}
		if len(m.ToolCalls) == 0 {
			break
		}
		for i := range m.ToolCalls {
			if m.ToolCalls[i].ID == "" {
				m.ToolCalls[i].ID = fmt.Sprintf("call_%d_%d", round+1, i+1)
			}
			if m.ToolCalls[i].Type == "" {
				m.ToolCalls[i].Type = "function"
			}
		}
		assistant := map[string]any{"role": "assistant", "content": m.Content, "tool_calls": m.ToolCalls}
		messages = append(messages, assistant)
		for _, call := range m.ToolCalls {
			var args map[string]any
			if json.Unmarshal([]byte(call.Function.Arguments), &args) != nil {
				args = map[string]any{}
			}
			a.emitAI(conv.ID, aiEvent{"tool", map[string]any{"name": call.Function.Name, "status": "running"}})
			result, ops, err := a.toolContext(conv.UserID, conv.ProjectID, call.Function.Name, args, &canvas)
			payload := any(result)
			if err != nil {
				payload = map[string]any{"error": err.Error()}
			}
			b, _ := json.Marshal(payload)
			messages = append(messages, map[string]any{"role": "tool", "tool_call_id": call.ID, "content": string(b)})
			if len(ops) > 0 {
				allOps = append(allOps, ops...)
				a.emitAI(conv.ID, aiEvent{"operations", ops})
			}
			a.emitAI(conv.ID, aiEvent{"tool", map[string]any{"name": call.Function.Name, "status": "complete", "error": fmt.Sprint(func() any {
				if err != nil {
					return err.Error()
				}
				return ""
			}())}})
		}
	}
	revision := conv.CanvasRevision
	if len(allOps) > 0 {
		revision++
	}
	canvasRaw, _ := json.Marshal(canvas)
	for i := range canvas.Nodes {
		if secret, ok := canvasSecrets[canvas.Nodes[i].ID]; ok && canvas.Nodes[i].Type == "http_webhook" {
			canvas.Nodes[i].Config["secret"] = secret
		}
	}
	opsRaw, _ := json.Marshal(allOps)
	msg := AIMessage{ConversationID: conv.ID, Role: "assistant", Content: final, Operations: string(opsRaw), Status: "complete"}
	a.db.Create(&msg)
	a.db.Model(&AIConversation{}).Where("id=?", conv.ID).Updates(map[string]any{"canvas_snapshot": string(canvasRaw), "canvas_revision": revision, "last_active_at": time.Now()})
	a.emitAI(conv.ID, aiEvent{"done", map[string]any{"message": msg, "canvas": canvas, "canvas_revision": revision, "operations": allOps}})
}
