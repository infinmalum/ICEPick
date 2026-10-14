import type { ConversationState } from "../hooks/useConversation";
type Props = {
  conversation: Pick<ConversationState, "searchRecords">;
};
export function SearchRecords(props: Props) {
  const { searchRecords } = props.conversation;
  return (
    <>
      {searchRecords.length > 0 && (
        <details className="search-records">
          <summary>联网搜索记录与来源（{searchRecords.length} 次请求）</summary>
          {searchRecords.map((record) => (
            <div key={record.id}>
              <p>
                {record.provider} · {record.state} · 未知来源 · 搜索次数{" "}
                {record.report.calls ?? "未知"} · 预算计数{" "}
                {record.report.budgetUnit === "grounded_request"
                  ? "宿主请求"
                  : "搜索调用"}{" "}
                · 独立费用 {record.report.cost ?? "未知"}
              </p>
              {record.report.text && <p>{record.report.text}</p>}
              {(record.report.citations ?? []).map((citation, index) => (
                <p key={index}>
                  <a
                    href={citation.url}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    {citation.title || citation.url}
                  </a>
                </p>
              ))}
              {(record.report.suggestions ?? []).map((html, index) => (
                <iframe
                  key={"suggestion:" + index}
                  title="Google 搜索建议"
                  sandbox="allow-popups"
                  referrerPolicy="no-referrer"
                  style={{ width: "100%", height: 160, border: 0 }}
                  srcDoc={
                    "<meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'\">" +
                    html
                  }
                />
              ))}
              {(record.report.events ?? []).map((event, index) => (
                <p className="hint" key={index}>
                  {event.type}
                  {event.query
                    ? " · " + event.query
                    : event.queries?.length
                      ? " · " + event.queries.join("；")
                      : ""}
                </p>
              ))}
              {record.error && <p className="error">{record.error}</p>}
            </div>
          ))}
        </details>
      )}
    </>
  );
}
