package app

import (
	"context"
	"encoding/json"

	"icepick/internal/agent"
	"icepick/internal/config"
	"icepick/internal/mcptools"
	"icepick/internal/model"
	"icepick/internal/storage"
)

// runRecorder adapts the storage layer to the engine's Recorder interface for
// one run. Every method maps to a single transactional store operation; the
// engine only proceeds with a request or a dispatch when the corresponding
// call returned nil, so the durable state always precedes the action.
type runRecorder struct {
	svc        *Service
	runID      string
	sessionID  string
	classifier mcptools.Classifier
}

var _ agent.Recorder = runRecorder{}

// ModelRequest persists the full request snapshot: the model parameters that
// will be used, the exact prompt history and the tool definitions offered.
func (r runRecorder) ModelRequest(ctx context.Context, source config.Model, messages []model.Message, tools []model.Tool) (string, error) {
	payload, err := json.Marshal(modelRequestSnapshot{Model: source, Messages: messages, Tools: tools, OutputTokenLimit: 8192})
	if err != nil {
		return "", err
	}
	step, err := r.svc.store.BeginModelStep(ctx, r.runID, string(payload))
	if err != nil {
		return "", err
	}
	return step.ID, nil
}

type modelRequestSnapshot struct {
	Model            config.Model    `json:"model"`
	Messages         []model.Message `json:"messages"`
	Tools            []model.Tool    `json:"tools"`
	OutputTokenLimit int             `json:"outputTokenLimit"`
}

func (r runRecorder) ModelDone(ctx context.Context, stepID string, chatErr error) error {
	return r.svc.store.CompleteModelStep(ctx, stepID, chatErr)
}

// Assistant persists the assistant message; when it requests tool calls,
// pending ToolCall rows and their steps commit in the same transaction. Once
// the message is durable, the run's streamed in-memory text is no longer
// needed and its buffer is dropped.
func (r runRecorder) Assistant(ctx context.Context, msg model.Message) error {
	calls := make([]storage.MessageToolCall, 0, len(msg.ToolCalls))
	for _, call := range msg.ToolCalls {
		calls = append(calls, storage.MessageToolCall{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments})
	}
	_, _, err := r.svc.store.AddAssistantMessage(ctx, r.sessionID, r.runID, msg.Content, calls)
	if err == nil {
		r.svc.clearStream(r.runID)
	}
	return err
}

// ResolveCall maps the model-provided call ID to the persisted row ID that
// approval decisions are bound to.
func (r runRecorder) ResolveCall(ctx context.Context, callID string) (string, error) {
	row, err := r.svc.store.FindRunToolCall(ctx, r.runID, callID)
	if err != nil {
		return "", err
	}
	return row.ID, nil
}

// ApproveAuto records that policy granted the call without user interaction.
func (r runRecorder) ApproveAuto(ctx context.Context, callID string) error {
	row, err := r.svc.store.FindRunToolCall(ctx, r.runID, callID)
	if err != nil {
		return err
	}
	return r.svc.store.ApproveToolCall(ctx, row.ID, storage.ApprovalAuto)
}

func (r runRecorder) BeginTool(ctx context.Context, callID string) error {
	row, err := r.svc.store.FindRunToolCall(ctx, r.runID, callID)
	if err != nil {
		return err
	}
	return r.svc.store.BeginToolCall(ctx, row.ID)
}

// FinishTool records the terminal outcome. A successful result is tagged
// with the sensitivity of the data the call touched, derived from the tool
// name and arguments through the run's classifier — never from the tool's
// own description.
func (r runRecorder) FinishTool(ctx context.Context, callID, state, result, errText string) error {
	row, err := r.svc.store.FindRunToolCall(ctx, r.runID, callID)
	if err != nil {
		return err
	}
	_, sens := r.classifier.Classify(row.ToolName, row.Arguments)
	scope := string(sens)
	return r.svc.store.FinishToolCall(ctx, row.ID, state, result, errText, scope)
}

// SaveSummary persists a compaction summary. The engine counts coverage on
// its own history window, which starts after the summary the session
// already had when the run began; the stored boundary is absolute, so the
// recorder adds that base back.
func (r runRecorder) SaveSummary(ctx context.Context, covered int, summary string) error {
	base, err := r.svc.store.SummaryCoveredEntries(ctx, r.sessionID)
	if err != nil {
		return err
	}
	_, err = r.svc.store.SaveSessionSummaryCovering(ctx, r.sessionID, base+covered, summary)
	return err
}
