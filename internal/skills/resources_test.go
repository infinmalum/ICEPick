package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResourceSnapshotFrozenAndBounded(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "example")
	os.MkdirAll(filepath.Join(dir, "scripts"), 0700)
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: example\ndescription: example skill\n---\nRead scripts/check.py."), 0600)
	script := filepath.Join(dir, "scripts", "check.py")
	os.WriteFile(script, []byte("print('old')"), 0600)
	c, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	snaps, err := c.Snapshot(false, []string{"example"})
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(script, []byte("print('new')"), 0600)
	got, err := snaps[0].ReadResource("scripts/check.py")
	if err != nil || got != "print('old')" {
		t.Fatalf("%s %v", got, err)
	}
	next, err := c.Snapshot(false, []string{"example"})
	if err != nil || next[0].Fingerprint() == snaps[0].Fingerprint() {
		t.Fatal("resource edit did not change fingerprint")
	}
	manifest, _ := snaps[0].ReadResource("")
	if !strings.Contains(manifest, "scripts/check.py") {
		t.Fatal(manifest)
	}
	for _, path := range []string{"../secret", "scripts/../SKILL.md", "/etc/passwd", "scripts\\check.py"} {
		if _, err := snaps[0].ReadResource(path); err == nil {
			t.Fatal("accepted", path)
		}
	}
	os.Symlink(filepath.Join(dir, "SKILL.md"), filepath.Join(dir, "scripts", "link"))
	if _, err := c.Snapshot(false, []string{"example"}); err == nil {
		t.Fatal("symlink accepted")
	}
	if err := c.RegisterManaged(snaps[0]); err == nil {
		t.Fatal("collision accepted")
	}
	managed, _ := Discover(t.TempDir())
	if err := managed.RegisterManaged(snaps[0]); err != nil {
		t.Fatal(err)
	}
	copy, _ := managed.ManagedSnapshot("example")
	copy.Resources["scripts/check.py"] = Resource{}
	actual, _ := managed.ManagedSnapshot("example")
	if len(actual.Resources["scripts/check.py"].Data) == 0 {
		t.Fatal("mutable managed resources")
	}
}
