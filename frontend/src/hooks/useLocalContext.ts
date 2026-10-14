import { useEffect, useRef, useState } from "react";
import { StatusService } from "../../bindings/icepick";
import type {
  ContextPolicy,
  LocalContext,
  ExfilGrant,
  Memory,
} from "../../bindings/icepick/internal/storage";
export function useLocalContext(contextID: string, selected: string | null) {
  const activeContextID = useRef(contextID);
  activeContextID.current = contextID;
  const [contextDraft, setContextDraft] = useState<LocalContext | null>(null);
  const [contextBusy, setContextBusy] = useState(false);
  const [contextError, setContextError] = useState("");
  const [grants, setGrants] = useState<ExfilGrant[]>([]);
  const [grantForm, setGrantForm] = useState<{
    destination: string;
    scopes: string[];
  }>({ destination: "", scopes: [] });
  const [grantBusy, setGrantBusy] = useState(false);
  const [grantError, setGrantError] = useState("");
  const [memories, setMemories] = useState<Memory[]>([]);
  const [memoryDraft, setMemoryDraft] = useState("");
  const [memoryEdit, setMemoryEdit] = useState<{
    id: string;
    content: string;
  } | null>(null);
  const [memoryBusy, setMemoryBusy] = useState(false);
  const [memoryError, setMemoryError] = useState("");
  useEffect(() => {
    let current = true;
    setGrants([]);
    setGrantError("");
    if (!contextID) return;
    StatusService.ListExfilGrants(contextID)
      .then((items) => {
        if (current) setGrants(items ?? []);
      })
      .catch((reason) => {
        if (current) setGrantError(String(reason));
      });
    return () => {
      current = false;
    };
  }, [contextID]);
  useEffect(() => {
    let current = true;
    setMemories([]);
    setMemoryError("");
    setMemoryEdit(null);
    if (!contextID) return;
    StatusService.ListMemories(contextID)
      .then((items) => {
        if (current) setMemories(items ?? []);
      })
      .catch((reason) => {
        if (current) setMemoryError(String(reason));
      });
    return () => {
      current = false;
    };
  }, [contextID]);
  useEffect(() => {
    let current = true;
    setContextDraft(null);
    setContextError("");
    if (selected)
      StatusService.GetLocalContext(contextID)
        .then((item) => {
          if (current) setContextDraft(item);
        })
        .catch((reason) => {
          if (current) setContextError(String(reason));
        });
    return () => {
      current = false;
    };
  }, [contextID, selected]);
  async function refreshContext() {
    if (selected) {
      const item = await StatusService.GetLocalContext(contextID);
      if (activeContextID.current === contextID) setContextDraft(item);
    }
  }
  async function saveContext(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!contextDraft || !selected) return;
    setContextBusy(true);
    try {
      await StatusService.UpdateContextPolicy(contextID, contextDraft.policy);
      await refreshContext();
      setContextError("");
    } catch (reason) {
      setContextError(String(reason));
    } finally {
      setContextBusy(false);
    }
  }
  async function saveGrant(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!grantForm.destination.trim() || grantForm.scopes.length === 0) return;
    setGrantBusy(true);
    try {
      await StatusService.GrantExfil(
        contextID,
        grantForm.destination,
        grantForm.scopes,
      );
      const updated = (await StatusService.ListExfilGrants(contextID)) ?? [];
      if (activeContextID.current !== contextID) return;
      setGrants(updated);
      setGrantForm({ destination: "", scopes: [] });
      setGrantError("");
    } catch (reason) {
      setGrantError(String(reason));
    } finally {
      setGrantBusy(false);
    }
  }
  async function revokeGrant(destination: string) {
    if (
      !window.confirm(
        `撤销向 ${destination} 发送会话数据的授权？后续远端发送将被拒绝。`,
      )
    )
      return;
    setGrantBusy(true);
    try {
      await StatusService.RevokeExfil(contextID, destination);
      const updated = (await StatusService.ListExfilGrants(contextID)) ?? [];
      if (activeContextID.current !== contextID) return;
      setGrants(updated);
      setGrantError("");
    } catch (reason) {
      setGrantError(String(reason));
    } finally {
      setGrantBusy(false);
    }
  }
  async function refreshMemories(contextID: string) {
    const items = (await StatusService.ListMemories(contextID)) ?? [];
    if (activeContextID.current === contextID) setMemories(items);
  }
  async function addMemory(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!memoryDraft.trim()) return;
    setMemoryBusy(true);
    try {
      await StatusService.AddMemory(contextID, memoryDraft);
      await refreshMemories(contextID);
      setMemoryDraft("");
      setMemoryError("");
    } catch (reason) {
      setMemoryError(String(reason));
    } finally {
      setMemoryBusy(false);
    }
  }
  async function saveMemoryEdit() {
    if (!memoryEdit || !memoryEdit.content.trim()) return;
    setMemoryBusy(true);
    try {
      await StatusService.UpdateMemory(
        contextID,
        memoryEdit.id,
        memoryEdit.content,
      );
      await refreshMemories(contextID);
      setMemoryEdit(null);
      setMemoryError("");
    } catch (reason) {
      setMemoryError(String(reason));
    } finally {
      setMemoryBusy(false);
    }
  }
  async function deleteMemory(id: string) {
    if (!window.confirm("删除这条记忆？删除后不可恢复。")) return;
    setMemoryBusy(true);
    try {
      await StatusService.DeleteMemory(contextID, id);
      await refreshMemories(contextID);
      setMemoryError("");
    } catch (reason) {
      setMemoryError(String(reason));
    } finally {
      setMemoryBusy(false);
    }
  }
  async function exportMemory() {
    try {
      const markdown = await StatusService.ExportMemories(contextID);
      const blob = new Blob([markdown], {
        type: "text/markdown;charset=utf-8",
      });
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = `icepick-memory-${contextID}.md`;
      anchor.click();
      URL.revokeObjectURL(url);
      setMemoryError("");
    } catch (reason) {
      setMemoryError(String(reason));
    }
  }
  function updatePolicy(patch: Partial<ContextPolicy>) {
    if (!contextDraft) return;
    setContextDraft({
      ...contextDraft,
      policy: { ...contextDraft.policy, ...patch },
    });
  }
  function updatePermissions(patch: Partial<ContextPolicy["permissions"]>) {
    if (!contextDraft) return;
    setContextDraft({
      ...contextDraft,
      policy: {
        ...contextDraft.policy,
        permissions: { ...contextDraft.policy.permissions, ...patch },
      },
    });
  }
  return {
    contextDraft,
    contextBusy,
    contextError,
    grants,
    grantForm,
    setGrantForm,
    grantBusy,
    grantError,
    memories,
    memoryDraft,
    setMemoryDraft,
    memoryEdit,
    setMemoryEdit,
    memoryBusy,
    memoryError,
    saveContext,
    saveGrant,
    revokeGrant,
    addMemory,
    saveMemoryEdit,
    deleteMemory,
    exportMemory,
    updatePolicy,
    updatePermissions,
  };
}
export type LocalContextState = ReturnType<typeof useLocalContext>;
