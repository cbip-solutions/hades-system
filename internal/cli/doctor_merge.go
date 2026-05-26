// SPDX-License-Identifier: MIT
// Package cli — doctor_merge.go (Plan 6 Phase F F-3 + C-2 wiring).
//
// `runMergeChecks` is the [plan-6 merge] section of `zen doctor`. It
// produces 4 spec §6.2 checks against the daemon's /v1/merge/* surface
// and the host git binary, returning a `[]CheckResult` slice that the
// doctorAggregateRunE caller appends to the unified results pipeline:
//
//	merge.daemon_up         — daemon CacheStatus responds < 100ms
//	merge.git_version       — git ≥2.40 (re-uses Phase A merge.VersionCheck)
//	merge.eventlog_writable — proxied via daemon CacheStatus success
//	                          (the daemon refuses to serve cache status
//	                          if the merge eventlog is unwritable, so a
//	                          successful CacheStatus is a transitive
//	                          attestation that the eventlog is healthy)
//	merge.cache_health      — cache size + hit rate + last rebuild
//
// Wiring (C-2 fix, 2026-05-05): doctorAggregateRunE calls runMergeChecks
// after the orchestrator section, sets Section="Merge" on each row,
// and appends to allResults so json/yaml/table renderers +
// --filter pipelines see the merge checks identically to every other
// section.
//
// Shape ([]CheckResult): mirrors the runOrchestratorChecks pattern from
// / "fail" matching the package-wide convention. Section is left empty —
// the caller stamps it so the same helper can be reused by a (future)
// dedicated `zen doctor merge` subcommand without double-scoping.
//
// The MergeClient interface is the same one consumed by `zen merge`
// (declared in merge.go via type alias of internal/client.MergeClient).
//
// Boundary note: this file imports internal/orchestrator/merge for
// VersionCheck + NewRealGit. inv-zen-104 forbids merge⊥store
// (compliance test pins ./internal/orchestrator/merge/... → no store);
// it does NOT forbid CLI ↔ merge. Per inv-zen-104 compliance test
// (tests/compliance/inv_zen_104_merge_no_store_test.go) the boundary
// is one-directional and unaffected by this import.
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

const daemonUpThreshold = 100 * time.Millisecond

func runMergeChecks(ctx context.Context, client MergeClient) []CheckResult {
	results := make([]CheckResult, 0, 4)

	start := time.Now()
	cs, daemonErr := client.CacheStatus(ctx)
	elapsed := time.Since(start)
	switch {
	case daemonErr != nil:
		results = append(results, CheckResult{
			Name:   "merge.daemon_up",
			Status: "fail",
			Detail: daemonErr.Error(),
			Hint:   "Ensure daemon is running: zen daemon start",
		})
	case elapsed > daemonUpThreshold:
		results = append(results, CheckResult{
			Name:   "merge.daemon_up",
			Status: "warn",
			Detail: fmt.Sprintf("slow: %s (>%s threshold)",
				elapsed.Round(time.Millisecond), daemonUpThreshold),
			Hint: "Inspect dispatcher saturation: zen orchestrator status",
		})
	default:
		results = append(results, CheckResult{
			Name:   "merge.daemon_up",
			Status: "ok",
			Detail: fmt.Sprintf("responded in %s", elapsed.Round(time.Millisecond)),
		})
	}

	gctx, gcancel := context.WithTimeout(ctx, 3*time.Second)
	if g, gerr := merge.NewRealGit(); gerr != nil {
		results = append(results, CheckResult{
			Name:   "merge.git_version",
			Status: "fail",
			Detail: gerr.Error(),
			Hint:   "Install git ≥2.40 (brew install git on macOS)",
		})
	} else if verr := merge.VersionCheck(gctx, g); verr != nil {
		results = append(results, CheckResult{
			Name:   "merge.git_version",
			Status: "fail",
			Detail: verr.Error(),
			Hint:   "Upgrade git ≥2.40 (brew upgrade git on macOS)",
		})
	} else {
		results = append(results, CheckResult{
			Name:   "merge.git_version",
			Status: "ok",
			Detail: "git ≥2.40 OK",
		})
	}
	gcancel()

	if daemonErr != nil {
		results = append(results, CheckResult{
			Name:   "merge.eventlog_writable",
			Status: "fail",
			Detail: "daemon unreachable (cannot probe eventlog)",
			Hint:   "Restart daemon: zen daemon start",
		})
	} else {
		results = append(results, CheckResult{
			Name:   "merge.eventlog_writable",
			Status: "ok",
			Detail: "proxied via /v1/merge/cache/status",
		})
	}

	switch {
	case daemonErr != nil:
		results = append(results, CheckResult{
			Name:   "merge.cache_health",
			Status: "fail",
			Detail: daemonErr.Error(),
			Hint:   "Restart daemon: zen daemon start",
		})
	case cs != nil && cs.RebuildError != "":
		results = append(results, CheckResult{
			Name:   "merge.cache_health",
			Status: "fail",
			Detail: fmt.Sprintf("rebuild_error: %s (size=%d, last_rebuilt=%s)",
				cs.RebuildError, cs.Size, cs.LastRebuilt),
			Hint: "Clear cache + restart: zen merge cache clear && zen daemon restart",
		})
	case cs != nil:
		results = append(results, CheckResult{
			Name:   "merge.cache_health",
			Status: "ok",
			Detail: fmt.Sprintf("size=%d, hit_rate=%.2f%%, last_rebuilt=%s",
				cs.Size, cs.HitRatePct, cs.LastRebuilt),
		})
	default:

		results = append(results, CheckResult{
			Name:   "merge.cache_health",
			Status: "fail",
			Detail: "daemon returned nil status",
			Hint:   "Daemon handler returned 200 with empty body — investigate /v1/merge/cache/status",
		})
	}

	return results
}
