import { useSkillDrafts } from "./useSkillDrafts";
import { useProfileDrafts } from "./useProfileDrafts";
import { useState } from "react";
import { useWorkspace } from "./useWorkspace";
import { useRuntimeSettings } from "./useRuntimeSettings";
import { useProfiles } from "./useProfiles";
import { useLocalContext } from "./useLocalContext";
import { useConversation } from "./useConversation";
export function useApp() {
  const workspace = useWorkspace();
  const runtime = useRuntimeSettings();
  const profiles = useProfiles(async () => {
    await workspace.refreshConversations();
  });
  const local = useLocalContext(workspace.contextID, workspace.selected);
  const conversation = useConversation(
    workspace,
    profiles.profiles,
    runtime.status,
  );
  const skillDrafts = useSkillDrafts(workspace.selected);
  const drafts = useProfileDrafts(
    workspace.selected,
    profiles.profileEditID,
    async (id) => {
      await profiles.refreshProfiles();
      await workspace.refreshConversations();
      profiles.setProfileEditID(id);
    },
  );
  const [page, setPage] = useState<"chat" | "assistants" | "settings">("chat");
  const [settingsTab, setSettingsTab] = useState("model");
  async function createSession() {
    const item = await workspace.createSession(
      workspace.activeConversation?.profileArchived
        ? null
        : (workspace.activeConversation?.profileId ?? null),
    );
    if (item) setPage("chat");
  }
  return {
    workspace,
    drafts,
    skillDrafts,
    runtime,
    profileState: profiles,
    local,
    conversation,
    error: runtime.error || workspace.workspaceError,
    page,
    setPage,
    settingsTab,
    setSettingsTab,
    createSession,
  };
}
