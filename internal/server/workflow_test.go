package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkflowDAGValidation(t *testing.T) {
	valid := WorkflowDefinition{Nodes: []WorkflowNode{{ID: "a", Name: "打包前端", Type: "archive"}, {ID: "b", Name: "验证摘要", Type: "checksum_verify"}}, Edges: []WorkflowEdge{{From: "a", To: "b"}}}
	if !validateDefinition(valid) {
		t.Fatal("valid DAG rejected")
	}
	valid.Nodes[0].Name = strings.Repeat("名", 81)
	if validateDefinition(valid) {
		t.Fatal("node name longer than 80 characters accepted")
	}
	valid.Nodes[0].Name = "打包前端"
	valid.Edges = append(valid.Edges, WorkflowEdge{From: "b", To: "a"})
	if validateDefinition(valid) {
		t.Fatal("cycle accepted")
	}
}

func TestScheduleTimezoneOverlapAndEmptyRun(t *testing.T) {
	next, err := nextSchedule("0 9 * * *", "Asia/Shanghai", time.Date(2026, 1, 1, 0, 30, 0, 0, time.UTC))
	if err != nil || next.Hour() != 1 {
		t.Fatalf("next=%v err=%v", next, err)
	}
	a, cfg := testApp(t)
	var user User
	a.db.First(&user)
	project := Project{UserID: user.ID, Name: "scheduled", Type: "scheduled"}
	a.db.Create(&project)
	a.db.Create(&ProjectMember{ProjectID: project.ID, UserID: user.ID, Role: "owner"})
	definition := WorkflowDefinition{Nodes: []WorkflowNode{{ID: "split", Type: "string_split", Config: map[string]any{"value": "{{trigger.type}}/{{trigger.scheduled_at}}", "separator": "/"}}}}
	raw, _ := json.Marshal(definition)
	workflow := Workflow{ProjectID: project.ID, Name: "timer", Enabled: true, TriggerType: "any", Definition: string(raw)}
	a.db.Create(&workflow)
	schedule := WorkflowSchedule{ProjectID: project.ID, WorkflowID: workflow.ID, Cron: "* * * * *", Timezone: "UTC", Enabled: true}
	a.db.Create(&schedule)
	a.db.Create(&WorkflowRun{ProjectID: project.ID, WorkflowID: workflow.ID, Status: "running"})
	event, run, err := a.fireSchedule(project, workflow, schedule, time.Now().UTC())
	if err != nil || event.Status != "skipped" || run != nil {
		t.Fatalf("event=%#v run=%#v err=%v", event, run, err)
	}
	schedule.AllowParallel = true
	event, run, err = a.fireSchedule(project, workflow, schedule, time.Now().UTC().Add(time.Second))
	if err != nil || event.Status != "created" || run == nil || run.ReleaseID != 0 || run.TriggerSource != "schedule" || !run.AllowParallel {
		t.Fatalf("event=%#v run=%#v err=%v", event, run, err)
	}
	_ = cfg
}
func TestSecretEncryption(t *testing.T) {
	key := "12345678901234567890123456789012"
	encrypted, e := encryptSecret(key, "private-value")
	if e != nil {
		t.Fatal(e)
	}
	if encrypted == "private-value" {
		t.Fatal("secret not encrypted")
	}
	plain, e := decryptSecret(key, encrypted)
	if e != nil || plain != "private-value" {
		t.Fatalf("decrypt=%q %v", plain, e)
	}
}
func TestTriggers(t *testing.T) {
	r := Release{Version: "v1.2.0", RefType: "tag"}
	if !triggerMatches(Workflow{TriggerType: "tag_glob", TriggerGlob: "v*"}, r) {
		t.Fatal("tag glob did not match")
	}
	if triggerMatches(Workflow{TriggerType: "commit"}, r) {
		t.Fatal("tag matched commit")
	}
	branchRelease := Release{Version: "abc123", RefType: "commit", Branch: "feature/deploy"}
	if !triggerMatches(Workflow{TriggerType: "branch_glob", TriggerGlob: "feature/*"}, branchRelease) {
		t.Fatal("branch glob did not match commit branch")
	}
	if triggerMatches(Workflow{TriggerType: "branch_glob", TriggerGlob: "feature/*"}, r) {
		t.Fatal("branch glob matched tag")
	}
}
func TestArchiveAndSafeExtract(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "input")
	work := filepath.Join(root, "work")
	os.MkdirAll(input, 0755)
	os.MkdirAll(work, 0755)
	os.WriteFile(filepath.Join(input, "a.txt"), []byte("hello"), 0644)
	if e := archiveModule(map[string]any{"input": "*.txt", "output": "bundle.zip", "format": "zip"}, input, work); e != nil {
		t.Fatal(e)
	}
	if e := extractModule(map[string]any{"source": "bundle.zip", "output": "out"}, input, work); e != nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile(filepath.Join(work, "out", "a.txt"))
	if e != nil || string(data) != "hello" {
		t.Fatalf("extract=%q %v", data, e)
	}
	if _, e := safePath(work, "../escape"); e == nil {
		t.Fatal("workspace escape accepted")
	}
}

func TestWorkspaceFileRegularExpression(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "input")
	work := filepath.Join(root, "work")
	os.MkdirAll(filepath.Join(input, "dist", "assets"), 0755)
	os.MkdirAll(work, 0755)
	os.WriteFile(filepath.Join(input, "dist", "index.html"), []byte("html"), 0644)
	os.WriteFile(filepath.Join(input, "dist", "assets", "app.js"), []byte("js"), 0644)
	os.WriteFile(filepath.Join(input, "README.txt"), []byte("text"), 0644)
	matches, err := matchWorkspaceFiles(input, work, `^dist/.*\.(html|js)$`)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 || matches[0].Name != "dist/assets/app.js" || matches[1].Name != "dist/index.html" {
		t.Fatalf("unexpected matches: %#v", matches)
	}
	if _, err = matchWorkspaceFiles(input, work, `[`); err == nil {
		t.Fatal("invalid regular expression accepted")
	}
}

func TestActionArchiveAndRemoteExtractCommand(t *testing.T) {
	release := Release{Files: []ArtifactFile{
		{OriginalName: "artifact.zip", Kind: "uploaded"},
		{OriginalName: "dist/index.html", Kind: "extracted"},
	}}
	archive, err := actionArchive(release)
	if err != nil || archive.OriginalName != "artifact.zip" {
		t.Fatalf("archive=%#v err=%v", archive, err)
	}
	command, err := remoteExtractCommand("/tmp/artifact.zip", "/opt/my app", "0750", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"unzip -oq", "'/opt/my app'", "'/tmp/artifact.zip.extracting'", "chmod -R 0750 '/tmp/artifact.zip.extracting'", "cp -a", "rm -f", "[阶段开始]", "[阶段成功]", "[阶段失败]", "解压产物", "设置文件权限", "复制到目标目录"} {
		if !strings.Contains(command, expected) {
			t.Fatalf("command missing %q: %s", expected, command)
		}
	}
	if strings.Contains(command, "chmod -R 0750 '/opt/my app'") {
		t.Fatalf("command recursively changes the existing destination: %s", command)
	}
	if _, err = remoteExtractCommand("/tmp/artifact.zip", "/opt/app", "9999", false); err == nil {
		t.Fatal("invalid permission accepted")
	}
	release.Files = append(release.Files, ArtifactFile{OriginalName: "other.tar.gz", Kind: "uploaded"})
	if _, err = actionArchive(release); err == nil {
		t.Fatal("multiple Action archives accepted")
	}
}

func TestReleaseWorkspaceSeparatesArchivesAndExtractedFiles(t *testing.T) {
	input, work := filepath.Join(t.TempDir(), "input"), filepath.Join(t.TempDir(), "work")
	archive := ArtifactFile{OriginalName: "artifact.zip", Kind: "uploaded"}
	extracted := ArtifactFile{OriginalName: "dist/index.html", Kind: "extracted"}
	archivePath, err := releaseWorkspacePath(input, archive)
	if err != nil {
		t.Fatal(err)
	}
	extractedPath, err := releaseWorkspacePath(input, extracted)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.ToSlash(archivePath) != filepath.ToSlash(filepath.Join(input, "archive", "artifact.zip")) {
		t.Fatalf("unexpected archive path: %s", archivePath)
	}
	if filepath.ToSlash(extractedPath) != filepath.ToSlash(filepath.Join(input, "files", "dist", "index.html")) {
		t.Fatalf("unexpected extracted path: %s", extractedPath)
	}
	for _, name := range []string{archivePath, extractedPath} {
		if err = os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(name, []byte("test"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	archives, err := matchWorkspaceFiles(input, work, `^archive/`)
	if err != nil || len(archives) != 1 || archives[0].Name != "archive/artifact.zip" {
		t.Fatalf("archive matches=%#v err=%v", archives, err)
	}
	files, err := matchWorkspaceFiles(input, work, `^files/`)
	if err != nil || len(files) != 1 || files[0].Name != "files/dist/index.html" {
		t.Fatalf("file matches=%#v err=%v", files, err)
	}
	legacy, err := matchWorkspaceFiles(input, work, `^dist/index\.html$`)
	if err != nil || len(legacy) != 1 || legacy[0].Name != "files/dist/index.html" {
		t.Fatalf("legacy matches=%#v err=%v", legacy, err)
	}
}

func TestRuntimeTemplatesAndConditionalEdges(t *testing.T) {
	matched := true
	values := runtimeValues{Env: environmentSnapshot{Global: map[string]string{"REGION": "global"}, Project: map[string]string{"REGION": "project"}}, Steps: map[string]map[string]any{"split": {"count": 2}}}
	rendered, err := renderRuntimeString("{{env.REGION}}/{{env.global.REGION}}/{{steps.split.outputs.count}}/{{loop.index}}/{{release.branch}}", values, Project{}, Release{Branch: "feature/deploy"}, "", "")
	if err != nil || rendered != "project/global/2/0/feature/deploy" {
		t.Fatalf("rendered=%q err=%v", rendered, err)
	}
	if !edgeActive(WorkflowEdge{Condition: "true"}, nodeResult{OK: true, Matched: &matched}) {
		t.Fatal("true edge inactive")
	}
	if edgeActive(WorkflowEdge{Condition: "false"}, nodeResult{OK: true, Matched: &matched}) {
		t.Fatal("false edge active")
	}
}

func TestServerListDefinitionAndTemplates(t *testing.T) {
	definition := WorkflowDefinition{Nodes: []WorkflowNode{
		{ID: "servers", Type: "server_list", Config: map[string]any{"connection_ids": []any{float64(1), float64(2)}, "mode": "parallel", "concurrency": float64(2), "end_node_id": "servers_end"}},
		{ID: "command", Type: "ssh_command", Config: map[string]any{"connection_id": float64(0), "commands": []any{"echo ok"}}},
		{ID: "servers_end", Type: "server_list_end", Config: map[string]any{"start_node_id": "servers"}},
	}, Edges: []WorkflowEdge{{From: "servers", To: "command"}, {From: "command", To: "servers_end"}}}
	if !validateDefinition(definition) {
		t.Fatal("valid server scope rejected")
	}
	values := runtimeValues{Server: &runtimeServer{ID: 2, Index: 1, Name: "prod", Host: "10.0.0.2", Port: 22, Username: "deploy", AuthType: "password"}}
	rendered, err := renderRuntimeString("{{server.index}}/{{server.name}}/{{server.username}}@{{server.host}}:{{server.port}}/{{server.auth_type}}", values, Project{}, Release{}, "", "")
	if err != nil || rendered != "1/prod/deploy@10.0.0.2:22/password" {
		t.Fatalf("rendered=%q err=%v", rendered, err)
	}
	definition.Nodes = append(definition.Nodes, WorkflowNode{ID: "nested", Type: "foreach", Config: map[string]any{"items_from": "steps.x.outputs.items", "end_node_id": "servers_end"}})
	definition.Edges = append(definition.Edges, WorkflowEdge{From: "servers", To: "nested"})
	if validateDefinition(definition) {
		t.Fatal("nested loop in server scope accepted")
	}
}

func TestServerListExecutesWholeScopePerServer(t *testing.T) {
	a, cfg := testApp(t)
	var user User
	a.db.First(&user)
	project := Project{Name: "server-list-project", UserID: user.ID}
	a.db.Create(&project)
	connections := []SSHConnection{{UserID: user.ID, Name: "one", Host: "host-one", Port: 22, Username: "deploy", AuthType: "password"}, {UserID: user.ID, Name: "two", Host: "host-two", Port: 2222, Username: "root", AuthType: "key"}}
	a.db.Create(&connections)
	definition := WorkflowDefinition{Nodes: []WorkflowNode{
		{ID: "servers", Type: "server_list", Config: map[string]any{"connection_ids": []any{float64(connections[0].ID), float64(connections[1].ID)}, "mode": "parallel", "concurrency": float64(2), "end_node_id": "servers_end"}},
		{ID: "derive", Type: "string_split", Config: map[string]any{"value": "{{server.name}}", "separator": ",", "trim": true, "drop_empty": true}},
		{ID: "servers_end", Type: "server_list_end", Config: map[string]any{"start_node_id": "servers"}},
	}, Edges: []WorkflowEdge{{From: "servers", To: "derive"}, {From: "derive", To: "servers_end"}}}
	run := WorkflowRun{ProjectID: project.ID, Status: "running"}
	a.db.Create(&run)
	a.db.Create(&WorkflowNodeRun{RunID: run.ID, NodeKey: "servers", Module: "server_list", Status: "pending"})
	a.db.Create(&WorkflowNodeRun{RunID: run.ID, NodeKey: "servers_end", Module: "server_list_end", Status: "pending"})
	root := t.TempDir()
	log := &runLogger{path: filepath.Join(root, "run.jsonl"), db: a.db, runID: run.ID}
	result := executeServerListNode(context.Background(), a.db, cfg, run, project, Release{}, definition, definition.Nodes[0], filepath.Join(root, "input"), filepath.Join(root, "work"), log, runtimeValues{Env: environmentSnapshot{}, Steps: map[string]map[string]any{}})
	if !result.OK {
		t.Fatalf("server list failed: %#v", result)
	}
	var runs []WorkflowNodeRun
	a.db.Where("run_id=? AND server_connection_id <> 0", run.ID).Order("server_index").Find(&runs)
	if len(runs) != 2 || runs[0].NodeKey != "derive[server:"+itoa(connections[0].ID)+"]" || runs[1].NodeKey != "derive[server:"+itoa(connections[1].ID)+"]" {
		t.Fatalf("node runs=%#v", runs)
	}
	var end WorkflowNodeRun
	a.db.Where("run_id=? AND node_key=?", run.ID, "servers_end").First(&end)
	var output map[string]any
	if json.Unmarshal([]byte(end.Outputs), &output) != nil || int(output["count"].(float64)) != 2 {
		t.Fatalf("end output=%s", end.Outputs)
	}
}

func TestEnvironmentSnapshotProjectOverrideAndEncryption(t *testing.T) {
	a, cfg := testApp(t)
	cfg.SecretEncryptionKey = "12345678901234567890123456789012"
	var user User
	a.db.First(&user)
	project := Project{Name: "env-project", UserID: user.ID}
	if err := a.db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	secret, err := encryptSecret(cfg.SecretEncryptionKey, "secret-value")
	if err != nil {
		t.Fatal(err)
	}
	values := []EnvironmentVariable{{UserID: user.ID, Name: "REGION", Value: "global"}, {UserID: user.ID, ProjectID: project.ID, Name: "REGION", Value: "project"}, {UserID: user.ID, Name: "TOKEN", Sensitive: true, ValueEncrypted: secret}}
	if err = a.db.Create(&values).Error; err != nil {
		t.Fatal(err)
	}
	raw, err := buildEnvironmentSnapshot(a.db, cfg, user.ID, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, "enc:") || strings.Contains(raw, "secret-value") {
		t.Fatal("snapshot was not encrypted")
	}
	snapshot, err := decodeEnvironmentSnapshot(cfg, raw)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Project["REGION"] != "project" || snapshot.Global["TOKEN"] != "secret-value" {
		t.Fatalf("snapshot=%#v", snapshot)
	}
}
