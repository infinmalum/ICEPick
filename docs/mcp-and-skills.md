# MCP 与 Agent Skills

本文描述当前已实现的 MCP/Skills 接入方式。2026-10-12 的 Profile/内置组件重新设计及迁移见[设计文档](architecture.md)，不表示新运行流程已经交付。

ICEPick 的产品能力按内部接口装配，但不以自有格式替代已有的外部标准。工具使用 [MCP 官方 Go SDK](https://github.com/modelcontextprotocol/go-sdk)，可复用指令按 [Agent Skills 规范](https://agentskills.io/specification) 放在 `SKILL.md`；独立系统互联计划使用可选 A2A，当前尚未实现。运行循环、模型适配和存储使用普通内部模块组织。项目不建设通用插件加载器或自有插件包格式。

当前 Profile 已独立保存，MCP/Skills 选择保存在 Profile，运行时与 Agent 本机授权取交集；切换 Profile 不转移会话、记忆或授权。v12 已用 contexts 兼容视图将原身份作为宿主自动管理的本机上下文，继续保留相同的访问边界；新对话自动创建独立上下文，组件调用仍走同一权限网关。

## MCP 工具

程序内置一个经 MCP 内存传输连接的本地服务，提供只读 `current_time` 和 `read_file`（文本文件，64 KiB 上限，受权限网关与审批约束）。模型看到的名称是 `local__current_time`、`local__read_file`；前缀用于区分不同 MCP 服务。

外部 MCP 服务使用标准 **stdio 传输**。在用户 XDG 配置目录的 `ICEPick/config.json` 中显式添加 `mcpServers`，重启应用后登记，任务首次需要所选服务时连接：

```json
{
  "model": {"provider": "lmstudio", "baseUrl": "http://127.0.0.1:1234/v1", "name": "你的模型 ID"},
  "mcpServers": [
    {"name": "example", "command": "/绝对路径/到/mcp-server", "args": []}
  ]
}
```

这是 ICEPick 的启动配置，**不是 MCP 协议规定的统一配置文件格式**。`example` 服务中的工具 `search` 在 ICEPick 中命名为 `example__search`；调用时 SDK 使用 MCP `tools/list` 与 `tools/call`。服务由应用统一连接，各 Agent 可分别选择“全部服务”或指定服务，也可用工具白名单进一步收窄。当前默认 Agent 选择全部已接入服务；目标规则是“全部”仅覆盖已安装且经用户信任确认的资源。外部服务进程由应用启动，仍应只配置可信任的程序。外部工具现在可被模型调用，但统一经过权限网关：按 effect/sensitivity 分类（外部服务一律按“外部副作用、敏感度未知”处理，任何模式下都需要用户确认），再按 Agent 审批模式等待批准或拒绝。

目标安全边界见[设计文档](architecture.md)：外部 stdio 服务完成信任确认且被实际使用时才启动（已实现：启动应用只登记；所选 Profile 首次需要其工具目录时启动，并发 Run 共享一个连接）；启动时明确工作目录、环境变量允许列表、超时和进程组清理，不默认继承 ICEPick 的密钥等环境变量（已实现：`workingDir`、`env`（只存变量名，默认仅传 PATH 与 HOME）、`timeoutSeconds`，子进程独立进程组，握手失败即清理）。记录可执行文件、服务版本（若可得）及工具 schema 指纹；变化后重新评估风险，暂停依赖旧登记的自动执行（已实现：指纹登记在数据库，变化后网关强制确认并写日志）。工具按执行效果和数据敏感度分类；“只读”不能自动访问凭据或未知范围（已实现：凭据目录/凭据文件名与未知路径的读取需要确认）。外部进程可自行读取其系统权限可见的数据，因此工具参数校验不能代替进程信任或隔离。

MCP 工具返回文本会交给模型继续推理，单条结果上限 16 KiB；单次任务最多 8 轮、10 分钟。每轮模型请求前，宿主都会检查本会话累计数据范围与目的地授权：回环本机模型不受限；远端端点默认拒绝发送，需要按 Agent 与端点 origin 登记覆盖全部累计范围（personal/工作区/凭据/未知）的外发授权。配置网络工具权限或模型凭据都不等于授权发送。所选服务连接失败会使该任务明确失败；未选择的服务不会启动，不影响其他 Profile 的任务。外部工具分派后连接中断记为结果未知，须人工核对后继续。

## Agent Skills

把每个 Skill 放在用户 XDG 配置目录的 `ICEPick/skills/<skill-name>/SKILL.md`，例如：

```markdown
---
name: review-code
description: Review code changes when the user asks for a code review.
---
# Code review
Read the changed files, identify concrete defects, and cite file locations.
```

启动时由应用统一发现 Skills，只读取标准 `name` 和 `description` 元数据。当前默认 Agent 启用全部已安装 Skills；其他 Agent 可启用全部、指定名称或都不启用。当前实现还要求允许 `local__load_skill` 工具（启用 `local` 服务且工具白名单未排除它），才能按需读取完整 `SKILL.md`；新任务开始时固定选中 SKILL.md 的内容、元数据与 SHA-256，根任务及其 worker 后续加载使用这些副本；修改或删除安装文件不影响在途任务，新任务读取最新内容。旧运行保留兼容加载行为。调用时会再次检查目标 Skill 是否在运行快照的选择范围内。目标架构将 Skill 发现与加载作为独立指令来源能力，不把“通过 MCP 工具读取 Skill”固定为唯一执行方式。用户主动安装或编辑 Skill 正文不要求每次重新授权；第三方可执行依赖及影响风险的变更单独评估，发现 Skill 不自动安装或执行其依赖。Skill 中的相对资源引用、脚本执行和权限声明目前尚未接入；加载 Skill 不会自动运行脚本，也不会授予工具权限。修改 Skill 后重启应用以刷新发现列表。
