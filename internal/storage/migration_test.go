package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigrateVersionOneRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, title TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE TABLE messages (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, role TEXT NOT NULL, content TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE TABLE runs (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, status TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`INSERT INTO sessions VALUES ('s', 'old', '2026-01-01')`,
		`INSERT INTO runs VALUES ('r', 's', 'running', '2026-01-01', '2026-01-01')`,
		`PRAGMA user_version = 1`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SetRunStatus(context.Background(), "r", "failed", "model unavailable"); err != nil {
		t.Fatal(err)
	}
	run, err := store.GetRun(context.Background(), "r")
	if err != nil || run.Error != "model unavailable" {
		t.Fatalf("migrated run: %+v, %v", run, err)
	}
}
