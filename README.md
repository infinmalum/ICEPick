# ICEPick

ICEPick 是一个本地优先、领域无关的 Agent 宿主。它使用 Go 和 Wails 3 构建桌面应用，同一二进制也提供命令行客户端。模型可接入本机 Ollama / LM Studio（无需密钥），或通过官方 Go SDK 接入 OpenAI、Anthropic 与 Gemini（凭据独立存储，发送前经数据外发授权检查）。

## 当前功能

- Profile 与 Agent 分离：Profile 保存可共享的提示词、模型与能力组合（可复制/编辑/恢复），Agent 持有稳定身份、会话与本机授权（审批模式 + 资源范围 + 记忆授权），支持切换 Profile 与 Run 级快照。
- 按 Agent 隔离会话、消息和任务记录；支持多轮对话、工具调用、流式输出与取消任务。
- 统一接入 MCP 服务和 Agent Skills，由 Profile 选择启用范围，与 Agent 授权取交集执行。
- 工具执行统一经权限网关：effect/sensitivity 分类、全人工/半自动/全自动三模式审批（绑定调用与参数哈希）、工作区硬边界、MCP 指纹变化暂停自动执行、每轮模型发送前的数据外发授权检查（桌面端可管理外发授权与远端凭据）。
- 上下文压缩（P4）：会话超过 Profile 上下文预算时自动把最旧的完整轮次归纳为摘要（保留当前任务与未闭合工具调用，原文不删），摘要继承数据范围并继续受外发检查约束；会话视图可查看摘要与覆盖区间。
- 逐 Agent 长期记忆（P4）：记忆按 `agent_id` 隔离，Profile 偏好与 Agent 授权同时开启才生效；运行开始时注入（来源与范围标注，范围计入外发检查），模型可经 `local__memory_save`（需审批）写入；桌面记忆面板与 `icepick memory` 命令提供查看/编辑/添加/删除/导出。
- 任务规划（P4）：模型经 `local__update_plan` 维护可恢复的计划，重启后随会话恢复并在下一运行注入；计划面板可查看与清除，计划不授予任何权限。
- 本机委派（P5）：Profile 开启后，模型经 `local__delegate_task` 把明确描述的子任务委派给允许列表中的其他 Agent（也可指定固定目标）。子任务有独立上下文与运行记录（`parent_run_id` 任务树），权限与外发取父任务与目标 Agent 的交集、审批取更严格者，取消父任务级联取消子任务；子任务不访问目标的会话与长期记忆。
- Profile 分享包（P5）：一键导出 zip（配置清单、人格/提示词独立文件、MCP 依赖引用、标准 Skills 目录），不含授权、会话、记忆、密钥与绝对路径；导入先预览缺失依赖与建议，确认后创建新 Profile（不自动安装资源、不创建/切换 Agent、不授权）。桌面端与 `icepick profiles export/import` 均可使用。
- CLI/IPC：桌面端或 `icepick serve` 构成每数据目录唯一宿主；`icepick` 命令行经本机 IPC 驱动与桌面相同的服务契约。

当前可运行的工具限于内置只读工具与记忆/计划/委派簿记工具。外部 MCP 工具执行、Jev 和 A2A 等功能尚未交付（见实施计划 P6–P7）。默认 Agent 虽已保存“全部服务和 Skills、权限范围不限、半自动审批”的配置，运行时仍受上述限制；长期记忆与委派默认需在 Profile 偏好与 Agent 授权（记忆授权 / 委派允许列表）中同时开启。

升级到数据库 v9–v11 会拆分 Profile 配置并保留原 ID、会话和运行快照（v11 增加委派的会话类型与父子运行关联）。旧全局外发授权保留供核对，但需按 Agent 重新授权；旧历史来源不全时按 unknown 处理。外部 MCP 改为任务首次使用所选服务时启动。

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
bin/icepick agents list      # Agent 列表（create/switch/update/delete；update 可配置委派允许列表）
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

- [设计文档](docs/architecture.md)：目标架构与可自定义能力。
- [实施计划](IMPLEMENTATION_PLAN.md)：开发阶段与验收标准。
- [MCP 与 Skills](docs/mcp-and-skills.md)：当前接入方式。
