# ICEPick

ICEPick 是一个本地优先、领域无关的 Agent 宿主。它使用 Go 和 Wails 3 构建桌面应用，同一二进制也提供命令行客户端。模型可接入本机 Ollama / LM Studio（无需密钥），或通过官方 Go SDK 接入 OpenAI、Anthropic 与 Gemini（凭据独立存储，发送前经数据外发授权检查）。

## 重新设计的方向（2026-10-12）

目标是让用户通过 **Profile** 定义完整场景，选择主执行、规划、worker、审查、配置改进等内置 Agent 组件，组合模型、指令、工具、Skills 和执行流程。运行实例由宿主创建，用户无需维护另一套 Agent 列表。计划按实际能力提供“日常助手”“研究分析”“写作编辑”“开发协作”四个经真实验收的场景 Profile 和空白创建入口。审查和复盘在当前场景中调用，无需切换 Profile；Profile/Skills 改进经过候选、验证、发布和回退流程。

R1–R2 两批增量已接入：桌面以 Profile 为入口，运行状态和授权由本机上下文自动管理，旧 Agent 表和命令保留兼容。已提供日常助手、写作编辑的可编辑模板副本与空白入口，标记为待真实模型验收。R3 已接入独立 planner、自动依赖调度和只读 worker 并行；研究分析与开发协作提供试验配置，需材料或工具依赖。独立审查、固定流程和配置改进仍待交付。实现范围、兼容迁移与后续阶段见[设计文档](docs/architecture.md)和[实施计划](IMPLEMENTATION_PLAN.md)；P4/P5 编号对应[历史实施记录](docs/history/implementation-v1.md)。

## 当前功能

- 直接选择 Profile 开始对话，无需创建 Agent；新对话默认隔离上下文，可显式沿用已有对话的记忆与权限。Profile 切换按对话保存，保留历史和授权，在途 Run 使用旧快照。
- main/worker 组件支持独立提示词、模型和工具子集；worker 来源可选当前或其他 Profile 的配置，运行固定修订、模型与所选 SKILL.md 内容；支持 direct 与 dynamic 流程。出厂模板可创建、编辑和按配置组恢复，删除本地 Profile 不删除程序模板、历史或授权。
- Profile schemaVersion=1、事务修订记录和归档删除；v5 分享包含流程、planner、预算及独立 worker 配置，兼容读取 v1/v2/v3/v4 包，不携带本机授权和出厂绑定。
- 按 Agent 隔离会话、消息和任务记录；支持多轮对话、工具调用、流式输出与取消任务。
- 统一接入 MCP 服务和 Agent Skills，由 Profile 选择启用范围，与 Agent 授权取交集执行。
- 工具执行统一经权限网关：effect/sensitivity 分类、全人工/半自动/全自动三模式审批（绑定调用与参数哈希）、工作区硬边界、MCP 指纹变化暂停自动执行、每轮模型发送前的数据外发授权检查（桌面端可管理外发授权与远端凭据）。
- 上下文压缩（P4）：会话超过 Profile 上下文预算时自动把最旧的完整轮次归纳为摘要（保留当前任务与未闭合工具调用，原文不删），摘要继承数据范围并继续受外发检查约束；会话视图可查看摘要与覆盖区间。
- 逐 Agent 长期记忆（P4）：记忆按 `agent_id` 隔离，Profile 偏好与 Agent 授权同时开启才生效；运行开始时注入（来源与范围标注，范围计入外发检查），模型可经 `local__memory_save`（需审批）写入；桌面记忆面板与 `icepick memory` 命令提供查看/编辑/添加/删除/导出。
- 任务计划：模型经 `local__update_plan` 维护带稳定 ID、依赖、验收条件与材料出处的计划；宿主校验依赖和修订，防止并发覆盖及已开始任务被重写。桌面/CLI 显示计划，重启后恢复；清除保留修订历史。状态由模型记录，尚未接入自动调度与独立验收。
- 动态协作：Profile 选择 dynamic 后，planner 按关键事件生成/修订依赖计划，宿主审批、领取并调度，main 整理交付。只读任务并行，写入和未知副作用串行；waiting/blocked/failed/complete 状态明确。根预算覆盖所有组件与重试，模型用量未知时保留预留并标记；安全自动重试需显式开启。重启保留游标与尝试，进入阻塞，不盲目重放。
- 本次任务执行计划：启用规划、委派和 worker 槽位后，main 可声明依赖任务，经审批按 ID 与条件指纹执行。宿主原子领取并关联子 Run、执行尝试和前置交付文本；状态由实际执行推进，未执行任务不能标记完成，中断和未知结果不会自动重跑。桌面与 `tasks status` 可查看完整计划和尝试；dynamic 已支持 planner 与受控只读并行；独立审查仍未提供。
- 本机委派（P5）：Profile 开启后，模型经 `local__delegate_task` 把明确描述的子任务委派给允许列表中的其他 Agent（也可指定固定目标）。子任务有独立上下文与运行记录（`parent_run_id` 任务树），权限与外发取父任务与目标 Agent 的交集、审批取更严格者，取消父任务级联取消子任务；子任务不访问目标的会话与长期记忆。
- Profile 分享包 v5（兼容 v1/v2/v3/v4）：可明确选择包含 worker 来源，展平为独立配置；一键导出 zip（配置清单、人格/提示词独立文件、MCP 依赖引用、标准 Skills 目录），不含授权、会话、记忆、密钥与绝对路径；导入先预览缺失依赖与建议，确认后创建新 Profile（不自动安装资源、不创建/切换 Agent、不授权）。桌面端与 `icepick profiles export/import` 均可使用。
- CLI/IPC：桌面端或 `icepick serve` 构成每数据目录唯一宿主；`icepick` 命令行经本机 IPC 驱动与桌面相同的服务契约。

当前内置工具包含只读工具与记忆/计划/委派簿记工具；外部 MCP 工具已可经权限网关和审批调用，统一按 external/unknown 处理。尚无通用内置写入、命令和网络工具，Jev、A2A 也未实现。默认 Agent 的一般资源策略为范围不限、半自动审批，远端外发另行授权；长期记忆与委派需在 Profile 偏好与 Agent 授权（记忆授权 / 委派允许列表）中同时开启。新 worker 使用对话本机授权，配置来源不提供权限或记忆。会话 Plan 保留待办语义；direct 支持 main 同步委派，dynamic 使用 planner 和宿主调度。动态交付物带消息引用、SHA256 与真实工具证据；worker 可按本根任务交换过程与观察，继承数据外发范围。Profile 可选择 off/on_demand/required 独立审查；required 未通过不能完成，结论绑定目标、交付文本及工具证据指纹。必要检查需指定工具成功执行并返回 JSON `{"passed":true}`，无法确认时为 inconclusive。桌面可单次复核结果；CLI `tasks review <sessionID> <runID>` 发起复核、`tasks status` 查看结论；旧任务终态保留。reviewer 当前只读取已有证据，自动返工与专用检查调度尚未实现；worker 的 succeeded/unreviewed 仍不等同于审查通过。

升级到数据库 v17 会保留原 ID、会话、记忆、运行快照和 grant；contexts 视图逐个承接旧 Agent 身份，不合并共享 Profile 的数据。新增对话级 Profile、组件配置、修订记录、归档状态、计划依赖/修订历史与根任务/执行尝试；启动迁移前自动备份，旧程序不能直接写入 v17 数据库。旧全局外发授权保留供核对，但需按 Agent 重新授权；旧历史来源不全时按 unknown 处理。外部 MCP 改为任务首次使用所选服务时启动。

## 运行

需要 Go 1.25+、Node.js、npm、Wails 3 CLI，以及当前平台的 Wails 桌面依赖。在本目录执行：

```sh
wails3 dev
```

构建 Linux 桌面应用：

```sh
wails3 build
```

产物在 `bin/icepick`。目前只验证了 Linux 构建。依赖使用 Go/npm 的用户级持久缓存；若需自定义缓存，应放在本项目 `.cache/`，不要放在系统临时目录。

无需模型或密钥也可启动；界面会显示“模型未连接”。要运行任务，先在“模型服务”选择提供方并保存模型 ID：本机 Ollama/LM Studio 确保服务已连接；远端 OpenAI/Anthropic/Gemini 需在“远端凭据”面板保存 API 密钥（保存在数据目录内的 0600 文件，不会回显），并按需在当前 Agent 的“数据外发授权”面板允许该端点。本机模型调用支持流式输出，界面会实时显示生成文本。

配置文件保存在用户 XDG 配置目录的 `ICEPick/config.json`；未创建时默认使用 Ollama 的 `http://127.0.0.1:11434/v1`。本机模型地址必须指向本机 HTTP 服务。数据库 `icepick.db`、日志 `icepick.log`、凭据 `credentials.json` 与 IPC 套接字位于用户 XDG 数据目录的 `ICEPick/` 下。

## 命令行

每个数据目录同时只有一个宿主：桌面应用或无头宿主。CLI 命令连接正在运行的宿主：

```sh
bin/icepick serve            # 启动无头宿主（Ctrl+C 退出）
bin/icepick status           # 宿主与模型状态
bin/icepick profiles list    # Profile 列表（create/copy/edit/restore/export/import/delete）
bin/icepick templates list   # 可用场景模板
bin/icepick templates create daily   # 创建日常助手 Profile
bin/icepick conversations create --profile <profileID>   # 新对话，自动隔离
bin/icepick conversations start <conversationID> "任务内容" --wait
bin/icepick conversations permissions <conversationID> --mode semi --workspace <项目路径>
bin/icepick conversations select <conversationID> --profile <profileID>
bin/icepick conversations show <conversationID>   # 查看历史、摘要与计划
bin/icepick agents list      # 旧身份兼容入口
bin/icepick sessions create <agentID>          # 新建会话
bin/icepick tasks start <agentID> <sessionID> "任务内容" --wait   # 执行任务（--wait 跟随并处理审批）
bin/icepick tasks status <runID> --agent <agentID>   # 任务状态与委派子任务树
bin/icepick approvals list   # 待审批与结果未知调用（含委派子会话）
bin/icepick memory list <agentID>   # 长期记忆（add/edit/delete/export）
bin/icepick logs 100         # 查看宿主日志
bin/icepick help             # 完整命令列表
```

无交互执行遇到审批请求时只报告、绝不默认放行；可在桌面端或用 `approvals approve/deny` 处理。

构建前运行核心测试：

```sh
go test ./internal/...
```

执行 `wails3 build` 后运行包含桌面服务层的测试：

```sh
go test ./...
```

## 目录

- `main.go`：入口；无参数启动 Wails 桌面，带参数进入 CLI。
- `statusservice.go`：模型配置、Profile/Agent、会话、任务与凭据 Wails 服务。
- `internal/config/`：本地配置、提供方校验与凭据存储。
- `internal/model/`：统一模型客户端（本地兼容 + OpenAI/Anthropic/Gemini 官方 SDK 适配、流式）与模型发现。
- `internal/agent/`：共享的 Agent 多轮循环、流式回调、上下文压缩、取消和轮次上限。
- `internal/app/`：界面无关的应用服务（运行编排、权限网关、外发检查、流缓冲、记忆与摘要服务）。
- `internal/host/`：桌面与无头宿主共用的启动链路。
- `internal/ipc/`：本机 IPC 服务器/客户端（JSON 行协议、令牌 + 同 UID 校验）。
- `internal/cli/`：命令行客户端。
- `internal/mcptools/`：使用官方 Go MCP SDK 连接工具服务；内置时间工具由本地 MCP 服务提供。
- `internal/skills/`：发现并按需读取 Agent Skills。
- `internal/storage/`：SQLite Profile/Agent、隔离会话、消息与任务状态。
- `frontend/`：React/TypeScript 界面。
- `build/`：Wails 平台构建配置。

## 文档

- [设计文档](docs/architecture.md)：以 Profile 为入口的新目标架构与可自定义能力。
- [实施计划](IMPLEMENTATION_PLAN.md)：新阶段、兼容迁移与验收标准。
- [历史实施记录](docs/history/implementation-v1.md)：现有 P0–P8 基线及未完成的真实验收。
- [MCP 与 Skills](docs/mcp-and-skills.md)：当前接入方式。

## 许可证

本项目采用 GNU Affero General Public License v3.0（`AGPL-3.0-only`），完整条款见 [LICENSE](LICENSE)。第三方依赖与保留版权声明的构建脚本遵循各自许可证。
