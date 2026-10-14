import { useEffect, useState } from "react";
import { StatusService } from "../../bindings/icepick";
import type { Conversation } from "../../bindings/icepick/internal/storage";
export function useWorkspace() {
  const [workspaceError, setWorkspaceError] = useState("");
  const [sessions, setSessions] = useState<Conversation[]>([]);
  const [reuseContext, setReuseContext] = useState(false);
  const [contextID, setContextID] = useState("");
  const [selected, setSelected] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const activeConversation =
    sessions.find((item) => item.id === selected) ?? null;
  async function refreshConversations() {
    setSessions((await StatusService.ListConversations()) ?? []);
  }
  function openConversation(item: Conversation) {
    setContextID(item.contextId);
    setSelected(item.id);
  }
  async function createSession(profileEditID: string | null) {
    setCreating(true);
    try {
      if (!profileEditID) throw new Error("请先创建或选择一个 Profile");
      const session = await StatusService.CreateConversation(
        profileEditID,
        reuseContext && selected && activeConversation?.kind !== "delegation"
          ? selected
          : "",
        "",
      );
      await refreshConversations();
      openConversation(session);
      setReuseContext(false);
      setWorkspaceError("");
    } catch (reason) {
      setWorkspaceError(String(reason));
    } finally {
      setCreating(false);
    }
  }
  useEffect(() => {
    refreshConversations().catch((reason) => setWorkspaceError(String(reason)));
  }, []);
  return {
    sessions,
    setSessions,
    reuseContext,
    setReuseContext,
    contextID,
    selected,
    creating,
    activeConversation,
    refreshConversations,
    openConversation,
    createSession,
    workspaceError,
  };
}
export type Workspace = ReturnType<typeof useWorkspace>;
