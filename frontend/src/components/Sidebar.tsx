import type { Workspace } from "../hooks/useWorkspace";
import type { RuntimeSettings } from "../hooks/useRuntimeSettings";
import type { Profiles } from "../hooks/useProfiles";
type Props = {
  workspace: Pick<
    Workspace,
    | "activeConversation"
    | "creating"
    | "openConversation"
    | "reuseContext"
    | "selected"
    | "sessions"
    | "setReuseContext"
  >;
  runtime: Pick<RuntimeSettings, "mcpServers" | "status">;
  profileState: Pick<
    Profiles,
    "profileEditID" | "profiles" | "setProfileEditID"
  >;
  createSession: () => Promise<void>;
};
export function Sidebar(props: Props) {
  const {
    activeConversation,
    creating,
    openConversation,
    reuseContext,
    selected,
    sessions,
    setReuseContext,
  } = props.workspace;
  const { mcpServers, status } = props.runtime;
  const { profileEditID, profiles, setProfileEditID } = props.profileState;
  const { createSession } = props;
  return (
    <aside className="sidebar">
      <div className="brand">
        <span className="brand-mark">◈</span> ICEPick
      </div>
      <div className="sidebar-label">工作区</div>
      <div className="sidebar-item active">概览</div>
      <div className="sidebar-label sessions-label">Profiles</div>
      {profiles.map((item) => (
        <button
          key={item.id}
          className={`sidebar-item session-item ${profileEditID === item.id ? "selected" : ""}`}
          onClick={() => setProfileEditID(item.id)}
        >
          {item.name}
        </button>
      ))}
      <div className="sidebar-label sessions-label">对话</div>
      <button
        className="new-session"
        onClick={createSession}
        disabled={creating || !profileEditID || !status?.storageReady}
      >
        ＋ 使用所选 Profile 开始对话
      </button>
      {selected && activeConversation?.kind !== "delegation" && (
        <label className="checkbox-label">
          <input
            type="checkbox"
            checked={reuseContext}
            onChange={(event) => setReuseContext(event.target.checked)}
          />
          新对话沿用当前记忆与权限
        </label>
      )}
      {sessions.map((session) => (
        <button
          key={session.id}
          className={`sidebar-item session-item ${selected === session.id ? "selected" : ""}`}
          onClick={() => openConversation(session)}
        >
          {session.title}
          <span className="context-profile-hint">
            {" "}
            · {session.profileName}
            {session.profileArchived ? "（已删除）" : ""}
          </span>
        </button>
      ))}
      <div className="sidebar-label sessions-label">MCP 服务</div>
      {mcpServers.map((server) => (
        <div
          key={server.name}
          className="sidebar-item muted"
          title={
            server.changed
              ? `${server.command}${(server.args ?? []).join(" ")}\n指纹变化：二进制 ${server.binarySha256.slice(0, 12)}… schema ${server.schemaSha256.slice(0, 12)}…\n自动执行已暂停，需要重新确认`
              : `${server.command}${(server.args ?? []).join(" ")}\n版本 ${server.version || "未知"} · 二进制 ${server.binarySha256.slice(0, 12)}… · schema ${server.schemaSha256.slice(0, 12)}…`
          }
        >
          MCP · {server.name}
          {server.changed ? " ⚠" : ""}
        </div>
      ))}
      <div className="sidebar-spacer" />
      <div className="sidebar-foot">本地 Agent 宿主</div>
    </aside>
  );
}
