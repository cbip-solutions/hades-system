// SPDX-License-Identifier: MIT
// Package ecosystem — budget_monitor.go
//
// Inv-zen-199: ingest budget ≤60 GB ceiling enforced; ≥ceiling → block all
// writes. State machine:
//
//	BudgetGreen    < 80% target (< 32 GB default) — no action
//	BudgetYellow   80-100% target (32-40 GB) — log + alert only
//	BudgetRed      100-150% target (40-60 GB) — block NEW ingest; updates OK
//	BudgetOverflow ≥ 150% target (≥60 GB) — block ALL writes; require operator prune
//
// BudgetMonitor is goroutine-safe: mu protects lastStatus + lastCheckedAt +
// priorState. Producer/consumers (Dispatcher.Ingest path, cron worker) gate on
// BlockNewIngest / BlockAllWrites before issuing any write op.
//
// Boundary (inv-zen-031): this file does NOT import internal/store or net/http.
// Sizer interface abstracts filesystem stat; AuditEmitter abstracts chain
// emission. Production wiring (Phase F daemon-init) injects DBSizer + a thin
// adapter over Phase A's RAGAuditChainEmitter.
//
// Inv-zen-199 three-place triple:
//
//	(1) Runtime: BudgetMonitor.Check() returns BudgetStatus.BlockAllWrites at
//	    Overflow; ClassifyBudgetState() is the single-source-of-truth for the
//	    threshold semantics.
//	(2) Property test: budget_monitor_test.go TestBudgetMonitor_PropertyState
//	    Classification samples 1000 random sizes and verifies classification
//	    against an independent piecewise re-derivation.
//	(3) Integration test: TestBudgetMonitor_StateTransitionCoverage walks the
//	    full state machine (Green→Yellow→Red→Overflow→Red→Green) with injected
//	    Sizer + AuditEmitter and verifies state, gates, and audit emission.
package ecosystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type BudgetState int

const (
	BudgetGreen BudgetState = iota

	BudgetYellow

	BudgetRed

	BudgetOverflow
)

func (s BudgetState) String() string {
	switch s {
	case BudgetGreen:
		return "green"
	case BudgetYellow:
		return "yellow"
	case BudgetRed:
		return "red"
	case BudgetOverflow:
		return "overflow"
	default:
		return "unknown"
	}
}

// BudgetStatus is the result of a BudgetMonitor.Check() call.
//
// Consumers MUST honour BlockNewIngest / BlockAllWrites before writing.
// Inv-zen-199 enforcement lives at the consumer site, not inside BudgetMonitor.
type BudgetStatus struct {
	State BudgetState

	TotalGB float64

	TargetGB float64

	CeilingGB float64

	BlockNewIngest bool

	BlockAllWrites bool

	CheckedAt time.Time
}

type BudgetSizer interface {
	TotalBytes(ctx context.Context) (int64, error)
}

type BudgetAuditEmitter interface {
	// EmitBudgetStateChange is called exactly once per state transition.
	// It MUST NOT block; callers cancel within a 2-second deadline.
	// A returned error is logged best-effort but does NOT propagate to the
	// BudgetMonitor.Check caller (per master §3.6).
	EmitBudgetStateChange(ctx context.Context, prev, next BudgetState, totalGB float64) error
}

type BudgetMonitorConfig struct {
	TargetGB float64

	CeilingGB float64

	Sizer BudgetSizer

	AuditEmitter BudgetAuditEmitter

	CacheTTL time.Duration
}

type BudgetMonitor struct {
	cfg           BudgetMonitorConfig
	mu            sync.Mutex
	lastStatus    *BudgetStatus
	lastCheckedAt time.Time
	priorState    *BudgetState
}

func NewBudgetMonitor(cfg BudgetMonitorConfig) *BudgetMonitor {
	if cfg.TargetGB <= 0 {
		panic("ecosystem.BudgetMonitor: TargetGB must be > 0")
	}
	if cfg.CeilingGB <= cfg.TargetGB {
		panic("ecosystem.BudgetMonitor: CeilingGB must be > TargetGB")
	}
	return &BudgetMonitor{cfg: cfg}
}

// Check queries the sizer (respecting CacheTTL), classifies the new BudgetState,
// emits a chain-anchored audit event on state transition, and returns BudgetStatus.
//
// Inv-zen-199 enforcement: callers MUST honour BudgetStatus.BlockAllWrites /
// BlockNewIngest before proceeding with any write operation. The monitor does
// not intercept writes — it returns status for callers to gate on.
//
// Returns error if Sizer is nil or Sizer.TotalBytes fails. Audit-emission
// errors are best-effort and do NOT propagate.
func (bm *BudgetMonitor) Check(ctx context.Context) (BudgetStatus, error) {
	if bm.cfg.Sizer == nil {
		return BudgetStatus{}, fmt.Errorf("ecosystem.BudgetMonitor: Sizer is nil — inject a BudgetSizer")
	}

	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.cfg.CacheTTL > 0 && bm.lastStatus != nil &&
		time.Since(bm.lastCheckedAt) < bm.cfg.CacheTTL {
		return *bm.lastStatus, nil
	}

	totalBytes, err := bm.cfg.Sizer.TotalBytes(ctx)
	if err != nil {
		return BudgetStatus{}, fmt.Errorf("ecosystem.BudgetMonitor.Check: sizer: %w", err)
	}

	totalGB := float64(totalBytes) / float64(1<<30)
	state := ClassifyBudgetState(totalGB, bm.cfg.TargetGB, bm.cfg.CeilingGB)

	status := BudgetStatus{
		State:          state,
		TotalGB:        totalGB,
		TargetGB:       bm.cfg.TargetGB,
		CeilingGB:      bm.cfg.CeilingGB,
		BlockNewIngest: state >= BudgetRed,
		BlockAllWrites: state == BudgetOverflow,
		CheckedAt:      time.Now(),
	}

	if bm.priorState != nil && *bm.priorState != state && bm.cfg.AuditEmitter != nil {
		eCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		// Audit failure is best-effort: log via returned error is NOT propagated
		// (master §3.6: audit emission MUST NOT block status return).
		_ = bm.cfg.AuditEmitter.EmitBudgetStateChange(eCtx, *bm.priorState, state, totalGB)
		cancel()
	}

	bm.lastStatus = &status
	bm.lastCheckedAt = status.CheckedAt
	copied := state
	bm.priorState = &copied

	return status, nil
}

func ClassifyBudgetState(totalGB, targetGB, ceilingGB float64) BudgetState {
	const yellowPct = 0.80
	switch {
	case totalGB < targetGB*yellowPct:
		return BudgetGreen
	case totalGB < targetGB:
		return BudgetYellow
	case totalGB < ceilingGB:
		return BudgetRed
	default:
		return BudgetOverflow
	}
}

type DBSizer struct {
	dirs []string
}

func NewDBSizer(dir string) *DBSizer {
	return &DBSizer{dirs: []string{dir}}
}

func (d *DBSizer) WithCASDir(casDir string) *DBSizer {
	dirs := make([]string, 0, len(d.dirs)+1)
	dirs = append(dirs, d.dirs...)
	dirs = append(dirs, casDir)
	return &DBSizer{dirs: dirs}
}

func (d *DBSizer) TotalBytes(_ context.Context) (int64, error) {
	var total int64
	for _, dir := range d.dirs {
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("DBSizer.TotalBytes: ReadDir(%s): %w", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() {

				sub := filepath.Join(dir, e.Name())
				subEntries, err := os.ReadDir(sub)
				if err != nil {
					continue
				}
				for _, se := range subEntries {
					if se.IsDir() {
						continue
					}
					fi, err := se.Info()
					if err != nil {
						continue
					}
					total += fi.Size()
				}
				continue
			}
			fi, err := e.Info()
			if err != nil {
				continue
			}
			total += fi.Size()
		}
	}
	return total, nil
}
