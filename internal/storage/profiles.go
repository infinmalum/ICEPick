package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"icepick/internal/config"
	"icepick/internal/skills"
)

// DefaultProfileID identifies the built-in configurable Profile.
const DefaultProfileID = "default"

// PermissionScope is the machine-local resource authorization of one Context.
type PermissionScope struct {
	Unrestricted   bool     `json:"unrestricted"`
	WorkspacePaths []string `json:"workspacePaths"`
	Commands       []string `json:"commands"`
	NetworkHosts   []string `json:"networkHosts"`
}

// DelegationPolicy is the Profile-level switch for worker delegation; the
// worker candidates and execution limits are captured when a root run starts.
// Workers are selected from Profile slots. Zero concurrency/time use host defaults.
type DelegationPolicy struct {
	Enabled        bool `json:"enabled"`
	MaxDepth       int  `json:"maxDepth"`
	MaxConcurrency int  `json:"maxConcurrency"`
	MaxSeconds     int  `json:"maxSeconds"`
}

const (
	// DefaultDelegationConcurrency caps concurrent subtask runs per parent.
	DefaultDelegationConcurrency = 2
	// maxDelegationSeconds mirrors the engine's per-run timeout (10
	// minutes): a subtask can never outlive the run limit, so the profile
	// budget only tightens it.
	maxDelegationSeconds = 600
	// MaxDelegationDepth bounds recursive delegation.
	MaxDelegationDepth = 4
)

// ProfileData is the shareable combination a Profile carries: prompt, model
// binding, capability selection and budgets. It never holds instance data,
// history, secrets or authorization, so copying a Profile is safe by
// construction.
type ProfileData struct {
	SchemaVersion     int              `json:"schemaVersion"`
	Execution         Execution        `json:"execution"`
	TemplateID        string           `json:"templateId,omitempty"`
	TemplateVersion   int              `json:"templateVersion,omitempty"`
	Persona           string           `json:"persona"`
	Description       string           `json:"description"`
	SystemPrompt      string           `json:"systemPrompt"`
	Model             config.Model     `json:"model"`
	AllMCPServers     bool             `json:"allMcpServers"`
	EnabledMCPServers []string         `json:"enabledMcpServers"`
	AllowedTools      []string         `json:"allowedTools"`
	AllSkills         bool             `json:"allSkills"`
	EnabledSkills     []string         `json:"enabledSkills"`
	Delegation        DelegationPolicy `json:"delegation"`
	ContextBudget     int              `json:"contextBudget"`
	// MemoryEnabled is the Profile's preference for per-Context long-term
	// memory: retrieval at run start and the memory_save tool. It only takes
	// effect together with the Context's own authorization.
	MemoryEnabled bool `json:"memoryEnabled"`
	// PlanEnabled is the Profile's preference for the optional plan
	// capability: the update_plan tool and the plan prompt fragment.
	PlanEnabled bool `json:"planEnabled"`
}

// ContextPolicy is local authorization, independent of Profile configuration.
type ContextPolicy struct {
	ApprovalMode string          `json:"approvalMode"`
	Permissions  PermissionScope `json:"permissions"`
	// Memory authorizes this Context to use long-term memory at all. It
	// defaults to false: existing and new Contexts opt in explicitly.
	Memory bool `json:"memory"`
}

// Profile is one named, versioned combination. Editing bumps the revision and
// affects every referencing Context's subsequent runs.
type Profile struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Data      ProfileData `json:"data"`
	Revision  int         `json:"revision"`
	IsDefault bool        `json:"isDefault"`
	CreatedAt string      `json:"createdAt"`
	UpdatedAt string      `json:"updatedAt"`
}

// RunProfile is the resolved combination one Run executes with: the
// Context's identity and machine policy plus the Profile content snapshot.
type RunProfile struct {
	Resources       *RunResources     `json:"-"`
	ResourcesFrozen bool              `json:"-"`
	FrozenSkills    []skills.Snapshot `json:"-"`
	ContextID       string            `json:"contextId"`
	ContextName     string            `json:"contextName"`
	ProfileID       string            `json:"profileId"`
	ProfileName     string            `json:"profileName"`
	ProfileData
	Policy          ContextPolicy `json:"policy"`
	ProfileRevision int           `json:"profileRevision"`
}

// BlankProfile is the built-in blank template: nothing enabled, everything
// explicit, manual approvals.
func BlankProfile() ProfileData {
	return ProfileData{SchemaVersion: ProfileSchemaVersion, Execution: Execution{Workflow: "direct"}, AllowedTools: []string{}, EnabledSkills: []string{}, ContextBudget: 8192}
}

// defaultProfileData is the built-in default combination: a short generic
// instruction, shared catalogs enabled, no role presets. Memory and plan
// bookkeeping are part of the shipped default; upgrading databases keep
// their stored runtime component until the user edits or restores it.
func defaultProfileData() ProfileData {
	d := BlankProfile()
	d.Description = "通用助手；使用已接入的工具和 Skills"
	d.SystemPrompt = "你是 ICEPick 助手。使用可用工具完成用户任务；无法完成时如实说明。"
	d.AllMCPServers = true
	d.AllSkills = true
	d.Delegation = DelegationPolicy{Enabled: true, MaxDepth: 1}
	d.MemoryEnabled = true
	d.PlanEnabled = true
	return d
}

// ValidateProfileData checks the stored combination shape; the share
// importer runs it before persisting an imported profile.
func ValidateProfileData(d ProfileData) error {
	return validateProfileData(d)
}

func validateProfileData(d ProfileData) error {
	if err := validateExecution(d); err != nil {
		return err
	}
	if len(d.Persona) > 32000 || len(d.SystemPrompt) > 32000 || len(d.Description) > 500 {
		return errors.New("系统提示词或说明过长")
	}
	if d.Model.Provider != "" {
		if err := d.Model.Validate(); err != nil {
			return err
		}
	}

	if d.Delegation.MaxDepth < 0 || d.Delegation.MaxDepth > MaxDelegationDepth {
		return errors.New("上下文预算或委派深度无效")
	}
	if d.Delegation.MaxConcurrency < 0 || d.Delegation.MaxConcurrency > 8 {
		return errors.New("委派并发上限无效")
	}
	if d.Delegation.MaxSeconds < 0 || (d.Delegation.MaxSeconds > 0 && d.Delegation.MaxSeconds < 30) || d.Delegation.MaxSeconds > maxDelegationSeconds {
		return errors.New("委派时间预算无效（30–600 秒）")
	}
	if d.ContextBudget < 1024 || d.ContextBudget > 1000000 {
		return errors.New("上下文预算或委派深度无效")
	}
	for _, name := range d.AllowedTools {
		if !strings.Contains(name, "__") {
			return fmt.Errorf("无效的工具名 %q", name)
		}
	}
	for _, name := range d.EnabledMCPServers {
		if name == "" || strings.Contains(name, "__") {
			return fmt.Errorf("无效的 MCP 服务名 %q", name)
		}
	}
	return nil
}

func validatePolicy(p ContextPolicy) error {
	if p.ApprovalMode != "manual" && p.ApprovalMode != "semi" && p.ApprovalMode != "automatic" {
		return errors.New("无效的审批模式")
	}

	return nil
}

func (d ProfileData) AllowsTool(name string) bool {
	parts := strings.SplitN(name, "__", 2)
	if len(parts) != 2 || parts[1] == "" {
		return false
	}
	if !d.AllMCPServers {
		found := false
		for _, server := range d.EnabledMCPServers {
			if server == parts[0] {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(d.AllowedTools) == 0 {
		return true
	}
	for _, tool := range d.AllowedTools {
		if tool == name {
			return true
		}
	}
	return false
}

func (d ProfileData) EnablesSkill(name string) bool {
	if d.AllSkills {
		return true
	}
	for _, skill := range d.EnabledSkills {
		if skill == name {
			return true
		}
	}
	return false
}

func scanProfile(scanner interface{ Scan(...any) error }) (Profile, error) {
	var p Profile
	var configJSON string
	if err := scanner.Scan(&p.ID, &p.Name, &configJSON, &p.Revision, &p.IsDefault, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return Profile{}, err
	}
	if err := json.Unmarshal([]byte(configJSON), &p.Data); err != nil {
		return Profile{}, fmt.Errorf("解析 Profile 配置: %w", err)
	}
	return p, nil
}

func (s *Store) ListProfiles(ctx context.Context) ([]Profile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, config, revision, is_default, created_at, updated_at FROM profiles WHERE archived_at IS NULL ORDER BY CASE id WHEN 'default' THEN 0 ELSE 1 END, created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Profile{}
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}

func (s *Store) GetProfile(ctx context.Context, id string) (Profile, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, config, revision, is_default, created_at, updated_at FROM profiles WHERE id = ?`, id)
	p, err := scanProfile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, errors.New("Profile 不存在")
	}
	return p, err
}

// CreateProfile persists a new Profile from the blank template or a copy the
// caller assembled. The new Profile starts at revision 1 and is never the
// built-in default.
func (s *Store) CreateProfile(ctx context.Context, name string, data ProfileData) (Profile, error) {
	data = normalizeProfile(data)
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return Profile{}, errors.New("Profile 名称无效")
	}
	if err := validateProfileData(data); err != nil {
		return Profile{}, err
	}
	id, err := newID()
	if err != nil {
		return Profile{}, err
	}
	created := now()
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		if err := validateWorkerReferences(ctx, tx, id, data); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO profiles(id,name,revision,is_default,created_at,updated_at) VALUES(?,?,1,0,?,?)`, id, name, created, created); err != nil {
			return err
		}
		if err := saveProfileConfig(ctx, tx, id, data); err != nil {
			return err
		}
		return saveProfileRevision(ctx, tx, id, 1, name, data)
	})
	if err != nil {
		return Profile{}, err
	}
	return s.GetProfile(ctx, id)
}

// UpdateProfile publishes component changes in one revision-checked transaction.
// Unchanged components are not rewritten.
func (s *Store) UpdateProfile(ctx context.Context, p Profile) (Profile, error) {
	p.Data = normalizeProfile(p.Data)
	if strings.TrimSpace(p.Name) == "" || len(p.Name) > 100 {
		return Profile{}, errors.New("Profile 名称无效")
	}
	if p.ID == "" || p.Revision < 1 {
		return Profile{}, errors.New("Profile ID 或版本无效")
	}
	old, err := s.GetProfile(ctx, p.ID)
	if err != nil {
		return Profile{}, err
	}
	if old.Revision != p.Revision {
		return Profile{}, errors.New("Profile 已被其他操作修改，请刷新后重试")
	}
	if err := validateProfileData(p.Data); err != nil {
		return Profile{}, err
	}
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		if err := validateWorkerReferences(ctx, tx, p.ID, p.Data); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE profiles SET name=?,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND archived_at IS NULL`, strings.TrimSpace(p.Name), now(), p.ID, p.Revision)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return errors.New("Profile 已被其他操作修改，请刷新后重试")
		}
		if err := saveProfileConfig(ctx, tx, p.ID, p.Data); err != nil {
			return err
		}
		return saveProfileRevision(ctx, tx, p.ID, p.Revision+1, strings.TrimSpace(p.Name), p.Data)
	})
	if err != nil {
		return Profile{}, err
	}
	return s.GetProfile(ctx, p.ID)
}

// RestoreDefaultProfileData resets the built-in default Profile's combination
// to the shipped content. Only the default Profile has a restore source.
func (s *Store) RestoreDefaultProfileData(ctx context.Context, revision int) (Profile, error) {
	old, err := s.GetProfile(ctx, DefaultProfileID)
	if err != nil {
		return Profile{}, errors.New("此 Profile 没有内置预设")
	}
	if old.Revision != revision {
		return Profile{}, errors.New("Profile 已被其他操作修改，请刷新后重试")
	}
	restored := old
	restored.Data = defaultProfileData()
	return s.UpdateProfile(ctx, restored)
}

// DeleteProfile archives the configuration while preserving existing history.
func (s *Store) DeleteProfile(ctx context.Context, id string) error {
	return s.ArchiveProfile(ctx, id)
}

func (s *Store) ResolveRunProfile(ctx context.Context, sessionID string) (RunProfile, error) {
	session, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return RunProfile{}, err
	}
	conversation, err := s.GetConversation(ctx, sessionID)
	if err != nil {
		return RunProfile{}, err
	}
	profile, err := s.GetProfile(ctx, conversation.ProfileID)
	if err != nil {
		return RunProfile{}, err
	}
	a, err := s.GetLocalContext(ctx, session.ContextID)
	if err != nil {
		return RunProfile{}, err
	}
	return RunProfile{ContextID: a.ID, ContextName: a.Name, ProfileID: profile.ID, ProfileName: profile.Name, ProfileData: profile.Data, Policy: a.Policy, ProfileRevision: profile.Revision}, nil
}
