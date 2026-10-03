package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"icepick/internal/config"
)

// legacyAgentProfile is the pre-v8 on-disk shape of agent_profiles.config:
// instance identity, combination and machine policy in one JSON record. It
// exists only so the v8 migration can split each record into a Profile and an
// Agent without losing fields.
type legacyAgentProfile struct {
	Description       string           `json:"description"`
	SystemPrompt      string           `json:"systemPrompt"`
	Model             config.Model     `json:"model"`
	AllMCPServers     bool             `json:"allMcpServers"`
	EnabledMCPServers []string         `json:"enabledMcpServers"`
	AllowedTools      []string         `json:"allowedTools"`
	AllSkills         bool             `json:"allSkills"`
	EnabledSkills     []string         `json:"enabledSkills"`
	Permissions       PermissionScope  `json:"permissions"`
	ApprovalMode      string           `json:"approvalMode"`
	Delegation        DelegationPolicy `json:"delegation"`
	ContextBudget     int              `json:"contextBudget"`
}

// migrateProfilesV8 splits agent_profiles into profiles (the shareable
// combination) and agents (the stable instance: id, name, profile reference
// and machine policy). Every old row becomes exactly one pair; sessions,
// messages and runs keep their agent_id ownership, and runs additionally
// record which profile they used. The migration refuses to finish on any
// ownership mismatch instead of leaving history reassigned.
func migrateProfilesV8(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `CREATE TABLE profiles (id TEXT PRIMARY KEY, name TEXT NOT NULL, config TEXT NOT NULL, revision INTEGER NOT NULL, is_default INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("Profile 拆分迁移: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE agents (id TEXT PRIMARY KEY, name TEXT NOT NULL, profile_id TEXT NOT NULL REFERENCES profiles(id), policy TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("Profile 拆分迁移: %w", err)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, name, config, revision, created_at, updated_at FROM agent_profiles`)
	if err != nil {
		return fmt.Errorf("Profile 拆分迁移: %w", err)
	}
	type pair struct {
		id         string
		agentName  string
		profileNam string
		data       ProfileData
		policy     AgentPolicy
		revision   int
		createdAt  string
		updatedAt  string
	}
	pairs := []pair{}
	for rows.Next() {
		var id, name, configJSON string
		var revision int
		var createdAt, updatedAt string
		if err := rows.Scan(&id, &name, &configJSON, &revision, &createdAt, &updatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("Profile 拆分迁移: %w", err)
		}
		var old legacyAgentProfile
		if err := json.Unmarshal([]byte(configJSON), &old); err != nil {
			rows.Close()
			return fmt.Errorf("Profile 拆分迁移: 解析 %s: %w", id, err)
		}
		profileName := name
		if id == DefaultProfileID {
			profileName = "默认 Profile"
		}
		pairs = append(pairs, pair{
			id: id, agentName: name, profileNam: profileName,
			data: ProfileData{
				Description:       old.Description,
				SystemPrompt:      old.SystemPrompt,
				Model:             old.Model,
				AllMCPServers:     old.AllMCPServers,
				EnabledMCPServers: old.EnabledMCPServers,
				AllowedTools:      old.AllowedTools,
				AllSkills:         old.AllSkills,
				EnabledSkills:     old.EnabledSkills,
				Delegation:        DelegationPolicy{Enabled: old.Delegation.Enabled, MaxDepth: old.Delegation.MaxDepth},
				ContextBudget:     old.ContextBudget,
			},
			policy:   AgentPolicy{ApprovalMode: old.ApprovalMode, Permissions: old.Permissions},
			revision: revision, createdAt: createdAt, updatedAt: updatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("Profile 拆分迁移: %w", err)
	}
	rows.Close()
	for _, item := range pairs {
		configJSON, err := json.Marshal(item.data)
		if err != nil {
			return fmt.Errorf("Profile 拆分迁移: %w", err)
		}
		policyJSON, err := json.Marshal(item.policy)
		if err != nil {
			return fmt.Errorf("Profile 拆分迁移: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO profiles (id, name, config, revision, is_default, created_at, updated_at) VALUES (?,?,?,?,?,?,?)`,
			item.id, item.profileNam, string(configJSON), item.revision, item.id == DefaultProfileID, item.createdAt, item.updatedAt); err != nil {
			return fmt.Errorf("Profile 拆分迁移: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO agents (id, name, profile_id, policy, created_at, updated_at) VALUES (?,?,?,?,?,?)`,
			item.id, item.agentName, item.id, string(policyJSON), item.createdAt, item.updatedAt); err != nil {
			return fmt.Errorf("Profile 拆分迁移: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE runs ADD COLUMN profile_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("Profile 拆分迁移: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE runs ADD COLUMN profile_snapshot TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("Profile 拆分迁移: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE runs SET profile_id = (SELECT profile_id FROM agents WHERE agents.id = runs.agent_id)`); err != nil {
		return fmt.Errorf("Profile 拆分迁移: %w", err)
	}
	for _, statement := range []struct {
		sql string
	}{
		{`DROP TRIGGER IF EXISTS sessions_agent_insert`},
		{`DROP TRIGGER IF EXISTS sessions_agent_update`},
		{`CREATE TRIGGER sessions_agent_insert BEFORE INSERT ON sessions WHEN NOT EXISTS (SELECT 1 FROM agents WHERE id = NEW.agent_id) BEGIN SELECT RAISE(ABORT, 'unknown agent'); END`},
		{`CREATE TRIGGER sessions_agent_update BEFORE UPDATE OF agent_id ON sessions WHEN NOT EXISTS (SELECT 1 FROM agents WHERE id = NEW.agent_id) BEGIN SELECT RAISE(ABORT, 'unknown agent'); END`},
	} {
		if _, err := tx.ExecContext(ctx, statement.sql); err != nil {
			return fmt.Errorf("Profile 拆分迁移: %w", err)
		}
	}
	// Ownership verification before the old table is dropped: every session
	// must still resolve to a real agent, and every agent must reference its
	// profile. FK violations here mean the migration must not commit.
	if problems, err := foreignKeyProblems(ctx, tx); err != nil {
		return fmt.Errorf("Profile 拆分迁移: %w", err)
	} else if len(problems) > 0 {
		return fmt.Errorf("Profile 拆分迁移: 外键校验失败: %s", problems)
	}
	var missing int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE agent_id NOT IN (SELECT id FROM agents)`).Scan(&missing); err != nil {
		return fmt.Errorf("Profile 拆分迁移: %w", err)
	}
	if missing > 0 {
		return fmt.Errorf("Profile 拆分迁移: %d 个会话失去 Agent 归属", missing)
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE agent_profiles`); err != nil {
		return fmt.Errorf("Profile 拆分迁移: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `PRAGMA user_version = 8`); err != nil {
		return fmt.Errorf("Profile 拆分迁移: %w", err)
	}
	return nil
}

// foreignKeyProblems runs PRAGMA foreign_key_check and renders the first
// violations; an empty result means the schema is consistent.
func foreignKeyProblems(ctx context.Context, tx *sql.Tx) (string, error) {
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	problems := ""
	for rows.Next() {
		var table string
		var rowid int
		var parent, fkid int
		if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			return "", err
		}
		if problems == "" {
			problems = table
		}
	}
	return problems, rows.Err()
}
