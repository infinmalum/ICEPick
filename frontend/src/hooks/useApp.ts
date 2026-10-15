import { useSkillDrafts } from "./useSkillDrafts";
import { useProfileDrafts } from "./useProfileDrafts";
import { useEffect } from "react";
import { useWorkspace } from "./useWorkspace";
import { useRuntimeSettings } from "./useRuntimeSettings";
import { useProfiles } from "./useProfiles";
import { useLocalContext } from "./useLocalContext";
import { useConversation } from "./useConversation";
export function useApp() {
  const workspace = useWorkspace();
  const runtime = useRuntimeSettings();
  const profiles = useProfiles(workspace.refreshConversations);
  const local = useLocalContext(workspace.contextID, workspace.selected);
  const conversation = useConversation(
    workspace,
    profiles.profiles,
    runtime.status,
  );
  const skillDrafts = useSkillDrafts(workspace.selected);
  const drafts = useProfileDrafts(workspace.selected, profiles.profileEditID, async (id) => {
    await profiles.refreshProfiles();
    await workspace.refreshConversations();
    profiles.setProfileEditID(id);
  });
  const active = workspace.activeConversation;
  useEffect(() => {
    if (active && !active.profileArchived)
      profiles.setProfileEditID(active.profileId);
  }, [active?.id, active?.profileId]);
  return {
    workspace,
    drafts,
    skillDrafts,
    runtime,
    profileState: profiles,
    local,
    conversation,
    error: runtime.error || workspace.workspaceError,
    createSession: () => workspace.createSession(profiles.profileEditID),
  };
}
