package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("HTTP_ADDR=:9091\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	previous, existed := os.LookupEnv("HTTP_ADDR")
	if err := os.Unsetenv("HTTP_ADDR"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv("HTTP_ADDR", previous)
		} else {
			_ = os.Unsetenv("HTTP_ADDR")
		}
	})

	if err := loadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":9091" {
		t.Fatalf("HTTPAddr = %q, want :9091", cfg.HTTPAddr)
	}
}

func TestLoadDotEnvDoesNotOverrideEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("HTTP_ADDR=:9091\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HTTP_ADDR", ":9092")

	if err := loadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":9092" {
		t.Fatalf("HTTPAddr = %q, want :9092", cfg.HTTPAddr)
	}
}
