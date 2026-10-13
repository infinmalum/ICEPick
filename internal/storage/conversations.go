package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

type LocalContext struct {
	ID     string      `json:"id"`
	Name   string      `json:"name"`
	Policy AgentPolicy `json:"policy"`
}

type Conversation struct {
	Kind            string `json:"kind"`
	ID              string `json:"id"`
	ContextID       string `json:"contextId"`
	ProfileID       string `json:"profileId"`
	ProfileName     string `json:"profileName"`
	Title           string `json:"title"`
	CreatedAt       string `json:"createdAt"`
	ProfileArchived bool   `json:"profileArchived"`
}

func (s *Store) GetLocalContext(ctx context.Context, id string) (LocalContext, error) {
	var item LocalContext
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT id,name,policy FROM contexts WHERE id=?`, id).Scan(&item.ID, &item.Name, &raw)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &item.Policy)
	}
	return item, err
}

func activeProfile(ctx context.Context, tx *sql.Tx, id string) error {
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM profiles WHERE id=? AND archived_at IS NULL`, id).Scan(&n); err != nil {
		return err
	}
	if n != 1 {
		return errors.New("Profile 不存在或已删除，请选择可用 Profile")
	}
	return nil
}

// The context and session are created atomically. reuseSession is explicit;
// an existing Profile on its own never causes history or grants to be shared.
func (s *Store) CreateConversation(ctx context.Context, profileID, reuseSession, title string) (Conversation, error) {
	if strings.TrimSpace(title) == "" {
		title = "新对话"
	}
	if len(title) > 500 {
		return Conversation{}, errors.New("对话标题过长")
	}
	sessionID, err := newID()
	if err != nil {
		return Conversation{}, err
	}
	contextID, err := newID()
	if err != nil {
		return Conversation{}, err
	}
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		if err := activeProfile(ctx, tx, profileID); err != nil {
			return err
		}
		if reuseSession != "" {
			if err := tx.QueryRowContext(ctx, `SELECT agent_id FROM sessions WHERE id=? AND kind='normal'`, reuseSession).Scan(&contextID); err != nil {
				return errors.New("找不到可复用的普通对话")
			}
		} else {
			policy, _ := json.Marshal(AgentPolicy{ApprovalMode: "semi"})
			if _, err := tx.ExecContext(ctx, `INSERT INTO agents(id,name,profile_id,policy,created_at,updated_at) VALUES(?,?,?,?,?,?)`, contextID, title, profileID, string(policy), now(), now()); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO sessions(id,agent_id,kind,title,created_at,profile_id) VALUES(?,?,'normal',?,?,?)`, sessionID, contextID, title, now(), profileID)
		return err
	})
	if err != nil {
		return Conversation{}, err
	}
	return s.GetConversation(ctx, sessionID)
}

const conversationSelect = `SELECT s.id,s.agent_id,COALESCE(NULLIF(s.profile_id,''),a.profile_id),p.name,s.title,s.created_at,p.archived_at IS NOT NULL,s.kind FROM sessions s JOIN agents a ON a.id=s.agent_id JOIN profiles p ON p.id=COALESCE(NULLIF(s.profile_id,''),a.profile_id)`

func scanConversation(row interface{ Scan(...any) error }) (Conversation, error) {
	var c Conversation
	err := row.Scan(&c.ID, &c.ContextID, &c.ProfileID, &c.ProfileName, &c.Title, &c.CreatedAt, &c.ProfileArchived, &c.Kind)
	return c, err
}

func (s *Store) GetConversation(ctx context.Context, id string) (Conversation, error) {
	return scanConversation(s.db.QueryRowContext(ctx, conversationSelect+` WHERE s.id=?`, id))
}

func (s *Store) ListConversations(ctx context.Context) ([]Conversation, error) {
	rows, err := s.db.QueryContext(ctx, conversationSelect+` WHERE s.kind='normal' ORDER BY s.created_at DESC,s.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Conversation{}
	for rows.Next() {
		item, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) SelectConversationProfile(ctx context.Context, id, profileID string) (Conversation, error) {
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if err := activeProfile(ctx, tx, profileID); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE sessions SET profile_id=? WHERE id=? AND kind='normal'`, profileID, id)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("找不到普通对话")
		}
		return nil
	})
	if err != nil {
		return Conversation{}, err
	}
	return s.GetConversation(ctx, id)
}

// Archive preserves references, histories, grants and in-flight snapshots.
// New runs require an explicit selection after archive, never a silent fallback.
func (s *Store) ArchiveProfile(ctx context.Context, id string) error {
	return s.ArchiveProfileReplacing(ctx, id, "", false, nil)
}

func (s *Store) CreateTemplateProfile(ctx context.Context, templateID, name string) (Profile, error) {
	t, err := ProfileTemplateByID(templateID)
	if err != nil {
		return Profile{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = t.Name
	}
	t.Data.TemplateID, t.Data.TemplateVersion = t.ID, t.Version
	return s.CreateProfile(ctx, name, t.Data)
}

func (s *Store) RestoreTemplateProfile(ctx context.Context, id string, revision int, groups []string) (Profile, error) {
	p, err := s.GetProfile(ctx, id)
	if err != nil {
		return Profile{}, err
	}
	if p.Revision != revision {
		return Profile{}, errors.New("Profile 已被修改，请刷新")
	}
	t, err := ProfileTemplateByID(p.Data.TemplateID)
	if err != nil {
		return Profile{}, err
	}
	if len(groups) == 0 {
		p.Data = t.Data
	} else {
		for _, group := range groups {
			switch group {
			case "prompt":
				p.Data.SystemPrompt = t.Data.SystemPrompt
			case "persona":
				p.Data.Persona = t.Data.Persona
				p.Data.Description = t.Data.Description
			case "model":
				p.Data.Model = t.Data.Model
			case "capabilities":
				p.Data.AllMCPServers = t.Data.AllMCPServers
				p.Data.EnabledMCPServers = t.Data.EnabledMCPServers
				p.Data.AllowedTools = t.Data.AllowedTools
				p.Data.AllSkills = t.Data.AllSkills
				p.Data.EnabledSkills = t.Data.EnabledSkills
				p.Data.Delegation = t.Data.Delegation
			case "runtime":
				p.Data.ContextBudget = t.Data.ContextBudget
				p.Data.MemoryEnabled = t.Data.MemoryEnabled
				p.Data.PlanEnabled = t.Data.PlanEnabled
			case "execution":
				p.Data.Execution = t.Data.Execution
			default:
				return Profile{}, errors.New("未知配置组")
			}
		}
	}
	p.Data.TemplateID, p.Data.TemplateVersion = t.ID, t.Version
	return s.UpdateProfile(ctx, p)
}
