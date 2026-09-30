# 外部能力清单（初次静态盘点）

状态：2026-09-30。来源为 ICEPick 同级目录 `../bot1/deepseek_bot/bot.py` 的 `TOOLS` 静态字面量和 `../bot1/knowledge/toolPlugins/*.json`。这里只登记声明，不表示实现可运行或已获授权；旧代码不会被 ICEPick 自动执行。迁移结果见 [实施计划](../MIGRATION_PLAN.md)。

## 旧 Bot：50 个内置工具

| 组别 | 工具名 | 数量 | 预期去向 |
|---|---|---:|---|
| 本地文件与命令 | `sh`, `read`, `write`, `edit`, `sys`, `file` | 6 | 文件/命令 MCP；`file` 的 Telegram 发送语义需拆开 |
| 网络与外部服务 | `search`, `url`, `coin`, `proxy`, `fofa`, `agent_reach`, `captcha` | 7 | 按需的网络 MCP；依赖、凭据和目标范围逐项审核 |
| 任务与状态 | `todo`, `goal`, `data`, `ask`, `memory`, `conversation_search`, `project` | 7 | Agent 应用服务、隔离记忆、Skills 或数据导入 |
| 媒体与解析 | `img`, `shot`, `pdf`, `whois`, `report`, `parse` | 6 | 本地 MCP 或 Skill；检查系统命令和文件格式依赖 |
| 编排与扩展 | `selfext`, `watch`, `team`, `subagent`, `workflow`, `ralph`, `schedule` | 7 | 子 Agent、任务编排与单独的调度服务；动态写入代码不直接迁入 |
| 安全工作流 | `lateral`, `privesc`, `credential`, `adaptive_chain`, `api_attack`, `c2`, `strix`, `cloud`, `container`, `evasion`, `exfil`, `playbook`, `waf` | 13 | 逐项核验用途、依赖、外部副作用与可授权范围后再决定 |
| 机器人平台 | `group`, `group_memory`, `tg`, `notify` | 4 | 不迁入 Telegram 语义；本地通知另行评估 |
| **合计** | | **50** | |

## 旧 Bot：15 个声明式插件

所有 JSON 声明都标记 `admin_only: true`，`exec` 指向旧的 `/opt/deepseek-bot` 绝对路径；这不是当前机器的可执行配置。除 `team_saber` 的复合命令外，脚本名与声明同名。

| 插件名 | 本地同名 Python 脚本 | 初步处理 |
|---|---|---|
| `b64tool` | 有 | 本地转换工具候选 |
| `cdn_trace` | 有 | 网络工具候选 |
| `dynamic_plan` | 有 | Skill/任务服务候选 |
| `env_model` | 有 | 配置/模型服务候选 |
| `hashdemo` | 有 | 本地转换工具候选 |
| `mcp_bridge` | 有 | 评估与标准 MCP 的重叠 |
| `obscura_render` | 有 | 媒体工具候选 |
| `pentest_orchestrator` | **缺少 `planner.py`** | 声明不可用；取得源码前不迁移 |
| `project_scan` | 有 | 工作区 MCP 候选 |
| `shell_session` | 有 | 命令 MCP 候选，需执行边界 |
| `skill_create` | 有 | 评估生成标准 `SKILL.md` 的能力 |
| `taskforge` | 有 | 任务服务候选 |
| `team_saber` | 有 | 声明的 `exec` 同时含 Python 路径和脚本参数，需拆解验证 |
| `tgmsg` | 有 | Telegram 语义不迁入 |
| `verify_chain` | 有 | Skill/任务服务候选 |

## ColdBrew：来源与许可初查

ICEPick 同级目录已有 `../unpacked/app/resources/app-asar-extracted/` 和 `../unpacked/app/resources/core/projects/`。四个目标项目为 `claude-coldbrew`、`codex-coldbrew`、`deepseek-harness`、`grok4.6-coldbrew`，另有 `shared/five_blade`。这与先前用户提供的目录说明相符，但具体功能还需阅读代码并运行测试。

Claude 和 Codex 项目的 README 声明使用 **ColdBrew Studio Community License v1.0**，并称其含再发布/商业使用限制、不是 OSI 开源许可证。许可文本、各子目录及第三方资产权属仍需逐项核验；在此之前只做功能对照，不复制代码或素材进 ICEPick。

## 下一轮核验

- 对 50 个工具的具体执行分支逐项记录命令、网络 API、凭据、文件路径和副作用，而不只看模型工具声明。
- 检查 15 个插件脚本能否在脱离 `/opt/deepseek-bot` 的环境中启动；记录缺失依赖和最小功能测试。
- 为两套来源建立代表性用户任务与 ICEPick 功能等价验收用例。每项分别标记已迁移、待实现、排除或缺失源码。
