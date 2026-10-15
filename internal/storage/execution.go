package storage

import (
	"errors"
	"fmt"
	"regexp"

	"icepick/internal/config"
)

const ProfileSchemaVersion = 1

// Execution is one independently persisted configuration group. Fixed stages
// and dynamic plans share host scheduling, claims, budgets and review evidence.
type Execution struct {
	Workflow        string          `json:"workflow"`
	Stages          []TaskSpec      `json:"stages,omitempty"`
	Main            ComponentConfig `json:"main"`
	Planner         ComponentConfig `json:"planner"`
	Reviewer        ComponentConfig `json:"reviewer"`
	Improver        ComponentConfig `json:"improver"`
	ImproverEnabled bool            `json:"improverEnabled"`
	Review          ReviewPolicy    `json:"review"`
	Budget          RootBudget      `json:"budget"`
	Worker          ComponentConfig `json:"worker"`
	WorkerSources   []WorkerSource  `json:"workerSources"`
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
		{"improver", "配置改进", 2, true, "按需求及明确选择的任务证据生成 Profile/Skill 草案，仅手动采用，不执行工具或自动发布"},
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
	if data.Execution.Workflow != "" && data.Execution.Workflow != "direct" && data.Execution.Workflow != "dynamic" && data.Execution.Workflow != "fixed" {
		return errors.New("流程必须为 direct、dynamic 或 fixed")
	}
	if err := validateFixedWorkflow(data); err != nil {
		return err
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
	for _, c := range []ComponentConfig{data.Execution.Main, data.Execution.Worker, data.Execution.Planner, data.Execution.Reviewer, data.Execution.Improver} {
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
// An explicit empty component tool subset means no tools; the common allowlist
// only narrows the selected catalogs when it is nonempty.
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
	case "improver":
		c = data.Execution.Improver
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
	if role == "improver" && c.SystemPrompt == "" {
		out.SystemPrompt = "根据用户需求提出可检查的 Profile 草案，不执行任务或修改权限。"
	}
	if role == "planner" || role == "reviewer" || role == "improver" {
		out.MemoryEnabled = false
		out.PlanEnabled = false
		out.Delegation = DelegationPolicy{}
		out.AllSkills = false
		out.EnabledSkills = nil
		out.AllMCPServers = false
		out.EnabledMCPServers = nil
		out.AllowedTools = nil
	}
	if c.RestrictTools && role != "planner" && role != "reviewer" && role != "improver" {
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
	Guide      SceneGuide  `json:"guide"`
}

// SceneGuide explains a template without introducing another user-managed object.
type SceneGuide struct {
	Input        string `json:"input"`
	Output       string `json:"output"`
	Requirements string `json:"requirements"`
	Example      string `json:"example"`
}

// Fresh values on every call keep editable copies from mutating defaults.
// Scene templates are not advertised as real-model validated yet.
func ProfileTemplates() []ProfileTemplate {
	daily := BlankProfile()
	daily.Description = "问答、解释、简单整理与轻量工具任务"
	daily.SystemPrompt = "你是 ICEPick 日常助手。理解问题、材料和用户要求的格式，直接给出答案或整理结果。简单任务直接处理，不强行分解；缺少影响结论的必要信息时询问。需要时使用已获准工具，文件或外部操作按用户要求执行；区分已验证结果与推测，无法完成时说明原因。不要把计划或工具调用成功当成任务已经完成。"
	daily.EnabledMCPServers = []string{"local"}
	daily.Execution.Review.Mode = "on_demand"
	daily.Execution.ImproverEnabled = true
	writing := BlankProfile()
	writing.Execution.Review.Mode = "on_demand"
	writing.Execution.ImproverEnabled = true
	writing.Description = "起草、改写、润色与文章结构整理"
	writing.SystemPrompt = "你是写作编辑助手。提取受众、目的、语气、篇幅、结构、必须保留与禁止添加的内容；有原文时以原文事实为边界。必要信息缺失时澄清，非关键偏好可说明假设后起草。交付可直接使用的正文，遵守用户的格式和长度限制；说明或修改清单仅在用户要求时附上。不得编造引用、经历、数据或承诺；不能满足的约束明确说明。"
	research := BlankProfile()
	research.Description = "试验配置：多来源材料分析与依赖汇总。提供资料或配置检索工具；必需独立审查；必要检查需自行配置。"
	research.SystemPrompt = "围绕用户的问题、比较维度、资料范围和时间要求分析材料。仅依据提供的资料或真实检索结果；缺少材料时请求补充，不编造出处。planner 按来源或问题安排 analyst，依赖分析完成后再汇总。交付结论、按维度比较、可核对的来源引用、分歧和信息缺口；区分资料事实、推断与建议。材料相互矛盾时保留分歧，不宣称未检索的内容是最新事实。"
	research.EnabledMCPServers = []string{"local"}
	research.PlanEnabled = true
	research.Execution.ImproverEnabled = true
	research.Delegation = DelegationPolicy{Enabled: true, MaxDepth: 1, MaxConcurrency: 2}
	research.Execution.Workflow = "dynamic"
	research.Execution.Review.Mode = "required"
	research.Execution.WorkerSources = []WorkerSource{{Name: "analyst"}}
	development := research
	development.Description = "试验配置：代码诊断、修改与验证。内置读写编辑和命令工具需本机工作区/执行授权；必需独立审查；必要检查需自行配置。"
	development.SystemPrompt = "依据用户的项目路径、问题或预期行为、修改范围和验收命令处理代码任务。先读实际代码，planner 按依赖安排 developer；只读调查可并行，写入和未知副作用任务串行。复用内置 read_file/write_file/edit_file/exec，依赖本机权限与审批；工具不可用时明确阻塞，不要求额外安装 MCP 才能修改代码。执行适合改动的检查；未执行、失败或结果未知的检查不能声称通过。交付行为变化、修改文件、实际检查结果以及遗留问题；不能用口头保证代替必要检查。"
	development.Execution.WorkerSources = []WorkerSource{{Name: "developer"}}
	return []ProfileTemplate{
		{ID: "daily", Name: "日常助手", Version: 4, Validation: "待真实模型验收", Data: daily, Guide: SceneGuide{
			Input:        "问题或待整理材料，可指定格式与长度。",
			Output:       "直接答案或整理结果；明确假设与未解决问题。",
			Requirements: "纯文本任务无需工具或 Skill；文件任务需设置工作区和权限。",
			Example:      "把这些事项整理为三项待办，每项包含负责人和期限；缺失的信息标为待确认：小林周五交稿，小周负责审核，发布时间未定。",
		}},
		{ID: "writing", Name: "写作编辑", Version: 4, Validation: "待真实模型验收", Data: writing, Guide: SceneGuide{
			Input:        "原文或主题、受众、目的、语气、篇幅和内容约束。",
			Output:       "可直接使用的正文；按要求附修改说明。",
			Requirements: "默认仅处理对话材料，无必需工具或 Skill；读写文件需自行启用 local 并授权。",
			Example:      "面向新用户，把下面通知改为亲切、清楚的两段正文，不超过120字，保留时间与联系方式，不添加承诺：周六10点进行维护，问题请联系support@example.com。",
		}},
		{ID: "research", Name: "研究分析（试验）", Version: 3, Validation: "试验：待真实场景验收", Data: research, Guide: SceneGuide{
			Input:        "研究问题、比较维度、资料及来源；联网时说明时间范围。",
			Output:       "结论、维度比较、来源引用、分歧与信息缺口；独立审查。",
			Requirements: "可直接提供材料，无必需 Skill；动态委派需审批。联网在对话开启，或配置检索 MCP 并通过 tool_search 发现。读取本机资料需工作区权限。",
			Example:      "只比较下面两份材料的价格和保留期限，引用[A]/[B]，不要联网；缺失项标未知：[A]每月20元，保留30天。[B]每月15元，未说明保留期限。",
		}},
		{ID: "development", Name: "开发协作（试验）", Version: 3, Validation: "试验：待真实场景验收", Data: development, Guide: SceneGuide{
			Input:        "工作区路径、现象或预期行为、允许修改范围及验收命令。",
			Output:       "修改结果、涉及文件、实际检查结果与遗留问题；独立审查。",
			Requirements: "内置 local 的读写编辑/exec 即可，无必需 MCP 或 Skill。设置工作区与可执行文件并审批委派/写入/命令；exec 是本机执行。必要检查可在 Profile 配置 local__exec，检查须实际返回 passed:true。",
			Example:      "在已授权工作区修复函数add把加法写成减法的问题，只修改calc.py，运行python3 -m unittest，报告修改文件与实际测试结果；无法执行时说明阻塞。",
		}},
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
