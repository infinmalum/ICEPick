import type { ConversationState } from "../hooks/useConversation";
import { ErrorNotice, stateLabel } from "../shared/errors";
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
    | "toolHistory"
    | "workerTraces"
    | "workerBusy"
    | "traceError"
    | "loadWorkerTraces"
  >;
};
export function TaskProgress({ conversation: c, onImprove }: Props) {
  const run = c.runs[0];
  const rootRunId = c.taskGraph?.rootRunId ?? run?.id ?? "";
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
          {c.toolHistory.length > 0 && (
            <details>
              <summary>工具活动 · {c.toolHistory.length} 次</summary>
              <ul className="activity-list">
                {c.toolHistory.slice(0, 40).map((call) => (
                  <li key={call.id}>
                    <span className="memory-meta">
                      {new Date(call.createdAt).toLocaleTimeString()} ·{" "}
                      {stateLabel(call.state)}
                    </span>{" "}
                    <code>{call.toolName}</code>
                    {call.error && <span> 失败：{call.error}</span>}
                  </li>
                ))}
              </ul>
            </details>
          )}
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
            <details>
              <summary>工作过程（worker 轨迹）</summary>
              <p className="hint">
                委派 worker 在这个根任务内的工具调用与显式观察，来自任务自身的记录。
              </p>
              <button
                type="button"
                className="secondary"
                disabled={c.workerBusy || !rootRunId}
                onClick={c.loadWorkerTraces}
              >
                {c.workerBusy ? "正在读取…" : "查看工作过程"}
              </button>
              <ErrorNotice error={c.traceError} />
              {c.workerTraces.length > 0 && (
                <ul className="activity-list">
                  {c.workerTraces.map((trace) => {
                    const text = trace.content || trace.error;
                    return (
                      <li key={trace.id}>
                        <span className="memory-meta">
                          {new Date(trace.createdAt).toLocaleTimeString()} ·{" "}
                          {trace.tool} · {stateLabel(trace.state)}
                        </span>
                        {text && (
                          <p>
                            {text.length > 200 ? text.slice(0, 200) + "…" : text}
                          </p>
                        )}
                      </li>
                    );
                  })}
                </ul>
              )}
            </details>
          </details>
        </details>
      )}
    </>
  );
}
