import type { Workspace } from "../hooks/useWorkspace";
import type { Profiles } from "../hooks/useProfiles";
import type { ConversationState } from "../hooks/useConversation";
type Props = {
  workspace: Pick<Workspace, "activeConversation">;
  profileState: Pick<Profiles, "profiles">;
  conversation: Pick<ConversationState, "selectConversationProfile">;
};
export function ConversationProfile(props: Props) {
  const { activeConversation } = props.workspace;
  const { profiles } = props.profileState;
  const { selectConversationProfile } = props.conversation;
  return (
    activeConversation && (
      <section className="session-card">
        <div className="card-title">此对话后续使用的 Profile</div>
        <select
          disabled={activeConversation.kind === "delegation"}
          value={
            activeConversation.profileArchived
              ? ""
              : activeConversation.profileId
          }
          onChange={(event) => selectConversationProfile(event.target.value)}
        >
          <option value="" disabled>
            请选择可用 Profile
          </option>
          {profiles.map((item) => (
            <option key={item.id} value={item.id}>
              {item.name}（v{item.revision}）
            </option>
          ))}
        </select>
        <p className="hint">
          切换保留此对话历史、记忆与权限；正在执行的任务使用启动时的配置。
        </p>
        {activeConversation.profileArchived && (
          <p className="error">
            原 Profile 已删除；历史可继续查看，启动新任务前请选择可用配置。
          </p>
        )}
      </section>
    )
  );
}
