package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestComponentsIndependentAndAtomic(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	p, err := s.GetProfile(ctx, DefaultProfileID)
	if err != nil {
		t.Fatal(err)
	}
	// Fail if a prompt-only edit writes the model component at all.
	if _, err := s.db.Exec(`CREATE TRIGGER model_unchanged BEFORE UPDATE ON profile_components WHEN OLD.kind='model' BEGIN SELECT RAISE(ABORT,'model unexpectedly rewritten'); END`); err != nil {
		t.Fatal(err)
	}
	p.Data.SystemPrompt = "new prompt"
	p.Data.Persona = "calm"
	p, err = s.UpdateProfile(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if p.Data.Persona != "calm" || p.Data.SystemPrompt != "new prompt" {
		t.Fatalf("lost components: %+v", p)
	}
	before := p
	p.Data.Model.Name = "new model"
	p.Data.SystemPrompt = "must roll back"
	if _, err = s.UpdateProfile(ctx, p); err == nil {
		t.Fatal("component failure must abort publication")
	}
	after, err := s.GetProfile(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision || after.Data.SystemPrompt != before.Data.SystemPrompt {
		t.Fatal("partial publication")
	}
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM pragma_table_info('profiles') WHERE name='config'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("monolithic config remains: %d %v", n, err)
	}
}

func TestV8MigrationSeparatesComponentsAndDoesNotRegrant(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v8.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.GetProfile(ctx, DefaultProfileID)
	p.Data.Persona = "persona"
	p.Data.SystemPrompt = "legacy prompt"
	p, err = s.UpdateProfile(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := s.CreateAgent(ctx, "second", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.CreateAgentSession(ctx, agent.ID, "history")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(p.Data)
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		for _, q := range []string{`DROP TABLE session_context_scopes`, `DROP VIEW resolved_profiles`, `ALTER TABLE profiles ADD COLUMN config TEXT NOT NULL DEFAULT '{}'`, `DROP TABLE profile_components`, `DROP TABLE exfil_grants`, `DROP TABLE agent_memories`, `DROP TABLE session_summaries`, `DROP TABLE session_plans`, `ALTER TABLE legacy_exfil_grants RENAME TO exfil_grants`, `PRAGMA user_version=8`} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE profiles SET config=? WHERE id=?`, string(raw), p.ID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO exfil_grants(destination,scopes,created_at) VALUES('https://example.com','["personal"]','old')`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p2, err := s.GetProfile(ctx, p.ID)
	if err != nil || p2.Data.Persona != "persona" || p2.Data.SystemPrompt != "legacy prompt" {
		t.Fatalf("migration: %+v %v", p2, err)
	}
	owner, err := s.AgentForSession(ctx, session.ID)
	if err != nil || owner.AgentID != agent.ID {
		t.Fatal("ownership changed", err)
	}
	for _, id := range []string{DefaultAgentID, agent.ID} {
		g, err := s.ListExfilGrants(ctx, id)
		if err != nil || len(g) != 0 {
			t.Fatal("global grant inherited", g, err)
		}
	}
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM legacy_exfil_grants`).Scan(&n); err != nil || n != 1 {
		t.Fatal("legacy authorization audit lost", err)
	}
}
