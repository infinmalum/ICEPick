package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"icepick/internal/config"
)

const DefaultAgentID = "chat"

type PermissionScope struct {
	WorkspacePaths []string `json:"workspacePaths"`
	Commands       []string `json:"commands"`
	NetworkHosts   []string `json:"networkHosts"`
}

type DelegationPolicy struct {
	Enabled        bool     `json:"enabled"`
	TargetAgentIDs []string `json:"targetAgentIds"`
	MaxDepth       int      `json:"maxDepth"`
}

type AgentProfile struct {
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	Description   string           `json:"description"`
	SystemPrompt  string           `json:"systemPrompt"`
	Model         config.Model     `json:"model"`
	AllowedTools  []string         `json:"allowedTools"`
	EnabledSkills []string         `json:"enabledSkills"`
	Permissions   PermissionScope  `json:"permissions"`
	ApprovalMode  string           `json:"approvalMode"`
	Delegation    DelegationPolicy `json:"delegation"`
	ContextBudget int              `json:"contextBudget"`
	Revision      int              `json:"revision"`
	Preset        string           `json:"preset"`
	CreatedAt     string           `json:"createdAt"`
	UpdatedAt     string           `json:"updatedAt"`
}

func BlankProfile() AgentProfile {
	return AgentProfile{AllowedTools: []string{}, EnabledSkills: []string{}, ApprovalMode: "manual", ContextBudget: 8192}
}

func presetProfile(id string) AgentProfile {
	p := BlankProfile()
	p.ID, p.Preset = id, id
	switch id {
	case "chat":
		p.Name = "Chat"
		p.Description = "日常对话"
		p.SystemPrompt = "你是 ICEPick 的 Chat 助手。回答清楚、准确；无法完成时如实说明。"
		p.AllowedTools = []string{"local__current_time"}
	case "work":
		p.Name = "Work"
		p.Description = "使用已授权工具完成任务"
		p.SystemPrompt = "你是 ICEPick 的 Work 助手。只使用已授权工具；无法完成时如实说明。"
		p.AllowedTools = []string{"local__current_time"}
		p.Delegation = DelegationPolicy{Enabled: true, MaxDepth: 1}
	}
	return p
}

func validateProfile(p AgentProfile) error {
	if strings.TrimSpace(p.Name) == "" || len(p.Name) > 100 || len(p.SystemPrompt) > 32000 {
		return errors.New("Agent 名称或系统提示词无效")
	}
	if p.ApprovalMode != "manual" && p.ApprovalMode != "semi" && p.ApprovalMode != "automatic" {
		return errors.New("无效的审批模式")
	}
	if p.Model.Provider != "" {
		if err := p.Model.Validate(); err != nil {
			return err
		}
	}
	if p.ContextBudget < 1024 || p.ContextBudget > 1000000 || p.Delegation.MaxDepth < 0 || p.Delegation.MaxDepth > 4 {
		return errors.New("上下文预算或委派深度无效")
	}
	for _, name := range p.AllowedTools {
		if !strings.Contains(name, "__") {
			return fmt.Errorf("无效的工具名 %q", name)
		}
	}
	return nil
}

func (s *Store) migrateProfiles() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, query := range []string{
		`CREATE TABLE agent_profiles (id TEXT PRIMARY KEY, name TEXT NOT NULL, config TEXT NOT NULL, revision INTEGER NOT NULL, preset TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`ALTER TABLE sessions ADD COLUMN agent_id TEXT NOT NULL DEFAULT 'chat'`,
		`CREATE INDEX sessions_agent_order ON sessions(agent_id, created_at, id)`,
		`CREATE TRIGGER sessions_agent_insert BEFORE INSERT ON sessions WHEN NOT EXISTS (SELECT 1 FROM agent_profiles WHERE id = NEW.agent_id) BEGIN SELECT RAISE(ABORT, 'unknown agent'); END`,
		`CREATE TRIGGER sessions_agent_update BEFORE UPDATE OF agent_id ON sessions WHEN NOT EXISTS (SELECT 1 FROM agent_profiles WHERE id = NEW.agent_id) BEGIN SELECT RAISE(ABORT, 'unknown agent'); END`,
	} {
		if _, err := tx.Exec(query); err != nil {
			return fmt.Errorf("Agent 数据库迁移: %w", err)
		}
	}
	for _, id := range []string{"chat", "work"} {
		p := presetProfile(id)
		p.Revision, p.CreatedAt, p.UpdatedAt = 1, now(), now()
		data, err := json.Marshal(p)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO agent_profiles (id,name,config,revision,preset,created_at,updated_at) VALUES (?,?,?,?,?,?,?)`, p.ID, p.Name, string(data), p.Revision, p.Preset, p.CreatedAt, p.UpdatedAt); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`PRAGMA user_version = 3`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) migrateAgentRecords() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, query := range []string{
		`ALTER TABLE messages ADD COLUMN agent_id TEXT NOT NULL DEFAULT 'chat'`,
		`ALTER TABLE runs ADD COLUMN agent_id TEXT NOT NULL DEFAULT 'chat'`,
		`ALTER TABLE runs ADD COLUMN profile_revision INTEGER NOT NULL DEFAULT 1`,
		`UPDATE messages SET agent_id=(SELECT agent_id FROM sessions WHERE sessions.id=messages.session_id)`,
		`UPDATE runs SET agent_id=(SELECT agent_id FROM sessions WHERE sessions.id=runs.session_id)`,
		`CREATE INDEX messages_agent_session_order ON messages(agent_id, session_id, created_at, id)`,
		`CREATE INDEX runs_agent_session_order ON runs(agent_id, session_id, created_at, id)`,
		`CREATE TRIGGER messages_agent_insert BEFORE INSERT ON messages WHEN NEW.agent_id != (SELECT agent_id FROM sessions WHERE id=NEW.session_id) BEGIN SELECT RAISE(ABORT, 'message agent mismatch'); END`,
		`CREATE TRIGGER messages_agent_update BEFORE UPDATE OF agent_id,session_id ON messages WHEN NEW.agent_id != (SELECT agent_id FROM sessions WHERE id=NEW.session_id) BEGIN SELECT RAISE(ABORT, 'message agent mismatch'); END`,
		`CREATE TRIGGER runs_agent_insert BEFORE INSERT ON runs WHEN NEW.agent_id != (SELECT agent_id FROM sessions WHERE id=NEW.session_id) BEGIN SELECT RAISE(ABORT, 'run agent mismatch'); END`,
		`CREATE TRIGGER runs_agent_update BEFORE UPDATE OF agent_id,session_id ON runs WHEN NEW.agent_id != (SELECT agent_id FROM sessions WHERE id=NEW.session_id) BEGIN SELECT RAISE(ABORT, 'run agent mismatch'); END`,
		`CREATE TRIGGER sessions_agent_reassign BEFORE UPDATE OF agent_id ON sessions WHEN EXISTS (SELECT 1 FROM messages WHERE session_id=OLD.id) OR EXISTS (SELECT 1 FROM runs WHERE session_id=OLD.id) BEGIN SELECT RAISE(ABORT, 'session with history cannot change agent'); END`,
		`PRAGMA user_version = 4`,
	} {
		if _, err := tx.Exec(query); err != nil {
			return fmt.Errorf("Agent 记录迁移: %w", err)
		}
	}
	return tx.Commit()
}

func (s *Store) ListAgents(ctx context.Context) ([]AgentProfile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT config FROM agent_profiles ORDER BY CASE id WHEN 'chat' THEN 0 WHEN 'work' THEN 1 ELSE 2 END, created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AgentProfile{}
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var p AgentProfile
		if err := json.Unmarshal([]byte(data), &p); err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}

func (s *Store) GetAgent(ctx context.Context, id string) (AgentProfile, error) {
	var data string
	if err := s.db.QueryRowContext(ctx, `SELECT config FROM agent_profiles WHERE id = ?`, id).Scan(&data); err != nil {
		return AgentProfile{}, err
	}
	var p AgentProfile
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		return AgentProfile{}, err
	}
	return p, nil
}

func (s *Store) CreateAgent(ctx context.Context, fromID string, p AgentProfile) (AgentProfile, error) {
	if fromID != "" {
		source, err := s.GetAgent(ctx, fromID)
		if err != nil {
			return AgentProfile{}, err
		}
		p = source
	} else if p.ApprovalMode == "" {
		p = BlankProfile()
	}
	id, err := newID()
	if err != nil {
		return AgentProfile{}, err
	}
	p.ID, p.Revision, p.Preset, p.CreatedAt, p.UpdatedAt = id, 1, "", now(), now()
	if fromID != "" {
		p.Name += " 副本"
	}
	if err := validateProfile(p); err != nil {
		return AgentProfile{}, err
	}
	data, err := json.Marshal(p)
	if err != nil {
		return AgentProfile{}, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO agent_profiles (id,name,config,revision,preset,created_at,updated_at) VALUES (?,?,?,?,?,?,?)`, p.ID, p.Name, string(data), p.Revision, p.Preset, p.CreatedAt, p.UpdatedAt)
	return p, err
}

func (s *Store) UpdateAgent(ctx context.Context, p AgentProfile) (AgentProfile, error) {
	if p.ID == "" || p.Revision < 1 {
		return AgentProfile{}, errors.New("Agent ID 或版本无效")
	}
	old, err := s.GetAgent(ctx, p.ID)
	if err != nil {
		return AgentProfile{}, err
	}
	if old.Revision != p.Revision {
		return AgentProfile{}, errors.New("Agent 已被其他操作修改，请刷新后重试")
	}
	p.Revision++
	p.Preset, p.CreatedAt, p.UpdatedAt = old.Preset, old.CreatedAt, now()
	if err := validateProfile(p); err != nil {
		return AgentProfile{}, err
	}
	data, err := json.Marshal(p)
	if err != nil {
		return AgentProfile{}, err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE agent_profiles SET name=?,config=?,revision=?,updated_at=? WHERE id=? AND revision=?`, p.Name, string(data), p.Revision, p.UpdatedAt, p.ID, old.Revision)
	if err != nil {
		return AgentProfile{}, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return AgentProfile{}, err
	}
	if n == 0 {
		return AgentProfile{}, errors.New("Agent 已被其他操作修改，请刷新后重试")
	}
	return p, nil
}

func (s *Store) RestorePreset(ctx context.Context, id string, revision int) (AgentProfile, error) {
	if id != "chat" && id != "work" {
		return AgentProfile{}, errors.New("此 Agent 没有内置预设")
	}
	old, err := s.GetAgent(ctx, id)
	if err != nil {
		return AgentProfile{}, err
	}
	if old.Revision != revision {
		return AgentProfile{}, errors.New("Agent 已被其他操作修改，请刷新后重试")
	}
	p := presetProfile(id)
	p.Revision = revision
	return s.UpdateAgent(ctx, p)
}

func (s *Store) AgentForSession(ctx context.Context, sessionID string) (AgentProfile, error) {
	session, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return AgentProfile{}, err
	}
	return s.GetAgent(ctx, session.AgentID)
}
