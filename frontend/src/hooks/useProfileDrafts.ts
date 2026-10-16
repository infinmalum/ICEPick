import { useEffect, useState } from "react";
import { useSendConsent } from "../shared/consent";
import { useDialog } from "../shared/dialogs";
import { StatusService } from "../../bindings/icepick";
import type {
  ProfileDraft,
  ProfileProposal,
  Run,
} from "../../bindings/icepick/internal/storage";

export function useProfileDrafts(
  sessionID: string | null,
  targetID: string | null,
  onApplied: (id: string) => Promise<void>,
) {
  const ensureConsent = useSendConsent();
  const dialog = useDialog();
  const [drafts, setDrafts] = useState<ProfileDraft[]>([]);
  const [evidenceIDs, setEvidenceIDs] = useState("");
  useEffect(() => {
    setEvidenceIDs("");
  }, [sessionID]);
  const [request, setRequest] = useState("");
  const [improve, setImprove] = useState(false);
  const [selected, chooseSelected] = useState("");
  // The proposal is edited as a typed object; raw JSON is only an advanced
  // view. This keeps broken JSON from silently disabling the friendly fields.
  const [proposal, setProposal] = useState<ProfileProposal | null>(null);
  const [run, setRun] = useState<Run | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const draft = drafts.find((item) => item.id === selected);
  async function setSelected(id: string) {
    if (
      draft &&
      proposal &&
      JSON.stringify(proposal) !== JSON.stringify(draft.proposal) &&
      !(await dialog.confirm({
        title: "放弃尚未保存的建议修改？",
        body: "先保存修改可以继续保留它。",
        confirm: "放弃并切换",
        danger: true,
      }))
    )
      return;
    chooseSelected(id);
  }
  async function refresh() {
    const items = await StatusService.ListProfileDrafts();
    setDrafts(items ?? []);
    return items ?? [];
  }
  useEffect(() => {
    refresh().catch((e) => setError(String(e)));
  }, []);
  useEffect(() => {
    setProposal(draft ? structuredClone(draft.proposal) : null);
  }, [draft?.id, draft?.revision]);
  useEffect(() => {
    if (!run || run.status !== "running") return;
    let current = true;
    let querying = false;
    const timer = window.setInterval(async () => {
      if (querying) return;
      querying = true;
      try {
        const next = await StatusService.GetRun(run.id);
        if (!current) return;
        if (next.status !== "running") {
          const items = await StatusService.ListProfileDrafts();
          if (!current) return;
          setDrafts(items ?? []);
          setSelected(
            (items ?? []).find((item) => item.runId === next.id)?.id ?? "",
          );
          setRun(next);
          if (next.status !== "completed") setError(next.error || next.status);
        }
      } catch (e) {
        if (current) setError(String(e));
      } finally {
        querying = false;
      }
    }, 700);
    return () => {
      current = false;
      window.clearInterval(timer);
    };
  }, [run?.id, run?.status]);
  async function generate() {
    if (!sessionID) return;
    setBusy(true);
    setError("");
    try {
      if (!(await ensureConsent(sessionID, false, "improver"))) return;
      setRun(
        await StatusService.StartProfileDraft(
          sessionID,
          improve ? (targetID ?? "") : "",
          request,
          evidenceIDs.split(/[\s,]+/).filter(Boolean),
        ),
      );
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  async function act(action: "save" | "apply" | "discard" | "revert") {
    if (!draft) return;
    setBusy(true);
    setError("");
    try {
      if (action === "save") {
        if (!proposal) return;
        if (!proposal.name.trim()) {
          setError("请先填写助手名称。");
          return;
        }
        await StatusService.EditProfileDraft(
          draft.id,
          draft.revision,
          proposal,
        );
      } else if (action === "discard") {
        await StatusService.DiscardProfileDraft(draft.id, draft.revision);
      } else {
        const profile =
          action === "apply"
            ? await StatusService.ApplyProfileDraft(draft.id, draft.revision)
            : await StatusService.RevertProfileDraft(draft.id, draft.revision);
        await onApplied(profile.id);
      }
      await refresh();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  async function cancel() {
    if (!run) return;
    try {
      await StatusService.CancelTask(run.id);
    } catch (e) {
      setError(String(e));
    }
  }
  return {
    evidenceIDs,
    setEvidenceIDs,
    drafts,
    draft,
    request,
    setRequest,
    improve,
    setImprove,
    selected,
    setSelected,
    proposal,
    setProposal,
    run,
    busy,
    error,
    generate,
    act,
    cancel,
    refresh,
  };
}
export type ProfileDrafts = ReturnType<typeof useProfileDrafts>;
