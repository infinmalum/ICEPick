# ICEPick

ICEPick 是从原项目拆出的本地桌面 Agent。当前版本可连接 Ollama 或 LM Studio、创建和编辑多个 Agent、按 Agent 隔离会话、发送任务、执行多轮工具调用、取消任务并保留历史。工具通过 [MCP](https://modelcontextprotocol.io/) 接入，任务指令使用 [Agent Skills](https://agentskills.io/specification) 的 `SKILL.md` 格式。目前运行时只开放配置允许的内置只读工具；完整审批、记忆、子 Agent、CLI 等仍在实施。目标行为见[设计文档](docs/architecture.md)，实施顺序见[计划](MIGRATION_PLAN.md)，当前接入方式见[MCP 与 Skills](docs/mcp-and-skills.md)。

仓库只跟踪桌面应用所需文件。Wails 生成的 Android/iOS 模板以及可重新生成的图标和桌面入口文件已从版本控制排除；当前只验证了 Linux 构建。

## 运行

需要 Go 1.25+、Node.js、npm、Wails 3 CLI，以及当前平台的 Wails 桌面依赖。在本目录执行：

```sh
wails3 dev
```

构建 Linux 桌面应用：

```sh
wails3 build
```

产物在 `bin/icepick`。Wails 会生成 `frontend/bindings/`，前端通过该绑定调用 `StatusService.GetStatus()`。依赖使用 Go/npm 的用户级持久缓存；若需自定义缓存，应放在本项目 `.cache/`，不要放在系统临时目录。

无需模型或云端密钥也可启动；界面会显示“模型未连接”。要运行任务，先在“本地模型”选择 Ollama 或 LM Studio，保存模型 ID，并确保服务已连接；然后选择 Chat、Work 或自建 Agent，新建会话并发送任务，例如“现在几点？”。Agent 配置可编辑提示词、模型与允许工具；目前运行时只开放配置允许的内置只读工具，外部 MCP 工具、审批、委派和上下文压缩还在实施。LM Studio 默认地址是 `http://127.0.0.1:1234/v1`，需先在 LM Studio 的 Developer 页面启动本地服务器。模型层使用 `/v1/chat/completions` 非流式请求；服务可达并不保证所选模型能正确调用工具。接口依据：[LM Studio OpenAI 兼容文档](https://lmstudio.ai/docs/developer/openai-compat)、[Ollama 兼容接口文档](https://github.com/ollama/ollama/blob/main/docs/api/openai-compatibility.mdx)。

配置文件保存在用户 XDG 配置目录的 `ICEPick/config.json`；未创建时默认使用 Ollama 的 `http://127.0.0.1:11434/v1`。当前只接受本机 HTTP 服务。数据库 `icepick.db` 与日志 `icepick.log` 位于用户 XDG 数据目录的 `ICEPick/` 下。

构建前可运行核心单元测试：

```sh
go test ./internal/...
```

执行 `wails3 build` 后，可运行包含 Wails 服务层的测试：

```sh
go test ./...
```

## 目录

- `main.go`：Wails 桌面入口与窗口。
- `statusservice.go`：模型配置、会话和任务 Wails 服务。
- `internal/config/`：本地配置和用户目录。
- `internal/model/`：本地模型服务发现与非流式兼容对话客户端；流式处理待后续实现。
- `internal/agent/`：共享的 Agent 多轮循环、取消和轮次上限。
- `internal/mcptools/`：使用官方 Go MCP SDK 连接工具服务；内置时间工具由本地 MCP 服务提供。
- `internal/skills/`：发现并按需读取 Agent Skills。
- `internal/storage/`：SQLite Agent 配置、隔离会话、消息与任务状态。
- `frontend/`：React/TypeScript 界面。
- `build/`：Wails 平台构建配置。
- `MIGRATION_PLAN.md`：后续迁移阶段与验收标准。
