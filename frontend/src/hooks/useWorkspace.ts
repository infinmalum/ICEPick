import { useEffect, useState } from "react";
import { StatusService } from "../../bindings/icepick";
import type { Conversation } from "../../bindings/icepick/internal/storage";
import { useDialog } from "../shared/dialogs";
export function useWorkspace() {
  const dialog = useDialog();
  const [workspaceError, setWorkspaceError] = useState("");
  const [sessions, setSessions] = useState<Conversation[]>([]);
  const [reuseContext, setReuseContext] = useState(false);
  const [selected, setSelected] = useState<string | null>(null);
  const [contextID, setContextID] = useState("");
  const [creating, setCreating] = useState(false);
  const [query, setQuery] = useState("");
  const [loaded, setLoaded] = useState(false);
  const activeConversation = sessions.find((s) => s.id === selected) ?? null;
  function openConversation(item: Conversation) {
    setContextID(item.contextId);
    setSelected(item.id);
    localStorage.setItem("icepick.lastConversation", item.id);
  }
  async function refreshConversations() {
    const items = (await StatusService.ListConversations()) ?? [];
    setSessions(items);
    return items;
  }
  async function createSession(profileID: string | null) {
    setCreating(true);
    try {
      let id = profileID;
      if (!id) {
        const profiles = (await StatusService.ListProfiles()) ?? [];
        id =
          (profiles.find((p) => p.isDefault) ?? profiles[0])?.id ??
          (await StatusService.CreateTemplateProfile("daily", "日常助手")).id;
      }
      const item = await StatusService.CreateConversation(
        id,
        reuseContext && selected ? selected : "",
        "",
      );
      await refreshConversations();
      openConversation(item);
      setReuseContext(false);
      setWorkspaceError("");
      return item;
    } catch (e) {
      setWorkspaceError(String(e));
      return null;
    } finally {
      setCreating(false);
    }
  }
  async function renameConversation(item: Conversation) {
    const title = await dialog.ask({
      title: "重命名对话",
      label: "对话名称",
      initial: item.title,
      confirm: "保存",
    });
    if (title === null) return;
    try {
      await StatusService.RenameConversation(item.id, title);
      await refreshConversations();
    } catch (e) {
      setWorkspaceError(String(e));
    }
  }
  async function deleteConversation(item: Conversation) {
    if (
      !(await dialog.confirm({
        title: "删除这个对话？",
        body: "将删除此对话、附件、执行记录和未采用的建议，无法恢复。助手配置和独立管理的记忆不会删除。",
        confirm: "删除对话",
        danger: true,
      }))
    )
      return;
    try {
      await StatusService.DeleteConversation(item.id);
      const items = await refreshConversations();
      if (selected === item.id) {
        if (items[0]) openConversation(items[0]);
        else {
          setSelected(null);
          setContextID("");
          localStorage.removeItem("icepick.lastConversation");
        }
      }
    } catch (e) {
      setWorkspaceError(String(e));
    }
  }
  useEffect(() => {
    let active = true;
    StatusService.ListConversations()
      .then((items) => {
        if (!active) return;
        setSessions(items ?? []);
        const last = (items ?? []).find(
          (i) => i.id === localStorage.getItem("icepick.lastConversation"),
        );
        if (last ?? items?.[0]) openConversation((last ?? items?.[0])!);
      })
      .catch((e) => setWorkspaceError(String(e)))
      .finally(() => {
        if (active) setLoaded(true);
      });
    return () => {
      active = false;
    };
  }, []);
  return {
    loaded,
    workspaceError,
    sessions,
    setSessions,
    reuseContext,
    setReuseContext,
    selected,
    contextID,
    creating,
    activeConversation,
    openConversation,
    refreshConversations,
    createSession,
    query,
    setQuery,
    renameConversation,
    deleteConversation,
  };
}
export type Workspace = ReturnType<typeof useWorkspace>;
