package storage

import (
	"context"
	"database/sql"
)

// Session kinds. Normal sessions are the user-visible conversation list;
// delegation sessions are the independent temporary contexts created for one
// delegated subtask each — they carry the subtask's messages for audit but
// never appear in the agent's normal session list.
const (
	SessionKindNormal     = "normal"
	SessionKindDelegation = "delegation"
)

// migrateDelegationV11 adds the P5 structures: session kinds plus the
// parent-child run relation. The child run keeps its own session, agent
// (the delegation target) and profile snapshot; the parent link is what the
// task tree reads and what cancel propagation is verified against.
func migrateDelegationV11(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `ALTER TABLE sessions ADD COLUMN kind TEXT NOT NULL DEFAULT 'normal'`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX sessions_agent_kind_order ON sessions(agent_id, kind, created_at, id)`); err != nil {
		return err
	}
	// The parent reference is a plain column (no FK clause): SQLite cannot
	// drop a column that participates in a foreign key, which would make the
	// downgrade paths unbuildable. The application layer only ever writes
	// parent ids it has just resolved from a live run row. Depth 0 marks a
	// top-level run, each delegation adds one.
	if _, err := tx.ExecContext(ctx, `ALTER TABLE runs ADD COLUMN parent_run_id TEXT`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE runs ADD COLUMN depth INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX runs_parent_order ON runs(parent_run_id, created_at, id)`); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `PRAGMA user_version=11`)
	return err
}
