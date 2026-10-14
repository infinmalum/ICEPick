import type { ConversationState } from "../hooks/useConversation";
type Props = {
  conversation: Pick<
    ConversationState,
    "messages" | "searchRecords" | "streamText"
  >;
};
export function Transcript(props: Props) {
  const { messages, searchRecords, streamText } = props.conversation;
  return (
    <>
      <div className="message-list" aria-live="polite">
        {messages.length === 0 && !streamText && (
          <p className="hint">
            还没有消息。可以试试“现在几点？”来验证 MCP 时间工具。
          </p>
        )}
        {messages.map((item) => (
          <div className={`message message-${item.role}`} key={item.id}>
            <span className="message-role">
              {item.role === "user"
                ? "你"
                : item.role === "tool"
                  ? "MCP 工具"
                  : "ICEPick"}
            </span>
            <p>{item.content}</p>
            {item.role === "assistant" &&
              searchRecords
                .filter(
                  (record) =>
                    record.runId === item.runId &&
                    record.state === "completed" &&
                    record.report.text === item.content,
                )
                .flatMap((record) =>
                  (record.report.citations ?? []).map((citation, index) => (
                    <p className="hint" key={record.id + ":" + index}>
                      来源：
                      <a
                        href={citation.url}
                        target="_blank"
                        rel="noopener noreferrer"
                      >
                        {citation.title || citation.url}
                      </a>
                    </p>
                  )),
                )}
          </div>
        ))}
        {streamText && (
          <div className="message message-assistant streaming">
            <span className="message-role">ICEPick · 生成中</span>
            <p>{streamText}</p>
          </div>
        )}
      </div>
    </>
  );
}
