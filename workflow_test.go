package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkflowDAGValidation(t *testing.T) {
	valid := WorkflowDefinition{Nodes: []WorkflowNode{{ID: "a", Type: "archive"}, {ID: "b", Type: "checksum_verify"}}, Edges: []WorkflowEdge{{From: "a", To: "b"}}}
	if !validateDefinition(valid) {
		t.Fatal("valid DAG rejected")
	}
	valid.Edges = append(valid.Edges, WorkflowEdge{From: "b", To: "a"})
	if validateDefinition(valid) {
		t.Fatal("cycle accepted")
	}
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
