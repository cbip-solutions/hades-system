// SPDX-License-Identifier: MIT
package worktreepool

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

var ErrInvalidConfig = errors.New("worktreepool: invalid PoolConfig")

var ErrPoolClosed = errors.New("worktreepool: pool closed")

var ErrPoolExhausted = errors.New("worktreepool: pool exhausted (elasticMax reached)")

type PoolConfig struct {
	// RepoRoot is the absolute path to the bare/normal git repo the pool
	// adds worktrees against. Phase D sets this from the project record.
	// MUST be absolute (filepath.IsAbs) and non-empty.
	RepoRoot string

	// WorktreeDir is the absolute parent directory under which worktree
	// subdirectories are materialised. Each lease becomes
	// {WorktreeDir}/{PoolID}-{leaseID}. MUST be absolute and non-empty.
	WorktreeDir string

	BranchBase string

	// Floor is the minimum number of warm worktrees the prewarm goroutine
	// keeps available. Doctrine map (Q4 C):
	//   max-scope=8, default=3, capa-firewall=5.
	// MUST be > 0.
	Floor int

	// ElasticMax is the upper bound on TOTAL worktrees (warm + leased).
	// Lease() blocks/spawns up to this ceiling. Doctrine map:
	//   max-scope=32, default=12, capa-firewall=15.
	// MUST be >= Floor.
	ElasticMax int

	// GCCadence is the periodic cadence for the orphanGC goroutine.
	// Default 5 * time.Minute when zero (Q4 C). Tests use shorter values.
	// MUST be >= 0 (zero = use default).
	GCCadence time.Duration

	Doctrine string

	// PoolID is a stable identifier embedded in worktree paths so multiple
	// pools sharing the same WorktreeDir do not collide. Default "p" when
	// empty.
	PoolID string

	// Clock is the injectable wall-clock + timer abstraction (Phase A
	// Q14 C). Production code leaves this nil so NewPool installs
	// clock.Real{}; tests inject *clock.Fake to drive deterministic
	// time advancement for the saturation debounce window
	// (exhaustEmitDebounce, B-4) and the GC ticker cadence (B-7).
	//
	// Doctrine note: every orchestrator-tier component that consumes
	// time MUST take a Clock seam (see clock package godoc). Direct
	// time.Now / time.NewTicker calls in production code are an
	// anti-pattern enforced by a Phase O lint pass.
	Clock clock.Clock
}

const minGCCadence = 1 * time.Second

const maxElasticMax = 1024

func degradedReason(err error) string {
	switch {
	case errors.Is(err, errClassENOSPC):
		return "ENOSPC"
	case errors.Is(err, errClassWorktreeLocked):
		return "WorktreeLocked"
	case errors.Is(err, errClassNetwork):
		return "Network"
	case errors.Is(err, errClassSignal):
		return "Signal"
	default:

		return "Unknown"
	}
}

const exhaustEmitDebounce = 100 * time.Millisecond

func (c *PoolConfig) validate() error {
	if c.RepoRoot == "" {
		return fmt.Errorf("%w: RepoRoot empty", ErrInvalidConfig)
	}
	if c.WorktreeDir == "" {
		return fmt.Errorf("%w: WorktreeDir empty", ErrInvalidConfig)
	}
	if !filepath.IsAbs(c.RepoRoot) {
		return fmt.Errorf("%w: RepoRoot not absolute: %q", ErrInvalidConfig, c.RepoRoot)
	}
	if !filepath.IsAbs(c.WorktreeDir) {
		return fmt.Errorf("%w: WorktreeDir not absolute: %q", ErrInvalidConfig, c.WorktreeDir)
	}
	if c.BranchBase == "" {
		return fmt.Errorf("%w: BranchBase empty", ErrInvalidConfig)
	}
	if c.Floor <= 0 {
		return fmt.Errorf("%w: Floor must be > 0, got %d", ErrInvalidConfig, c.Floor)
	}
	if c.ElasticMax < c.Floor {
		return fmt.Errorf("%w: ElasticMax (%d) < Floor (%d)", ErrInvalidConfig, c.ElasticMax, c.Floor)
	}
	if c.ElasticMax > maxElasticMax {
		return fmt.Errorf("%w: ElasticMax (%d) exceeds sanity ceiling %d (doctrine max-scope=32; if more is genuinely needed, amend this guard with rationale)",
			ErrInvalidConfig, c.ElasticMax, maxElasticMax)
	}
	if c.GCCadence < 0 {
		return fmt.Errorf("%w: GCCadence must be >= 0, got %v", ErrInvalidConfig, c.GCCadence)
	}
	if c.GCCadence > 0 && c.GCCadence < minGCCadence {
		return fmt.Errorf("%w: GCCadence (%v) must be >= %v or zero (default)",
			ErrInvalidConfig, c.GCCadence, minGCCadence)
	}
	switch c.Doctrine {
	case "", "max-scope", "default", "capa-firewall":

	default:
		return fmt.Errorf("%w: Doctrine %q not in {max-scope, default, capa-firewall}",
			ErrInvalidConfig, c.Doctrine)
	}
	return nil
}

type Worktree struct {
	path      string
	id        int64
	branch    string
	createdAt time.Time
}

func (w *Worktree) Path() string { return w.path }

func (w *Worktree) ID() int64 { return w.id }

func (w *Worktree) Branch() string { return w.branch }

func (w *Worktree) CreatedAt() time.Time { return w.createdAt }

// Pool is the public API. Implementations MUST be safe for concurrent use
// from arbitrary goroutines (HRA voting fans out N FMV samples in parallel).
type Pool interface {
	Lease(ctx context.Context) (*Worktree, error)

	Release(ctx context.Context, w *Worktree) error

	PruneOrphans(ctx context.Context) (PruneReport, error)

	Close(ctx context.Context) error
}

type PruneReport struct {
	GitPruned        int
	FilesystemSwept  int
	AdminOnlyCleared int
	Errors           []string
	Duration         time.Duration
}

type EventEmitter = eventlog.Appender

type Executor interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type concretePool struct {
	cfg     PoolConfig
	emitter EventEmitter
	exec    Executor
	clk     clock.Clock

	mu        sync.Mutex
	warm      []*Worktree
	leased    map[int64]*Worktree
	nextID    atomic.Int64
	closed    atomic.Bool
	closeOnce sync.Once

	lastExhaustedAt time.Time

	total atomic.Int32

	signalSlot chan struct{}

	prewarmCtx    context.Context
	prewarmCancel context.CancelFunc
	prewarmDone   chan struct{}

	gcCtx    context.Context
	gcCancel context.CancelFunc
	gcDone   chan struct{}
}

func NewPool(cfg PoolConfig, emitter EventEmitter, exec Executor) (Pool, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if emitter == nil {
		return nil, fmt.Errorf("%w: nil EventEmitter", ErrInvalidConfig)
	}
	if exec == nil {
		return nil, fmt.Errorf("%w: nil Executor", ErrInvalidConfig)
	}
	if cfg.PoolID == "" {
		cfg.PoolID = "p"
	}
	if cfg.GCCadence == 0 {
		cfg.GCCadence = 5 * time.Minute
	}
	// IMP-2: Phase A Clock seam. Default to clock.Real{} so production
	// callers never need to think about it; tests inject *clock.Fake
	// for deterministic time advancement (saturation debounce window,
	// future B-7 GC cadence). The cfg field captures the operator-
	// supplied Clock; the cached clk on concretePool is what hot paths
	// read so they do not pay the cfg-struct-field deref every Now().
	clk := cfg.Clock
	if clk == nil {
		clk = clock.Real{}
		cfg.Clock = clk
	}

	p := &concretePool{
		cfg:        cfg,
		emitter:    emitter,
		exec:       exec,
		clk:        clk,
		leased:     make(map[int64]*Worktree, cfg.ElasticMax),
		signalSlot: make(chan struct{}, cfg.ElasticMax),
	}
	p.prewarmCtx, p.prewarmCancel = context.WithCancel(context.Background())
	p.gcCtx, p.gcCancel = context.WithCancel(context.Background())
	p.prewarmDone = make(chan struct{})
	p.gcDone = make(chan struct{})

	go p.prewarmLoop()
	go p.gcLoop()

	return p, nil
}

func (p *concretePool) Close(ctx context.Context) error {
	if p.closed.Load() {
		return nil
	}
	var firstErr error
	p.closeOnce.Do(func() {
		p.closed.Store(true)
		p.prewarmCancel()
		p.gcCancel()

		select {
		case <-p.prewarmDone:
		case <-ctx.Done():
			firstErr = ctx.Err()
		}
		if firstErr == nil {
			select {
			case <-p.gcDone:
			case <-ctx.Done():
				firstErr = ctx.Err()
			}
		}

		p.mu.Lock()
		p.warm = nil
		p.leased = nil
		p.signalSlot = nil
		p.mu.Unlock()
	})
	return firstErr
}

// Lease and Release are the lease-side interface fulfilment surface;
// PruneOrphans (B-7 + B-8) lives in gc.go. The closed-pool guards on all
// three remain canonical: closed.Load is a fast-path hint, an under-mu
// re-check is REQUIRED for any path that touches warm/leased/signalSlot.
//
// IMPORTANT for B-3..B-5 implementers: the closed.Load() fast-path is a
// fail-fast hint, not a synchronization point. Real Lease/Release bodies
// MUST re-check closed under mu before touching warm/leased/signalSlot
// (Close races with Lease/Release: closed.Store(true) precedes mu.Lock +
// nil maps; an unsynchronized Lease that already passed closed.Load can
// panic on send to nil signalSlot). Pattern:
//
//	if p.closed.Load() {
//	    return nil, ErrPoolClosed   // fast-path hint
//	}
//	p.mu.Lock()
//	defer p.mu.Unlock()
//	if p.closed.Load() {            // re-check under mu — REQUIRED
//	    return nil, ErrPoolClosed
//	}
//	// ... touch warm/leased/signalSlot here, safe under mu
//
// Forward-looking sentinels (declared in subsequent tasks):
//   - ErrPoolDegraded: B-2 ENOSPC + subsequent disk-pressure recovery
//     (HRA Phase I uses to downgrade to plurality-only per Q8)
//   - ErrSubprocessTimeout: B-2 git worktree subprocess deadlines

// Lease returns a worktree for write. B-3 implements the warm fast path:
// under p.mu, if len(p.warm) > 0 pop the tail entry, move it into the
// leased map keyed by worktree.id, return. If warm is empty (or the
// caller observes closed under mu) the slow path runs (B-4 fills body).
//
// Concurrency contract (carry-forward from B-1 docblock above):
// closed.Load is a fast-path hint, not a synchronization point. Lease
// MUST re-check closed under mu before touching warm/leased/signalSlot,
// because Close races with Lease — closed.Store(true) precedes the
// mu-protected nilling of warm/leased/signalSlot, and a Lease call that
// already passed the outer closed.Load can panic on a nil leased map or
// nil signalSlot if it does not re-check.
func (p *concretePool) Lease(ctx context.Context) (*Worktree, error) {
	if p.closed.Load() {
		return nil, ErrPoolClosed
	}

	p.mu.Lock()
	if p.closed.Load() {
		p.mu.Unlock()
		return nil, ErrPoolClosed
	}
	if n := len(p.warm); n > 0 {
		w := p.warm[n-1]
		p.warm = p.warm[:n-1]
		p.leased[w.id] = w
		p.mu.Unlock()
		return w, nil
	}
	p.mu.Unlock()

	return p.leaseSlowPath(ctx)
}

func (p *concretePool) leaseSlowPath(ctx context.Context) (*Worktree, error) {
	for {

		p.mu.Lock()
		if p.closed.Load() {
			p.mu.Unlock()
			return nil, ErrPoolClosed
		}
		if n := len(p.warm); n > 0 {
			w := p.warm[n-1]
			p.warm = p.warm[:n-1]
			p.leased[w.id] = w
			p.mu.Unlock()
			return w, nil
		}

		if int(p.total.Load()) < p.cfg.ElasticMax {

			p.total.Add(1)
			p.mu.Unlock()

			w, err := p.spawnOne(ctx)
			if err != nil {

				p.total.Add(-1)
				// B-4 IMP-1: emit Degraded for ALL transient infra
				// pressure classes (ENOSPC + WorktreeLocked + Network
				// + Signal). The classifier (subprocess.go) attaches
				// ErrPoolDegraded as the `extra` sentinel on each of
				// those four classes, so a single errors.Is check
				// captures the full HRA Phase I Q8 cost-pressure
				// surface uniformly. The reason field disambiguates
				// for downstream consumers (dashboards, HRA voting
				// strategy selection). Non-pressure classes (Branch
				// already exists, NotARepo, Panic, Other) do NOT
				// wrap ErrPoolDegraded — those are deterministic
				// configuration / bug failures and HRA must not
				// downgrade voting strategy on them.
				if errors.Is(err, ErrPoolDegraded) {

					_, _ = p.emitter.Append(ctx, eventlog.Event{
						Type: eventlog.EvtWorktreePoolDegraded,
						Payload: map[string]any{
							"reason":      degradedReason(err),
							"doctrine":    p.cfg.Doctrine,
							"pool_id":     p.cfg.PoolID,
							"elastic_max": p.cfg.ElasticMax,
							"error":       err.Error(),
						},
					})
				}
				return nil, err
			}

			p.mu.Lock()
			if p.closed.Load() {

				p.total.Add(-1)
				p.mu.Unlock()
				return nil, ErrPoolClosed
			}
			p.leased[w.id] = w
			p.mu.Unlock()
			return w, nil
		}

		now := p.clk.Now()
		shouldEmit := now.Sub(p.lastExhaustedAt) >= exhaustEmitDebounce
		if shouldEmit {
			p.lastExhaustedAt = now
		}

		ch := p.signalSlot
		p.mu.Unlock()

		if shouldEmit {
			_, _ = p.emitter.Append(ctx, eventlog.Event{
				Type: eventlog.EvtWorktreePoolExhausted,
				Payload: map[string]any{
					"doctrine":    p.cfg.Doctrine,
					"pool_id":     p.cfg.PoolID,
					"elastic_max": p.cfg.ElasticMax,
				},
			})
		}

		select {
		case <-ch:

		case <-ctx.Done():

			return nil, fmt.Errorf("%w: %w", ErrPoolExhausted, ctx.Err())
		}
	}
}

// spawnOne materialises one fresh worktree under cfg.WorktreeDir via
// gitWorktreeAdd (B-2 wrapper). Caller is responsible for total-counter
// accounting: leaseSlowPath increments before calling and decrements on
// error so the slot reservation race is closed.
//
// Branch naming convention: zen-pool-{PoolID}-{leaseID}. PoolID is set by
// PoolConfig (defaults to "p"). leaseID is allocated atomically from
// nextID so concurrent spawns never collide.
//
// Directory naming: {WorktreeDir}/{PoolID}-{leaseID}. The unique leaseID
// suffix means two pools sharing the same WorktreeDir do not collide as
// long as PoolIDs differ.
//
// On error the caller MUST decrement total. Errors classified by B-2's
// classify(): ENOSPC, BranchExists, NotARepo, Network, WorktreeLocked,
// Timeout, Signal, Panic, Other.
func (p *concretePool) spawnOne(ctx context.Context) (*Worktree, error) {
	id := p.nextID.Add(1)
	branch := fmt.Sprintf("zen-pool-%s-%d", p.cfg.PoolID, id)
	dir := filepath.Join(p.cfg.WorktreeDir, fmt.Sprintf("%s-%d", p.cfg.PoolID, id))
	if err := gitWorktreeAdd(ctx, p.exec, p.cfg.RepoRoot, dir, branch, p.cfg.BranchBase); err != nil {
		return nil, err
	}
	return &Worktree{
		id:   id,
		path: dir,

		branch:    branch,
		createdAt: p.clk.Now(),
	}, nil
}

func (p *concretePool) Release(ctx context.Context, w *Worktree) error {
	if p.closed.Load() {
		return ErrPoolClosed
	}
	if w == nil {
		return errors.New("worktreepool: Release nil Worktree")
	}
	p.mu.Lock()
	if p.closed.Load() {
		p.mu.Unlock()
		return ErrPoolClosed
	}
	if _, ok := p.leased[w.id]; !ok {
		p.mu.Unlock()
		return fmt.Errorf("worktreepool: Release: worktree %d not in leased map (double-release?)", w.id)
	}
	delete(p.leased, w.id)
	p.mu.Unlock()

	if err := gitReset(ctx, p.exec, w.path, p.cfg.BranchBase); err != nil {
		p.destroyWorktree(ctx, w, fmt.Sprintf("reset failed: %v", err))
		return nil
	}
	if err := gitClean(ctx, p.exec, w.path); err != nil {
		p.destroyWorktree(ctx, w, fmt.Sprintf("clean failed: %v", err))
		return nil
	}

	p.mu.Lock()
	if p.closed.Load() {
		p.mu.Unlock()
		return ErrPoolClosed
	}
	p.warm = append(p.warm, w)
	ch := p.signalSlot
	p.mu.Unlock()

	select {
	case ch <- struct{}{}:
	default:
	}
	return nil
}

func (p *concretePool) destroyWorktree(ctx context.Context, w *Worktree, reason string) {
	_ = gitWorktreeRemove(ctx, p.exec, p.cfg.RepoRoot, w.path)
	p.total.Add(-1)

	p.mu.Lock()
	ch := p.signalSlot
	p.mu.Unlock()

	select {
	case ch <- struct{}{}:
	default:
	}

	_, _ = p.emitter.Append(ctx, eventlog.Event{
		Type: eventlog.EvtWorktreePoolDegraded,
		Payload: map[string]any{
			"reason":      "release-destroyed",
			"detail":      reason,
			"worktree":    w.path,
			"doctrine":    p.cfg.Doctrine,
			"pool_id":     p.cfg.PoolID,
			"elastic_max": p.cfg.ElasticMax,
		},
	})
}
