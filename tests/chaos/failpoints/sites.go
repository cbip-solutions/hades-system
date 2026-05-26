//go:build chaos

// SPDX-License-Identifier: MIT

package failpoints

// Site is one canonical gofail-injected hot path. Each Site MUST
// correspond exactly to a `// gofail: var <Name> struct{}` comment in
// the upstream source tree (see Makefile GOFAIL_PKGS). The Site
// catalogue is the single source of truth for the chaos failpoint
// matrix: tests iterate over Sites() to assert (a) every documented
// site has at least one test and (b) every test names a documented
// site (no orphan tests).
type Site struct {
	Name string

	Package string

	File string

	Description string
}

func Sites() []Site {
	return []Site{
		{
			Name:        "aggregatorIndexCorruption",
			Package:     "github.com/cbip-solutions/hades-system/internal/daemon/aggregatorbridge",
			File:        "internal/daemon/aggregatorbridge/bridge.go",
			Description: "aggregator index re-build on corruption",
		},
		{
			Name:        "auditChainAnchor",
			Package:     "github.com/cbip-solutions/hades-system/internal/audit/chain",
			File:        "internal/audit/chain/backfill.go",
			Description: "audit chain-anchor commit boundary (Backfill UPDATE)",
		},
		{
			Name:        "auditWALFsync",
			Package:     "github.com/cbip-solutions/hades-system/internal/audit/chain",
			File:        "internal/audit/chain/seal.go",
			Description: "tessera seal-leaf append (audit-WAL fsync analogue)",
		},
		{
			Name:        "breakerTransitionRace",
			Package:     "github.com/cbip-solutions/hades-system/internal/daemon/orchestrator",
			File:        "internal/daemon/orchestrator/circuit_breaker.go",
			Description: "circuit-breaker state transition race window",
		},
		{
			Name:        "costLedgerRebuild",
			Package:     "github.com/cbip-solutions/hades-system/internal/daemon/orchestrator",
			File:        "internal/daemon/orchestrator/cost_counters.go",
			Description: "cost-ledger rebuild on counter desync",
		},
		{
			Name:        "dispatcherCancelMidFlight",
			Package:     "github.com/cbip-solutions/hades-system/internal/daemon/dispatcher",
			File:        "internal/daemon/dispatcher/dispatcher.go",
			Description: "dispatcher per-attempt backend cancel mid-flight",
		},
		{
			Name:        "doctrineReloadRace",
			Package:     "github.com/cbip-solutions/hades-system/internal/doctrine/reload",
			File:        "internal/doctrine/reload/validate_swap.go",
			Description: "doctrine validate-then-swap atomicity",
		},
		{
			Name:        "litestreamWALFlush",
			Package:     "github.com/cbip-solutions/hades-system/internal/audit/litestream",
			File:        "internal/audit/litestream/rsync.go",
			Description: "litestream WAL rsync flush failure",
		},
		{
			Name:        "mergeEngineApplyConflict",
			Package:     "github.com/cbip-solutions/hades-system/internal/orchestrator/merge",
			File:        "internal/orchestrator/merge/candidate_apply.go",
			Description: "MergeEngine candidate apply conflict path",
		},
		{
			Name:        "pluginRPCBoundary",
			Package:     "github.com/cbip-solutions/hades-system/internal/daemon/handlers",
			File:        "internal/daemon/handlers/hermes_probe.go",
			Description: "plugin RPC boundary timeout",
		},
		{
			Name:        "privacyClassifierSidecarTimeout",
			Package:     "github.com/cbip-solutions/hades-system/internal/augment",
			File:        "internal/augment/privacy.go",
			Description: "privacy classifier sidecar timeout",
		},
		{
			Name:        "schedulerTickMiss",
			Package:     "github.com/cbip-solutions/hades-system/internal/scheduler",
			File:        "internal/scheduler/fire.go",
			Description: "scheduler tick fire miss / late",
		},
		{
			Name:        "sidecarRPCBoundary",
			Package:     "github.com/cbip-solutions/hades-system/internal/daemon/handlers",
			File:        "internal/daemon/handlers/bypass.go",
			Description: "sidecar bypass RPC boundary failure",
		},
		{
			Name:        "tesseraTileUpload",
			Package:     "github.com/cbip-solutions/hades-system/internal/audit/tessera",
			File:        "internal/audit/tessera/adapter.go",
			Description: "tessera tile-upload retry / partition not-sealed-on-fail",
		},
		{
			Name:        "worktreepoolAcquireTimeout",
			Package:     "github.com/cbip-solutions/hades-system/internal/orchestrator/worktreepool",
			File:        "internal/orchestrator/worktreepool/pool.go",
			Description: "worktree-pool acquire timeout under saturation",
		},
	}
}

const CanonicalSiteCount = 15

func SiteByName(name string) *Site {
	for i, s := range Sites() {
		if s.Name == name {
			out := Sites()[i]
			return &out
		}
	}
	return nil
}
