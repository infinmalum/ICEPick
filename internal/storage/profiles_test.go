package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestAgentProfilesAndSessionIsolation(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "agents.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	profiles, err := s.ListAgents(ctx)
	if err != nil || len(profiles) != 2 || profiles[0].ID != "chat" || profiles[1].ID != "work" {
		t.Fatalf("presets: %+v %v", profiles, err)
	}
	blank := BlankProfile()
	blank.Name = "Research"
	created, err := s.CreateAgent(ctx, "", blank)
	if err != nil {
		t.Fatal(err)
	}
	chat, err := s.CreateAgentSession(ctx, "chat", "chat task")
	if err != nil {
		t.Fatal(err)
	}
	custom, err := s.CreateAgentSession(ctx, created.ID, "research task")
	if err != nil {
		t.Fatal(err)
	}
	message, err := s.AddMessage(ctx, custom.ID, "user", "hello")
	if err != nil || message.AgentID != created.ID { t.Fatalf("custom message: %+v %v", message, err) }
	run, err := s.CreateRun(ctx, custom.ID)
	if err != nil || run.AgentID != created.ID || run.ProfileRevision != 1 { t.Fatalf("custom run: %+v %v", run, err) }
	if _, err := s.db.ExecContext(ctx, `UPDATE messages SET agent_id='chat' WHERE id=?`, message.ID); err == nil { t.Fatal("cross-Agent message rewrite accepted") }
	if _, err := s.db.ExecContext(ctx, `UPDATE sessions SET agent_id='chat' WHERE id=?`, custom.ID); err == nil { t.Fatal("session with history reassigned") }
	chatSessions, err := s.ListAgentSessions(ctx, "chat")
	if err != nil || len(chatSessions) != 1 || chatSessions[0].ID != chat.ID {
		t.Fatalf("chat sessions: %+v %v", chatSessions, err)
	}
	customSessions, err := s.ListAgentSessions(ctx, created.ID)
	if err != nil || len(customSessions) != 1 || customSessions[0].ID != custom.ID {
		t.Fatalf("custom sessions: %+v %v", customSessions, err)
	}
	created.SystemPrompt = "You are a researcher"
	updated, err := s.UpdateAgent(ctx, created)
	if err != nil || updated.Revision != 2 {
		t.Fatalf("update: %+v %v", updated, err)
	}
	oldSnapshotRun, err := s.CreateRunAtRevision(ctx, custom.ID, 1)
	if err != nil || oldSnapshotRun.ProfileRevision != 1 { t.Fatalf("run snapshot: %+v %v", oldSnapshotRun, err) }
	if _, err := s.UpdateAgent(ctx, created); err == nil {
		t.Fatal("stale revision accepted")
	}
	copy, err := s.CreateAgent(ctx, created.ID, AgentProfile{})
	if err != nil || copy.ID == created.ID || copy.SystemPrompt != updated.SystemPrompt {
		t.Fatalf("copy: %+v %v", copy, err)
	}
	copySessions, err := s.ListAgentSessions(ctx, copy.ID)
	if err != nil || len(copySessions) != 0 {
		t.Fatalf("copy inherited sessions: %+v %v", copySessions, err)
	}
	if _, err := s.CreateAgentSession(ctx, "missing", "invalid"); err == nil {
		t.Fatal("unknown agent accepted")
	}
}

func TestVersionTwoMigrationAssignsChat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, title TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE TABLE messages (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, role TEXT NOT NULL, content TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE TABLE runs (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, status TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, error TEXT NOT NULL DEFAULT '')`,
		`INSERT INTO sessions VALUES ('old', 'legacy', '2026-01-01')`,
		`INSERT INTO messages VALUES ('m', 'old', 'user', 'hello', '2026-01-01')`,
		`INSERT INTO runs VALUES ('r', 'old', 'completed', '2026-01-01', '2026-01-01', '')`,
		`PRAGMA user_version = 2`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	session, err := s.GetSession(ctx, "old")
	if err != nil || session.AgentID != "chat" {
		t.Fatalf("migrated session: %+v %v", session, err)
	}
	messages, err := s.ListMessages(ctx, "old")
	if err != nil || len(messages) != 1 || messages[0].Content != "hello" || messages[0].AgentID != "chat" {
		t.Fatalf("messages: %+v %v", messages, err)
	}
	run, err := s.GetRun(ctx, "r")
	if err != nil || run.Status != "completed" || run.AgentID != "chat" || run.ProfileRevision != 1 {
		t.Fatalf("run: %+v %v", run, err)
	}
}
