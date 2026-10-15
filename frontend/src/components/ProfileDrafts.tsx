import type { ProfileDrafts as DraftState } from "../hooks/useProfileDrafts";
import { TaskEvidence, type EvidenceOption } from "./TaskEvidence";
import { ErrorNotice, stateLabel } from "../shared/errors";
export function ProfileDrafts({
  state: s,
  hasConversation,
  targetName,
  tasks = [],
}: {
  state: DraftState;
  hasConversation: boolean;
  targetName: string;
  tasks?: EvidenceOption[];
}) {
  const d = s.draft;
  let proposal = d?.proposal;
  try {
    proposal = JSON.parse(s.editor);
  } catch {}
  const unsaved = !!d && s.editor !== JSON.stringify(d.proposal, null, 2);
  return (
    <section className="session-card">
      <h2>让 AI 帮你设计助手</h2>
      <p>描述想完成的工作，先生成可检查的建议，由你决定是否采用。</p>
      <label className="checkbox-label">
        <input
          type="checkbox"
          checked={s.improve}
          onChange={(e) => s.setImprove(e.target.checked)}
        />
        改进正在编辑的助手：{targetName || "请先选择助手"}
      </label>
      <label>
        你的想法
        <textarea
          value={s.request}
          onChange={(e) => s.setRequest(e.target.value)}
          placeholder="例如：帮我创建一个能把长资料整理成简明报告的助手"
          rows={3}
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
          !s.request.trim() ||
          (s.improve && !targetName)
        }
        onClick={s.generate}
      >
        {s.run?.status === "running" ? "正在准备建议…" : "生成建议"}
      </button>
      {!hasConversation && <p>先开始一个对话，选择要使用的模型。</p>}
      {s.run?.status === "running" && (
        <button className="secondary" onClick={s.cancel}>
          停止生成
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
              {v.proposal.name} · {stateLabel(v.state)}
            </option>
          ))}
        </select>
      </label>
      {d && proposal && (
        <>
          <p>{d.proposal.summary}</p>
          <ul>
            {d.proposal.reasons?.map((v, i) => (
              <li key={i}>{v}</li>
            ))}
          </ul>
          <label>
            助手名称
            <input
              value={proposal.name}
              disabled={d.state !== "draft"}
              onChange={(e) =>
                s.setEditor(
                  JSON.stringify(
                    { ...proposal, name: e.target.value },
                    null,
                    2,
                  ),
                )
              }
            />
          </label>
          <label>
            工作方式
            <textarea
              rows={5}
              disabled={d.state !== "draft"}
              value={proposal.data.systemPrompt}
              onChange={(e) =>
                s.setEditor(
                  JSON.stringify(
                    {
                      ...proposal,
                      data: { ...proposal!.data, systemPrompt: e.target.value },
                    },
                    null,
                    2,
                  ),
                )
              }
            />
          </label>
          <p>建议尚未经过实际场景验证，采用前请检查内容和依赖。</p>
          {d.proposal.limitations?.map((v, i) => (
            <p key={i}>{v}</p>
          ))}
          <details className="developer-settings">
            <summary>开发者设置 · 完整草案与依据</summary>
            <textarea
              rows={12}
              value={s.editor}
              disabled={d.state !== "draft"}
              onChange={(e) => s.setEditor(e.target.value)}
            />
            <pre>
              {JSON.stringify(
                {
                  changes: d.changes,
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
                  采用建议
                </button>
                <button
                  className="secondary"
                  disabled={s.busy}
                  onClick={() => s.act("discard")}
                >
                  丢弃
                </button>
              </>
            )}
            {d.state === "applied" && d.targetProfileId && (
              <button className="secondary" onClick={() => s.act("revert")}>
                撤销这次修改
              </button>
            )}
          </div>
        </>
      )}
      <ErrorNotice error={s.error} />
    </section>
  );
}
