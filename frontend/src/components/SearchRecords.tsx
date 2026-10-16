import type { ConversationState } from "../hooks/useConversation";
import { stateLabel } from "../shared/errors";
export function SearchRecords({
  conversation: c,
}: {
  conversation: Pick<ConversationState, "searchRecords">;
}) {
  const citations = [
    ...new Map(
      c.searchRecords
        .flatMap((r) => r.report.citations ?? [])
        .map((v) => [v.url, v]),
    ).values(),
  ];
  return c.searchRecords.length > 0 ? (
    <details className="sources">
      <summary>
        参考来源{citations.length ? ` · ${citations.length}` : ""}
      </summary>
      {citations.length ? (
        citations.map((v) => (
          <a key={v.url} href={v.url} target="_blank" rel="noopener noreferrer">
            {v.title || v.url}
          </a>
        ))
      ) : (
        <p>本次未返回可列出的网页来源。</p>
      )}
      {c.searchRecords
        .flatMap((r) => r.report.suggestions ?? [])
        .map((html, i) => (
          <iframe
            key={i}
            title="Google 搜索建议"
            sandbox="allow-popups"
            referrerPolicy="no-referrer"
            srcDoc={
              "<meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'\">" +
              html
            }
          />
        ))}
      <details className="search-record-log">
        <summary>联网检索记录 · {c.searchRecords.length} 次</summary>
        {c.searchRecords.map((r) => (
          <div className="search-record" key={r.id}>
            <p>
              {r.provider} · {new Date(r.createdAt).toLocaleString()} ·{" "}
              {stateLabel(r.state)}
            </p>
            {r.error && <p>未完成：{r.error}</p>}
            {r.report.text && (
              <p>
                {r.report.text.length > 400
                  ? r.report.text.slice(0, 400) + "…"
                  : r.report.text}
              </p>
            )}
          </div>
        ))}
      </details>
    </details>
  ) : null;
}
