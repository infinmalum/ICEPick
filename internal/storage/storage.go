package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Store struct{ db *sql.DB }

type Session struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt string `json:"createdAt"`
}

type Message struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt string `json:"createdAt"`
}

type Run struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", path+"?_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > 1 {
		return fmt.Errorf("数据库版本 %d 高于当前程序支持的版本", version)
	}
	if version == 1 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, query := range []string{
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, title TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE TABLE messages (id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE, role TEXT NOT NULL CHECK(role IN ('user','assistant','tool')), content TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE INDEX messages_session_order ON messages(session_id, created_at, id)`,
		`CREATE TABLE runs (id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE, status TEXT NOT NULL CHECK(status IN ('running','completed','cancelled','failed','interrupted')), created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`PRAGMA user_version = 1`,
	} {
		if _, err := tx.Exec(query); err != nil {
			return fmt.Errorf("数据库迁移: %w", err)
		}
	}
	return tx.Commit()
}

func newID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func (s *Store) CreateSession(ctx context.Context, title string) (Session, error) {
	if title == "" {
		title = "新会话"
	}
	id, err := newID()
	if err != nil {
		return Session{}, err
	}
	item := Session{ID: id, Title: title, CreatedAt: now()}
	_, err = s.db.ExecContext(ctx, `INSERT INTO sessions VALUES (?, ?, ?)`, item.ID, item.Title, item.CreatedAt)
	return item, err
}

func (s *Store) ListSessions(ctx context.Context) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, title, created_at FROM sessions ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Session{}
	for rows.Next() {
		var item Session
		if err := rows.Scan(&item.ID, &item.Title, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) AddMessage(ctx context.Context, sessionID, role, content string) (Message, error) {
	if role != "user" && role != "assistant" && role != "tool" {
		return Message{}, errors.New("无效的消息角色")
	}
	id, err := newID()
	if err != nil {
		return Message{}, err
	}
	item := Message{ID: id, SessionID: sessionID, Role: role, Content: content, CreatedAt: now()}
	_, err = s.db.ExecContext(ctx, `INSERT INTO messages VALUES (?, ?, ?, ?, ?)`, item.ID, item.SessionID, item.Role, item.Content, item.CreatedAt)
	return item, err
}

func (s *Store) ListMessages(ctx context.Context, sessionID string) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, session_id, role, content, created_at FROM messages WHERE session_id = ? ORDER BY created_at, id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Message{}
	for rows.Next() {
		var item Message
		if err := rows.Scan(&item.ID, &item.SessionID, &item.Role, &item.Content, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateRun(ctx context.Context, sessionID string) (Run, error) {
	id, err := newID()
	if err != nil {
		return Run{}, err
	}
	item := Run{ID: id, SessionID: sessionID, Status: "running", CreatedAt: now(), UpdatedAt: now()}
	_, err = s.db.ExecContext(ctx, `INSERT INTO runs VALUES (?, ?, ?, ?, ?)`, item.ID, item.SessionID, item.Status, item.CreatedAt, item.UpdatedAt)
	return item, err
}

func (s *Store) SetRunStatus(ctx context.Context, id, status string) error {
	if status != "completed" && status != "cancelled" && status != "failed" && status != "interrupted" {
		return errors.New("无效的任务状态")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE runs SET status = ?, updated_at = ? WHERE id = ? AND status = 'running'`, status, now(), id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("任务不存在或已结束")
	}
	return nil
}

func (s *Store) GetRun(ctx context.Context, id string) (Run, error) {
	var item Run
	err := s.db.QueryRowContext(ctx, `SELECT id, session_id, status, created_at, updated_at FROM runs WHERE id = ?`, id).Scan(&item.ID, &item.SessionID, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (s *Store) RecoverInterrupted(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE runs SET status = 'interrupted', updated_at = ? WHERE status = 'running'`, now())
	return err
}
