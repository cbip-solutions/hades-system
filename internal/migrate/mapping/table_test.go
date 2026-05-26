package mapping

import "testing"

func TestAllTableEntries_NonEmpty(t *testing.T) {
	t.Parallel()
	entries := allTableEntries()
	if len(entries) < 7 {
		t.Errorf("got %d entries; expected ≥7 surface kinds per amendment §2.4", len(entries))
	}
}

func TestAllTableEntries_AllKindsUnique(t *testing.T) {
	t.Parallel()
	seen := map[EntryKind]bool{}
	for _, e := range allTableEntries() {
		if seen[e.Kind] {
			t.Errorf("duplicate EntryKind %q in table", e.Kind)
		}
		seen[e.Kind] = true
	}
}
