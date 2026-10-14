import type { ConversationState } from "../hooks/useConversation";
type Props = {
  conversation: Pick<
    ConversationState,
    | "approvalBusy"
    | "approveCall"
    | "denyCall"
    | "pendingCalls"
    | "resolveUnknown"
    | "unknownCalls"
  >;
};
export function ToolApprovals(props: Props) {
  const {
    approvalBusy,
    approveCall,
    denyCall,
    pendingCalls,
    resolveUnknown,
    unknownCalls,
  } = props.conversation;
  return (
    <>
      {(pendingCalls.length > 0 || unknownCalls.length > 0) && (
        <div className="approval-panel">
          <div className="card-title">待处理工具调用</div>
          {pendingCalls.map((call) => (
            <div className="approval-row" key={call.id}>
              <div className="approval-info">
                <code>{call.toolName}</code>
                <pre>{call.arguments}</pre>
              </div>
              <div className="approval-actions">
                <button
                  disabled={approvalBusy}
                  onClick={() => approveCall(call)}
                >
                  批准执行
                </button>
                <button
                  className="secondary"
                  disabled={approvalBusy}
                  onClick={() => denyCall(call)}
                >
                  拒绝
                </button>
              </div>
            </div>
          ))}
          {unknownCalls.map((call) => (
            <div className="approval-row" key={call.id}>
              <div className="approval-info">
                <code>{call.toolName}</code>
                <p className="error">{call.error || "结果未知，等待处置"}</p>
              </div>
              <div className="approval-actions">
                <button
                  disabled={approvalBusy}
                  onClick={() => resolveUnknown(call, true)}
                >
                  确认为成功
                </button>
                <button
                  className="secondary"
                  disabled={approvalBusy}
                  onClick={() => resolveUnknown(call, false)}
                >
                  确认为失败
                </button>
              </div>
            </div>
          ))}
          <p className="hint">
            批准只对当前这条调用的参数生效；结果未知的调用需要你说明依据后处置，会话才能继续。
          </p>
        </div>
      )}
    </>
  );
}
