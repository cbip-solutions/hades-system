package v1_test

import (
	"testing"

	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

func TestTightenRegistry_NonEmpty(t *testing.T) {
	reg := v1.TightenRegistry()
	if len(reg) == 0 {
		t.Fatal("registry empty; build broken")
	}

	wantPresent := []string{
		"AutoUpgrade",
		"Workforce.MaxDepth",
		"Workforce.MinDepth",
		"Workforce.MaxWidthPerLayer",
		"HRA.LayersEnabled",
		"Transverse.NoTechDebt",
		"Transverse.NoStubs",
		"Transverse.BuildFinalProduct",
		"Transverse.NoDefer",
		"Autonomy.Mode",
		"Autonomy.ConfirmationPolicy.BudgetBreachThreshold",
		"Autonomy.Voting.PluralityThresholdPct",
		"Autonomy.Voting.EMSEnable",
		"Merge.Mode",
		"Merge.ScoringWeights.TestPass",
		"Merge.ScoringWeights.Diff",
		"Notifications.QuietHoursStart",
		"ZenDayCadence.MorningBriefIfWithinHours",
	}
	for _, key := range wantPresent {
		if _, ok := reg[key]; !ok {
			t.Errorf("registry missing %q", key)
		}
	}
}

func TestTightenRegistry_DirectionsParsed(t *testing.T) {
	reg := v1.TightenRegistry()

	cases := []struct {
		path  string
		dir   v1.TightenDirection
		ranks []string
	}{
		{"AutoUpgrade", v1.TightenDirRank, []string{"none", "major", "minor", "patch"}},
		{"Workforce.MaxDepth", v1.TightenDirDecrease, nil},
		{"Workforce.MinDepth", v1.TightenDirIncrease, nil},
		{"HRA.LayersEnabled", v1.TightenDirAddOnly, nil},
		{"Transverse.NoStubs", v1.TightenDirTruth, nil},
		{"Autonomy.Mode", v1.TightenDirRank, []string{"assisted", "agent", "pure"}},
		{"Autonomy.Voting.EMSEnable", v1.TightenDirBidirectional, nil},
		{"Notifications.QuietHoursStart", v1.TightenDirBidirectional, nil},
	}
	for _, c := range cases {
		rule, ok := reg[c.path]
		if !ok {
			t.Errorf("missing %s", c.path)
			continue
		}
		if rule.Direction != c.dir {
			t.Errorf("%s direction = %v; want %v", c.path, rule.Direction, c.dir)
		}
		if c.ranks != nil {
			if len(rule.RankList) != len(c.ranks) {
				t.Errorf("%s ranks len = %d; want %d (%v)", c.path, len(rule.RankList), len(c.ranks), c.ranks)
			} else {
				for i := range c.ranks {
					if rule.RankList[i] != c.ranks[i] {
						t.Errorf("%s ranks[%d] = %q; want %q", c.path, i, rule.RankList[i], c.ranks[i])
					}
				}
			}
		}
	}
}

func TestTightenRegistry_SkipsSectionMarkers(t *testing.T) {
	reg := v1.TightenRegistry()
	// Section parents declare tighten:"-" — they MUST NOT appear in the leaf
	// registry (only their leaves do).
	for _, parent := range []string{"Workforce", "HRA", "Autonomy", "Merge", "Transverse", "Notifications"} {
		if _, ok := reg[parent]; ok {
			t.Errorf("section parent %q should not appear in leaf registry", parent)
		}
	}
}

func TestTightenRegistry_StableUnderRepeatedCalls(t *testing.T) {
	reg1 := v1.TightenRegistry()
	reg2 := v1.TightenRegistry()
	if len(reg1) != len(reg2) {
		t.Fatalf("registry size unstable: %d vs %d", len(reg1), len(reg2))
	}

	for k := range reg1 {
		if _, ok := reg2[k]; !ok {
			t.Errorf("key %q lost between calls", k)
		}
	}
}

func TestTightenRegistry_RequiresOperator(t *testing.T) {

	reg := v1.TightenRegistry()
	for path, rule := range reg {
		if rule.RequiresOperator {
			t.Errorf("unexpected RequiresOperator on %s; no Schema field declares this in v1", path)
		}
	}
}

func TestTightenRegistry_CallerMutationDoesNotPersist(t *testing.T) {
	reg1 := v1.TightenRegistry()
	original, hadKey := reg1["Workforce.MaxDepth"]
	if !hadKey {
		t.Fatal("setup: Workforce.MaxDepth missing from registry")
	}

	delete(reg1, "Workforce.MaxDepth")
	reg1["INJECTED.SyntheticPath"] = v1.TightenRule{Direction: v1.TightenDirSkip}

	reg2 := v1.TightenRegistry()

	if _, ok := reg2["Workforce.MaxDepth"]; !ok {
		t.Error("Workforce.MaxDepth deletion leaked into next TightenRegistry() call")
	}
	if got, ok := reg2["Workforce.MaxDepth"]; ok && got.Direction != original.Direction {
		t.Errorf("Workforce.MaxDepth Direction mutated: got %v want %v", got.Direction, original.Direction)
	}
	if _, ok := reg2["INJECTED.SyntheticPath"]; ok {
		t.Error("synthetic INJECTED.SyntheticPath persisted across calls")
	}

}
