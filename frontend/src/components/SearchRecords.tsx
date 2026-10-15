import type { ConversationState } from "../hooks/useConversation";
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
      <details>
        <summary>技术记录</summary>
        <pre>{JSON.stringify(c.searchRecords, null, 2)}</pre>
      </details>
    </details>
  ) : null;
}
