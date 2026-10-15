import type { ConversationState } from "../hooks/useConversation";
import { stateLabel } from "../shared/errors";
type Props = {
  onImprove?: () => void;
  conversation: Pick<
    ConversationState,
    | "clearPlan"
    | "reviewBusy"
    | "reviewResult"
    | "retrospectResult"
    | "retrospectives"
    | "reviews"
    | "runs"
    | "sessionPlan"
    | "sessionSummary"
    | "taskGraph"
  >;
};
export function TaskProgress({ conversation: c, onImprove }: Props) {
  const run = c.runs[0];
  return (
    <>
      {c.sessionSummary && (
        <p className="compact-note">已自动整理较早的聊天内容，原消息仍保留。</p>
      )}
      {run && (
        <details className="task-details">
          <summary>{stateLabel(run.status)} · 任务详情</summary>
          {c.taskGraph?.coordination?.reason && (
            <p>{c.taskGraph.coordination.reason}</p>
          )}
          <ol className="progress-list">
            {(c.taskGraph?.tasks ?? []).map((item) => (
              <li key={item.spec.id}>
                <span>{stateLabel(item.state)}</span> {item.spec.task}
                {item.input && <p>已补充：{item.input}</p>}
              </li>
            ))}
          </ol>
          {!c.taskGraph?.tasks?.length &&
            c.sessionPlan?.steps?.map((step) => (
              <p key={step.id}>{step.title}</p>
            ))}
          {run.status !== "running" && run.status !== "waiting" && (
            <div className="actions">
              <button
                className="secondary"
                disabled={c.reviewBusy}
                onClick={c.reviewResult}
              >
                检查这个结果
              </button>
              <button
                className="secondary"
                disabled={c.reviewBusy}
                onClick={c.retrospectResult}
              >
                回顾这次工作
              </button>
              {onImprove && (
                <button className="secondary" onClick={onImprove}>
                  根据这次工作改进助手
                </button>
              )}
            </div>
          )}
          {c.reviews.map((r) => (
            <section key={r.id}>
              <strong>
                {stateLabel(r.result.status)}
                {!r.current ? "（内容已变化）" : ""}
              </strong>
              <p>{r.result.summary}</p>
              {r.result.defects?.map((d, i) => (
                <p key={i}>{d.description}</p>
              ))}
            </section>
          ))}
          {c.retrospectives.map((r) => (
            <section key={r.id}>
              <p>{r.result.summary}</p>
              <ul>
                {r.result.recommendations?.map((v, i) => (
                  <li key={i}>{v}</li>
                ))}
              </ul>
              <p>{r.result.limitations?.join("；")}</p>
            </section>
          ))}
          <details className="developer-settings">
            <summary>开发者记录</summary>
            <pre>
              {JSON.stringify(
                {
                  run,
                  graph: c.taskGraph,
                  plan: c.sessionPlan,
                  summary: c.sessionSummary,
                  reviews: c.reviews,
                  retrospectives: c.retrospectives,
                },
                null,
                2,
              )}
            </pre>
            {c.sessionPlan && (
              <button className="secondary" onClick={c.clearPlan}>
                清除会话待办
              </button>
            )}
          </details>
        </details>
      )}
    </>
  );
}
