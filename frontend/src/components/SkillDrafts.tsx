import type { SkillDrafts as DraftState } from "../hooks/useSkillDrafts";
export function SkillDrafts({ state, hasConversation }: { state: DraftState; hasConversation: boolean }) {
  const { draft, run, busy } = state;
  const running = run?.status === "running";
  const unsaved = draft && state.editor !== JSON.stringify(draft.proposal, null, 2);
  return <section className="session-card">
    <div className="card-title">AI Skill 草案</div>
    <p className="hint">只生成单个 SKILL.md，不执行工具、脚本或自动修改配置。采用会发布独立副本，保留源 Skill 和显式引用；选择全部 Skills 的配置在后续运行中可见新副本。</p>
    <label>源 Skill 名称（留空新建）<input value={state.source} onChange={(e) => state.setSource(e.target.value)} /></label>
    <label>Skill 需求<textarea rows={3} value={state.request} onChange={(e) => state.setRequest(e.target.value)} /></label>
    <label>任务证据 Run ID（可选，最多3条，当前上下文已结束任务；逗号分隔）<input value={state.evidenceIDs} onChange={(e) => state.setEvidenceIDs(e.target.value)} /></label>
    <button disabled={!hasConversation || busy || running || !state.request.trim()} onClick={state.generate}>生成 Skill 草案</button>
    {!hasConversation && <p className="hint">先选择对话，使用其 improver 配置与外发授权。</p>}
    {running && <button className="secondary" onClick={state.cancel}>取消 Skill 草案生成</button>}
    <button className="secondary" disabled={busy} onClick={() => state.refresh().catch(() => {})}>刷新 Skill 草案</button>
    <select aria-label="Skill 草案" value={state.selected} onChange={(e) => state.setSelected(e.target.value)}><option value="">选择 Skill 草案</option>{state.drafts.map((item) => <option key={item.id} value={item.id}>{item.proposal.summary} · {item.state} · v{item.revision}</option>)}</select>
    {draft && <>
      <p>{draft.proposal.summary}</p>
      <p className="hint">需求：{draft.request}</p>
      <p className="hint">源 Skill：{draft.source.name || "新建"}；原引用配置 {(draft.impact ?? []).map((item) => item.name).join(", ") || "无"}。采用后不替换这些显式引用。</p>
      <p className="hint">全部 Skills 配置：{(draft.allSkillsImpact ?? []).map((item) => item.name).join(", ") || "无"}；采用后可见新副本。</p>
      <p className="hint">证据任务：{(draft.evidence ?? []).map((item) => item.rootRunId).join(", ") || "未选择"}；引用：{(draft.proposal.evidenceRefs ?? []).join(", ") || "无"}</p>
      <ul>{(draft.proposal.reasons ?? []).map((item,i) => <li key={i}>{item}</li>)}</ul>
      <ul>{(draft.proposal.expectedChecks ?? []).map((item,i) => <li key={i}>待验收：{item}</li>)}</ul>
      <ul>{(draft.proposal.limitations ?? []).map((item,i) => <li key={i}>{item}</li>)}</ul>
      <details><summary>查看源内容与候选</summary><pre>{draft.source.content || "无源内容"}</pre><pre>{draft.proposal.content}</pre></details>
      <details><summary>编辑 Skill 草案 JSON</summary><textarea rows={18} value={state.editor} disabled={draft.state !== "draft"} onChange={(e) => state.setEditor(e.target.value)} /><button disabled={busy || draft.state !== "draft" || !unsaved} onClick={() => state.act("save")}>校验并保存 Skill 草案</button></details>
      {draft.state === "draft" && <><p className="hint">采用前检查私人资料、凭据和依赖；没有执行候选验收。先保存编辑，再查看候选内容。</p><button disabled={busy || !!unsaved} onClick={() => state.act("apply")}>采用为独立 Skill</button><button className="secondary" disabled={busy} onClick={() => state.act("discard")}>丢弃 Skill 草案</button></>}
      {draft.state === "applied" && <button className="secondary" disabled={busy} onClick={() => state.act("withdraw")}>撤销 Skill 副本</button>}
      {draft.state === "applied" && <p className="hint">已发布：{draft.published.name}。可在 Profile 的 Skills 选择中使用；在途任务保留原快照。</p>}
    </>}
    {state.error && <p className="error">{state.error}</p>}
  </section>;
}
