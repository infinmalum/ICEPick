package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsAndInvalidConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	got, err := Load(path)
	if err != nil || got.Model.Provider != "ollama" || got.Model.BaseURL != "http://127.0.0.1:11434/v1" {
		t.Fatalf("defaults: %+v, %v", got, err)
	}
	if err := os.WriteFile(path, []byte(`{"model":{"provider":"other","baseUrl":"https://example.com"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("unsupported provider accepted")
	}
	if err := os.WriteFile(path, []byte(`{broken`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}
