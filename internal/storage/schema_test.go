package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnsupportedDatabaseFormatLeavesDataIntact(t *testing.T) {
	for _, version := range []int{19, currentVersion + 1} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "data.db")
			db, err := sql.Open("sqlite3", path)
			if err != nil {
				t.Fatal(err)
			}
			_, err = db.Exec(fmt.Sprintf(`CREATE TABLE preserved(content TEXT); INSERT INTO preserved VALUES('keep'); PRAGMA user_version=%d`, version))
			if err != nil {
				t.Fatal(err)
			}
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if store, err := Open(path); err == nil {
				store.Close()
				t.Fatal("unsupported format accepted")
			} else if !strings.Contains(err.Error(), "新的数据目录") {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(before) != string(after) {
				t.Fatalf("rejected database was modified: %v", err)
			}
			files, err := os.ReadDir(filepath.Dir(path))
			if err != nil || len(files) != 1 {
				t.Fatalf("unexpected migration artifacts: %v %v", files, err)
			}
		})
	}
}
