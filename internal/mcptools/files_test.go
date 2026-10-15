package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadDirectoryBoundedAndNonRecursive(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 205; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%03d.txt", i)), []byte("secret-content"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := readLocalFile(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Entries   []any `json:"entries"`
		Truncated bool  `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Entries) != 200 || !out.Truncated || strings.Contains(raw, "secret-content") {
		t.Fatal(raw)
	}
}
