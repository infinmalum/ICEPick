package storage

import (
	"errors"
	"fmt"
	"regexp"

	"icepick/internal/config"
)

const ProfileSchemaVersion = 1

// Execution is one independently persisted configuration group. Later
// workflows must be implemented before their identifiers become valid here.
type Execution struct {
	Workflow      string          `json:"workflow"`
	Main          ComponentConfig `json:"main"`
	Planner       ComponentConfig `json:"planner"`
	Reviewer      ComponentConfig `json:"reviewer"`
	Review        ReviewPolicy    `json:"review"`
	Budget        RootBudget      `json:"budget"`
	Worker        ComponentConfig `json:"worker"`
	WorkerSources []WorkerSource  `json:"workerSources"`
}

// Empty ProfileID selects this Profile's own worker component. Referenced
// Profiles contribute only their worker configuration, never their workflow.
type WorkerSource struct {
	Name          string               `json:"name"`
	ProfileID     string               `json:"profileId"`
	Configuration *WorkerConfiguration `json:"configuration,omitempty"`
}

// Portable one-level worker configuration, excluding workflow, identity,
// history, authorization and recursive Profile dependencies.
type WorkerConfiguration struct {
	Name              string       `json:"name"`
	Persona           string       `json:"persona"`
	SystemPrompt      string       `json:"systemPrompt"`
	Model             config.Model `json:"model"`
	AllMCPServers     bool         `json:"allMcpServers"`
	EnabledMCPServers []string     `json:"enabledMcpServers"`
	AllowedTools      []string     `json:"allowedTools"`
	AllSkills         bool         `json:"allSkills"`
	EnabledSkills     []string     `json:"enabledSkills"`
	ContextBudget     int          `json:"contextBudget"`
}

func WorkerConfigurationFrom(p Profile) (WorkerConfiguration, error) {
	d, err := p.Data.ResolveComponent("worker")
	if err != nil {
		return WorkerConfiguration{}, err
	}
	return WorkerConfiguration{Name: p.Name, Persona: d.Persona, SystemPrompt: d.SystemPrompt, Model: d.Model, AllMCPServers: d.AllMCPServers, EnabledMCPServers: d.EnabledMCPServers, AllowedTools: d.AllowedTools, AllSkills: d.AllSkills, EnabledSkills: d.EnabledSkills, ContextBudget: d.ContextBudget}, nil
}

func (c WorkerConfiguration) ProfileData() ProfileData {
	d := BlankProfile()
	d.Persona = c.Persona
	d.SystemPrompt = c.SystemPrompt
	d.Model = c.Model
	d.AllMCPServers = c.AllMCPServers
	d.EnabledMCPServers = c.EnabledMCPServers
	d.AllowedTools = c.AllowedTools
	d.AllSkills = c.AllSkills
	d.EnabledSkills = c.EnabledSkills
	d.ContextBudget = c.ContextBudget
	return d
}

var workerName = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N}_-]{0,31}$`)

type ComponentConfig struct {
	SystemPrompt  string        `json:"systemPrompt"`
	Model         *config.Model `json:"model,omitempty"`
	RestrictTools bool          `json:"restrictTools"`
	AllowedTools  []string      `json:"allowedTools"`
}

type Component struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     int    `json:"version"`
	Available   bool   `json:"available"`
	Description string `json:"description"`
}

// Components are program-owned definitions, independent of stored Profiles.
func Components() []Component {
	return []Component{
		{"main", "主执行", 2, true, "直接处理用户任务，使用共享模型和工具循环"},
		{"worker", "子任务执行", 2, true, "按固定配置执行子任务，支持受控并行"},
		{"planner", "规划", 1, true, "提取目标、生成依赖计划并按关键事件重规划"},
		{"reviewer", "审查", 1, true, "独立上下文审查交付与已有工具证据，不执行工具"},
		{"improver", "配置改进", 1, false, "配置候选与评估发布尚未交付"},
	}
}

func normalizeProfile(data ProfileData) ProfileData {
	if data.SchemaVersion == 0 {
		data.SchemaVersion = ProfileSchemaVersion
	}
	if data.Execution.Workflow == "" {
		data.Execution.Workflow = "direct"
	}
	return data
}

func validateExecution(data ProfileData) error {
	if len(data.Execution.WorkerSources) > 8 {
		return errors.New("最多配置 8 个 worker 来源")
	}
	seen := map[string]bool{}
	for _, source := range data.Execution.WorkerSources {
		if source.Configuration != nil {
			if source.ProfileID != "" {
				return errors.New("worker 配置与 Profile 引用不能同时指定")
			}
			if len(source.Configuration.Name) > 100 {
				return errors.New("worker 配置名称过长")
			}
			if err := validateProfileData(source.Configuration.ProfileData()); err != nil {
				return err
			}
		}
		if !workerName.MatchString(source.Name) || seen[source.Name] {
			return errors.New("worker 名称需唯一，使用文字、数字、下划线或连字符，最多 32 字符")
		}
		seen[source.Name] = true
	}
	if data.SchemaVersion != 0 && data.SchemaVersion != ProfileSchemaVersion {
		return errors.New("不支持的 Profile schemaVersion")
	}
	if data.Execution.Workflow != "" && data.Execution.Workflow != "direct" && data.Execution.Workflow != "dynamic" {
		return errors.New("流程必须为 direct 或 dynamic")
	}
	if err := data.Execution.Review.Validate(); err != nil {
		return err
	}
	if err := data.Execution.Budget.Validate(); err != nil {
		return err
	}
	if data.Execution.Workflow == "dynamic" && (!data.PlanEnabled || !data.Delegation.Enabled || data.Delegation.MaxDepth < 1 || len(data.Execution.WorkerSources) == 0) {
		return errors.New("动态流程需要规划、委派、至少一级深度与 worker 槽位")
	}
	if data.Execution.Workflow == "dynamic" {
		for _, tool := range []string{"local__delegate_task", "local__task_plan"} {
			allowed := data.AllowsTool(tool)
			if data.Execution.Main.RestrictTools {
				found := false
				for _, name := range data.Execution.Main.AllowedTools {
					if name == tool {
						found = true
					}
				}
				allowed = allowed && found
			}
			if !allowed {
				return errors.New("动态流程需要规划与委派能力；当前公共或 main 工具限制禁用了它们")
			}
		}
	}
	for _, c := range []ComponentConfig{data.Execution.Main, data.Execution.Worker, data.Execution.Planner, data.Execution.Reviewer} {
		if len(c.SystemPrompt) > 32000 {
			return errors.New("组件提示词过长")
		}
		if c.Model != nil {
			if c.Model.Provider == "" {
				return errors.New("组件模型需明确提供方")
			}
			if err := c.Model.Validate(); err != nil {
				return err
			}
		}
		if len(c.AllowedTools) > 200 {
			return errors.New("组件工具列表过长")
		}
		for _, tool := range c.AllowedTools {
			if !data.AllowsTool(tool) {
				return fmt.Errorf("组件不能增加 Profile 未选择的工具 %q", tool)
			}
		}
	}
	return nil
}

// ResolveComponent makes a single-level immutable execution projection.
// An explicit empty tool subset means no tools, unlike the legacy whitelist.
func (data ProfileData) ResolveComponent(role string) (ProfileData, error) {
	if err := validateExecution(data); err != nil {
		return ProfileData{}, err
	}
	var c ComponentConfig
	switch role {
	case "main":
		c = data.Execution.Main
	case "worker":
		c = data.Execution.Worker
	case "planner":
		c = data.Execution.Planner
	case "reviewer":
		c = data.Execution.Reviewer
	default:
		return ProfileData{}, fmt.Errorf("组件 %q 尚不可执行", role)
	}
	out := data
	if c.SystemPrompt != "" {
		out.SystemPrompt = c.SystemPrompt
	}
	if out.SystemPrompt == "" {
		if role == "worker" {
			out.SystemPrompt = "完成明确交给你的子任务，使用获准工具；遇到范围之外的问题时报告阻塞，不自行扩大任务。"
		} else {
			out.SystemPrompt = "你是 ICEPick 助手。理解用户任务，使用获准工具完成工作；无法完成或缺少信息时如实说明。"
		}
	}
	if role == "reviewer" && c.SystemPrompt == "" {
		out.SystemPrompt = "独立评估交付是否满足用户目标，指出具体缺陷与无法确认的事项。"
	}
	if c.Model != nil {
		out.Model = *c.Model
	}
	if role == "planner" || role == "reviewer" {
		out.MemoryEnabled = false
		out.PlanEnabled = false
		out.Delegation = DelegationPolicy{}
		out.AllSkills = false
		out.EnabledSkills = nil
		out.AllMCPServers = false
		out.EnabledMCPServers = nil
		out.AllowedTools = nil
	}
	if c.RestrictTools && role != "planner" && role != "reviewer" {
		out.AllowedTools = append([]string(nil), c.AllowedTools...)
		if len(c.AllowedTools) == 0 {
			out.AllMCPServers = false
			out.EnabledMCPServers = nil
		}
	}
	return out, nil
}

type ProfileTemplate struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Version    int         `json:"version"`
	Validation string      `json:"validation"`
	Data       ProfileData `json:"data"`
}

// Fresh values on every call keep editable copies from mutating defaults.
// Scene templates are not advertised as real-model validated yet.
func ProfileTemplates() []ProfileTemplate {
	daily := BlankProfile()
	daily.Description = "问答、解释、简单整理与轻量工具任务"
	daily.SystemPrompt = "你是 ICEPick 日常助手。准确理解用户要求，直接、清楚地回答。需要时使用已获准工具；区分已验证结果与推测，无法完成时说明原因。"
	daily.EnabledMCPServers = []string{"local"}
	daily.Execution.Review.Mode = "on_demand"
	writing := BlankProfile()
	writing.Execution.Review.Mode = "on_demand"
	writing.Description = "起草、改写、润色与文章结构整理"
	writing.SystemPrompt = "你是写作编辑助手。先理解用户给出的受众、目的、语气、篇幅与内容约束，必要时澄清。按要求起草或编辑，保留原文事实，不编造引用或经历。交付正文；用户需要时解释修改。"
	research := BlankProfile()
	research.Description = "试验配置：多来源材料分析与依赖汇总。提供资料或配置检索工具；必需独立审查；必要检查需自行配置。"
	research.SystemPrompt = "按用户的问题比较来源、区分事实与推断、保留引用出处。通过规划和 worker 分析与汇总；缺少材料时请求补充，不编造出处。"
	research.EnabledMCPServers = []string{"local"}
	research.PlanEnabled = true
	research.Delegation = DelegationPolicy{Enabled: true, MaxDepth: 1, MaxConcurrency: 2}
	research.Execution.Workflow = "dynamic"
	research.Execution.Review.Mode = "required"
	research.Execution.WorkerSources = []WorkerSource{{Name: "analyst"}}
	development := research
	development.Description = "试验配置：代码诊断、协作规划与整合。修改和测试需配置 MCP；必需独立审查；必要检查需自行配置。"
	development.SystemPrompt = "依据实际代码与工具证据诊断问题、规划依赖步骤、实现与检查。缺少修改或测试工具时明确阻塞；未运行测试不声称通过，写入和未知副作用任务串行。"
	development.Execution.WorkerSources = []WorkerSource{{Name: "developer"}}
	return []ProfileTemplate{
		{"daily", "日常助手", 2, "待真实模型验收", daily},
		{"writing", "写作编辑", 2, "待真实模型验收", writing},
		{"research", "研究分析（试验）", 1, "试验：待真实场景验收", research},
		{"development", "开发协作（试验）", 1, "试验：需工具依赖，待真实场景验收", development},
	}
}

func ProfileTemplateByID(id string) (ProfileTemplate, error) {
	for _, t := range ProfileTemplates() {
		if t.ID == id {
			return t, nil
		}
	}
	return ProfileTemplate{}, errors.New("出厂模板不存在或尚未实现")
}

func componentVersion(role string) int {
	for _, c := range Components() {
		if c.ID == role {
			return c.Version
		}
	}
	return 1
}
