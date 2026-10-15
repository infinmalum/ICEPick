import type { Workspace } from "../hooks/useWorkspace";
import type { ConversationState } from "../hooks/useConversation";
import type { Profiles } from "../hooks/useProfiles";
import type { RuntimeSettings } from "../hooks/useRuntimeSettings";
import { TaskProgress } from "./TaskProgress";
import { ToolApprovals } from "./ToolApprovals";
import { Transcript } from "./Transcript";
import { SearchRecords } from "./SearchRecords";
type Props = {
  workspace: Pick<Workspace, "activeConversation" | "selected" | "sessions">;
  conversation: Pick<
    ConversationState,
    | "attachments"
    | "addAttachments"
    | "removeAttachment"
    | "activeRun"
    | "approvalBusy"
    | "approveCall"
    | "cancelTask"
    | "childRuns"
    | "clearPlan"
    | "denyCall"
    | "loadingSession"
    | "messages"
    | "openChildSession"
    | "pendingCalls"
    | "prompt"
    | "resolveUnknown"
    | "reviewBusy"
    | "reviewResult"
    | "retrospectResult"
    | "retrospectives"
    | "reviews"
    | "runs"
    | "searchAvailability"
    | "searchBusy"
    | "searchRecords"
    | "sendTask"
    | "sending"
    | "sessionPlan"
    | "sessionSummary"
    | "setPrompt"
    | "streamText"
    | "taskError"
    | "taskGraph"
    | "toggleSearch"
    | "unknownCalls"
  >;
  profileState: Pick<Profiles, "profileName">;
  runtime: Pick<RuntimeSettings, "status">;
};
export function ConversationPanel(props: Props) {
  const { activeConversation, selected, sessions } = props.workspace;
  const {
    attachments,
    addAttachments,
    removeAttachment,
    activeRun,
    cancelTask,
    childRuns,
    loadingSession,
    openChildSession,
    prompt,
    runs,
    searchAvailability,
    searchBusy,
    sendTask,
    sending,
    setPrompt,
    taskError,
    taskGraph,
    toggleSearch,
  } = props.conversation;
  const { profileName } = props.profileState;
  const { status } = props.runtime;
  return (
    <section className="session-card">
      <div className="card-title">
        {selected
          ? sessions.find((item) => item.id === selected)?.title
          : "空会话"}
      </div>
      {!selected ? (
        <p className="hint">先新建或选择会话。</p>
      ) : (
        <>
          <TaskProgress conversation={props.conversation} />
          <ToolApprovals conversation={props.conversation} />
          {activeRun && runs[0] && (
            <p className="hint profile-run-hint">
              本次运行使用 Profile「{profileName(runs[0].profileId)}」快照 v
              {runs[0].profileRevision}；后续运行将使用「
              {activeConversation?.profileArchived
                ? "请选择新 Profile"
                : (activeConversation?.profileName ?? "…")}
              」当前内容。
            </p>
          )}
          {(childRuns ?? []).length > 0 && (
            <div className="task-tree">
              <span className="card-title">子任务（任务树）</span>
              {(childRuns ?? []).map((child) => (
                <div className="tree-child" key={child.id}>
                  <span className={`tree-state tree-${child.status}`}>
                    {child.status}
                  </span>
                  <span>
                    {profileName(child.profileId)} · 深度 {child.depth}
                  </span>
                  {child.error && (
                    <span className="tree-error">{child.error}</span>
                  )}
                  <button
                    type="button"
                    className="secondary"
                    onClick={() => openChildSession(child)}
                  >
                    查看子会话
                  </button>
                </div>
              ))}
              <p className="hint">
                子任务使用冻结的 worker
                配置与独立执行会话，不注入其他对话或长期记忆；权限与外发受本根任务上限及当前对话授权约束。
              </p>
            </div>
          )}
          <Transcript conversation={props.conversation} />
          <SearchRecords conversation={props.conversation} />
          <form className="task-form" onSubmit={sendTask}>
            <textarea
              value={prompt}
              disabled={sending}
              onChange={(event) => setPrompt(event.target.value)}
              placeholder="描述任务、粘贴网页 URL，或指定本地目录…"
              rows={3}
            />
            {attachments.length > 0 && (
              <ul className="attachment-list" aria-label="待发送附件">
                {attachments.map((file, index) => (
                  <li key={index}>
                    <span>
                      {file.name} · {Math.ceil(file.size / 1024)} KiB
                    </span>
                    <button
                      type="button"
                      className="secondary"
                      disabled={sending}
                      onClick={() => removeAttachment(index)}
                      aria-label={`移除 ${file.name}`}
                    >
                      移除
                    </button>
                  </li>
                ))}
              </ul>
            )}
            <div className="model-actions">
              <label className="attachment-picker">
                添加附件
                <input
                  aria-label="添加对话附件"
                  type="file"
                  multiple
                  accept="image/png,image/jpeg,image/webp,image/gif,.pdf,.docx,.txt,.md,.csv,.json,.yaml,.yml,.xml,.html,.log,.py,.js,.ts,.go"
                  disabled={
                    sending ||
                    loadingSession ||
                    !!activeRun ||
                    activeConversation?.kind === "delegation"
                  }
                  onChange={(event) => {
                    addAttachments(event.target.files);
                    event.target.value = "";
                  }}
                />
              </label>
              <label>
                <input
                  type="checkbox"
                  checked={activeConversation?.options?.webSearch ?? false}
                  disabled={
                    searchBusy ||
                    loadingSession ||
                    activeConversation?.kind === "delegation"
                  }
                  onChange={(event) => toggleSearch(event.target.checked)}
                />
                联网搜索
              </label>
              <button
                type="submit"
                disabled={
                  searchBusy ||
                  sending ||
                  (activeConversation?.options?.webSearch &&
                    searchAvailability?.available !== true) ||
                  loadingSession ||
                  !!activeRun ||
                  activeConversation?.kind === "delegation" ||
                  activeConversation?.profileArchived ||
                  !status?.storageReady ||
                  (!prompt.trim() && attachments.length === 0)
                }
              >
                发送任务
              </button>
              {activeRun && (
                <button
                  type="button"
                  className="secondary"
                  onClick={cancelTask}
                >
                  取消任务
                </button>
              )}
            </div>
            <p className="hint">
              支持图片、PDF、DOCX 和文本；最多8个，单个10MiB，总计20MiB；文本和
              DOCX
              提取正文限256KiB。目录路径可直接写入消息，读取受本机授权范围约束。
            </p>
          </form>
          {activeConversation?.options?.webSearch && (
            <p className="hint">
              {searchAvailability?.reason ?? "正在检查搜索能力…"}
              。粘贴 URL
              后可按需读取指定网页；仅影响后续任务，数据外发仍需授权。
            </p>
          )}
          {activeConversation?.options?.webSearch &&
            searchAvailability?.available === false && (
              <details className="search-configuration">
                <summary>配置联网搜索</summary>
                <p>
                  可在模型设置中选择支持原生搜索的 OpenAI、Anthropic 或 Gemini
                  模型，或接入一个搜索 MCP。
                </p>
                <p>
                  搜索 MCP 配置文件：
                  <code>
                    {searchAvailability.configPath ||
                      "用户 XDG 配置目录的 ICEPick/config.json"}
                  </code>
                  。在对应服务项填写 <code>webSearchTools</code>，例如{" "}
                  <code>{'{"webSearchTools":["web_search"]}'}</code>，再在
                  Profile 中允许该服务和工具。修改配置文件后重启 ICEPick。
                </p>
              </details>
            )}
          {activeRun && <p className="hint">任务正在运行…</p>}
          {taskError && <p className="error">{taskError}</p>}
          {runs[0]?.status === "failed" &&
            taskGraph?.coordination?.state !== "waiting" && (
              <p className="error">任务失败：{runs[0].error}</p>
            )}
          {taskGraph?.coordination?.state === "waiting" && (
            <p className="hint">
              等待补充信息；发送补充内容可继续对话，此前任务和执行记录会保留。
            </p>
          )}
          {runs[0]?.status === "cancelled" && (
            <p className="hint">任务已取消。</p>
          )}
        </>
      )}
    </section>
  );
}
