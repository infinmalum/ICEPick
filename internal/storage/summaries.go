package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// HistoryEntry is one unit of the model history rendered from the persisted
// records: a user message, an assistant turn, or one tool result of an
// assistant turn. The order here is exactly the order the run sends to the
// model, so summary coverage counted on entries maps 1:1 to prompt positions.
type HistoryEntry struct {
	RunID         string
	ProviderState []json.RawMessage
	MessageID     string
	Seq           int
	Role          string // user, assistant, tool
	Content       string
	Calls         []MessageToolCall // assistant entries: the calls the model requested
	CallID        string            // tool entries: the model-facing call id
	CallIndex     int               // tool entries: position of this result in the batch
	CallTotal     int               // assistant and tool entries: size of the call batch
}

// HistoryEntries renders the ordered model-history units of a session from
// the persisted messages and tool calls. Nothing is fabricated and nothing
// is removed: it is the single rendering both the run history and the
// summary coverage bookkeeping go through.
func (s *Store) HistoryEntries(ctx context.Context, sessionID string) ([]HistoryEntry, error) {
	messages, err := s.ListMessages(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	calls, err := s.ListSessionToolCalls(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	byMessage := map[string][]ToolCall{}
	for _, call := range calls {
		byMessage[call.MessageID] = append(byMessage[call.MessageID], call)
	}
	entries := []HistoryEntry{}
	for _, item := range messages {
		switch item.Role {
		case RoleUser:
			entries = append(entries, HistoryEntry{MessageID: item.ID, Seq: item.Seq, Role: RoleUser, Content: item.Content})
		case RoleAssistant:
			entries = append(entries, HistoryEntry{
				MessageID: item.ID, Seq: item.Seq, Role: RoleAssistant,
				RunID: item.RunID, ProviderState: item.ProviderState, Content: item.Content, Calls: append([]MessageToolCall(nil), item.ToolCalls...),
				CallTotal: len(item.ToolCalls),
			})
			for i, call := range byMessage[item.ID] {
				entries = append(entries, HistoryEntry{
					MessageID: item.ID, Seq: item.Seq, Role: RoleTool,
					Content: modelVisibleResult(call), CallID: call.CallID,
					CallIndex: i, CallTotal: len(item.ToolCalls),
				})
			}
		}
	}
	return entries, nil
}

// modelVisibleResult renders a persisted tool call outcome for the model.
// Denied and failed calls surface their recorded error; an unresolved
// unknown result never reaches a live send because such sessions cannot
// start a run, but it is still rendered defensively instead of dropped.
func modelVisibleResult(call ToolCall) string {
	switch call.State {
	case CallSucceeded:
		return call.Result
	case CallFailed:
		return "工具错误：" + call.Error
	case CallCancelled:
		return "工具错误：调用已取消：" + call.Error
	case CallExecutionUnknown:
		return "工具错误：结果未知，等待处置：" + call.Error
	default:
		return fmt.Sprintf("工具错误：调用处于未完成状态 %q", call.State)
	}
}

// SessionSummary is the compaction summary of one session. It replaces the
// oldest history entries in the prompt; the originals stay on disk and the
// summary belongs to the owning Context only.
type SessionSummary struct {
	SessionID   string `json:"sessionId"`
	ContextID   string `json:"contextId"`
	CoversSeq   int    `json:"coversSeq"`
	CoversCalls int    `json:"coversCalls"`
	Summary     string `json:"summary"`
	Scope       string `json:"scope"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

func (s *Store) GetSessionSummary(ctx context.Context, sessionID string) (SessionSummary, bool, error) {
	var item SessionSummary
	err := s.db.QueryRowContext(ctx, `SELECT session_id, (SELECT context_id FROM sessions WHERE id=session_summaries.session_id), covers_seq, covers_calls, summary, scope, created_at, updated_at FROM session_summaries WHERE session_id = ?`, sessionID).
		Scan(&item.SessionID, &item.ContextID, &item.CoversSeq, &item.CoversCalls, &item.Summary, &item.Scope, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionSummary{}, false, nil
	}
	if err != nil {
		return SessionSummary{}, false, err
	}
	return item, true, nil
}

// SummaryCoverage returns how many oldest history entries the summary
// replaces, and reports ok=false when the recorded boundary no longer lands
// on a closed entry — such a summary is stale and must be ignored (the next
// compaction regenerates it from the full history).
func SummaryCoverage(entries []HistoryEntry, sum SessionSummary) (covered int, ok bool) {
	if sum.CoversSeq <= 0 {
		return 0, false
	}
	last := -1
	for i, entry := range entries {
		if entry.Seq > sum.CoversSeq {
			break
		}
		last = i
	}
	if last < 0 {
		return 0, false
	}
	boundary := entries[last]
	switch {
	case boundary.Role == RoleTool:
		// The boundary must sit after the last result of its batch; a
		// partially covered tool batch would orphan call requests.
		if sum.CoversCalls != boundary.CallTotal || boundary.CallIndex != boundary.CallTotal-1 {
			return 0, false
		}
	case boundary.Role == RoleAssistant:
		// An assistant turn that requested calls can never be a boundary by
		// itself: its results belong to the same batch.
		if len(boundary.Calls) != 0 || sum.CoversCalls != 0 {
			return 0, false
		}
	default:
		if sum.CoversCalls != 0 {
			return 0, false
		}
	}
	return last + 1, true
}

// SummaryCoveredEntries reports how many history entries the session's
// current summary validly covers; 0 when there is none or it is stale.
func (s *Store) SummaryCoveredEntries(ctx context.Context, sessionID string) (int, error) {
	sum, found, err := s.GetSessionSummary(ctx, sessionID)
	if err != nil || !found {
		return 0, err
	}
	entries, err := s.HistoryEntries(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	covered, ok := SummaryCoverage(entries, sum)
	if !ok {
		return 0, nil
	}
	return covered, nil
}

// SaveSessionSummaryCovering persists (or replaces) the session's summary
// with an absolute coverage of `covered` history entries. The boundary must
// be a closed turn — pending tool call batches are never cut in half — and
// at least one entry must remain outside the summary. The summary inherits
// the most restrictive data scope of the content it covers, so compaction
// never loosens the exfiltration policy.
func (s *Store) SaveSessionSummaryCovering(ctx context.Context, sessionID string, covered int, summary string) (SessionSummary, error) {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return SessionSummary{}, errors.New("压缩摘要为空")
	}
	entries, err := s.HistoryEntries(ctx, sessionID)
	if err != nil {
		return SessionSummary{}, err
	}
	if covered < 1 || covered >= len(entries) {
		return SessionSummary{}, fmt.Errorf("压缩覆盖范围无效：会话共 %d 条历史，要求覆盖 %d 条", len(entries), covered)
	}
	boundary := entries[covered-1]
	if boundary.Role == RoleAssistant && len(boundary.Calls) > 0 {
		return SessionSummary{}, errors.New("压缩边界不能切断未完成的工具调用批次")
	}
	if boundary.Role == RoleTool && boundary.CallIndex != boundary.CallTotal-1 {
		return SessionSummary{}, errors.New("压缩边界不能切断未完成的工具调用批次")
	}
	coversCalls := 0
	if boundary.Role == RoleTool {
		coversCalls = boundary.CallIndex + 1
	}
	scope, err := s.summaryScope(ctx, sessionID, boundary.Seq)
	if err != nil {
		return SessionSummary{}, err
	}
	item := SessionSummary{
		SessionID: sessionID, CoversSeq: boundary.Seq, CoversCalls: coversCalls,
		Summary: summary, Scope: scope, CreatedAt: now(), UpdatedAt: now(),
	}
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `INSERT INTO session_summaries (session_id, covers_seq, covers_calls, summary, scope, created_at, updated_at)
			SELECT ?, ?, ?, ?, ?, ?, ? FROM sessions WHERE id = ?
			ON CONFLICT(session_id) DO UPDATE SET covers_seq = excluded.covers_seq, covers_calls = excluded.covers_calls, summary = excluded.summary, scope = excluded.scope, updated_at = excluded.updated_at
			RETURNING (SELECT context_id FROM sessions WHERE id=session_id), created_at`,
			sessionID, item.CoversSeq, item.CoversCalls, item.Summary, item.Scope, item.CreatedAt, item.UpdatedAt, sessionID).
			Scan(&item.ContextID, &item.CreatedAt)
	})
	if err != nil {
		return SessionSummary{}, err
	}
	return item, nil
}

// summaryScope folds the data scopes of every entry the summary covers
// (messages with seq <= the boundary seq) into the single most restrictive
// token. Unknown wins over everything: a summary must never claim more
// certainty than its sources.
func (s *Store) summaryScope(ctx context.Context, sessionID string, coversSeq int) (string, error) {
	var textRecords int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE session_id = ? AND role IN ('user','assistant') AND seq <= ?`, sessionID, coversSeq).Scan(&textRecords); err != nil {
		return "", err
	}
	scope := ""
	if textRecords > 0 {
		scope = ScopePersonal
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT CASE WHEN c.source_scope = '' THEN 'unknown' ELSE c.source_scope END
		FROM tool_calls c JOIN runs r ON r.id = c.run_id JOIN messages m ON m.id = c.message_id
		WHERE r.session_id = ? AND m.seq <= ?`, sessionID, coversSeq)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var item string
		if err := rows.Scan(&item); err != nil {
			return "", err
		}
		scope = MaxScope(scope, item)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if scope == "" {
		scope = ScopeUnknown
	}
	return scope, nil
}

var scopeSeverity = map[string]int{
	ScopePublic: 0, ScopeWorkspace: 1, ScopePersonal: 2, ScopeCredential: 3, ScopeUnknown: 4,
}

// MaxScope folds two data scope tokens into the more restrictive one. An
// empty operand is neutral so callers can fold from zero.
func MaxScope(a, b string) string {
	sa, oka := scopeSeverity[a]
	sb, okb := scopeSeverity[b]
	if !oka && !okb {
		return ""
	}
	if !oka {
		return b
	}
	if !okb {
		return a
	}
	if sa >= sb {
		return a
	}
	return b
}
