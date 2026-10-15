import { useEffect, useRef, useState } from "react";
import { Events } from "@wailsio/runtime";
import { StatusService, type AppStatus } from "../../bindings/icepick";
import type {
  SearchRequest,
  TaskReview,
  TaskRetrospective,
  TaskGraph,
  Message,
  Profile,
  Run,
  SessionSummary,
  ToolCall,
} from "../../bindings/icepick/internal/storage";
import type {
  SearchAvailability,
  SessionPlanView,
} from "../../bindings/icepick/internal/app";
import type { Workspace } from "./useWorkspace";
export function useConversation(
  workspace: Workspace,
  profiles: Profile[],
  status: AppStatus | null,
) {
  const {
    contextID,
    selected,
    activeConversation,
    setSessions,
    openConversation,
    refreshConversations,
  } = workspace;
  const [messages, setMessages] = useState<Message[]>([]);
  const [toolCalls, setToolCalls] = useState<ToolCall[]>([]);
  const [approvalBusy, setApprovalBusy] = useState(false);
  const [runs, setRuns] = useState<Run[]>([]);
  const [activeRun, setActiveRun] = useState<string | null>(null);
  const [prompt, setPrompt] = useState("");
  const [attachments, setAttachments] = useState<File[]>([]);
  useEffect(() => {
    setAttachments([]);
    setPrompt("");
  }, [selected]);
  function addAttachments(files: FileList | null) {
    if (!files) return;
    const next = [...attachments, ...Array.from(files)];
    if (
      next.length > 8 ||
      next.some((f) => f.size > 10 * 1024 * 1024) ||
      next.reduce((n, f) => n + f.size, 0) > 20 * 1024 * 1024
    ) {
      setTaskError("最多8个附件，单个10MiB，总计20MiB。");
      return;
    }
    setAttachments(next);
    setTaskError("");
  }
  function removeAttachment(index: number) {
    setAttachments((current) => current.filter((_, i) => i !== index));
  }
  const [taskError, setTaskError] = useState("");
  const [searchAvailability, setSearchAvailability] =
    useState<SearchAvailability | null>(null);
  const [searchRecords, setSearchRecords] = useState<SearchRequest[]>([]);
  const [searchBusy, setSearchBusy] = useState(false);
  const [sending, setSending] = useState(false);
  const [loadingSession, setLoadingSession] = useState(false);
  const [streamText, setStreamText] = useState("");
  const [sessionSummary, setSessionSummary] = useState<SessionSummary | null>(
    null,
  );
  const [sessionPlan, setSessionPlan] = useState<SessionPlanView | null>(null);
  const [reviews, setReviews] = useState<TaskReview[]>([]);
  const [retrospectives, setRetrospectives] = useState<TaskRetrospective[]>([]);
  const [reviewBusy, setReviewBusy] = useState(false);
  const [taskGraph, setTaskGraph] = useState<TaskGraph | null>(null);
  const [childRuns, setChildRuns] = useState<Run[]>([]);
  function refreshSessionKnowledge(id: string) {
    const current = () =>
      activeContextID.current === contextID && activeSessionID.current === id;
    StatusService.ListConversationSearch(id)
      .then((value) => {
        if (current()) setSearchRecords(value ?? []);
      })
      .catch(() => {});
    StatusService.GetSessionSummary(id)
      .then((value) => {
        if (current()) setSessionSummary(value);
      })
      .catch(() => {
        if (current()) setSessionSummary(null);
      });
    StatusService.GetSessionPlan(id)
      .then((value) => {
        if (current()) setSessionPlan(value);
      })
      .catch(() => {
        if (current()) setSessionPlan(null);
      });
  }
  const activeContextID = useRef(contextID);
  activeContextID.current = contextID;
  const activeSessionID = useRef(selected);
  activeSessionID.current = selected;
  useEffect(() => {
    if (!selected) {
      setMessages([]);
      setRuns([]);
      setToolCalls([]);
      setActiveRun(null);
      setLoadingSession(false);
      return;
    }
    let cancelled = false;
    setStreamText("");
    setMessages([]);
    setRuns([]);
    setToolCalls([]);
    setChildRuns([]);
    setSessionSummary(null);
    setSessionPlan(null);
    setTaskError("");
    setLoadingSession(true);
    setTaskGraph(null);
    setReviews([]);
    setRetrospectives([]);
    Promise.all([
      StatusService.ListMessages(selected),
      StatusService.ListRuns(selected),
      StatusService.ListToolCalls(selected),
    ])
      .then(([history, previousRuns, calls]) => {
        if (cancelled) return;
        setMessages(history ?? []);
        setRuns(previousRuns ?? []);
        setToolCalls(calls ?? []);
        setActiveRun(
          (previousRuns ?? []).find((item) => item.status === "running")?.id ??
            null,
        );
        setLoadingSession(false);
        refreshChildRuns((previousRuns ?? [])[0]?.id);
      })
      .catch((reason: unknown) => {
        if (!cancelled) {
          setTaskError(String(reason));
          setLoadingSession(false);
        }
      });
    refreshSessionKnowledge(selected);
    return () => {
      cancelled = true;
    };
  }, [contextID, selected]);
  useEffect(() => {
    if (!activeRun || !selected) return;
    let cancelled = false;
    // Reconnect: pick up the streamed-but-uncommitted text so a page reload
    // or reconnect keeps the live transcript continuous.
    StatusService.PendingStreamText(activeRun)
      .then((value) => {
        if (!cancelled) setStreamText(value ?? "");
      })
      .catch(() => {});
    const timer = window.setInterval(async () => {
      try {
        const [run, history, calls, pending] = await Promise.all([
          StatusService.GetRun(activeRun),
          StatusService.ListMessages(selected),
          StatusService.ListToolCalls(selected),
          StatusService.PendingStreamText(activeRun),
        ]);
        if (cancelled) return;
        setMessages(history ?? []);
        setToolCalls(calls ?? []);
        setStreamText(pending ?? "");
        setRuns((current) => [
          run,
          ...current.filter((item) => item.id !== run.id),
        ]);
        refreshChildRuns(run.id);
        refreshSessionKnowledge(selected);
        if (run.status !== "running") setActiveRun(null);
      } catch (reason) {
        if (!cancelled) setTaskError(String(reason));
      }
    }, 800);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [activeRun, contextID, selected]);
  useEffect(() => {
    if (!activeRun) return;
    const off = Events.On("icepick:service-event", (event) => {
      const detail =
        (event.data as {
          type?: string;
          runId?: string;
          detail?: string;
        }) ?? {};
      if (
        detail.type === "stream.delta" &&
        detail.runId === activeRun &&
        detail.detail
      ) {
        setStreamText((current) => current + detail.detail);
      }
    });
    return () => {
      off();
    };
  }, [activeRun]);
  useEffect(() => {
    setSearchAvailability(null);
    setSearchRecords([]);
    if (!selected) return;
    let cancelled = false;
    StatusService.ConversationSearchAvailability(selected)
      .then((value) => {
        if (!cancelled) setSearchAvailability(value);
      })
      .catch((reason) => {
        if (!cancelled) setTaskError(String(reason));
      });
    return () => {
      cancelled = true;
    };
  }, [selected, activeConversation?.profileId, profiles, status?.model]);
  async function toggleSearch(enabled: boolean) {
    if (!selected || !activeConversation) return;
    const id = selected;
    setSearchBusy(true);
    try {
      const conversation = await StatusService.SetConversationOptions(id, {
        webSearch: enabled,
      });
      setSessions((current) =>
        current.map((item) => (item.id === id ? conversation : item)),
      );
      if (activeSessionID.current === id) setTaskError("");
    } catch (reason) {
      if (activeSessionID.current === id) setTaskError(String(reason));
    } finally {
      setSearchBusy(false);
    }
  }
  useEffect(() => {
    if (!selected) return;
    const off = Events.On("icepick:service-event", (event) => {
      const detail =
        (event.data as {
          type?: string;
          sessionId?: string;
        }) ?? {};
      if (
        detail.type === "conversation.updated" &&
        detail.sessionId === selected
      ) {
        StatusService.GetConversation(selected)
          .then((conversation) =>
            setSessions((current) =>
              current.map((item) =>
                item.id === conversation.id ? conversation : item,
              ),
            ),
          )
          .catch(() => {});
      }
    });
    return () => off();
  }, [selected]);
  const pendingCalls = toolCalls.filter((item) => item.state === "pending");
  const unknownCalls = toolCalls.filter(
    (item) => item.state === "execution_unknown",
  );
  async function refreshCalls() {
    if (!selected) return;
    try {
      setToolCalls((await StatusService.ListToolCalls(selected)) ?? []);
    } catch (reason) {
      setTaskError(String(reason));
    }
  }
  async function approveCall(call: ToolCall) {
    setApprovalBusy(true);
    try {
      await StatusService.ApproveToolCall(call.id);
      setTaskError("");
      await refreshCalls();
    } catch (reason) {
      setTaskError(String(reason));
    } finally {
      setApprovalBusy(false);
    }
  }
  async function denyCall(call: ToolCall) {
    const note =
      window.prompt(`拒绝 ${call.toolName} 的原因（可留空）`, "") ?? "";
    setApprovalBusy(true);
    try {
      await StatusService.DenyToolCall(call.id, note);
      setTaskError("");
      await refreshCalls();
    } catch (reason) {
      setTaskError(String(reason));
    } finally {
      setApprovalBusy(false);
    }
  }
  async function resolveUnknown(call: ToolCall, asSuccess: boolean) {
    const note =
      window.prompt(
        `处置依据（必填）：说明你核实到的 ${call.toolName} 的实际情况`,
        "",
      ) ?? "";
    if (!note.trim()) return;
    let result = "";
    if (asSuccess) {
      result = window.prompt("经核对的实际结果（必填）", "") ?? "";
      if (!result.trim()) return;
    }
    setApprovalBusy(true);
    try {
      await StatusService.ResolveToolCall(
        call.id,
        asSuccess ? "succeeded" : "failed",
        result,
        note,
      );
      setTaskError("");
      await refreshCalls();
    } catch (reason) {
      setTaskError(String(reason));
    } finally {
      setApprovalBusy(false);
    }
  }
  async function selectConversationProfile(profileID: string) {
    if (!selected) return;
    try {
      await StatusService.SelectConversationProfile(selected, profileID);
      await refreshConversations();
      setTaskError("");
    } catch (reason) {
      setTaskError(String(reason));
    }
  }
  async function clearPlan() {
    if (!selected) return;
    try {
      await StatusService.ClearSessionPlan(selected);
      setSessionPlan(null);
      setTaskError("");
    } catch (reason) {
      setTaskError(String(reason));
    }
  }
  async function refreshChildRuns(latestRunID: string | undefined) {
    if (!latestRunID) {
      setTaskGraph(null);
      setReviews([]);
      setRetrospectives([]);
      setChildRuns([]);
      return;
    }
    try {
      const sessionID = activeSessionID.current;
      const [items, graph, reviewItems, retrospectiveItems] = await Promise.all(
        [
          StatusService.ListChildRuns(latestRunID),
          StatusService.GetTaskGraph(latestRunID).catch(() => null),
          StatusService.ListTaskReviews(latestRunID).catch(() => []),
          StatusService.ListTaskRetrospectives(latestRunID).catch(() => []),
        ],
      );
      if (
        activeContextID.current === contextID &&
        activeSessionID.current === sessionID
      ) {
        setChildRuns(items ?? []);
        setTaskGraph(graph);
        setReviews(reviewItems ?? []);
        setRetrospectives(retrospectiveItems ?? []);
      }
    } catch {
      setChildRuns([]);
    }
  }
  async function openChildSession(child: Run) {
    try {
      const item = await StatusService.GetConversation(child.sessionId);
      setSessions((current) =>
        current.some((c) => c.id === item.id) ? current : [item, ...current],
      );
      openConversation(item);
    } catch (reason) {
      setTaskError(String(reason));
    }
  }
  async function sendTask(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selected || (!prompt.trim() && !attachments.length) || sending) return;
    const sessionID = selected;
    setSending(true);
    setStreamText("");
    try {
      const uploads = await Promise.all(
        attachments.map(
          (file) =>
            new Promise<{ name: string; data: string }>((resolve, reject) => {
              const reader = new FileReader();
              reader.onerror = () =>
                reject(new Error(`读取 ${file.name} 失败`));
              reader.onload = () =>
                resolve({
                  name: file.name,
                  data: String(reader.result).split(",")[1],
                });
              reader.readAsDataURL(file);
            }),
        ),
      );
      const run = await StatusService.StartConversationTask(
        sessionID,
        prompt,
        uploads,
      );
      if (activeSessionID.current !== sessionID) return;
      setAttachments([]);
      setPrompt("");
      setTaskError("");
      setRuns((current) => [run, ...current]);
      setActiveRun(run.id);
      const history = await StatusService.ListMessages(sessionID);
      if (activeSessionID.current === sessionID) setMessages(history ?? []);
    } catch (reason) {
      if (activeSessionID.current === sessionID) setTaskError(String(reason));
    } finally {
      setSending(false);
    }
  }
  async function cancelTask() {
    if (!activeRun) return;
    try {
      await StatusService.CancelTask(activeRun);
      setTaskError("");
    } catch (reason) {
      setTaskError(String(reason));
    }
  }
  async function inspectResult(retrospective: boolean) {
    if (!selected || !runs[0]) return;
    setReviewBusy(true);
    try {
      const sessionID = selected;
      const start = retrospective
        ? StatusService.StartConversationRetrospective
        : StatusService.StartConversationReview;
      const target =
        retrospectives[0]?.rootRunId ?? reviews[0]?.rootRunId ?? runs[0].id;
      const run = await start(sessionID, target);
      if (activeSessionID.current !== sessionID) return;
      setRuns((current) => [run, ...current]);
      setActiveRun(run.id);
    } catch (reason) {
      setTaskError(String(reason));
    } finally {
      setReviewBusy(false);
    }
  }
  return {
    attachments,
    addAttachments,
    removeAttachment,
    messages,
    approvalBusy,
    runs,
    activeRun,
    prompt,
    setPrompt,
    taskError,
    searchAvailability,
    searchRecords,
    searchBusy,
    sending,
    loadingSession,
    streamText,
    sessionSummary,
    sessionPlan,
    reviews,
    reviewBusy,
    taskGraph,
    childRuns,
    toggleSearch,
    pendingCalls,
    unknownCalls,
    approveCall,
    denyCall,
    resolveUnknown,
    selectConversationProfile,
    clearPlan,
    openChildSession,
    sendTask,
    cancelTask,
    reviewResult: () => inspectResult(false),
    retrospectResult: () => inspectResult(true),
    retrospectives,
  };
}
export type ConversationState = ReturnType<typeof useConversation>;
