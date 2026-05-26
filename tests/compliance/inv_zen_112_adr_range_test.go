package compliance

import (
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

func TestInvZen112ADRRangeReserved(t *testing.T) {
	start, end := merge.Plan6ADRRange()
	if start != 30 {
		t.Errorf("inv-zen-112: Plan6ADRRangeStart = %d want 30", start)
	}
	if end != 39 {
		t.Errorf("inv-zen-112: Plan6ADRRangeEnd = %d want 39", end)
	}
	if end-start+1 != 10 {
		t.Errorf("inv-zen-112: range size = %d want 10", end-start+1)
	}
}

func TestInvZen112ADRRangeContainsExpectedSlots(t *testing.T) {
	start, end := merge.Plan6ADRRange()
	expectedSlots := []int{30, 31, 32, 33, 34, 35, 36, 37, 38, 39}
	for _, slot := range expectedSlots {
		if slot < start || slot > end {
			t.Errorf("inv-zen-112: slot %d outside reserved range [%d,%d]", slot, start, end)
		}
	}
}

func TestInvZen112ADRRangeConstantsMatch(t *testing.T) {
	start, end := merge.Plan6ADRRange()
	if start != merge.Plan6ADRRangeStart {
		t.Errorf("Plan6ADRRange().start = %d, but Plan6ADRRangeStart = %d (drift)",
			start, merge.Plan6ADRRangeStart)
	}
	if end != merge.Plan6ADRRangeEnd {
		t.Errorf("Plan6ADRRange().end = %d, but Plan6ADRRangeEnd = %d (drift)",
			end, merge.Plan6ADRRangeEnd)
	}
}
