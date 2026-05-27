// SPDX-License-Identifier: MIT
// internal/orchestrator/dispatcher.go
//
//
// The dispatcher is a thin coordination layer between the §3.1 RunStage4
// lifecycle and release's workforce.Manager. Given a DispatchRequest it:
//
// 1. Validates Width > 0 (else ErrInvalidBuildRequest).
// 2. Leases Width worktrees from the WorktreePool. On any
// lease error the partial set is released and the error is
// propagated unwrapped so the caller can errors.Is against
// worktreepool.ErrPoolExhausted ( recovery uses that
// classification to decide retry budget vs. user-visible error).
// 3. Tracks the leased set on the dispatcher under d.mu so a
// concurrent Shutdown call can release them on the abort path.
// 4. Emits one EvtWorkerDispatched per leased worktree (payload:
// worktree_path + depth).
// 5. Hands the leases to Workforce.SpawnWorkers; on error returns
// wrapped fmt.Errorf("workforce spawn: %w", err) and the deferred
// releaseAll cleans the partial lease set.
// 6. Drains the per-worker result channel, recording each completion
// as EvtWorkerCheckpoint and partitioning the counters by Status.
// On ctx.Done a single Workforce.AbortAll(ctx) is invoked and the
// drain continues until the workforce closes the channel or until
// DrainDeadline elapses (defense-in-depth against a workforce that
// never closes its result channel after abort).
// 7. Returns the aggregated DispatchResult plus ctx.Err() if abort was
// invoked, or nil on the clean path. If the drain deadline fires
// first, returns errors.Join(ctx.Err(), ErrDrainDeadlineExceeded).
//
// Shutdown is idempotent: it marks the dispatcher closed under d.mu,
// swaps the tracked-lease slice, releases those worktrees, and calls
// Workforce.AbortAll(ctx). Concurrent Shutdown calls observe the
// shutdown flag and short-circuit to nil.
//
// Privacy contract (IMP-3): event payloads + error messages name field
// keys and sentinel constants but never echo BuildRequest values; the
// only worker-side string surfaced is r.Err.Error() (release's worker
// surface is itself bound to the same redaction discipline).
//
// Boundary invariants (carry-forward from orchestrator.go):
// - inv-hades-090: this file does NOT import internal/workforce/queue.
// The release workforce.Manager is consumed via the WorkforceManager
// interface declared here so eventlog (durable) ⊥ queue (transient)
// stays a clean separation; bootstrap ( adapter / daemon
// main) wires the real impl when release is merged.
// - inv-hades-089: this file does NOT import internal/store. Persistence
// flows through the eventlog.Appender contract.
//
// Concurrency contract:
// - The drain loop's ctx.Done arm guards AbortAll with abortInvoked
// so the workforce never sees two concurrent abort signals from the
// same Dispatch call.
// - d.leased mutation (append / swap on Shutdown) is mu-guarded.
// - releaseAll is best-effort: per-worktree Release errors are
// swallowed so a single corrupt worktree never blocks cleanup of
// the rest of the lease set.
// - ctx discipline (IMP-1): every awaitable boundary (Append, Lease,
// Release, AbortAll) honours ctx.

package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/worktreepool"
)

type WorkforceManager interface {
	SpawnWorkers(ctx context.Context, req SpawnRequest) (<-chan WorkerResult, error)
	AbortAll(ctx context.Context) error
}

// SpawnRequest is the release contract input. ships the minimum
// fields release's Manager.Spawn signature requires; richer fields
// (HRA reviewer hierarchy, cost-tier fan-out, doctrine-driven retry
// budget) land in release Phases F/G/H/M without narrowing this struct.
//
// Worktrees is a slice of pointers so
// the workforce.Manager can cheaply route per-worker context (path,
// branch, lease id) without copying the value. The dispatcher owns
// the lease lifecycle: release workers MUST NOT call Release.
type SpawnRequest struct {
	SessionID string
	ProjectID string
	Doctrine  string
	Worktrees []*worktreepool.Worktree
	Spec      Spec
	Depth     int
}

type WorkerResult struct {
	WorkerID string
	Status   string
	Output   []byte
	Err      error
}

type DispatcherConfig struct {
	Clock         clock.Clock
	EventLog      eventlog.Appender
	Pool          worktreepool.Pool
	Workforce     WorkforceManager
	DrainDeadline time.Duration
}

var ErrDispatcherExhausted = errors.New("orchestrator: dispatcher exhausted (pool unavailable)")

var ErrDrainDeadlineExceeded = errors.New("orchestrator: dispatcher drain deadline exceeded after abort")

type dispatcher struct {
	cfg DispatcherConfig

	mu       sync.Mutex
	leased   []*worktreepool.Worktree
	shutdown bool
}

func NewDispatcher(cfg DispatcherConfig) (Dispatcher, error) {
	if cfg.Clock == nil {
		return nil, fmt.Errorf("%w: clock is nil", ErrInvalidConfig)
	}
	if cfg.EventLog == nil {
		return nil, fmt.Errorf("%w: eventlog is nil", ErrInvalidConfig)
	}
	if cfg.Pool == nil {
		return nil, fmt.Errorf("%w: worktree pool is nil", ErrInvalidConfig)
	}
	if cfg.Workforce == nil {
		return nil, fmt.Errorf("%w: workforce manager is nil", ErrInvalidConfig)
	}
	return &dispatcher{cfg: cfg}, nil
}

const defaultDrainDeadline = 30 * time.Second

func (d *dispatcher) drainDeadline() time.Duration {
	if d.cfg.DrainDeadline > 0 {
		return d.cfg.DrainDeadline
	}
	return defaultDrainDeadline
}

func (d *dispatcher) Dispatch(ctx context.Context, req DispatchRequest) (DispatchResult, error) {
	if req.Width <= 0 {
		return DispatchResult{}, fmt.Errorf("%w: width must be > 0", ErrInvalidBuildRequest)
	}
	if err := ctx.Err(); err != nil {
		return DispatchResult{}, fmt.Errorf("dispatcher.Dispatch: ctx cancelled before start: %w", err)
	}

	leased, err := d.leaseWorktrees(ctx, req.Width)
	if err != nil {
		return DispatchResult{}, err
	}

	d.mu.Lock()
	d.leased = append(d.leased, leased...)
	d.mu.Unlock()
	defer d.unregisterAndRelease(ctx, leased)

	for _, wt := range leased {
		d.appendDispatched(ctx, req, wt)
	}

	resultCh, err := d.cfg.Workforce.SpawnWorkers(ctx, SpawnRequest{
		SessionID: req.SessionID,
		ProjectID: req.ProjectID,
		Doctrine:  req.Doctrine,
		Worktrees: leased,
		Spec:      req.Spec,
		Depth:     req.Depth,
	})
	if err != nil {
		return DispatchResult{}, fmt.Errorf("workforce spawn: %w", err)
	}

	res := DispatchResult{WorkersSpawned: len(leased)}
	abortInvoked := false
	var drainDeadlineC <-chan time.Time
	for {
		select {
		case r, ok := <-resultCh:
			if !ok {
				if abortInvoked {
					return res, ctx.Err()
				}
				return res, nil
			}
			d.recordCompletion(ctx, req, r)
			switch r.Status {
			case "ok":
				res.Completed++
			case "aborted":
				res.Aborted++
			default:

				res.Errors++
			}
		case <-ctx.Done():
			if !abortInvoked {
				_ = d.cfg.Workforce.AbortAll(ctx)
				abortInvoked = true

				drainDeadlineC = time.After(d.drainDeadline())
			}

		case <-drainDeadlineC:

			return res, errors.Join(ctx.Err(), ErrDrainDeadlineExceeded)
		}
	}
}

func (d *dispatcher) leaseWorktrees(ctx context.Context, n int) ([]*worktreepool.Worktree, error) {
	out := make([]*worktreepool.Worktree, 0, n)
	for i := 0; i < n; i++ {
		wt, err := d.cfg.Pool.Lease(ctx)
		if err != nil {
			d.releaseSlice(ctx, out)
			return nil, err
		}
		out = append(out, wt)
	}
	return out, nil
}

func (d *dispatcher) appendDispatched(ctx context.Context, req DispatchRequest, wt *worktreepool.Worktree) {
	_, _ = d.cfg.EventLog.Append(ctx, eventlog.Event{
		Type:      eventlog.EvtWorkerDispatched,
		SessionID: req.SessionID,
		ProjectID: req.ProjectID,
		Timestamp: d.cfg.Clock.Now(),
		Payload: map[string]any{
			"worktree_path": wt.Path(),
			"depth":         req.Depth,
		},
	})
}

func (d *dispatcher) recordCompletion(ctx context.Context, req DispatchRequest, r WorkerResult) {
	payload := map[string]any{
		"worker_id": r.WorkerID,
		"status":    r.Status,
	}
	if r.Err != nil {
		payload["error"] = r.Err.Error()
	}

	appendCtx := context.WithoutCancel(ctx)
	_, _ = d.cfg.EventLog.Append(appendCtx, eventlog.Event{
		Type:      eventlog.EvtWorkerCheckpoint,
		SessionID: req.SessionID,
		ProjectID: req.ProjectID,
		Timestamp: d.cfg.Clock.Now(),
		Payload:   payload,
	})
}

func (d *dispatcher) unregisterAndRelease(ctx context.Context, leased []*worktreepool.Worktree) {
	d.mu.Lock()
	d.leased = removePointerSet(d.leased, leased)
	d.mu.Unlock()
	d.releaseSlice(ctx, leased)
}

func (d *dispatcher) releaseSlice(ctx context.Context, leased []*worktreepool.Worktree) {
	for _, wt := range leased {
		_ = d.cfg.Pool.Release(ctx, wt)
	}
}

func removePointerSet(src, remove []*worktreepool.Worktree) []*worktreepool.Worktree {
	if len(src) == 0 || len(remove) == 0 {
		return src
	}
	out := src[:0:len(src)]
	for _, p := range src {
		drop := false
		for _, r := range remove {
			if p == r {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, p)
		}
	}
	return out
}

func (d *dispatcher) Shutdown(ctx context.Context) error {
	d.mu.Lock()
	if d.shutdown {
		d.mu.Unlock()
		return nil
	}
	d.shutdown = true
	leased := d.leased
	d.leased = nil
	d.mu.Unlock()

	d.releaseSlice(ctx, leased)
	return d.cfg.Workforce.AbortAll(ctx)
}
