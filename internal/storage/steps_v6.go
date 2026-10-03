package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// createStepTablesV6 creates the per-run step ledger and the tool call
// records with their lifecycle states. Every model request and every tool
// call of a run becomes a step with a run-internal sequence number; tool
// calls reference the assistant message that requested them.
func createStepTablesV6(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `CREATE TABLE steps (id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE, seq INTEGER NOT NULL, kind TEXT NOT NULL CHECK(kind IN ('model','tool')), status TEXT NOT NULL CHECK(status IN ('pending','running','succeeded','failed','cancelled','paused')), error TEXT NOT NULL DEFAULT '', payload TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(run_id, seq))`); err != nil {
		return fmt.Errorf("结构化运行迁移: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX steps_run_order ON steps(run_id, seq)`); err != nil {
		return fmt.Errorf("结构化运行迁移: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE tool_calls (id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE, step_id TEXT NOT NULL UNIQUE REFERENCES steps(id), message_id TEXT NOT NULL REFERENCES messages(id), call_id TEXT NOT NULL, tool_name TEXT NOT NULL, arguments TEXT NOT NULL, state TEXT NOT NULL CHECK(state IN ('pending','approved','executing','succeeded','failed','cancelled','execution_unknown')), result TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(run_id, call_id))`); err != nil {
		return fmt.Errorf("结构化运行迁移: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX tool_calls_run_order ON tool_calls(run_id, created_at, id)`); err != nil {
		return fmt.Errorf("结构化运行迁移: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX tool_calls_session ON tool_calls(message_id)`); err != nil {
		return fmt.Errorf("结构化运行迁移: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `PRAGMA user_version = 6`); err != nil {
		return err
	}
	return nil
}
