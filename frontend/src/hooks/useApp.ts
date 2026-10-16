import { useSkillDrafts } from "./useSkillDrafts";
import { useProfileDrafts } from "./useProfileDrafts";
import { useEffect, useState } from "react";
import { Events } from "@wailsio/runtime";
import { StatusService } from "../../bindings/icepick";
import type { SkillInfo } from "../../bindings/icepick/internal/app";
import type { MCPServer } from "../../bindings/icepick/internal/config";
import { useWorkspace } from "./useWorkspace";
import { useRuntimeSettings } from "./useRuntimeSettings";
import { useProfiles } from "./useProfiles";
import { useLocalContext } from "./useLocalContext";
import { useConversation } from "./useConversation";
export type ActivityEntry = {
  at: number;
  type: string;
  sessionId: string;
  runId: string;
  detail: string;
};
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
  // The installed-capability catalogs: assistant editing picks from these
  // instead of typing names, and the MCP panel edits the same source.
  const [skillCatalog, setSkillCatalog] = useState<SkillInfo[]>([]);
  const [mcpCatalog, setMcpCatalog] = useState<MCPServer[]>([]);
  const [catalogError, setCatalogError] = useState("");
  async function refreshCapabilities() {
    try {
      const [skills, servers] = await Promise.all([
        StatusService.ListSkills(),
        StatusService.ListConfiguredMCPServers(),
      ]);
      setSkillCatalog(skills ?? []);
      setMcpCatalog(servers ?? []);
      setCatalogError("");
    } catch (reason) {
      setCatalogError(String(reason));
    }
  }
  useEffect(() => {
    void refreshCapabilities();
  }, []);
  // Every post-commit host event reaches the webview already; keep a rolling
  // activity view so "what is the host doing" is visible without the CLI.
  const [activity, setActivity] = useState<ActivityEntry[]>([]);
  useEffect(() => {
    const off = Events.On("icepick:service-event", (event) => {
      const detail =
        (event.data as {
          type?: string;
          sessionId?: string;
          runId?: string;
          detail?: string;
        }) ?? {};
      setActivity((current) =>
        [
          {
            at: Date.now(),
            type: detail.type ?? "unknown",
            sessionId: detail.sessionId ?? "",
            runId: detail.runId ?? "",
            detail: detail.detail ?? "",
          },
          ...current,
        ].slice(0, 200),
      );
    });
    return () => off();
  }, []);
  async function createSession(profileID?: string) {
    setPage("chat");
    workspace.setQuery("");
    const item = await workspace.createSession(
      profileID ??
        (workspace.activeConversation?.profileArchived
          ? null
          : (workspace.activeConversation?.profileId ?? null)),
    );
    if (item) await profiles.refreshProfiles().catch(profiles.reportError);
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
    skillCatalog,
    mcpCatalog,
    catalogError,
    refreshCapabilities,
    activity,
  };
}
