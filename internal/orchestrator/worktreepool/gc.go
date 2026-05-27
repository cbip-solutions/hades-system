// SPDX-License-Identifier: MIT
package worktreepool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

// gcLoop runs Layer-A `git worktree prune` on the cadence cfg.GCCadence.
//
// Layer model (Q4 C, defense-in-depth):
//
// - Layer A — `git worktree prune`: removes.git/worktrees/{name} admin
// entries that no longer reference an existing directory on disk.
// Cheap, idempotent, safe to run from a periodic ticker.
// - Layer B — filesystem `rm -rf` of leaked dirs (worktree dir without
// admin entry; admin entry without dir): more expensive + intent-
// driven, runs only via the public PruneOrphans API.
//
// gcLoop drives Layer A every tick. Layer B is invoked synchronously from
// PruneOrphans (operator-driven via `hades doctor worktree-pool prune` →
// orchestrator adapter, ). On a Layer-A failure, gcLoop emits
// EvtWorktreePoolDegraded with reason=gc-prune-failed so HRA Q8
// cost-pressure downgrade can react. Success is silent (the norm).
//
// Concurrency contract:
// - Subprocess invocation runs UNLOCKED — the prune can take a moment
// under disk pressure and we MUST NOT block Lease/Release/Close behind
// a slow git invocation. The prune wrapper itself touches no pool
// state; it is a pure subprocess fan-out.
// - p.gcCtx threads the context through prune so Close-driven
// cancellation kills any in-flight prune subprocess via
// osExecExecutor's exec.CommandContext.
// - On p.gcCtx.Done observation, the loop returns cleanly. defer
// close(p.gcDone) signals the Close path to stop waiting.
//
// Clock seam (Q14 C, IMP-2): the ticker is built via
// p.clk.NewTicker so tests inject *clock.Fake to drive cadence
// deterministically (no wall-clock sleeps in test mode).
func (p *concretePool) gcLoop() {
	defer close(p.gcDone)
	tick := p.clk.NewTicker(p.cfg.GCCadence)
	defer tick.Stop()

	for {
		select {
		case <-tick.C():
			if err := p.runLayerA(p.gcCtx); err != nil {

				_, _ = p.emitter.Append(p.gcCtx, eventlog.Event{
					Type: eventlog.EvtWorktreePoolDegraded,
					Payload: map[string]any{
						"reason":      "gc-prune-failed",
						"source":      "gc-loop",
						"doctrine":    p.cfg.Doctrine,
						"pool_id":     p.cfg.PoolID,
						"elastic_max": p.cfg.ElasticMax,
						"error":       err.Error(),
					},
				})
			}
		case <-p.gcCtx.Done():
			return
		}
	}
}

func (p *concretePool) runLayerA(ctx context.Context) error {
	return gitWorktreePrune(ctx, p.exec, p.cfg.RepoRoot)
}

// runLayerB performs the filesystem sweep: list worktrees known to git
// via `git worktree list --porcelain`, scan WorktreeDir on disk one
// level deep, identify dirs whose name matches the pool prefix BUT are
// absent from the git-known set AND not in the pool's leased + warm
// membership maps, then `rm -rf` them.
//
// Safety guards (defense-in-depth Q4 C):
//
// - Pool-prefix filter: only dirs named `{PoolID}-...` or
// `hades-pool-{PoolID}-...` are candidates. WorktreeDir may legitimately
// host dirs from other pools or operator artefacts (rare but
// possible) — we do NOT touch them.
// - leased + warm membership: a dir with the right prefix that
// happens to back an in-flight worktree (e.g., spawnOne in flight,
// or warm slice live) is skipped. This closes the race where Layer
// B observes a dir on disk before its `git worktree add` completes
// and registers the admin entry.
// - Errors accumulate in report.Errors and do NOT abort the sweep —
// the partial-success contract lets dashboards see counters AND
// failure detail in a single report.
//
// B-7 ships the full Layer B body. B-8 adds adversarial test coverage
// (concurrent leaseSlowPath spawn vs Layer B sweep, mid-sweep Close,
// partial os.RemoveAll failures) and the.AdminOnlyCleared accounting
// (admin entries with no dir on disk — Layer A clears these but the
// count is reported here via list diffs in PruneOrphans).
func (p *concretePool) runLayerB(ctx context.Context, report *PruneReport) {
	entries, err := gitWorktreeList(ctx, p.exec, p.cfg.RepoRoot)
	if err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("layerB-list: %v", err))
		return
	}

	known := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		known[filepath.Clean(e.path)] = struct{}{}
	}

	dirEntries, err := os.ReadDir(p.cfg.WorktreeDir)
	if err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("layerB-readdir: %v", err))
		return
	}
	zenPrefix := "hades-pool-" + p.cfg.PoolID + "-"
	bareprefix := p.cfg.PoolID + "-"
	for _, de := range dirEntries {
		if !de.IsDir() {
			continue
		}
		name := de.Name()
		if !strings.HasPrefix(name, bareprefix) && !strings.HasPrefix(name, zenPrefix) {
			continue
		}
		full := filepath.Join(p.cfg.WorktreeDir, name)
		clean := filepath.Clean(full)
		if _, ok := known[clean]; ok {
			continue
		}
		if p.isLeased(clean) {
			continue
		}
		if err := os.RemoveAll(clean); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("layerB-remove %s: %v", clean, err))
			continue
		}
		report.FilesystemSwept++
	}
}

func (p *concretePool) isLeased(path string) bool {
	clean := filepath.Clean(path)
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, w := range p.leased {
		if filepath.Clean(w.path) == clean {
			return true
		}
	}
	for _, w := range p.warm {
		if filepath.Clean(w.path) == clean {
			return true
		}
	}
	return false
}

func (p *concretePool) PruneOrphans(ctx context.Context) (PruneReport, error) {
	if p.closed.Load() {
		return PruneReport{}, ErrPoolClosed
	}
	start := p.clk.Now()
	var report PruneReport

	preEntries, _ := gitWorktreeList(ctx, p.exec, p.cfg.RepoRoot)

	if err := p.runLayerA(ctx); err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("layerA: %v", err))

	} else {
		postEntries, _ := gitWorktreeList(ctx, p.exec, p.cfg.RepoRoot)
		if delta := len(preEntries) - len(postEntries); delta > 0 {
			report.GitPruned = delta

			report.AdminOnlyCleared = delta
		}
	}

	p.runLayerB(ctx, &report)
	report.Duration = p.clk.Now().Sub(start)

	if len(report.Errors) > 0 {
		_, _ = p.emitter.Append(ctx, eventlog.Event{
			Type: eventlog.EvtWorktreePoolDegraded,
			Payload: map[string]any{
				"reason":      "prune-orphans-partial",
				"source":      "prune-orphans",
				"errors":      report.Errors,
				"doctrine":    p.cfg.Doctrine,
				"pool_id":     p.cfg.PoolID,
				"elastic_max": p.cfg.ElasticMax,
			},
		})

	}
	return report, nil
}
