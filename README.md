# ICEPick

ICEPick 是从原项目拆出的本地桌面 Agent。当前阶段有 Wails 3 + Go + React/TypeScript/Vite 桌面入口、本地配置与 SQLite 会话存储、任务取消基础模块。Agent 循环、模型适配和工具尚待迁移，详见 [迁移计划](MIGRATION_PLAN.md)。

## 运行

需要 Go 1.24+、Node.js、npm、Wails 3 CLI，以及当前平台的 Wails 桌面依赖。在本目录执行：

```sh
wails3 dev
```

构建 Linux 桌面应用：

```sh
wails3 build
```

产物在 `bin/icepick`。Wails 会生成 `frontend/bindings/`，前端通过该绑定调用 `StatusService.GetStatus()`。依赖使用 Go/npm 的用户级持久缓存；若需自定义缓存，应放在本项目 `.cache/`，不要放在系统临时目录。

无需模型或云端密钥也可启动；界面会显示“模型未连接”。可新建和切换空会话，任务发送将在 P2 接入。配置文件可放在用户 XDG 配置目录的 `ICEPick/config.json`，例如 `{"model":{"provider":"ollama","baseUrl":"http://127.0.0.1:11434/v1","name":""}}`；未创建时使用这个默认值。当前阶段只接受本机 Ollama 地址，也不会主动连接模型。数据库 `icepick.db` 与日志 `icepick.log` 位于用户 XDG 数据目录的 `ICEPick/` 下。

无窗口单元测试：

```sh
go test ./internal/...
```

## 目录

- `main.go`：Wails 桌面入口与窗口。
- `statusservice.go`：状态和会话 Wails 服务。
- `internal/config/`：本地配置和用户目录。
- `internal/storage/`：SQLite 会话、消息与任务状态。
- `internal/agent/`：运行取消和中断恢复基础模块。
- `frontend/`：React/TypeScript 界面。
- `build/`：Wails 平台构建配置。
- `MIGRATION_PLAN.md`：后续迁移阶段与验收标准。
