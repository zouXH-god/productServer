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
