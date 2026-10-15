import type { SkillDrafts as DraftState } from "../hooks/useSkillDrafts";
import { TaskEvidence, type EvidenceOption } from "./TaskEvidence";
import { ErrorNotice, stateLabel } from "../shared/errors";
export function SkillDrafts({
  state: s,
  hasConversation,
  tasks = [],
  useMethod,
}: {
  state: DraftState;
  hasConversation: boolean;
  tasks?: EvidenceOption[];
  useMethod?: (name: string) => Promise<void>;
}) {
  const d = s.draft;
  let proposal = d?.proposal;
  try {
    proposal = JSON.parse(s.editor);
  } catch {}
  const unsaved = !!d && s.editor !== JSON.stringify(d.proposal, null, 2);
  const header =
    proposal?.content.match(/^---\r?\n[\s\S]*?\r?\n---\r?\n/)?.[0] ?? "";
  return (
    <section className="session-card">
      <h2>教助手一种工作方法</h2>
      <p>
        例如固定的报告写法、资料检查步骤或你的表达习惯。生成后由你检查并保存。
      </p>
      <label>
        希望助手学会什么？
        <textarea
          rows={3}
          value={s.request}
          onChange={(e) => s.setRequest(e.target.value)}
        />
      </label>
      <TaskEvidence
        options={tasks}
        value={s.evidenceIDs}
        onChange={s.setEvidenceIDs}
      />
      <button
        disabled={
          !hasConversation ||
          s.busy ||
          s.run?.status === "running" ||
          !s.request.trim()
        }
        onClick={s.generate}
      >
        {s.run?.status === "running" ? "正在准备…" : "生成工作方法"}
      </button>
      {s.run?.status === "running" && (
        <button className="secondary" onClick={s.cancel}>
          停止
        </button>
      )}
      <label>
        已保存的建议
        <select
          value={s.selected}
          onChange={(e) => s.setSelected(e.target.value)}
        >
          <option value="">选择建议</option>
          {s.drafts.map((v) => (
            <option key={v.id} value={v.id}>
              {v.proposal.summary} · {stateLabel(v.state)}
            </option>
          ))}
        </select>
      </label>
      {d && proposal && (
        <>
          <p>{d.proposal.summary}</p>
          <label>
            具体步骤
            <textarea
              rows={8}
              disabled={d.state !== "draft"}
              value={proposal.content.slice(header.length)}
              onChange={(e) =>
                s.setEditor(
                  JSON.stringify(
                    { ...proposal, content: header + e.target.value },
                    null,
                    2,
                  ),
                )
              }
            />
          </label>
          <p>不会自动执行这些步骤。采用前请检查私人信息和所需服务。</p>
          <details className="developer-settings">
            <summary>开发者设置 · 来源与完整内容</summary>
            <label>
              现有 Skill 名称（留空新建）
              <input
                value={s.source}
                onChange={(e) => s.setSource(e.target.value)}
              />
            </label>
            <textarea
              rows={12}
              value={s.editor}
              onChange={(e) => s.setEditor(e.target.value)}
            />
            <pre>
              {JSON.stringify(
                {
                  source: d.source,
                  impact: d.impact,
                  evidence: d.proposal.evidenceRefs,
                },
                null,
                2,
              )}
            </pre>
          </details>
          <div className="actions">
            {d.state === "draft" && (
              <>
                <button
                  className="secondary"
                  disabled={s.busy || !unsaved}
                  onClick={() => s.act("save")}
                >
                  保存修改
                </button>
                <button
                  disabled={s.busy || unsaved}
                  onClick={() => s.act("apply")}
                >
                  保存工作方法
                </button>
                <button className="secondary" onClick={() => s.act("discard")}>
                  丢弃
                </button>
              </>
            )}
            {d.state === "applied" && (
              <>
                <button onClick={() => useMethod?.(d.published.name)}>
                  用于正在编辑的助手
                </button>
                <button className="secondary" onClick={() => s.act("withdraw")}>
                  撤销此方法
                </button>
              </>
            )}
          </div>
        </>
      )}
      <ErrorNotice error={s.error} />
    </section>
  );
}
