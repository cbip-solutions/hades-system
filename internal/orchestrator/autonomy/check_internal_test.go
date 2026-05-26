package autonomy

import "testing"

func TestCanonicalIndex_UnknownNameSortsToEnd(t *testing.T) {
	got := canonicalIndex("not_a_real_check")
	if want := len(AllCheckNames()); got != want {
		t.Fatalf("unknown check should sort to %d; got %d", want, got)
	}
}

func TestTierStrictness_UnknownTierReturnsZero(t *testing.T) {
	if got := tierStrictness(Tier(99)); got != 0 {
		t.Fatalf("tierStrictness(Tier(99)): want 0, got %d", got)
	}
	if got := tierStrictness(Tier(0)); got != 0 {
		t.Fatalf("tierStrictness(Tier(0)): want 0, got %d", got)
	}
}

func TestApplyOverride_WeakerOverrideKeepsBaseline(t *testing.T) {
	if got := applyOverride(TierHard, TierSoft); got != TierHard {
		t.Fatalf("weaker override must not loosen at engine layer; got %v", got)
	}
}

func TestTierStrictness_AllValidTiers(t *testing.T) {
	cases := []struct {
		t    Tier
		want int
	}{
		{TierInformational, 1},
		{TierSoft, 2},
		{TierHard, 3},
	}
	for _, c := range cases {
		if got := tierStrictness(c.t); got != c.want {
			t.Fatalf("tierStrictness(%v): want %d got %d", c.t, c.want, got)
		}
	}
}
