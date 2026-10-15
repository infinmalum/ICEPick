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
import { useDialog } from "../shared/dialogs";
import { useSendConsent } from "../shared/consent";
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
  const dialog = useDialog();
  const ensureConsent = useSendConsent();
  const draftCache = useRef(
    new Map<string, { prompt: string; attachments: File[] }>(),
  );
  const [messages, setMessages] = useState<Message[]>([]);
  const [hasEarlier, setHasEarlier] = useState(false);
  const [loadingEarlier, setLoadingEarlier] = useState(false);
  const earliestLoaded = useRef(0);
  async function fetchMessages(id: string) {
    const page = await StatusService.ListChatMessages(id, 0, 80);
    if (activeSessionID.current === id && earliestLoaded.current === 0) {
      earliestLoaded.current = page.beforeSeq;
      setHasEarlier(page.hasMore);
    }
    return page.messages ?? [];
  }
  function mergeMessages(next: Message[]) {
    setMessages((current) =>
      next.length
        ? [...current.filter((m) => m.seq < next[0].seq), ...next]
        : current,
    );
  }
  async function loadEarlier() {
    if (!selected || loadingEarlier) return;
    const id = selected;
    setLoadingEarlier(true);
    try {
      const page = await StatusService.ListChatMessages(
        id,
        earliestLoaded.current,
        80,
      );
      if (activeSessionID.current === id) {
        setMessages((current) => [...(page.messages ?? []), ...current]);
        earliestLoaded.current = page.beforeSeq;
        setHasEarlier(page.hasMore);
      }
    } catch (e) {
      setTaskError(String(e));
    } finally {
      setLoadingEarlier(false);
    }
  }
  const [toolCalls, setToolCalls] = useState<ToolCall[]>([]);
  const [approvalBusy, setApprovalBusy] = useState(false);
  const [runs, setRuns] = useState<Run[]>([]);
  const [activeRun, setActiveRun] = useState<string | null>(null);
  const [prompt, setPrompt] = useState("");
  const [attachments, setAttachments] = useState<File[]>([]);
  useEffect(() => {
    const draft = selected ? draftCache.current.get(selected) : null;
    let text = "";
    try {
      text = selected
        ? (sessionStorage.getItem("icepick.draft." + selected) ?? "")
        : "";
    } catch {}
    setPrompt(draft?.prompt ?? text);
    setAttachments(draft?.attachments ?? []);
  }, [selected]);
  function updateDraft(text: string, files: File[]) {
    if (!selected) return;
    draftCache.current.set(selected, { prompt: text, attachments: files });
    try {
      sessionStorage.setItem("icepick.draft." + selected, text);
    } catch {}
  }
  function changePrompt(value: string) {
    setPrompt(value);
    updateDraft(value, attachments);
  }
  function addAttachments(files: FileList | File[] | null) {
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
    updateDraft(prompt, next);
    setTaskError("");
  }
  function removeAttachment(index: number) {
    const next = attachments.filter((_, i) => i !== index);
    setAttachments(next);
    updateDraft(prompt, next);
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
    earliestLoaded.current = 0;
    setHasEarlier(false);
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
      fetchMessages(selected),
      StatusService.ListRuns(selected),
      StatusService.PendingApprovals(contextID),
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
    let querying = false;
    const timer = window.setInterval(async () => {
      if (querying) return;
      querying = true;
      try {
        const [run, history, calls, pending] = await Promise.all([
          StatusService.GetRun(activeRun),
          fetchMessages(selected),
          StatusService.PendingApprovals(contextID),
          StatusService.PendingStreamText(activeRun),
        ]);
        if (cancelled) return;
        mergeMessages(history ?? []);
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
      } finally {
        querying = false;
      }
    }, 1000);
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
      setToolCalls((await StatusService.PendingApprovals(contextID)) ?? []);
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
    const note = await dialog.ask({
      title: "拒绝这次操作",
      body: "助手会收到你的决定，并尝试其他可行方法。",
      label: "原因（可选）",
      confirm: "拒绝操作",
    });
    if (note === null) return;
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
    const note = await dialog.ask({
      title: asSuccess ? "记录已核实的结果" : "记录未完成的操作",
      body: "只记录你已实际核实的情况。无法确认时选择“暂不确定”，助手将保持暂停，不会重做可能已经生效的操作。",
      label: "你核对到了什么？",
      confirm: "记录核对结论",
      cancel: "暂不确定",
    });
    if (!note?.trim()) return;
    let result = "";
    if (asSuccess) {
      result =
        (await dialog.ask({
          title: "保存实际结果",
          body: "填写实际看到的结果，供助手继续判断。不要猜测工具输出。",
          label: "实际结果",
          confirm: "保存",
          cancel: "暂不确定",
        })) ?? "";
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
    await submitTask(false);
  }
  async function resumeTask() {
    await submitTask(true);
  }
  async function submitTask(continuation: boolean) {
    if (
      !selected ||
      (!continuation && !prompt.trim() && !attachments.length) ||
      sending
    )
      return;
    const sessionID = selected;
    setSending(true);
    setStreamText("");
    try {
      if (
        !continuation &&
        activeConversation?.options?.webSearch &&
        searchAvailability?.available !== true
      ) {
        if (
          !(await dialog.confirm({
            title: "当前模型暂不支持联网",
            body: "可以关闭联网，继续普通聊天；也可以到模型设置选择支持联网的模型。",
            confirm: "关闭联网并发送",
            cancel: "返回修改",
          }))
        )
          return;
        await StatusService.SetConversationOptions(sessionID, {
          webSearch: false,
        });
        await refreshConversations();
      }
      if (
        !continuation &&
        !(await ensureConsent(sessionID, attachments.length > 0))
      )
        return;
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
      const run = continuation
        ? await StatusService.ContinueConversationTask(
            runs[0].id,
            taskGraph?.waitingTaskId ?? "",
            prompt,
            uploads,
          )
        : await StatusService.StartConversationTask(sessionID, prompt, uploads);
      draftCache.current.delete(sessionID);
      try {
        sessionStorage.removeItem("icepick.draft." + sessionID);
      } catch {}
      if (activeSessionID.current !== sessionID) return;
      setAttachments([]);
      setPrompt("");
      setTaskError("");
      setRuns((current) => [
        run,
        ...current.filter((item) => item.id !== run.id),
      ]);
      setActiveRun(run.id);
      await refreshConversations();
      const history = await fetchMessages(sessionID);
      if (activeSessionID.current === sessionID) mergeMessages(history ?? []);
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
      setRuns((current) => [
        run,
        ...current.filter((item) => item.id !== run.id),
      ]);
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
    hasEarlier,
    loadingEarlier,
    loadEarlier,
    approvalBusy,
    runs,
    activeRun,
    prompt,
    setPrompt: changePrompt,
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
    resumeTask,
    cancelTask,
    reviewResult: () => inspectResult(false),
    retrospectResult: () => inspectResult(true),
    retrospectives,
  };
}
export type ConversationState = ReturnType<typeof useConversation>;
