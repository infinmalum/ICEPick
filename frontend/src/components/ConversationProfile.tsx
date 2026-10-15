import type { Workspace } from "../hooks/useWorkspace";
import type { Profiles } from "../hooks/useProfiles";
import type { ConversationState } from "../hooks/useConversation";
export function ConversationProfile({
  workspace: w,
  profileState: p,
  conversation: c,
}: {
  workspace: Workspace;
  profileState: Profiles;
  conversation: ConversationState;
}) {
  return w.activeConversation ? (
    <label className="assistant-selector">
      <span className="sr-only">当前对话的助手</span>
      <select
        aria-label="当前助手"
        value={
          w.activeConversation.profileArchived
            ? ""
            : w.activeConversation.profileId
        }
        disabled={w.activeConversation.kind === "delegation"}
        onChange={(e) => c.selectConversationProfile(e.target.value)}
      >
        <option value="" disabled>
          请选择助手
        </option>
        {p.profiles.map((v) => (
          <option key={v.id} value={v.id}>
            {v.name}
          </option>
        ))}
      </select>
    </label>
  ) : (
    <span>日常助手</span>
  );
}
