import type { Profiles } from "../hooks/useProfiles";

type Props = { profileState: Pick<Profiles, "profileDraft" | "setProfileDraft"> };

export function ProfileStages({ profileState }: Props) {
  const { profileDraft, setProfileDraft } = profileState;
  if (!profileDraft || profileDraft.data.execution.workflow !== "fixed") return null;
  const stages = profileDraft.data.execution.stages ?? [];
  const workers = profileDraft.data.execution.workerSources ?? [];
  const setStages = (next: typeof stages) => setProfileDraft({
    ...profileDraft,
    data: { ...profileDraft.data, execution: { ...profileDraft.data.execution, stages: next } },
  });
  const update = (index: number, patch: Partial<typeof stages[number]>) =>
    setStages(stages.map((stage, i) => i === index ? { ...stage, ...patch } : stage));
  return (
    <section aria-label="固定流程阶段">
      <div className="card-title">固定阶段</div>
      <p className="hint">宿主按依赖执行全部阶段，无需 planner。无依赖的只读阶段可并行，写入及未知副作用阶段串行；每次派发仍受审批约束。阶段材料包含本次用户目标和直接前置的交付。首版失败即停止，不支持条件、等待或自动返工。</p>
      {stages.map((stage, index) => (
        <div className="model-form" key={index}>
          <label>阶段 ID<input value={stage.id} onChange={e => update(index, { id: e.target.value })} /></label>
          <label>Worker 槽位<select value={stage.worker} onChange={e => update(index, { worker: e.target.value })}>
            {!workers.some(w => w.name === stage.worker) && <option value={stage.worker}>{stage.worker || "请选择"}（缺失）</option>}
            {workers.map(w => <option key={w.name} value={w.name}>{w.name}</option>)}
          </select></label>
          <label>任务<textarea rows={3} value={stage.task} onChange={e => update(index, { task: e.target.value })} /></label>
          <label>补充材料<textarea rows={2} value={stage.materials ?? ""} onChange={e => update(index, { materials: e.target.value })} /></label>
          <label>前置阶段 ID（逗号分隔）<input value={(stage.dependsOn ?? []).join(",")} onChange={e => update(index, { dependsOn: e.target.value.split(",").map(s => s.trim()).filter(Boolean) })} /></label>
          <label>访问方式<select value={stage.accessMode ?? "unknown"} onChange={e => update(index, { accessMode: e.target.value })}>
            <option value="read">只读（禁止写入、命令与外部未知副作用工具）</option>
            <option value="write">写入（串行）</option>
            <option value="unknown">未知副作用（串行）</option>
          </select></label>
          <label>验收要求<textarea rows={2} value={stage.acceptance ?? ""} onChange={e => update(index, { acceptance: e.target.value })} /></label>
          <button type="button" className="secondary" onClick={() => setStages(stages.filter((_, i) => i !== index))}>移除此阶段</button>
        </div>
      ))}
      <button type="button" className="secondary" disabled={stages.length >= 20} onClick={() => {
        let n = 1;
        while (stages.some(s => s.id === `stage${n}`)) n++;
        setStages([...stages, { id: `stage${n}`, worker: workers[0]?.name ?? "", task: "", accessMode: "read", dependsOn: [], acceptance: "", materials: "" }]);
      }}>＋ 添加阶段</button>
      <p className="hint">保存时检查 ID、worker 引用、依赖环与预算。改名或删除阶段后请同时修改依赖。无后继的阶段均须成功，再由 main 汇总，必要时执行独立审查。配置修改只影响新任务。</p>
    </section>
  );
}
