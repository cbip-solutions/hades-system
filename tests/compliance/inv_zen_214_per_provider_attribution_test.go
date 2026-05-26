package compliance_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestInvZen214_PerProviderAttribution(t *testing.T) {

	mig064 := readSource(t, filepath.Join("internal", "store", "migrations", "064_cost_ledger_provider.sql"))
	if !strings.Contains(mig064, "ADD COLUMN provider") {
		t.Error("inv-zen-214: migration 064 must add cost_ledger.provider column")
	}

	mig065 := readSource(t, filepath.Join("internal", "store", "migrations", "065_tier_health_samples.sql"))
	if !strings.Contains(mig065, "tier_health_samples") {
		t.Error("inv-zen-214: migration 065 must create tier_health_samples table")
	}
	if !strings.Contains(mig065, "provider") {
		t.Error("inv-zen-214: tier_health_samples table must have a provider column")
	}

	disp := readSource(t, filepath.Join("internal", "daemon", "dispatcher", "dispatcher.go"))
	if !strings.Contains(disp, "Provider string") {
		t.Error("inv-zen-214: dispatcher.CostEvent must carry a Provider field")
	}

	cl := readSource(t, filepath.Join("internal", "store", "cost_ledger.go"))
	if !strings.Contains(cl, "Provider") {
		t.Error("inv-zen-214: store.CostLedgerRow must carry a Provider field")
	}
}
