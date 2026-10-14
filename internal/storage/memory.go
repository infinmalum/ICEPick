package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Memory is one cross-session knowledge item of a single Context. Memories are
// keyed by context_id: conversations using the same Profile never implicitly share
// them, and copying or sharing a Profile copies nothing, because a Profile
// carries no instance data at all. Source records who wrote the item and
// scope records the data sensitivity of its content, inherited from the
// session context it was derived from; any future send of that content must
// clear the same scope.
type Memory struct {
	ID              string `json:"id"`
	ContextID       string `json:"contextId"`
	Content         string `json:"content"`
	Source          string `json:"source"` // model: saved by the agent during a run; user: added in the UI
	Scope           string `json:"scope"`
	SourceSessionID string `json:"sourceSessionId,omitempty"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

const (
	MemorySourceModel = "model"
	MemorySourceUser  = "user"
)

// MaxMemoryContent bounds one memory item; memories are injected into the
// system prompt, so single items stay small.
const MaxMemoryContent = 8192

func (s *Store) AddContextMemory(ctx context.Context, contextID, content, source, sourceSessionID, scope string) (Memory, error) {
	content = strings.TrimSpace(content)
	if content == "" || len(content) > MaxMemoryContent {
		return Memory{}, fmt.Errorf("记忆内容长度必须在 1 到 %d 字节之间", MaxMemoryContent)
	}
	if source != MemorySourceModel && source != MemorySourceUser {
		return Memory{}, fmt.Errorf("无效的记忆来源 %q", source)
	}
	if !ValidScope(scope) {
		return Memory{}, fmt.Errorf("无效的记忆数据范围 %q", scope)
	}
	id, err := newID()
	if err != nil {
		return Memory{}, err
	}
	item := Memory{ID: id, ContextID: contextID, Content: content, Source: source, Scope: scope, SourceSessionID: sourceSessionID, CreatedAt: now(), UpdatedAt: now()}
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `INSERT INTO context_memories (id, context_id, content, source, scope, source_session_id, created_at, updated_at)
			SELECT ?, id, ?, ?, ?, ?, ?, ? FROM contexts WHERE id = ? RETURNING context_id`,
			item.ID, item.Content, item.Source, item.Scope, item.SourceSessionID, item.CreatedAt, item.UpdatedAt, contextID).
			Scan(&item.ContextID)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Memory{}, errors.New("本机上下文不存在")
	}
	if err != nil {
		return Memory{}, err
	}
	return item, nil
}

// ListContextMemories returns the context's memories, newest first when limit is
// positive; retrieval inverts the order so the prompt reads chronologically.
func (s *Store) ListContextMemories(ctx context.Context, contextID string, limit int) ([]Memory, error) {
	query := `SELECT id, context_id, content, source, scope, source_session_id, created_at, updated_at FROM context_memories WHERE context_id = ? ORDER BY created_at DESC, id DESC`
	args := []any{contextID}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Memory{}
	for rows.Next() {
		var item Memory
		if err := rows.Scan(&item.ID, &item.ContextID, &item.Content, &item.Source, &item.Scope, &item.SourceSessionID, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// UpdateContextMemory edits the content of one memory. Only the owning context's
// rows are reachable; scope stays as recorded because the edit does not
// change where the knowledge came from.
func (s *Store) UpdateContextMemory(ctx context.Context, contextID, id, content string) (Memory, error) {
	content = strings.TrimSpace(content)
	if content == "" || len(content) > MaxMemoryContent {
		return Memory{}, fmt.Errorf("记忆内容长度必须在 1 到 %d 字节之间", MaxMemoryContent)
	}
	result, err := s.db.ExecContext(ctx, `UPDATE context_memories SET content = ?, updated_at = ? WHERE id = ? AND context_id = ?`, content, now(), id, contextID)
	if err != nil {
		return Memory{}, err
	}
	if n, err := result.RowsAffected(); err != nil {
		return Memory{}, err
	} else if n == 0 {
		return Memory{}, errors.New("记忆不存在")
	}
	var item Memory
	err = s.db.QueryRowContext(ctx, `SELECT id, context_id, content, source, scope, source_session_id, created_at, updated_at FROM context_memories WHERE id = ?`, id).
		Scan(&item.ID, &item.ContextID, &item.Content, &item.Source, &item.Scope, &item.SourceSessionID, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (s *Store) DeleteContextMemory(ctx context.Context, contextID, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM context_memories WHERE id = ? AND context_id = ?`, id, contextID)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return errors.New("记忆不存在")
	}
	return nil
}
