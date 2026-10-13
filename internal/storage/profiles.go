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

const DefaultAgentID = "default"

// DefaultProfileID is the id of the built-in default Profile; the built-in
// default Agent references it.
const DefaultProfileID = "default"

// PermissionScope is the machine-local resource authorization of one Agent.
type PermissionScope struct {
	Unrestricted   bool     `json:"unrestricted"`
	WorkspacePaths []string `json:"workspacePaths"`
	Commands       []string `json:"commands"`
	NetworkHosts   []string `json:"networkHosts"`
}

// DelegationPolicy is the Profile-level switch for sub-Agent delegation; the
// actual target list is bounded by each delegating Agent's own authorization.
// Selection picks how the target is chosen: "model" lets the model pick from
// the allowed list, "pinned" always uses TargetAgentID (a local identity that
// share export strips). Zero MaxConcurrency/MaxSeconds mean the runtime
// defaults.
type DelegationPolicy struct {
	Enabled        bool   `json:"enabled"`
	MaxDepth       int    `json:"maxDepth"`
	MaxConcurrency int    `json:"maxConcurrency"`
	MaxSeconds     int    `json:"maxSeconds"`
	Selection      string `json:"selection"`
	TargetAgentID  string `json:"targetAgentId,omitempty"`
}

const (
	// DelegationModelSelection lets the model choose a target from the
	// agent's allowed list; the list bounds the choice.
	DelegationModelSelection = "model"
	// DelegationPinnedSelection delegates every call to one configured
	// target Agent of this machine.
	DelegationPinnedSelection = "pinned"

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
	// MemoryEnabled is the Profile's preference for per-Agent long-term
	// memory: retrieval at run start and the memory_save tool. It only takes
	// effect together with the Agent's own authorization.
	MemoryEnabled bool `json:"memoryEnabled"`
	// PlanEnabled is the Profile's preference for the optional plan
	// capability: the update_plan tool and the plan prompt fragment.
	PlanEnabled bool `json:"planEnabled"`
}

// AgentPolicy is the machine-local policy that belongs to an Agent instance,
// not to any Profile: the approval mode, the resource authorization, the
// long-term memory authorization and the delegation allowlist. A new Agent
// initializes its own copy; referencing a Profile never copies another
// instance's authorization.
type AgentPolicy struct {
	ApprovalMode string          `json:"approvalMode"`
	Permissions  PermissionScope `json:"permissions"`
	// Memory authorizes this Agent to use long-term memory at all. It
	// defaults to false: existing and new Agents opt in explicitly.
	Memory bool `json:"memory"`
	// DelegationTargets lists the Agents this one may delegate subtasks to.
	// An empty list disables delegation regardless of the Profile switch.
	DelegationTargets []string `json:"delegationTargets,omitempty"`
}

// Profile is one named, versioned combination. Editing bumps the revision and
// affects every referencing Agent's subsequent runs.
type Profile struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Data      ProfileData `json:"data"`
	Revision  int         `json:"revision"`
	IsDefault bool        `json:"isDefault"`
	CreatedAt string      `json:"createdAt"`
	UpdatedAt string      `json:"updatedAt"`
}

// Agent is a stable instance identity: sessions, memory, runs and policy
// belong to it, while the Profile it points at can change over time.
type Agent struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	ProfileID string      `json:"profileId"`
	Policy    AgentPolicy `json:"policy"`
	CreatedAt string      `json:"createdAt"`
	UpdatedAt string      `json:"updatedAt"`
}

// AgentProfile is the resolved combination one Run executes with: the
// Agent's identity and machine policy plus the Profile content snapshot.
type AgentProfile struct {
	Resources       *RunResources     `json:"-"`
	ResourcesFrozen bool              `json:"-"`
	FrozenSkills    []skills.Snapshot `json:"-"`
	AgentID         string            `json:"agentId"`
	AgentName       string            `json:"agentName"`
	ProfileID       string            `json:"profileId"`
	ProfileName     string            `json:"profileName"`
	ProfileData
	Policy          AgentPolicy `json:"policy"`
	ProfileRevision int         `json:"profileRevision"`
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
	switch d.Delegation.Selection {
	case "", DelegationModelSelection, DelegationPinnedSelection:
	default:
		return errors.New("无效的委派目标选择方式")
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

func validatePolicy(p AgentPolicy) error {
	if p.ApprovalMode != "manual" && p.ApprovalMode != "semi" && p.ApprovalMode != "automatic" {
		return errors.New("无效的审批模式")
	}
	seen := map[string]bool{}
	for _, target := range p.DelegationTargets {
		if target == "" || seen[target] {
			return errors.New("委派允许列表包含空项或重复项")
		}
		seen[target] = true
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
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, config, revision, is_default, created_at, updated_at FROM resolved_profiles WHERE archived_at IS NULL ORDER BY CASE id WHEN 'default' THEN 0 ELSE 1 END, created_at, id`)
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
	row := s.db.QueryRowContext(ctx, `SELECT id, name, config, revision, is_default, created_at, updated_at FROM resolved_profiles WHERE id = ?`, id)
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
		if err := saveProfileComponents(ctx, tx, id, data); err != nil {
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
		if err := saveProfileComponents(ctx, tx, p.ID, p.Data); err != nil {
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

// DeleteProfile refuses to delete a Profile that any Agent still references;
// the user must reassign those Agents first.
func (s *Store) DeleteProfile(ctx context.Context, id string) error {
	referencing, err := s.AgentsReferencingProfile(ctx, id)
	if err != nil {
		return err
	}
	if len(referencing) > 0 {
		names := make([]string, 0, len(referencing))
		for _, agent := range referencing {
			names = append(names, agent.Name)
		}
		return fmt.Errorf("Profile 仍被 Agent 引用（%s），请先为这些 Agent 选择其他 Profile", strings.Join(names, "、"))
	}
	// Legacy clients keep their reference check, but deletion must also
	// preserve conversations that explicitly selected this Profile.
	return s.ArchiveProfile(ctx, id)
}

func scanAgent(scanner interface{ Scan(...any) error }) (Agent, error) {
	var a Agent
	var policyJSON string
	if err := scanner.Scan(&a.ID, &a.Name, &a.ProfileID, &policyJSON, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return Agent{}, err
	}
	if err := json.Unmarshal([]byte(policyJSON), &a.Policy); err != nil {
		return Agent{}, fmt.Errorf("解析 Agent 策略: %w", err)
	}
	return a, nil
}

func (s *Store) ListAgents(ctx context.Context) ([]Agent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, profile_id, policy, created_at, updated_at FROM agents ORDER BY CASE id WHEN 'default' THEN 0 ELSE 1 END, created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Agent{}
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, a)
	}
	return items, rows.Err()
}

func (s *Store) GetAgent(ctx context.Context, id string) (Agent, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, profile_id, policy, created_at, updated_at FROM agents WHERE id = ?`, id)
	a, err := scanAgent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Agent{}, errors.New("Agent 不存在")
	}
	return a, err
}

// AgentsReferencingProfile lists the Agents whose current profile_id points
// at the given Profile; the UI shows this reference relation.
func (s *Store) AgentsReferencingProfile(ctx context.Context, profileID string) ([]Agent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, profile_id, policy, created_at, updated_at FROM agents WHERE profile_id = ? ORDER BY created_at, id`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Agent{}
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, a)
	}
	return items, rows.Err()
}

// CreateAgent registers a new instance referencing an existing Profile. The
// new Agent initializes its own restrictive machine policy; authorization is
// never copied from another instance.
func (s *Store) CreateAgent(ctx context.Context, name, profileID string) (Agent, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return Agent{}, errors.New("Agent 名称无效")
	}
	if _, err := s.GetProfile(ctx, profileID); err != nil {
		return Agent{}, err
	}
	id, err := newID()
	if err != nil {
		return Agent{}, err
	}
	policy := AgentPolicy{ApprovalMode: "manual"}
	policyJSON, err := json.Marshal(policy)
	if err != nil {
		return Agent{}, err
	}
	created := now()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO agents (id, name, profile_id, policy, created_at, updated_at) SELECT ?, ?, id, ?, ?, ? FROM profiles WHERE id = ?`,
		id, name, string(policyJSON), created, created, profileID); err != nil {
		return Agent{}, err
	}
	return s.GetAgent(ctx, id)
}

// UpdateAgentLocal saves the Agent-owned fields: name and machine policy.
func (s *Store) UpdateAgentLocal(ctx context.Context, id, name string, policy AgentPolicy) (Agent, error) {
	old, err := s.GetAgent(ctx, id)
	if err != nil {
		return Agent{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return Agent{}, errors.New("Agent 名称无效")
	}
	if err := validatePolicy(policy); err != nil {
		return Agent{}, err
	}
	// Every delegation target must exist on this machine: the allowlist is
	// agent-local authorization over real instances, never free-form text.
	for _, target := range policy.DelegationTargets {
		if _, err := s.GetAgent(ctx, target); err != nil {
			return Agent{}, fmt.Errorf("委派目标不存在: %w", err)
		}
	}
	policyJSON, err := json.Marshal(policy)
	if err != nil {
		return Agent{}, err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE agents SET name=?, policy=?, updated_at=? WHERE id=?`, name, string(policyJSON), now(), old.ID); err != nil {
		return Agent{}, err
	}
	return s.GetAgent(ctx, id)
}

// SwitchAgentProfile points the Agent at another existing Profile. Sessions,
// history and policy stay with the Agent; the next run picks up the newly
// selected Profile's latest committed content.
func (s *Store) SwitchAgentProfile(ctx context.Context, agentID, profileID string) (Agent, error) {
	old, err := s.GetAgent(ctx, agentID)
	if err != nil {
		return Agent{}, err
	}
	if _, err := s.GetProfile(ctx, profileID); err != nil {
		return Agent{}, err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE agents SET profile_id=?, updated_at=? WHERE id=?`, profileID, now(), old.ID); err != nil {
		return Agent{}, err
	}
	return s.GetAgent(ctx, agentID)
}

// DeleteAgent removes an instance. Agents with session history are refused so
// no reference dangles, as are Agents that still appear in another Agent's
// delegation allowlist; a shared Profile is never deleted with the agent.
func (s *Store) DeleteAgent(ctx context.Context, id string) error {
	var sessions int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE agent_id = ?`, id).Scan(&sessions); err != nil {
		return err
	}
	if sessions > 0 {
		return errors.New("Agent 仍有会话历史，请先删除其会话")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, policy FROM agents`)
	if err != nil {
		return err
	}
	referencing := []string{}
	for rows.Next() {
		var otherID, policyJSON string
		if err := rows.Scan(&otherID, &policyJSON); err != nil {
			rows.Close()
			return err
		}
		if otherID == id {
			continue
		}
		var other AgentPolicy
		if json.Unmarshal([]byte(policyJSON), &other) != nil {
			continue
		}
		for _, target := range other.DelegationTargets {
			if target == id {
				referencing = append(referencing, otherID)
				break
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(referencing) > 0 {
		return fmt.Errorf("Agent 仍在 %s 的委派允许列表中，请先移除该委派目标", strings.Join(referencing, "、"))
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM agents WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return errors.New("Agent 不存在")
	}
	return nil
}

// ResolvedAgentProfile joins the Agent with its current Profile: the view a
// new Run snapshots. Running gates read only the live Agent policy.
func (s *Store) ResolvedAgentProfile(ctx context.Context, agentID string) (AgentProfile, error) {
	agent, err := s.GetAgent(ctx, agentID)
	if err != nil {
		return AgentProfile{}, err
	}
	profile, err := s.GetProfile(ctx, agent.ProfileID)
	if err != nil {
		return AgentProfile{}, err
	}
	return AgentProfile{
		AgentID: agent.ID, AgentName: agent.Name,
		ProfileID: profile.ID, ProfileName: profile.Name,
		ProfileData:     profile.Data,
		Policy:          agent.Policy,
		ProfileRevision: profile.Revision,
	}, nil
}

// AgentForSession resolves the owning Agent's current profile combination for
// a session.
func (s *Store) AgentForSession(ctx context.Context, sessionID string) (AgentProfile, error) {
	session, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return AgentProfile{}, err
	}
	conversation, err := s.GetConversation(ctx, sessionID)
	if err != nil {
		return AgentProfile{}, err
	}
	profile, err := s.GetProfile(ctx, conversation.ProfileID)
	if err != nil {
		return AgentProfile{}, err
	}
	a, err := s.GetAgent(ctx, session.AgentID)
	if err != nil {
		return AgentProfile{}, err
	}
	return AgentProfile{AgentID: a.ID, AgentName: a.Name, ProfileID: profile.ID, ProfileName: profile.Name, ProfileData: profile.Data, Policy: a.Policy, ProfileRevision: profile.Revision}, nil
}

// legacyDefaultConfig renders the pre-v8 JSON record for the built-in
// default so the v3/v5 migrations keep writing the shape the v8 split reads.
func legacyDefaultConfig() string {
	data, err := json.Marshal(legacyAgentProfile{
		Description:   "通用助手；使用已接入的工具和 Skills",
		SystemPrompt:  "你是 ICEPick 助手。使用可用工具完成用户任务；无法完成时如实说明。",
		AllMCPServers: true,
		AllSkills:     true,
		Permissions:   PermissionScope{Unrestricted: true},
		ApprovalMode:  "semi",
		Delegation:    DelegationPolicy{Enabled: true, MaxDepth: 1},
		ContextBudget: 8192,
		MemoryEnabled: true,
		PlanEnabled:   true,
	})
	if err != nil {
		panic(err)
	}
	return string(data)
}

// migrateProfiles establishes the pre-v8 agent_profiles table (v3). Every
// statement is a literal; the migration chain ends at v8, which splits this
// table into profiles and agents.
func (s *Store) migrateProfiles() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TABLE agent_profiles (id TEXT PRIMARY KEY, name TEXT NOT NULL, config TEXT NOT NULL, revision INTEGER NOT NULL, preset TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("Agent 数据库迁移: %w", err)
	}
	if _, err := tx.Exec(`ALTER TABLE sessions ADD COLUMN agent_id TEXT NOT NULL DEFAULT 'default'`); err != nil {
		return fmt.Errorf("Agent 数据库迁移: %w", err)
	}
	if _, err := tx.Exec(`UPDATE sessions SET agent_id='default'`); err != nil {
		return fmt.Errorf("Agent 数据库迁移: %w", err)
	}
	if _, err := tx.Exec(`CREATE INDEX sessions_agent_order ON sessions(agent_id, created_at, id)`); err != nil {
		return fmt.Errorf("Agent 数据库迁移: %w", err)
	}
	if _, err := tx.Exec(`CREATE TRIGGER sessions_agent_insert BEFORE INSERT ON sessions WHEN NOT EXISTS (SELECT 1 FROM agent_profiles WHERE id = NEW.agent_id) BEGIN SELECT RAISE(ABORT, 'unknown agent'); END`); err != nil {
		return fmt.Errorf("Agent 数据库迁移: %w", err)
	}
	if _, err := tx.Exec(`CREATE TRIGGER sessions_agent_update BEFORE UPDATE OF agent_id ON sessions WHEN NOT EXISTS (SELECT 1 FROM agent_profiles WHERE id = NEW.agent_id) BEGIN SELECT RAISE(ABORT, 'unknown agent'); END`); err != nil {
		return fmt.Errorf("Agent 数据库迁移: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO agent_profiles (id,name,config,revision,preset,created_at,updated_at) VALUES (?,?,?,?,?,?,?)`, DefaultProfileID, "默认 Agent", legacyDefaultConfig(), 1, DefaultProfileID, now(), now()); err != nil {
		return err
	}
	if _, err := tx.Exec(`PRAGMA user_version = 3`); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateAgentRecords stamps existing messages and runs with their session's
// agent ownership (v4).
func (s *Store) migrateAgentRecords() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`ALTER TABLE messages ADD COLUMN agent_id TEXT NOT NULL DEFAULT 'default'`); err != nil {
		return fmt.Errorf("Agent 记录迁移: %w", err)
	}
	if _, err := tx.Exec(`ALTER TABLE runs ADD COLUMN agent_id TEXT NOT NULL DEFAULT 'default'`); err != nil {
		return fmt.Errorf("Agent 记录迁移: %w", err)
	}
	if _, err := tx.Exec(`ALTER TABLE runs ADD COLUMN profile_revision INTEGER NOT NULL DEFAULT 1`); err != nil {
		return fmt.Errorf("Agent 记录迁移: %w", err)
	}
	if _, err := tx.Exec(`UPDATE messages SET agent_id=(SELECT agent_id FROM sessions WHERE sessions.id=messages.session_id)`); err != nil {
		return fmt.Errorf("Agent 记录迁移: %w", err)
	}
	if _, err := tx.Exec(`UPDATE runs SET agent_id=(SELECT agent_id FROM sessions WHERE sessions.id=runs.session_id)`); err != nil {
		return fmt.Errorf("Agent 记录迁移: %w", err)
	}
	if _, err := tx.Exec(`CREATE INDEX messages_agent_session_order ON messages(agent_id, session_id, created_at, id)`); err != nil {
		return fmt.Errorf("Agent 记录迁移: %w", err)
	}
	if _, err := tx.Exec(`CREATE INDEX runs_agent_session_order ON runs(agent_id, session_id, created_at, id)`); err != nil {
		return fmt.Errorf("Agent 记录迁移: %w", err)
	}
	if _, err := tx.Exec(`CREATE TRIGGER messages_agent_insert BEFORE INSERT ON messages WHEN NEW.agent_id != (SELECT agent_id FROM sessions WHERE id=NEW.session_id) BEGIN SELECT RAISE(ABORT, 'message agent mismatch'); END`); err != nil {
		return fmt.Errorf("Agent 记录迁移: %w", err)
	}
	if _, err := tx.Exec(`CREATE TRIGGER messages_agent_update BEFORE UPDATE OF agent_id,session_id ON messages WHEN NEW.agent_id != (SELECT agent_id FROM sessions WHERE id=NEW.session_id) BEGIN SELECT RAISE(ABORT, 'message agent mismatch'); END`); err != nil {
		return fmt.Errorf("Agent 记录迁移: %w", err)
	}
	if _, err := tx.Exec(`CREATE TRIGGER runs_agent_insert BEFORE INSERT ON runs WHEN NEW.agent_id != (SELECT agent_id FROM sessions WHERE id=NEW.session_id) BEGIN SELECT RAISE(ABORT, 'run agent mismatch'); END`); err != nil {
		return fmt.Errorf("Agent 记录迁移: %w", err)
	}
	if _, err := tx.Exec(`CREATE TRIGGER runs_agent_update BEFORE UPDATE OF agent_id,session_id ON runs WHEN NEW.agent_id != (SELECT agent_id FROM sessions WHERE id=NEW.session_id) BEGIN SELECT RAISE(ABORT, 'run agent mismatch'); END`); err != nil {
		return fmt.Errorf("Agent 记录迁移: %w", err)
	}
	if _, err := tx.Exec(`CREATE TRIGGER sessions_agent_reassign BEFORE UPDATE OF agent_id ON sessions WHEN EXISTS (SELECT 1 FROM messages WHERE session_id=OLD.id) OR EXISTS (SELECT 1 FROM runs WHERE session_id=OLD.id) BEGIN SELECT RAISE(ABORT, 'session with history cannot change agent'); END`); err != nil {
		return fmt.Errorf("Agent 记录迁移: %w", err)
	}
	if _, err := tx.Exec(`PRAGMA user_version = 4`); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateSingleDefault removes the two early development presets (v5).
// ICEPick has no released users yet, but existing local development
// conversations remain.
func (s *Store) migrateSingleDefault() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT OR IGNORE INTO agent_profiles (id,name,config,revision,preset,created_at,updated_at) VALUES (?,?,?,?,?,?,?)`, DefaultProfileID, "默认 Agent", legacyDefaultConfig(), 1, DefaultProfileID, now(), now()); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TRIGGER IF EXISTS sessions_agent_reassign`); err != nil {
		return fmt.Errorf("默认 Agent 迁移: %w", err)
	}
	if _, err := tx.Exec(`UPDATE sessions SET agent_id='default' WHERE agent_id IN ('chat','work')`); err != nil {
		return fmt.Errorf("默认 Agent 迁移: %w", err)
	}
	if _, err := tx.Exec(`UPDATE messages SET agent_id='default' WHERE agent_id IN ('chat','work')`); err != nil {
		return fmt.Errorf("默认 Agent 迁移: %w", err)
	}
	if _, err := tx.Exec(`UPDATE runs SET agent_id='default' WHERE agent_id IN ('chat','work')`); err != nil {
		return fmt.Errorf("默认 Agent 迁移: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM agent_profiles WHERE id IN ('chat','work')`); err != nil {
		return fmt.Errorf("默认 Agent 迁移: %w", err)
	}
	if _, err := tx.Exec(`CREATE TRIGGER sessions_agent_reassign BEFORE UPDATE OF agent_id ON sessions WHEN EXISTS (SELECT 1 FROM messages WHERE session_id=OLD.id) OR EXISTS (SELECT 1 FROM runs WHERE session_id=OLD.id) BEGIN SELECT RAISE(ABORT, 'session with history cannot change agent'); END`); err != nil {
		return fmt.Errorf("默认 Agent 迁移: %w", err)
	}
	if _, err := tx.Exec(`PRAGMA user_version = 5`); err != nil {
		return err
	}
	return tx.Commit()
}
