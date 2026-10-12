package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// currentVersion is the schema version this binary writes. Opening a database
// with a higher version is refused so an old binary never writes a new schema.
const currentVersion = 11

type Store struct {
	db   *sql.DB
	path string
	// backupPath names the pre-migration file snapshot that must be kept
	// until the migration to the current version succeeded.
	backupPath string
}

type Session struct {
	ID        string `json:"id"`
	AgentID   string `json:"agentId"`
	Title     string `json:"title"`
	Kind      string `json:"kind"`
	CreatedAt string `json:"createdAt"`
}

// MessageToolCall mirrors a model tool call request persisted on an assistant
// message. Tool results live in ToolCall rows, never in this projection.
type MessageToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Message struct {
	ID        string            `json:"id"`
	SessionID string            `json:"sessionId"`
	AgentID   string            `json:"agentId"`
	RunID     string            `json:"runId"`
	Seq       int               `json:"seq"`
	Role      string            `json:"role"`
	Content   string            `json:"content"`
	ToolCalls []MessageToolCall `json:"toolCalls,omitempty"`
	Legacy    bool              `json:"legacy"`
	CreatedAt string            `json:"createdAt"`
}

type Run struct {
	ID              string `json:"id"`
	SessionID       string `json:"sessionId"`
	AgentID         string `json:"agentId"`
	ProfileID       string `json:"profileId"`
	ProfileRevision int    `json:"profileRevision"`
	// ParentRunID links a delegated subtask run to the run whose tool call
	// started it; top-level runs leave it empty.
	ParentRunID string `json:"parentRunId,omitempty"`
	// Depth is 0 for top-level runs and increments per delegation level.
	Depth     int    `json:"depth"`
	Status    string `json:"status"`
	Error     string `json:"error"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	backup, err := snapshotDatabaseFiles(path)
	if err != nil {
		return nil, fmt.Errorf("迁移前备份: %w", err)
	}
	db, err := sql.Open("sqlite3", path+"?_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		removeIfAny(backup)
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, path: path, backupPath: backup}
	migrated, err := s.migrate()
	if err != nil {
		// Keep the pre-migration snapshot: this is exactly when it is
		// needed to roll back to the previous binary.
		db.Close()
		return nil, err
	}
	if !migrated {
		removeIfAny(s.backupPath)
		s.backupPath = ""
	}
	if err := os.Chmod(path, 0600); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// snapshotDatabaseFiles copies the database and its rollback journal before
// any connection touches the file, so the pair is exactly restorable by an
// older binary. The snapshot is only kept when a migration actually runs.
func snapshotDatabaseFiles(path string) (string, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	stamp := time.Now().UTC().Format("20060102T150405.000000000")
	backup := path + ".pre-migration-" + stamp + ".bak"
	if err := copyFile(path, backup); err != nil {
		removeIfAny(backup)
		return "", err
	}
	for _, suffix := range []string{"-journal", "-wal", "-shm"} {
		side := path + suffix
		if _, err := os.Stat(side); err == nil {
			if err := copyFile(side, backup+suffix); err != nil {
				removeIfAny(backup)
				removeIfAny(backup + suffix)
				return "", err
			}
		}
	}
	return backup, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

func removeIfAny(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}

func (s *Store) Close() error { return s.db.Close() }

// BackupPath reports the pre-migration snapshot kept next to the database, if
// a migration ran during Open. The snapshot stays until manually removed so a
// rollback to the previous binary can restore it.
func (s *Store) BackupPath() string { return s.backupPath }

// migrate applies pending schema migrations and reports whether the database
// file was actually migrated (a fresh empty database does not count).
func (s *Store) migrate() (bool, error) {
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return false, err
	}
	if version > currentVersion {
		return false, fmt.Errorf("数据库版本 %d 高于当前程序支持的版本", version)
	}
	migrated := version > 0 && version < currentVersion
	if version == 0 {
		if err := s.migrateFresh(); err != nil {
			return false, err
		}
	}
	if version < 2 {
		if err := s.withTx(context.Background(), func(tx *sql.Tx) error {
			if _, err := tx.Exec(`ALTER TABLE runs ADD COLUMN error TEXT NOT NULL DEFAULT ''`); err != nil {
				return fmt.Errorf("数据库迁移: %w", err)
			}
			if _, err := tx.Exec(`PRAGMA user_version = 2`); err != nil {
				return err
			}
			return nil
		}); err != nil {
			return false, err
		}
	}
	if version < 3 {
		if err := s.migrateProfiles(); err != nil {
			return false, err
		}
	}
	if version < 4 {
		if err := s.migrateAgentRecords(); err != nil {
			return false, err
		}
	}
	if version < 5 {
		if err := s.migrateSingleDefault(); err != nil {
			return false, err
		}
	}
	if version < 6 {
		if err := s.migrateStructuredRuns(); err != nil {
			return false, err
		}
	}
	if version < 7 {
		if err := s.withTx(context.Background(), func(tx *sql.Tx) error {
			return migrateApprovalsV7(context.Background(), tx)
		}); err != nil {
			return false, err
		}
	}
	if version < 8 {
		if err := s.withTx(context.Background(), func(tx *sql.Tx) error {
			return migrateProfilesV8(context.Background(), tx)
		}); err != nil {
			return false, err
		}
	}
	if version < 9 {
		if err := s.withTx(context.Background(), func(tx *sql.Tx) error { return migrateComponentsV9(context.Background(), tx) }); err != nil {
			return false, err
		}
	}
	if version < 10 {
		if err := s.withTx(context.Background(), func(tx *sql.Tx) error { return migrateMemoryV10(context.Background(), tx) }); err != nil {
			return false, err
		}
	}
	if version < 11 {
		if err := s.withTx(context.Background(), func(tx *sql.Tx) error { return migrateDelegationV11(context.Background(), tx) }); err != nil {
			return false, err
		}
	}
	return migrated, nil
}

func (s *Store) migrateFresh() error {
	return s.withTx(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.Exec(`CREATE TABLE sessions (id TEXT PRIMARY KEY, title TEXT NOT NULL, created_at TEXT NOT NULL)`); err != nil {
			return fmt.Errorf("数据库迁移: %w", err)
		}
		if _, err := tx.Exec(`CREATE TABLE messages (id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE, role TEXT NOT NULL CHECK(role IN ('user','assistant','tool')), content TEXT NOT NULL, created_at TEXT NOT NULL)`); err != nil {
			return fmt.Errorf("数据库迁移: %w", err)
		}
		if _, err := tx.Exec(`CREATE INDEX messages_session_order ON messages(session_id, created_at, id)`); err != nil {
			return fmt.Errorf("数据库迁移: %w", err)
		}
		if _, err := tx.Exec(`CREATE TABLE runs (id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE, status TEXT NOT NULL CHECK(status IN ('running','completed','cancelled','failed','interrupted')), created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
			return fmt.Errorf("数据库迁移: %w", err)
		}
		if _, err := tx.Exec(`PRAGMA user_version = 1`); err != nil {
			return err
		}
		return nil
	})
}

func newID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// withTx runs fn in a single transaction and commits it. Related facts must
// commit together or not at all, so every multi-row write goes through here.
func (s *Store) withTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) CreateSession(ctx context.Context, title string) (Session, error) {
	return s.CreateAgentSession(ctx, DefaultAgentID, title)
}

func (s *Store) CreateAgentSession(ctx context.Context, agentID, title string) (Session, error) {
	if title == "" {
		title = "新会话"
	}
	id, err := newID()
	if err != nil {
		return Session{}, err
	}
	item := Session{ID: id, AgentID: agentID, Title: title, Kind: SessionKindNormal, CreatedAt: now()}
	_, err = s.db.ExecContext(ctx, `INSERT INTO sessions (id, agent_id, kind, title, created_at) SELECT ?, id, 'normal', ?, ? FROM agents WHERE id = ?`, item.ID, item.Title, item.CreatedAt, agentID)
	if err == nil {
		var found string
		err = s.db.QueryRowContext(ctx, `SELECT id FROM sessions WHERE id = ?`, item.ID).Scan(&found)
	}
	return item, err
}

// CreateDelegationSession opens the independent temporary context one
// delegated subtask runs in. It belongs to the target Agent and is excluded
// from the normal session list; the messages stay for audit.
func (s *Store) CreateDelegationSession(ctx context.Context, agentID, title string) (Session, error) {
	if title == "" {
		title = "委派子任务"
	}
	id, err := newID()
	if err != nil {
		return Session{}, err
	}
	item := Session{ID: id, AgentID: agentID, Title: title, Kind: SessionKindDelegation, CreatedAt: now()}
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `INSERT INTO sessions (id, agent_id, kind, title, created_at) SELECT ?, id, 'delegation', ?, ? FROM agents WHERE id = ?`, item.ID, item.Title, item.CreatedAt, agentID)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return errors.New("Agent 不存在")
		}
		return nil
	})
	if err != nil {
		return Session{}, err
	}
	return item, nil
}

func (s *Store) ListSessions(ctx context.Context) ([]Session, error) {
	return s.ListAgentSessions(ctx, DefaultAgentID)
}

// ListAgentSessions returns the agent's normal sessions. Delegation sessions
// are temporary subtask contexts owned by runs, not conversation history.
func (s *Store) ListAgentSessions(ctx context.Context, agentID string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, agent_id, title, kind, created_at FROM sessions WHERE agent_id = ? AND kind = 'normal' ORDER BY created_at DESC, id DESC`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Session{}
	for rows.Next() {
		var item Session
		if err := rows.Scan(&item.ID, &item.AgentID, &item.Title, &item.Kind, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ListAgentSessionsAll returns every session of the agent including hidden
// delegation sessions; the approval sweep needs those to surface subtask
// approval requests.
func (s *Store) ListAgentSessionsAll(ctx context.Context, agentID string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, agent_id, title, kind, created_at FROM sessions WHERE agent_id = ? ORDER BY created_at DESC, id DESC`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Session{}
	for rows.Next() {
		var item Session
		if err := rows.Scan(&item.ID, &item.AgentID, &item.Title, &item.Kind, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetSession(ctx context.Context, sessionID string) (Session, error) {
	var item Session
	err := s.db.QueryRowContext(ctx, `SELECT id, agent_id, title, kind, created_at FROM sessions WHERE id = ?`, sessionID).Scan(&item.ID, &item.AgentID, &item.Title, &item.Kind, &item.CreatedAt)
	return item, err
}

// AddMessage persists a user or assistant message at the next per-session
// sequence number. Assistant tool call requests are persisted via
// AddAssistantMessage so the call rows and the message commit atomically.
func (s *Store) AddMessage(ctx context.Context, sessionID, runID, role, content string) (Message, error) {
	if role != RoleUser && role != RoleAssistant {
		return Message{}, errors.New("无效的消息角色")
	}
	id, err := newID()
	if err != nil {
		return Message{}, err
	}
	created := now()
	item := Message{ID: id, SessionID: sessionID, RunID: runID, Role: role, Content: content, CreatedAt: created}
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `INSERT INTO messages (id, session_id, agent_id, run_id, seq, role, content, tool_calls, legacy, created_at)
			SELECT ?, id, agent_id, ?, COALESCE((SELECT MAX(seq) FROM messages WHERE session_id = ?), 0) + 1, ?, ?, '', 0, ? FROM sessions WHERE id = ?
			RETURNING agent_id, seq`,
			item.ID, runID, sessionID, role, content, created, sessionID).
			Scan(&item.AgentID, &item.Seq)
	})
	if err != nil {
		return Message{}, err
	}
	return item, nil
}

// AddAssistantMessage persists an assistant message together with the tool
// calls it requested. Every call gets a pending ToolCall row and a tool Step
// in the same transaction; if any part fails nothing is persisted, so a
// partial batch can never be replayed later.
func (s *Store) AddAssistantMessage(ctx context.Context, sessionID, runID, content string, calls []MessageToolCall) (Message, []ToolCall, error) {
	if len(calls) == 0 {
		item, err := s.AddMessage(ctx, sessionID, runID, RoleAssistant, content)
		return item, nil, err
	}
	seen := map[string]bool{}
	for _, call := range calls {
		if call.ID == "" || seen[call.ID] {
			return Message{}, nil, errors.New("模型工具调用 ID 缺失或重复")
		}
		seen[call.ID] = true
	}
	id, err := newID()
	if err != nil {
		return Message{}, nil, err
	}
	created := now()
	item := Message{ID: id, SessionID: sessionID, RunID: runID, Role: RoleAssistant, Content: content, ToolCalls: append([]MessageToolCall(nil), calls...), CreatedAt: created}
	payload, err := json.Marshal(item.ToolCalls)
	if err != nil {
		return Message{}, nil, err
	}
	rows := make([]ToolCall, 0, len(calls))
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `INSERT INTO messages (id, session_id, agent_id, run_id, seq, role, content, tool_calls, legacy, created_at)
			SELECT ?, id, agent_id, ?, COALESCE((SELECT MAX(seq) FROM messages WHERE session_id = ?), 0) + 1, ?, ?, ?, 0, ? FROM sessions WHERE id = ?
			RETURNING agent_id, seq`,
			item.ID, runID, sessionID, RoleAssistant, content, string(payload), created, sessionID).
			Scan(&item.AgentID, &item.Seq); err != nil {
			return err
		}
		for _, call := range calls {
			stepID, err := newID()
			if err != nil {
				return err
			}
			callRowID, err := newID()
			if err != nil {
				return err
			}
			callCreated := now()
			if _, err := tx.ExecContext(ctx, `INSERT INTO steps (id, run_id, seq, kind, status, error, payload, created_at, updated_at)
				SELECT ?, ?, COALESCE((SELECT MAX(seq) FROM steps WHERE run_id = ?), 0) + 1, 'tool', 'pending', '', '', ?, ?`,
				stepID, runID, runID, callCreated, callCreated); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO tool_calls (id, run_id, step_id, message_id, call_id, tool_name, arguments, state, result, error, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', '', '', ?, ?)`,
				callRowID, runID, stepID, item.ID, call.ID, call.Name, call.Arguments, callCreated, callCreated); err != nil {
				return err
			}
			rows = append(rows, ToolCall{
				ID: callRowID, RunID: runID, StepID: stepID, MessageID: item.ID,
				CallID: call.ID, ToolName: call.Name, Arguments: call.Arguments,
				State: "pending", CreatedAt: callCreated, UpdatedAt: callCreated,
			})
		}
		return nil
	})
	if err != nil {
		return Message{}, nil, err
	}
	return item, rows, nil
}

func (s *Store) ListMessages(ctx context.Context, sessionID string) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, session_id, agent_id, run_id, seq, role, content, tool_calls, legacy, created_at FROM messages WHERE session_id = ? ORDER BY seq`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Message{}
	for rows.Next() {
		var item Message
		var payload string
		if err := rows.Scan(&item.ID, &item.SessionID, &item.AgentID, &item.RunID, &item.Seq, &item.Role, &item.Content, &payload, &item.Legacy, &item.CreatedAt); err != nil {
			return nil, err
		}
		if payload != "" {
			if err := json.Unmarshal([]byte(payload), &item.ToolCalls); err != nil {
				return nil, fmt.Errorf("解析消息工具调用: %w", err)
			}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// CreateRunOptions distinguishes top-level runs from delegated subtask runs.
type CreateRunOptions struct {
	ParentRunID string
	Depth       int
}

// CreateRun starts a run for a session and resolves the owning Agent's
// current Profile inside the same transaction: the run row records the
// profile id, revision and a full content snapshot, so in-flight approvals
// and historical runs read exactly what they started with, even after the
// Profile is later edited or deleted. The returned AgentProfile is the
// snapshot the engine must execute with.
func (s *Store) CreateRun(ctx context.Context, sessionID string) (Run, AgentProfile, error) {
	return s.CreateRunWithOptions(ctx, sessionID, CreateRunOptions{})
}

func (s *Store) CreateRunWithOptions(ctx context.Context, sessionID string, opts CreateRunOptions) (Run, AgentProfile, error) {
	id, err := newID()
	if err != nil {
		return Run{}, AgentProfile{}, err
	}
	item := Run{ID: id, SessionID: sessionID, ParentRunID: opts.ParentRunID, Depth: opts.Depth, Status: "running", CreatedAt: now(), UpdatedAt: now()}
	var resolved AgentProfile
	// The single-active-run rule and the profile resolution are checked in
	// the same transaction as the insert so a session can never carry two
	// running runs and the snapshot is exactly the latest committed profile.
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		var active int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs WHERE session_id = ? AND status = 'running'`, sessionID).Scan(&active); err != nil {
			return err
		}
		if active > 0 {
			return errors.New("该会话已有运行中的任务")
		}
		var agentID, profileID, name, configJSON, policyJSON string
		var revision int
		if err := tx.QueryRowContext(ctx, `SELECT a.id, a.profile_id, p.name, p.config, p.revision, a.policy FROM sessions s JOIN agents a ON a.id = s.agent_id JOIN resolved_profiles p ON p.id = a.profile_id WHERE s.id = ?`, sessionID).
			Scan(&agentID, &profileID, &name, &configJSON, &revision, &policyJSON); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errors.New("会话没有可用的 Agent 或 Profile")
			}
			return err
		}
		var data ProfileData
		if err := json.Unmarshal([]byte(configJSON), &data); err != nil {
			return fmt.Errorf("解析 Profile 配置: %w", err)
		}
		var policy AgentPolicy
		if err := json.Unmarshal([]byte(policyJSON), &policy); err != nil {
			return fmt.Errorf("解析 Agent 策略: %w", err)
		}
		snapshot, err := json.Marshal(data)
		if err != nil {
			return err
		}
		var parent any
		if opts.ParentRunID != "" {
			parent = opts.ParentRunID
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO runs (id, session_id, agent_id, profile_id, profile_revision, profile_snapshot, parent_run_id, depth, status, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			item.ID, sessionID, agentID, profileID, revision, string(snapshot), parent, opts.Depth, item.Status, item.CreatedAt, item.UpdatedAt); err != nil {
			return err
		}
		item.AgentID, item.ProfileID, item.ProfileRevision = agentID, profileID, revision
		resolved = AgentProfile{
			AgentID: agentID, ProfileID: profileID, ProfileName: name,
			ProfileData: data, Policy: policy, ProfileRevision: revision,
		}
		return nil
	})
	if err != nil {
		return Run{}, AgentProfile{}, err
	}
	var agentName string
	err = s.db.QueryRowContext(ctx, `SELECT name FROM agents WHERE id = ?`, item.AgentID).Scan(&agentName)
	if err == nil {
		resolved.AgentName = agentName
	}
	return item, resolved, nil
}

// ProfileSnapshot returns the resolved profile content a finished or running
// run recorded at start. The second result reports whether a snapshot exists
// (runs from before v8 carry none and fall back to profile lookups).
func (s *Store) ProfileSnapshot(ctx context.Context, runID string) (ProfileData, bool, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT profile_snapshot FROM runs WHERE id = ?`, runID).Scan(&raw)
	if err != nil {
		return ProfileData{}, false, err
	}
	if raw == "" {
		return ProfileData{}, false, nil
	}
	var data ProfileData
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return ProfileData{}, false, fmt.Errorf("解析运行快照: %w", err)
	}
	return data, true, nil
}

// runTransitions guards the run lifecycle.
var runTransitions = map[string][]string{
	"running": {"completed", "cancelled", "failed", "interrupted"},
}

func (s *Store) SetRunStatus(ctx context.Context, id, status, detail string) error {
	run, err := s.GetRun(ctx, id)
	if err != nil {
		return err
	}
	allowed, ok := runTransitions[run.Status]
	if !ok {
		return fmt.Errorf("任务状态 %q 不能变更为 %q", run.Status, status)
	}
	valid := false
	for _, next := range allowed {
		if next == status {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("任务状态 %q 不能变更为 %q", run.Status, status)
	}
	// Run state and the state of its in-flight tool calls commit in one
	// transaction: a finished run never leaves un-dispatched or in-flight
	// calls behind without an explicit, persisted disposition.
	return s.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE runs SET status = ?, error = ?, updated_at = ? WHERE id = ? AND status = ?`, status, detail, now(), id, run.Status)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return errors.New("任务不存在或已结束")
		}
		return sweepRunArtifacts(ctx, tx, id)
	})
}

// sweepRunArtifacts gives every non-terminal tool call and step of a finished
// run an explicit persisted state. Pending calls were never dispatched, so
// they become cancelled; executing calls may have taken effect, so they
// become execution_unknown and block further progress until resolved.
func sweepRunArtifacts(ctx context.Context, tx *sql.Tx, runID string) error {
	ts := now()
	if _, err := tx.ExecContext(ctx, `UPDATE tool_calls SET state='cancelled', error='任务已结束，调用未执行', updated_at=? WHERE run_id=? AND state IN ('pending','approved')`, ts, runID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tool_calls SET state='execution_unknown', error='任务结束时工具仍在执行，结果未知', updated_at=? WHERE run_id=? AND state='executing'`, ts, runID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE steps SET status='cancelled', error='任务已结束，调用未执行', updated_at=? WHERE run_id=? AND kind='tool' AND status IN ('pending','running') AND id IN (SELECT step_id FROM tool_calls WHERE run_id=? AND state='cancelled')`, ts, runID, runID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE steps SET status='paused', error='结果未知，等待处置', updated_at=? WHERE run_id=? AND kind='tool' AND status IN ('pending','running') AND id IN (SELECT step_id FROM tool_calls WHERE run_id=? AND state='execution_unknown')`, ts, runID, runID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE steps SET status='failed', error='任务已结束，模型请求未完成', updated_at=? WHERE run_id=? AND kind='model' AND status='running'`, ts, runID); err != nil {
		return err
	}
	return nil
}

const runColumns = `id, session_id, agent_id, profile_id, profile_revision, parent_run_id, depth, status, error, created_at, updated_at`

func scanRun(scanner interface{ Scan(...any) error }) (Run, error) {
	var item Run
	var parent sql.NullString
	err := scanner.Scan(&item.ID, &item.SessionID, &item.AgentID, &item.ProfileID, &item.ProfileRevision, &parent, &item.Depth, &item.Status, &item.Error, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return Run{}, err
	}
	item.ParentRunID = parent.String
	return item, nil
}

func (s *Store) GetRun(ctx context.Context, id string) (Run, error) {
	return scanRun(s.db.QueryRowContext(ctx, `SELECT `+runColumns+` FROM runs WHERE id = ?`, id))
}

func (s *Store) ListRuns(ctx context.Context, sessionID string) ([]Run, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+runColumns+` FROM runs WHERE session_id = ? ORDER BY created_at DESC, id DESC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Run{}
	for rows.Next() {
		item, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ListChildRuns returns the delegated subtask runs of one parent run, oldest
// first; the task tree renders them under their parent.
func (s *Store) ListChildRuns(ctx context.Context, parentRunID string) ([]Run, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+runColumns+` FROM runs WHERE parent_run_id = ? ORDER BY created_at, id`, parentRunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Run{}
	for rows.Next() {
		item, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// AncestorRunIDs walks the parent chain of a run, oldest ancestor first, and
// includes the run itself. Delegation uses it to refuse cycles.
func (s *Store) AncestorRunIDs(ctx context.Context, runID string) ([]string, error) {
	chain := []string{runID}
	seen := map[string]bool{runID: true}
	for {
		current := chain[0]
		var parent sql.NullString
		err := s.db.QueryRowContext(ctx, `SELECT parent_run_id FROM runs WHERE id = ?`, current).Scan(&parent)
		if err != nil {
			return nil, err
		}
		if !parent.Valid || parent.String == "" {
			return chain, nil
		}
		if seen[parent.String] {
			return chain, nil
		}
		seen[parent.String] = true
		chain = append([]string{parent.String}, chain...)
	}
}

// FirstUserMessage returns the session's first user message — the task text a
// delegated subtask was started with — for duplicate-delegation detection.
func (s *Store) FirstUserMessage(ctx context.Context, sessionID string) (string, error) {
	var content string
	err := s.db.QueryRowContext(ctx, `SELECT content FROM messages WHERE session_id = ? AND role = 'user' ORDER BY seq LIMIT 1`, sessionID).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return content, err
}
