# 桌面产品体验核对（2026-10-15）

核对基线：Git HEAD `a1bf70a`，加当前工作区已完成但未提交的 R5 输入等待与显式续跑改动。结论来自代码检查，不冒充人工桌面或用户研究验收。本轮不修改界面实现。

产品目标：普通用户接入模型后即可对话和完成工作；无需先理解 Agent 体系。Profile 的高度自定义能力保留，通过“助手”和设置呈现。开发者能力进入任务详情、开发者设置和 CLI。该原则已写入根目录 [AGENTS.md](../AGENTS.md)。

当前修复进度：本轮已按本文完成桌面产品化代码，具体行为、验证和限制见[桌面使用指南](desktop.md)。以下表格保留改版前核对事实，不能继续将其全部视为当前未修复列表。

## 原清单逐项核对

下表“成立”指代码支持该问题判断，不代表应原样采纳每个解决方案。路径均相对仓库根目录。

| 原编号 | 结论 | 当前证据与需精确说明之处 |
|---|---|---|
| 1 | 成立 | `frontend/src/App.tsx` 主标题上方仍为 AGENT HARNESS / DESKTOP，介绍先要求选择 Profile。 |
| 2 | 部分成立 | `useProfiles.ts` 启动自动选中列表第一项，`storage/profiles.go` 已有默认配置；并非完全没有默认助手。但 `Sidebar.tsx`、`useWorkspace.ts` 仍把 Profile 作为创建条件和按钮主语，空首页不能直接输入。 |
| 3 | 成立 | `Sidebar.tsx` 默认展示 Profiles、MCP 服务与“本地 Agent 宿主”；tooltip 包含命令与指纹。 |
| 4 | 成立 | `useApp.ts` 用 profileEditID 创建新对话；`ConversationProfile.tsx` 单独修改当前对话配置。点侧栏助手不切换当前对话；反过来打开对话又会更改编辑目标。 |
| 5 | 成立 | `Sidebar.tsx` 直接展示“新对话沿用当前记忆与权限”。 |
| 6 | 成立 | `ModelSettings.tsx`、`CredentialsPanel.tsx`、`ExfilPanel.tsx` 分开配置模型、密钥和发送授权，没有贯通的接入流程。 |
| 7 | 成立 | `ExfilPanel.tsx` 要手填 destination 和选择来源范围；后端已经知道实际模型地址。自定义端点也必须按实际目的地解释，不能只显示服务商品牌。 |
| 8 | 成立 | `ConversationPanel.tsx` 搜索不可用帮助要求编辑 XDG/config.json、webSearchTools、接 MCP 并重启。 |
| 9 | 成立 | `ConversationPanel.tsx` 开启联网且能力不可用时禁用普通发送；后端 `StartConversationTask` 也拒绝该选项。应提供明确的“关闭联网并发送”或修复入口，不能静默忽略用户联网意图。 |
| 10 | 成立 | `ToolApprovals.tsx` 的审批主体是 toolName 与原始 arguments。 |
| 11 | 成立 | `ToolApprovals.tsx` 只有“确认为成功/失败”；`useConversation.ts` 用 prompt 要求依据和实际结果，没有“暂不确定”的引导。不能把具体核对问题的一次回答直接当作任意工具的成功证明。 |
| 12 | 成立 | `ConversationPanel.tsx` 在 Transcript 前渲染 TaskProgress、快照说明、worker 子任务树与深度。 |
| 13 | 成立 | `TaskProgress.tsx` 主区默认展示 revision、调度状态、事件序号和预算；阶段虽然可折叠，summary 仍直接显示 ID/worker/state/accessMode。SHA 与 attempt/run ID 在展开内容内。 |
| 14 | 成立 | `TaskProgress.tsx`、`ConversationPanel.tsx`、草案列表等直接显示状态值；应按任务语义转换为处理、检查、等待和完成等表达。 |
| 15 | 成立 | `Transcript.tsx` 空态提示验证 MCP 时间工具，工具消息标为“MCP 工具”；app.ListMessages 确实会合成工具消息。 |
| 16 | 成立 | `Transcript.tsx` 普通回复与流式回复都是 p/content，没有 Markdown 渲染。 |
| 17 | 成立 | `frontend/public/style.css:46` 的消息区 max-height 420px、overflow auto，与页面纵向滚动并存。 |
| 18 | 成立 | Transcript 无滚动引用或跟随输出逻辑；需要同时保留用户向上阅读的位置。 |
| 19 | 成立 | 输入框无 onKeyDown，没有 Enter 发送/Shift+Enter 换行约定。实现时必须处理中文输入法 composition。 |
| 20 | 成立 | 只有文件选择、文件名/大小和移除；无拖拽、粘贴图片或缩略图。 |
| 21 | 成立 | `ConversationPanel.tsx` 在输入区常驻所有数量、大小、正文提取和权限限制。 |
| 22 | 成立 | 桌面绑定和前端均未找到目录选择入口，只有手输路径的提示。 |
| 23 | 成立 | `useWorkspace.ts`、`conversationservice.go`、`storage/conversations.go` 没有对话重命名、删除和搜索管理入口；需要补齐产品所需后端接口。 |
| 24 | 成立 | 多个 hook 用 String(reason) 写入 UI；尚无统一的用户解释、恢复操作与技术详情层。 |
| 25 | 成立 | `App.tsx` 把权限、记忆、Profile、草案、模型设置放在同一 main 下；details 折叠不等于独立设置页。 |
| 26 | 成立 | `ProfileEditor.tsx` 默认配置字段包含系统提示词、服务名、工具名单、Skills、模型地址和预算。联网仍应留在对话区，不照建议再加 Profile 联网开关。 |
| 27 | 成立 | `ProfileExecution.tsx` 名称为“高级：执行组件”，内容直接使用 direct/dynamic/fixed、组件模型、必要检查及 JSON passed:true。 |
| 28 | 成立 | `ProfileStages.tsx` 直接编辑 ID、worker、依赖和访问方式。本轮已新增 inputPrompt 等待能力，但仍在同一专业表单中。 |
| 29 | 成立 | `ProfileDrafts.tsx` 直接输入逗号分隔 Run ID。应从真实任务列表或当前任务选择，并保留显式选择证据的边界。 |
| 30 | 部分成立 | Profile 草案公开修订、影响与 evidence refs；JSON 编辑和差异已经折叠，但仍是唯一的候选修改方式，没有普通表单。 |
| 31 | 成立 | `SkillDrafts.tsx` 要输入源 Skill 名称与 Run ID，展示 SKILL.md、副本/引用语义，编辑依赖 JSON。部分技术内容已折叠。 |
| 32 | 成立 | `ProfileImport.tsx` 默认预览包含 token、委派深度/并发、MCP 命令和 worker 来源。 |
| 33 | 成立 | `ProfileTemplates.tsx` 介绍包含动态流程试验、必需独立审查、待真实模型验收；模板数据也有试验名称。应保留诚实的试用/限制提示，不虚称已稳定验收。 |
| 34 | 成立 | `ContextPolicyPanel.tsx` 要手填路径、命令和网络目标；没有按具体资源引导授权的常用入口。选择文件夹与授予访问应明确区分。 |
| 35 | 部分成立 | `storage/conversations.go` 已默认 semi，但 UI 只显示“全人工/半自动/全自动”，没有解释或推荐说明。 |
| 36 | 成立 | `MemoryPanel.tsx` 解释 LocalContext/Profile/来源与工具名；虽然已有查看、编辑和删除，表达仍偏实现。 |
| 37 | 成立 | `SearchRecords.tsx` 已折叠，但展开后先展示 provider/state/计量/费用，再混合来源、全文和事件日志。 |
| 38 | 成立 | `useProfiles.ts`、`useLocalContext.ts`、`useConversation.ts` 多处使用原生 prompt/confirm。 |
| 39 | 成立 | `useProfiles.ts:28` 在 profileEditID 或整个 profiles 列表改变时覆盖编辑草稿；无 dirty guard。与当前编辑对象无关的列表刷新也可能覆盖草稿。 |
| 40 | 部分成立 | `ProfileEditor.tsx:114` 已有 profileRefs.length > 0 判断，不能说无依赖时也要求选择。问题在于有依赖时仍用 worker 引用术语，且在平时编辑区直接出现。 |
| 41 | 成立 | 与第12项同源，快照/修订提示常驻运行中的聊天区，适合移至任务详情。 |
| 42 | 成立 | `TaskProgress.tsx` 压缩提示默认 open，展示 coversSeq、scope 和全文摘要。 |
| 43 | 成立 | CSS 的 content 850px、session-card 650px、顶部 padding 76px、h1 最高66px、消息区420px均存在。它们共同让聊天面积让位于配置和标题。 |

## 额外发现

| 优先级 | 问题与用户影响 | 证据 |
|---|---|---|
| P0 | 切换对话立即丢失未发送文字与附件，无按会话草稿或提醒。 | `useConversation.ts:40` selected 改变即清空 prompt/attachments。 |
| P0 | 子任务的工具审批没有在主对话聚合。用户通常需进入子会话才能看到其审批，主对话只显示“运行中”，容易以为卡住。 | `useConversation.ts` pendingCalls 仅过滤当前会话 toolCalls；refreshChildRuns 只拉取运行/图/审查，不聚合子会话审批；`ToolApprovals.tsx` 不接收子任务待办。后端已有覆盖委派会话的 PendingApprovals，可优先复用，不必重复建设聚合能力。 |
| P0 | 未提交的授权表单和新增记忆草稿不随 Context 绑定/清理。切到另一对话后，旧表单仍在，保存使用新的 contextID，存在误授权或误存位置的风险。 | `useLocalContext.ts` 切换只清 grants、memories、memoryEdit，未清 grantForm/memoryDraft；saveGrant/addMemory 使用当前 contextID。这是界面作用域问题，不是发现了后端越权。 |
| P1 | App 创建的对话全部叫“新对话”，而列表没有最后消息、最近活动、分组或运行/待审批提示。 | `useWorkspace.ts` 始终传空 title，storage 默认标题；`Sidebar.tsx` 只显示标题和 Profile，列表按创建时间排序。 |
| P1 | 历史附件只有名称/大小，无法从消息里打开、预览或另存核对。 | `Transcript.tsx` 无附件操作，`conversationservice.go` 未暴露对应读取/保存入口。源文件仍在存储，不能误称数据丢失。 |
| P1 | “拒绝原因”弹框按取消仍会拒绝工具调用。 | `useConversation.ts:299` 用 prompt(...) ?? ""，没有区分取消和空原因。 |
| P1 | 保存密钥后只刷新凭据列表，不重新检测模型；用户可能仍看到此前“未连接”，也没有一步继续聊天。刷新连接反而会覆盖未保存的模型表单。 | `useRuntimeSettings.ts` saveCredential/deleteCredential 与 refreshModel。 |
| P1 | 启动不恢复上次对话，加载列表后也没有直接可输入的默认对话；用户每次仍先挑会话。 | `useWorkspace.ts` selected 初始 null，refreshConversations 只更新列表。 |
| P1 | 消息、代码没有复制操作；失败没有统一的可恢复动作，未运行的内容也不能方便重发/修订。 | `Transcript.tsx` 和 `ConversationPanel.tsx` 目前只有显示、发送、取消；重试必须区分未发送失败与可能有副作用的执行失败。 |
| P1 | 大量禁用按钮不给具体原因，缺模型、Profile 已删、读取历史中、联网不可用的处理路径散落在不同面板。 | `ConversationPanel.tsx`、`ProfileDrafts.tsx`、`Sidebar.tsx` 的 disabled 条件。 |
| P1 | 缺少清晰的键盘与窄窗口体验。 | 聊天 textarea 没有关联 label/aria-label；CSS body min-width 600px，侧栏固定，窄窗口仅缩侧栏和 padding；应实际验证焦点路径、缩放和中文输入法。 |
| P1（性能待测） | 运行时每800ms全量拉消息、工具、流文本，再拉子任务/图/审查/来源；历史不分页，Transcript 全量渲染，长会话可能越来越迟缓。 | `useConversation.ts` 的 interval，`Store.ListMessages`/ListConversations 与 `Transcript.tsx`。目前是代码层风险，尚无性能测量结论。 |

## 修复顺序与边界

1. 先完成普通用户路径：默认助手、直接新对话、统一模型接入和具体发送授权、单一助手选择语义；失败或能力不可用时提供可点击的下一步。
2. 重排信息架构：聊天、助手管理和设置分开；任务详情与开发者设置承载原有高级能力。保持 Profile 自定义能力，避免只改中文名称、却仍让用户操作运行时模型。
3. 补齐聊天基本体验：Markdown/代码/表格、单一主滚动、跟随新消息、输入法友好的键盘发送、附件预览/拖拽/粘贴、文件夹选择、按会话草稿与对话管理。
4. 审批与恢复做成具体操作卡片，聚合子任务待办，允许用户保持不确定。既保留审计，也不要求普通用户填写工具结果 JSON 或猜测副作用。
5. 助手编辑、导入和改进提供普通表单/任务选择，保护未保存内容；完整组件配置和 JSON 保留在开发者设置。联网继续使用对话区既有开关。

有些修复需要增加产品接口（对话管理、附件读取、文件夹选择、具体授权请求），不能承诺仅靠 CSS 或折叠完成。此类接口服务于已有能力的完整使用路径，不应借此扩展新的 Agent 调度功能。原有权限、来源检查、快照与未知效果处理不应为简化界面而被关闭。

验收应从干净用户启动，完成接模型、发送消息、读取附件、按需授权、遇错恢复和找回旧对话；不能继续用“用户能找到 Run ID、编辑 JSON 并审批内部工具”作为桌面可用性的标准。
