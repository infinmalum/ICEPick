package agent

import (
	"context"
	"errors"
	"sync"
	"time"

	"icepick/internal/config"
	"icepick/internal/storage"
)

// Runs owns cancellation signals for active tasks.
type Runs struct {
	store  *storage.Store
	mu     sync.Mutex
	active map[string]context.CancelFunc
}

const RunTimeout = 10 * time.Minute

func NewRuns(store *storage.Store) *Runs {
	return &Runs{store: store, active: map[string]context.CancelFunc{}}
}

// Start creates the run and resolves the owning Agent's current Profile in
// one transaction. The returned resolved profile is the snapshot the run
// must execute with; later profile edits affect only subsequent runs.
func (r *Runs) Start(ctx context.Context, sessionID string) (storage.Run, context.Context, storage.AgentProfile, error) {
	return r.start(ctx, sessionID, storage.CreateRunOptions{})
}

func (r *Runs) StartPrepared(ctx context.Context, sessionID string, model config.Model, snapshot storage.SkillSnapshotter) (storage.Run, context.Context, storage.AgentProfile, error) {
	return r.start(ctx, sessionID, storage.CreateRunOptions{CaptureResources: true, DefaultModel: model, SnapshotSkills: snapshot})
}

func (r *Runs) StartWorker(ctx context.Context, sessionID, parentID string, depth int, name string) (storage.Run, context.Context, storage.AgentProfile, error) {
	return r.start(ctx, sessionID, storage.CreateRunOptions{ParentRunID: parentID, Depth: depth, WorkerName: name})
}

func (r *Runs) StartPlannedWorker(ctx context.Context, sessionID, parentID string, spec storage.TaskSpec) (storage.Run, context.Context, storage.AgentProfile, error) {
	return r.start(ctx, sessionID, storage.CreateRunOptions{ParentRunID: parentID, Depth: 1, WorkerName: spec.Worker, TaskSpec: &spec})
}

// StartChild registers a delegated subtask run: the child session already
// exists and belongs to the target Agent, and the run row links to its
// parent. The child context derives from the parent run's context, so
// cancelling the parent cancels every surviving subtask.
func (r *Runs) StartChild(ctx context.Context, sessionID, parentRunID string, depth int) (storage.Run, context.Context, storage.AgentProfile, error) {
	return r.start(ctx, sessionID, storage.CreateRunOptions{ParentRunID: parentRunID, Depth: depth})
}

func (r *Runs) start(ctx context.Context, sessionID string, opts storage.CreateRunOptions) (storage.Run, context.Context, storage.AgentProfile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return storage.Run{}, nil, storage.AgentProfile{}, err
	}
	item, resolved, err := r.store.CreateRunWithOptions(ctx, sessionID, opts)
	if err != nil {
		return storage.Run{}, nil, storage.AgentProfile{}, err
	}
	runCtx, cancel := context.WithTimeout(ctx, RunTimeout)
	r.active[item.ID] = cancel
	return item, runCtx, resolved, nil
}

func (r *Runs) Cancel(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cancel, ok := r.active[id]
	if !ok {
		return errors.New("任务未运行")
	}
	if err := r.store.SetRunStatus(context.Background(), id, "cancelled", ""); err != nil {
		return err
	}
	cancel()
	delete(r.active, id)
	return nil
}

func (r *Runs) Finish(id, status, detail string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cancel, ok := r.active[id]
	if !ok {
		return errors.New("任务未运行")
	}
	if err := r.store.SetRunStatus(context.Background(), id, status, detail); err != nil {
		return err
	}
	cancel()
	delete(r.active, id)
	return nil
}

func (r *Runs) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, cancel := range r.active {
		_ = r.store.SetRunStatus(context.Background(), id, "interrupted", "应用已关闭")
		cancel()
		delete(r.active, id)
	}
}
