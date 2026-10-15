import type { ProfileDrafts as DraftState } from "../hooks/useProfileDrafts";
export function ProfileDrafts({ state, hasConversation, targetName }: { state: DraftState; hasConversation: boolean; targetName: string }) {
  const { draft, run, busy } = state;
  const running = run?.status === "running";
  const unsaved = draft && state.editor !== JSON.stringify(draft.proposal, null, 2);
  return <section className="session-card">
    <div className="card-title">AI 配置草案</div>
    <p className="hint">用当前对话 Profile 的配置草案组件生成，单次调用，只读取明确提供的任务证据，不隐式读取历史或执行工具。采用前检查私人信息和依赖；草案尚未经过场景验收。</p>
    <label className="checkbox-label"><input type="checkbox" checked={state.improve} onChange={(e) => state.setImprove(e.target.checked)} />改进当前编辑的 Profile：{targetName || "未选择"}</label>
    <label>配置需求<textarea rows={3} value={state.request} onChange={(e) => state.setRequest(e.target.value)} placeholder="例如：创建一个面向初学者的写作助手，直接执行，不使用工具，允许按需审查。" /></label>
    <label>任务证据 Run ID（可选，最多3条，当前上下文已结束任务；逗号分隔）<input value={state.evidenceIDs} onChange={(e) => state.setEvidenceIDs(e.target.value)} /></label>
    <button disabled={!hasConversation || busy || running || !state.request.trim() || (state.improve && !targetName)} onClick={state.generate}>{state.improve ? "生成改进草案" : "生成新 Profile 草案"}</button>
    {!hasConversation && <p className="hint">先选择对话，用它的模型配置与数据外发授权生成草案。</p>}
    {running && <><p className="hint">草案生成中：{run.id}</p><button className="secondary" onClick={state.cancel}>取消草案生成</button></>}
    <button className="secondary" disabled={busy} onClick={() => state.refresh().catch(() => {})}>刷新草案</button>
    <select aria-label="配置草案" value={state.selected} onChange={(e) => state.setSelected(e.target.value)}><option value="">选择草案</option>{state.drafts.map((item) => <option key={item.id} value={item.id}>{item.proposal.name} · {item.state} · 草案v{item.revision}</option>)}</select>
    {draft && <>
      <p>{draft.proposal.summary}</p>
      <p className="hint">需求：{draft.request}</p>
      <p className="hint">{draft.targetProfileId ? `基础：${draft.base.name} v${draft.baseRevision}；影响${draft.conversations}个对话及${draft.impact?.length ?? 0}个引用配置的后续运行。在途任务保留快照。` : "采用后创建独立 Profile，不切换当前对话。"}</p>
      {(draft.impact ?? []).map((item) => <p className="hint" key={item.profileId}>引用：{item.name} · worker {(item.workers ?? []).join(", ")}</p>)}
      <p className="hint">证据任务：{(draft.evidence ?? []).map((item) => item.rootRunId).join(", ") || "未选择"}；引用：{(draft.proposal.evidenceRefs ?? []).join(", ") || "无"}</p>
      <ul>{(draft.proposal.expectedChecks ?? []).map((item,i) => <li key={i}>待验收：{item}</li>)}</ul>
      <ul>{(draft.proposal.reasons ?? []).map((reason, i) => <li key={i}>{reason}</li>)}</ul>
      <ul>{(draft.proposal.limitations ?? []).map((reason, i) => <li key={i}>{reason}</li>)}</ul>
      <details><summary>查看已保存的配置差异（{(draft.changes ?? []).length}项）</summary>{(draft.changes ?? []).map((item) => <div key={item.field}><strong>{item.field}</strong><pre>{JSON.stringify(item.before, null, 2)}</pre><pre>{JSON.stringify(item.after, null, 2)}</pre></div>)}</details>
      <details><summary>编辑草案 JSON</summary><textarea rows={18} value={state.editor} disabled={draft.state !== "draft"} onChange={(e) => state.setEditor(e.target.value)} /><button disabled={busy || draft.state !== "draft" || !unsaved} onClick={() => state.act("save")}>校验并保存草案</button></details>
      {draft.state === "draft" && <><button disabled={busy || !!unsaved} onClick={() => state.act("apply")}>采用已保存草案</button><button className="secondary" disabled={busy} onClick={() => state.act("discard")}>丢弃草案</button>{unsaved && <p className="hint">先保存编辑，再检查差异并采用。</p>}</>}
      {draft.state === "applied" && draft.targetProfileId && <button className="secondary" disabled={busy} onClick={() => state.act("revert")}>撤销此配置改动</button>}
    </>}
    {state.error && <p className="error">{state.error}</p>}
  </section>;
}
