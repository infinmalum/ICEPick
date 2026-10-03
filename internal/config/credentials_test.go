package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCredentialsRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	creds, err := LoadCredentials(path)
	if err != nil || len(creds.Providers) != 0 {
		t.Fatalf("empty store: %+v %v", creds, err)
	}
	if err := creds.SetCredential("openai", " sk-openai "); err != nil {
		t.Fatal(err)
	}
	if err := creds.SetCredential("gemini", "sk-gemini"); err != nil {
		t.Fatal(err)
	}
	if err := creds.SetCredential("ollama", "nope"); err == nil {
		t.Fatal("local provider accepted")
	}
	if err := creds.SetCredential("anthropic", "  "); err == nil {
		t.Fatal("empty key accepted")
	}
	if err := SaveCredentials(path, creds); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadCredentials(path)
	if err != nil {
		t.Fatal(err)
	}
	if key, ok := reloaded.Key("openai"); !ok || key != "sk-openai" {
		t.Fatalf("openai key: %q %v", key, ok)
	}
	if _, ok := reloaded.Key("lmstudio"); ok {
		t.Fatal("unexpected lmstudio key")
	}
	reloaded.DeleteCredential("openai")
	if err := SaveCredentials(path, reloaded); err != nil {
		t.Fatal(err)
	}
	final, err := LoadCredentials(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := final.Key("openai"); ok {
		t.Fatal("deleted key survived")
	}
	if _, ok := final.Key("gemini"); !ok {
		t.Fatal("gemini key lost")
	}
}

func TestCredentialsRejectUnknownProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	creds := Credentials{Providers: map[string]string{"mystery": "k"}}
	if err := SaveCredentials(path, creds); err == nil || !strings.Contains(err.Error(), "mystery") {
		t.Fatalf("unknown provider accepted: %v", err)
	}
}

func TestFileCredentialLookup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	lookup := FileCredentialLookup(path)
	if _, ok := lookup("anthropic"); ok {
		t.Fatal("key before save")
	}
	var creds Credentials
	creds.SetCredential("anthropic", "ak-1")
	if err := SaveCredentials(path, creds); err != nil {
		t.Fatal(err)
	}
	// The lookup re-reads the file, so saving applies without a restart.
	if key, ok := lookup("anthropic"); !ok || key != "ak-1" {
		t.Fatalf("lookup after save: %q %v", key, ok)
	}
}
