package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverAndLoadSkill(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "review-code")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: review-code\ndescription: Review Go changes when asked for code review.\n---\n# Steps\nRead the diff.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(catalog.Summary(), "review-code: Review Go") || strings.Contains(catalog.Summary(), "Read the diff") {
		t.Fatalf("metadata loading: %q", catalog.Summary())
	}
	loaded, err := catalog.Load("review-code")
	if err != nil || loaded != content {
		t.Fatalf("load: %q, %v", loaded, err)
	}
	if _, err := catalog.Load("unknown"); err == nil {
		t.Fatal("unknown skill accepted")
	}
}

func TestRejectsInvalidSkillName(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "review-code")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: Other\ndescription: test\n---\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(root); err == nil {
		t.Fatal("invalid skill accepted")
	}
}
