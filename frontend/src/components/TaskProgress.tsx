import type { ConversationState } from "../hooks/useConversation";
import { scopeLabels } from "../shared/config";
type Props = {
  conversation: Pick<
    ConversationState,
    | "clearPlan"
    | "reviewBusy"
    | "reviewResult"
    | "reviews"
    | "runs"
    | "sessionPlan"
    | "sessionSummary"
    | "taskGraph"
  >;
};
export function TaskProgress(props: Props) {
  const {
    clearPlan,
    reviewBusy,
    reviewResult,
    reviews,
    runs,
    sessionPlan,
    sessionSummary,
    taskGraph,
  } = props.conversation;
  return (
    <>
      {sessionSummary && (
        <details className="summary-banner" open>
          <summary>
            已压缩历史 · 覆盖至消息 #{sessionSummary.coversSeq} · 范围{" "}
            {scopeLabels[sessionSummary.scope] ?? sessionSummary.scope}
            （原文仍在下方记录中）
          </summary>
          <pre className="summary-text">{sessionSummary.summary}</pre>
        </details>
      )}
      {sessionPlan && sessionPlan.steps && (
        <div className="plan-panel">
          <div className="plan-head">
            <span className="card-title">
              当前计划 · 修订 {sessionPlan.revision}
            </span>
            <button type="button" className="secondary" onClick={clearPlan}>
              清除计划
            </button>
          </div>
          <ul className="plan-steps">
            {sessionPlan.steps.map((step, index) => (
              <li
                key={step.id || index}
                className={`plan-step plan-${step.status}`}
              >
                <span className="plan-marker">
                  {step.status === "done"
                    ? "✓"
                    : step.status === "in_progress"
                      ? "▶"
                      : "·"}
                </span>
                {step.title}
                {step.note && (
                  <span className="plan-note">（{step.note}）</span>
                )}
                <small className="plan-note"> ID: {step.id}</small>
                {!!step.dependsOn?.length && (
                  <small className="plan-note">
                    {" "}
                    前置：{step.dependsOn.join("、")}
                  </small>
                )}
                {step.acceptance && (
                  <small className="plan-note"> 验收：{step.acceptance}</small>
                )}
                {!!step.inputRefs?.length && (
                  <small className="plan-note">
                    {" "}
                    材料：{step.inputRefs.join("、")}
                  </small>
                )}
              </li>
            ))}
          </ul>
          <p className="hint">
            前置步骤完成后才能开始后继，更新有修订保护。会话待办状态由模型记录；实际
            worker 调度与审查状态见本次执行计划，工具调用仍走审批。
          </p>
        </div>
      )}
      {runs[0] && runs[0].status !== "running" && (
        <button
          type="button"
          className="secondary"
          disabled={reviewBusy}
          onClick={reviewResult}
        >
          单次审查此结果
        </button>
      )}
      {reviews.length > 0 && (
        <div className="plan-panel">
          {reviews.map((review) => (
            <details key={review.id}>
              <summary>
                独立审查：{review.result.status} ·{" "}
                {review.current ? "当前交付" : "已失效"}
              </summary>
              <p>{review.result.summary}</p>
              <p>绑定指纹：{review.binding}</p>
              {(review.result.defects ?? []).map((d, i) => (
                <p key={i}>
                  {d.reference}：{d.description}
                </p>
              ))}
            </details>
          ))}
        </div>
      )}
      {taskGraph &&
        (!!taskGraph.tasks?.length ||
          (taskGraph.coordination?.rounds ?? 0) > 0) && (
          <div className="plan-panel task-graph">
            <span className="card-title">
              本次执行计划 · 修订 {taskGraph.revision}
            </span>
            <p>{taskGraph.goal}</p>
            {taskGraph.coordination && (
              <p>
                调度：{taskGraph.coordination.state} · 规划{" "}
                {taskGraph.coordination.rounds} 轮 · 事件{" "}
                {taskGraph.coordination.plannedSeq}/
                {taskGraph.coordination.eventSeq}
                {taskGraph.coordination.reason &&
                  ` · ${taskGraph.coordination.reason}`}
              </p>
            )}
            {taskGraph.budget && (
              <p>
                根预算：模型 {taskGraph.budget.modelCalls}/
                {taskGraph.budget.limits.maxModelCalls} 次 · 工具{" "}
                {taskGraph.budget.toolCalls}/
                {taskGraph.budget.limits.maxToolCalls} 次 · token{" "}
                {taskGraph.budget.tokens}
                {taskGraph.budget.unknownUsage > 0 &&
                  `（${taskGraph.budget.unknownUsage} 次用量未知，保留预留额度）`}
              </p>
            )}
            {(taskGraph.tasks ?? []).map((item) => (
              <details key={item.spec.id}>
                <summary>
                  {item.spec.id} · {item.spec.worker} · {item.state} ·{" "}
                  {item.spec.accessMode || "unknown"}
                </summary>
                <p>{item.spec.task}</p>
                {item.spec.materials && <pre>{item.spec.materials}</pre>}
                {!!item.spec.dependsOn?.length && (
                  <p>前置任务：{item.spec.dependsOn.join("、")}</p>
                )}
                {item.spec.acceptance && (
                  <p>验收条件：{item.spec.acceptance}</p>
                )}
                {!!item.spec.resources?.length && (
                  <p>共享资源：{item.spec.resources.join("、")}</p>
                )}
                {item.spec.replaces && (
                  <p>替代失败任务：{item.spec.replaces}</p>
                )}
                {(taskGraph.artifacts ?? [])
                  .filter((a) => a.taskId === item.spec.id)
                  .map((a) => (
                    <p key={a.id}>
                      交付物 {a.id} · {a.verification} · SHA256 {a.sha256}
                    </p>
                  ))}
                {(taskGraph.attempts ?? [])
                  .filter((a) => a.taskId === item.spec.id)
                  .map((a) => (
                    <p key={a.id}>
                      执行尝试 {a.id} · {a.status} · 运行 {a.runId}
                    </p>
                  ))}
              </details>
            ))}
            <p className="hint">
              状态来自实际执行；succeeded
              表示执行结束，验收结论见独立审查。领取前可展开查看任务条件；中断或结果未知的任务不会自动重跑。
            </p>
          </div>
        )}
    </>
  );
}
