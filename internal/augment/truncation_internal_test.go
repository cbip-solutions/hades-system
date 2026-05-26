package augment

import "testing"

func TestEstimateSummaryTokens_EmptyFloors(t *testing.T) {
	got := estimateSummaryTokens(CommunitySummary{})
	if got != 1 {
		t.Errorf("empty summary: want 1 (floor), got %d", got)
	}
}
