package storage

import (
	"context"
	"database/sql"
)

// migrateMemoryV10 adds the P4 knowledge tables: per-Agent long-term
// memories, per-session compaction summaries and per-session plan state.
// All three are owned by an Agent (via the session for summaries and plans),
// never by a Profile, so sharing or copying a Profile shares nothing.
func migrateMemoryV10(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `CREATE TABLE agent_memories (
		id TEXT PRIMARY KEY,
		agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
		content TEXT NOT NULL,
		source TEXT NOT NULL CHECK(source IN ('model','user')),
		scope TEXT NOT NULL,
		source_session_id TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX agent_memories_order ON agent_memories(agent_id, created_at, id)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE session_summaries (
		session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
		agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
		covers_seq INTEGER NOT NULL,
		covers_calls INTEGER NOT NULL DEFAULT 0,
		summary TEXT NOT NULL,
		scope TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE session_plans (
		session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
		agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
		plan TEXT NOT NULL CHECK(json_valid(plan)),
		updated_at TEXT NOT NULL
	)`); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `PRAGMA user_version=10`)
	return err
}
