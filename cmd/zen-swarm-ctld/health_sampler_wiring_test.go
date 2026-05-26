// Package main — health_sampler_wiring_test.go (v0.17.7 A-6).
//
// Tests the health-sampler compute closure built by buildHealthComputeClosure.
// Asserts
//   - The closure returns a snapshot containing all five health dependency keys.
//   - The snapshot never contains autonomy-only keys (verify_docs, ci_consecutive,
//     adrs_valid, etc.) — the compute MUST NOT call CheckEngine.RunCheck.
//
// This is a unit test at the compute-closure level (the plan's "assert at the
// compute-closure level" fallback — a full daemon boot test would require
// a running store + all MCPs).
package main

import (
	"context"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon"
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestratoradapter"
	"github.com/cbip-solutions/hades-system/internal/store"
)

func buildTestPlan5Service(t *testing.T, st *store.Store) *daemon.Plan5OrchestratorService {
	t.Helper()
	a, err := orchestratoradapter.New(st)
	if err != nil {
		t.Fatalf("orchestratoradapter.New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	svc, err := daemon.NewPlan5OrchestratorService(daemon.Plan5OrchestratorServiceConfig{
		Adapter: a,
	})
	if err != nil {
		t.Fatalf("NewPlan5OrchestratorService: %v", err)
	}
	return svc
}

var healthDepKeys = []string{
	"research_mcp_up",
	"gitnexus_up",
	"event_log_writable",
	"adapters_clean",
	"last_session_clean",
}

var autonomyOnlyKeys = []string{
	"verify_docs",
	"ci_consecutive",
	"adrs_valid",
	"lint_clean",
	"plans_green",
	"amendment_dry_run",
}

func TestHealthComputeClosure_ContainsFiveKeysNotAutonomyOnly(t *testing.T) {
	st := openTestStore(t)
	svc := buildTestPlan5Service(t, st)

	compute := buildHealthComputeClosure(svc)
	snap := compute(context.Background())

	for _, key := range healthDepKeys {
		if _, ok := snap.Deps[key]; !ok {
			t.Errorf("snapshot missing required health key %q", key)
		}
	}
	for _, key := range autonomyOnlyKeys {
		if _, ok := snap.Deps[key]; ok {
			t.Errorf("snapshot must not contain autonomy-only key %q (no RunCheck allowed)", key)
		}
	}
}
