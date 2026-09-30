# MCP 与 Agent Skills

ICEPick 不定义自己的插件协议。工具使用 [MCP 官方 Go SDK](https://github.com/modelcontextprotocol/go-sdk)，任务指令按 [Agent Skills 规范](https://agentskills.io/specification) 放在 `SKILL.md`。

## MCP 工具

程序内置一个经 MCP 内存传输连接的本地服务，提供只读 `current_time`。模型看到的名称是 `local__current_time`；前缀用于区分不同 MCP 服务。

外部 MCP 服务使用标准 **stdio 传输**。在用户 XDG 配置目录的 `ICEPick/config.json` 中显式添加 `mcpServers`，重启应用后连接：

```json
{
  "model": {"provider": "lmstudio", "baseUrl": "http://127.0.0.1:1234/v1", "name": "你的模型 ID"},
  "mcpServers": [
    {"name": "example", "command": "/绝对路径/到/mcp-server", "args": []}
  ]
}
```

这是 ICEPick 的启动配置，**不是 MCP 协议规定的统一配置文件格式**。`example` 服务中的工具 `search` 在 ICEPick 中命名为 `example__search`；调用时 SDK 使用 MCP `tools/list` 与 `tools/call`。外部服务进程由应用启动，仍应只配置可信任的程序。目前即使把外部工具写入 Agent 的允许列表，运行时也不会执行；它们要等策略网关和审批界面完成后才会开放。

MCP 工具返回文本会交给模型继续推理，单条结果上限 16 KiB；单次任务最多 8 轮、10 分钟。服务连接失败会写入应用日志，不影响内置工具和聊天。

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

启动时只读取标准 `name` 和 `description` 元数据。需要在 Agent 配置的“启用 Skills”中写入 Skill 名称，并把 `local__load_skill` 加到该 Agent 的允许工具列表；之后 Agent 才能按需读取完整 `SKILL.md`。调用时还会检查该 Skill 是否仍在当前 Agent 的启用列表中。Skill 中的相对资源引用、脚本执行和权限声明目前尚未接入；加载 Skill 不会自动运行脚本，也不会授予工具权限。修改 Skill 后重启应用以刷新发现列表。
