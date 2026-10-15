import { useEffect, useState } from "react";
import { StatusService } from "../../bindings/icepick";
import type { ProfileDraft, ProfileProposal, Run } from "../../bindings/icepick/internal/storage";

export function useProfileDrafts(sessionID: string | null, targetID: string | null, onApplied: (id: string) => Promise<void>) {
  const [drafts, setDrafts] = useState<ProfileDraft[]>([]);
  const [evidenceIDs, setEvidenceIDs] = useState("");
  useEffect(() => { setEvidenceIDs(""); }, [sessionID]);
  const [request, setRequest] = useState("");
  const [improve, setImprove] = useState(false);
  const [selected, setSelected] = useState("");
  const [editor, setEditor] = useState("");
  const [run, setRun] = useState<Run | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const draft = drafts.find((item) => item.id === selected);
  async function refresh() {
    const items = await StatusService.ListProfileDrafts();
    setDrafts(items ?? []);
    return items ?? [];
  }
  useEffect(() => { refresh().catch((e) => setError(String(e))); }, []);
  useEffect(() => {
    setEditor(draft ? JSON.stringify(draft.proposal, null, 2) : "");
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
          setSelected((items ?? []).find((item) => item.runId === next.id)?.id ?? "");
          setRun(next);
          if (next.status !== "completed") setError(next.error || next.status);
        }
      } catch (e) { if (current) setError(String(e)); }
      finally { querying = false; }
    }, 700);
    return () => { current = false; window.clearInterval(timer); };
  }, [run?.id, run?.status]);
  async function generate() {
    if (!sessionID) return;
    setBusy(true); setError("");
    try { setRun(await StatusService.StartProfileDraft(sessionID, improve ? targetID ?? "" : "", request, evidenceIDs.split(/[\s,]+/).filter(Boolean))); }
    catch (e) { setError(String(e)); }
    finally { setBusy(false); }
  }
  async function act(action: "save" | "apply" | "discard" | "revert") {
    if (!draft) return;
    setBusy(true); setError("");
    try {
      if (action === "save") {
        const proposal = JSON.parse(editor) as ProfileProposal;
        await StatusService.EditProfileDraft(draft.id, draft.revision, proposal);
      } else if (action === "discard") {
        await StatusService.DiscardProfileDraft(draft.id, draft.revision);
      } else {
        const profile = action === "apply"
          ? await StatusService.ApplyProfileDraft(draft.id, draft.revision)
          : await StatusService.RevertProfileDraft(draft.id, draft.revision);
        await onApplied(profile.id);
      }
      await refresh();
    } catch (e) { setError(String(e)); }
    finally { setBusy(false); }
  }
  async function cancel() {
    if (!run) return;
    try { await StatusService.CancelTask(run.id); }
    catch (e) { setError(String(e)); }
  }
  return { evidenceIDs,setEvidenceIDs,drafts, draft, request, setRequest, improve, setImprove, selected, setSelected, editor, setEditor, run, busy, error, generate, act, cancel, refresh };
}
export type ProfileDrafts = ReturnType<typeof useProfileDrafts>;
