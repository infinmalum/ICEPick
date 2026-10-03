package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// migrateStructuredRuns moves the schema to version 6: tool calls and steps
// become first-class persisted records and messages carry their run, sequence
// number and structured tool call requests.
//
// Pre-v6 conversations stored tool activity as free-form messages with role
// 'tool'. Their call IDs and arguments cannot be reconstructed, so those rows
// are kept for display but flagged legacy and excluded from model history
// rebuilding. No call parameters or results are invented.
func (s *Store) migrateStructuredRuns() error {
	return s.withTx(context.Background(), func(tx *sql.Tx) error {
		if err := migrateMessagesV6(tx); err != nil {
			return err
		}
		if err := createStepTablesV6(context.Background(), tx); err != nil {
			return err
		}
		return nil
	})
}

// migrateMessagesV6 adds the structured message columns and backfills
// sequence numbers from the pre-existing display order.
func migrateMessagesV6(tx *sql.Tx) error {
	if _, err := tx.Exec(`ALTER TABLE messages ADD COLUMN run_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("结构化运行迁移: %w", err)
	}
	if _, err := tx.Exec(`ALTER TABLE messages ADD COLUMN seq INTEGER NOT NULL DEFAULT 0`); err != nil {
		return fmt.Errorf("结构化运行迁移: %w", err)
	}
	if _, err := tx.Exec(`ALTER TABLE messages ADD COLUMN tool_calls TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("结构化运行迁移: %w", err)
	}
	if _, err := tx.Exec(`ALTER TABLE messages ADD COLUMN legacy INTEGER NOT NULL DEFAULT 0`); err != nil {
		return fmt.Errorf("结构化运行迁移: %w", err)
	}
	// Sequence numbers follow the pre-existing display order so old
	// conversations keep their ordering; new rows append per session.
	if _, err := tx.Exec(`UPDATE messages SET seq = (SELECT COUNT(*) FROM messages AS earlier WHERE earlier.session_id = messages.session_id AND (earlier.created_at < messages.created_at OR (earlier.created_at = messages.created_at AND earlier.id <= messages.id)))`); err != nil {
		return fmt.Errorf("结构化运行迁移: %w", err)
	}
	if _, err := tx.Exec(`UPDATE messages SET legacy = 1 WHERE role = 'tool'`); err != nil {
		return fmt.Errorf("结构化运行迁移: %w", err)
	}
	if _, err := tx.Exec(`CREATE UNIQUE INDEX messages_session_seq ON messages(session_id, seq)`); err != nil {
		return fmt.Errorf("结构化运行迁移: %w", err)
	}
	return nil
}
