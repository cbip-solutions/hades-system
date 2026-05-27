// SPDX-License-Identifier: MIT
// Package cli — doctor_knowledge.go
//
// Task J-3: knowledge subsystem doctor probe. Five aspects
// (integrity, last_indexed, cpu_budget, watcher_status, extension_hooks)
// per spec §6.7. Implementation delegates to KnowledgeProber interface;
// fakes in tests, real adapter in internal/knowledge/prober.go.
//
// invariant: NEVER issues HTTP request; --remote handling is daemon-side
// . RunKnowledgeProbe stays local.
// invariant: probe surface includes a sanity check that extension hook
// columns are NULL by default in release.
package cli

import (
	"context"
	"fmt"
	"time"
)

// KnowledgeProber is the contract RunKnowledgeProbe consumes. The
// internal/knowledge package satisfies it via knowledge.NewProber. Tests
// pass a fakeKnowledgeProber.
//
// All methods MUST be safe for concurrent use, MUST honour ctx, and
// SHOULD return within 1 second each (5 aspects × 1s = 5s probe budget).
type KnowledgeProber interface {
	IntegrityCheck(ctx context.Context) (string, error)

	LastIndexedAt(ctx context.Context) (time.Time, error)

	IndexerCPUBudget(ctx context.Context) (used, warn, fail int, err error)

	WatcherHeartbeat(ctx context.Context) (time.Time, error)

	ExtensionHookNullCount(ctx context.Context) (nullCount, totalCount int, err error)
}

const (
	knowledgeStaleWarnAfter   = 30 * time.Minute
	knowledgeStaleFailAfter   = 2 * time.Hour
	knowledgeWatcherDeadAfter = 60 * time.Second
)

func RunKnowledgeProbe(ctx context.Context, p KnowledgeProber) ([]ProbeResult, error) {
	out := make([]ProbeResult, 0, 5)
	out = append(out, runKnowledgeIntegrity(ctx, p))
	out = append(out, runKnowledgeLastIndexed(ctx, p))
	out = append(out, runKnowledgeCPUBudget(ctx, p))
	out = append(out, runKnowledgeWatcher(ctx, p))
	out = append(out, runKnowledgeExtensionHooks(ctx, p))
	return out, nil
}

func runKnowledgeIntegrity(ctx context.Context, p KnowledgeProber) ProbeResult {
	r := ProbeResult{Name: "knowledge.index.integrity"}
	out, err := p.IntegrityCheck(ctx)
	if err != nil {
		r.Status = ProbeFail
		r.Message = "integrity_check call failed"
		r.Detail = err.Error()
		r.Hint = "daemon may be down or knowledge-index/index.db locked"
		return r
	}
	if out == "ok" {
		r.Status = ProbeOK
		r.Message = "PRAGMA integrity_check = ok"
		return r
	}
	r.Status = ProbeFail
	r.Message = "PRAGMA integrity_check reported corruption"
	r.Detail = out
	r.Hint = "run: hades knowledge reindex --full (offline-safe; rebuilds from per-project sources)"
	return r
}

func runKnowledgeLastIndexed(ctx context.Context, p KnowledgeProber) ProbeResult {
	r := ProbeResult{Name: "knowledge.index.last_indexed"}
	t, err := p.LastIndexedAt(ctx)
	if err != nil {
		r.Status = ProbeFail
		r.Message = "last_indexed query failed"
		r.Detail = err.Error()
		return r
	}
	if t.IsZero() {
		r.Status = ProbeWarn
		r.Message = "no rows indexed yet (fresh install?)"
		r.Hint = "run: hades knowledge reindex"
		return r
	}
	age := time.Since(t)
	switch {
	case age >= knowledgeStaleFailAfter:
		r.Status = ProbeFail
		r.Message = fmt.Sprintf("last update %s ago (>%s)", age.Round(time.Second), knowledgeStaleFailAfter)
		r.Hint = "run: hades knowledge reindex; check fsnotify watcher status (watcher.status probe)"
	case age >= knowledgeStaleWarnAfter:
		r.Status = ProbeWarn
		r.Message = fmt.Sprintf("last update %s ago (>%s)", age.Round(time.Second), knowledgeStaleWarnAfter)
		r.Hint = "if persistent: hades knowledge reindex"
	default:
		r.Status = ProbeOK
		r.Message = fmt.Sprintf("last update %s ago", age.Round(time.Second))
	}
	return r
}

func runKnowledgeCPUBudget(ctx context.Context, p KnowledgeProber) ProbeResult {
	r := ProbeResult{Name: "knowledge.indexer.cpu_budget"}
	used, warn, fail, err := p.IndexerCPUBudget(ctx)
	if err != nil {
		r.Status = ProbeFail
		r.Message = "cpu_budget query failed"
		r.Detail = err.Error()
		return r
	}
	r.Message = fmt.Sprintf("%d%% used (warn=%d%%, fail=%d%%)", used, warn, fail)
	switch {
	case fail > 0 && used >= fail:
		r.Status = ProbeFail
		r.Hint = "indexer is saturating CPU budget; consider lowering watcher debounce or pausing reindex via: hades knowledge reindex --pause"
	case warn > 0 && used >= warn:
		r.Status = ProbeWarn
	default:
		r.Status = ProbeOK
	}
	return r
}

func runKnowledgeWatcher(ctx context.Context, p KnowledgeProber) ProbeResult {
	r := ProbeResult{Name: "knowledge.watcher.status"}
	t, err := p.WatcherHeartbeat(ctx)
	if err != nil {
		r.Status = ProbeFail
		r.Message = "watcher heartbeat query failed"
		r.Detail = err.Error()
		r.Hint = "restart daemon: hades daemon restart"
		return r
	}
	if t.IsZero() {
		r.Status = ProbeFail
		r.Message = "watcher never started"
		r.Hint = "restart daemon: hades daemon restart"
		return r
	}
	age := time.Since(t)
	if age >= knowledgeWatcherDeadAfter {
		r.Status = ProbeFail
		r.Message = fmt.Sprintf("watcher heartbeat stale (%s)", age.Round(time.Second))
		r.Hint = "restart daemon to revive fsnotify goroutine: hades daemon restart"
		return r
	}
	r.Status = ProbeOK
	r.Message = fmt.Sprintf("heartbeat %s ago", age.Round(time.Second))
	return r
}

func runKnowledgeExtensionHooks(ctx context.Context, p KnowledgeProber) ProbeResult {
	r := ProbeResult{Name: "knowledge.extension_hooks.null_default"}
	nullCount, total, err := p.ExtensionHookNullCount(ctx)
	if err != nil {
		r.Status = ProbeFail
		r.Message = "extension_hook query failed"
		r.Detail = err.Error()
		return r
	}
	if total == 0 {
		r.Status = ProbeOK
		r.Message = "no rows yet (NULL-by-default contract trivially satisfied)"
		return r
	}
	r.Message = fmt.Sprintf("%d/%d rows have NULL audit_chain_anchor (Plan 9 hook)", nullCount, total)
	if nullCount == total {
		r.Status = ProbeOK
		return r
	}

	r.Status = ProbeWarn
	r.Hint = "Plan 7 ships extension columns NULL by default; non-NULL rows imply Plan 9 (audit hash-chain) or Plan 14 (ecosystem RAG) wired upstream — verify intentional"
	return r
}
