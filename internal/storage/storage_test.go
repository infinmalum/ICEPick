package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPersistenceAndRecovery(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "icepick.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateSession(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddMessage(ctx, session.ID, "user", "hello"); err != nil {
		t.Fatal(err)
	}
	run, err := store.CreateRun(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("database permissions: %v, %v", info, err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.RecoverInterrupted(ctx); err != nil {
		t.Fatal(err)
	}
	sessions, err := store.ListSessions(ctx)
	if err != nil || len(sessions) != 1 || sessions[0].ID != session.ID {
		t.Fatalf("sessions: %+v, %v", sessions, err)
	}
	messages, err := store.ListMessages(ctx, session.ID)
	if err != nil || len(messages) != 1 || messages[0].Content != "hello" {
		t.Fatalf("messages: %+v, %v", messages, err)
	}
	recovered, err := store.GetRun(ctx, run.ID)
	if err != nil || recovered.Status != "interrupted" {
		t.Fatalf("run: %+v, %v", recovered, err)
	}
	if _, err := store.AddMessage(ctx, "missing", "user", "x"); err == nil {
		t.Fatal("foreign key constraint missing")
	}
}
