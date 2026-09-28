package agent

import (
	"context"
	"errors"
	"sync"

	"icepick/internal/storage"
)

// Runs owns cancellation signals for active tasks. Task execution is added in P2.
type Runs struct {
	store  *storage.Store
	mu     sync.Mutex
	active map[string]context.CancelFunc
}

func NewRuns(store *storage.Store) *Runs {
	return &Runs{store: store, active: map[string]context.CancelFunc{}}
}

func (r *Runs) Start(ctx context.Context, sessionID string) (storage.Run, context.Context, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return storage.Run{}, nil, err
	}
	item, err := r.store.CreateRun(ctx, sessionID)
	if err != nil {
		return storage.Run{}, nil, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	r.active[item.ID] = cancel
	return item, runCtx, nil
}

func (r *Runs) Cancel(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cancel, ok := r.active[id]
	if !ok {
		return errors.New("任务未运行")
	}
	if err := r.store.SetRunStatus(context.Background(), id, "cancelled"); err != nil {
		return err
	}
	cancel()
	delete(r.active, id)
	return nil
}

func (r *Runs) Finish(id, status string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cancel, ok := r.active[id]
	if !ok {
		return errors.New("任务未运行")
	}
	if err := r.store.SetRunStatus(context.Background(), id, status); err != nil {
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
		_ = r.store.SetRunStatus(context.Background(), id, "interrupted")
		cancel()
		delete(r.active, id)
	}
}
