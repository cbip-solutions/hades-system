package checks_test

import (
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy/checks"
)

func TestAll_OrderMatchesAllCheckNames(t *testing.T) {
	got := checks.All(checks.Deps{})
	want := autonomy.AllCheckNames()
	if len(got) != len(want) {
		t.Fatalf("All() len: want %d got %d", len(want), len(got))
	}
	for i, c := range got {
		if c.Name() != want[i] {
			t.Fatalf("All()[%d].Name: want %q got %q", i, want[i], c.Name())
		}
	}
}

func TestAll_ConstructsValidEngine(t *testing.T) {

	all := checks.All(checks.Deps{})
	if _, err := autonomy.NewCheckEngine(autonomy.EngineDeps{Checks: all}); err != nil {
		t.Fatalf("NewCheckEngine(All(...)) must succeed: %v", err)
	}
}
