//go:build chaos

package failpoints

import (
	"testing"
)

func TestMergeEngineApplyConflictSiteRegistered(t *testing.T) {
	site := SiteByName("mergeEngineApplyConflict")
	if site == nil {
		t.Fatal("Site mergeEngineApplyConflict missing from catalogue")
	}
	if site.Package != "github.com/cbip-solutions/hades-system/internal/orchestrator/merge" {
		t.Errorf("Package = %q", site.Package)
	}
}

func TestMergeEngineApplyConflictActivation(t *testing.T) {
	term := Term{Name: "mergeEngineApplyConflict", Mode: ModeReturn, Arg: `"conflict"`}
	restore := Activate(term)
	defer restore()
	if !envContains(term.String()) {
		t.Errorf("env var missing %q after Activate", term)
	}
}

func TestSchedulerTickMissSiteRegistered(t *testing.T) {
	if SiteByName("schedulerTickMiss") == nil {
		t.Fatal("Site schedulerTickMiss missing")
	}
}

func TestSchedulerTickMissActivation(t *testing.T) {
	term := Term{Name: "schedulerTickMiss", Mode: ModeSleep, Arg: "10ms"}
	restore := Activate(term)
	defer restore()
	if !envContains(term.String()) {
		t.Errorf("env var missing %q after Activate", term)
	}
}

func TestWorktreepoolAcquireTimeoutSiteRegistered(t *testing.T) {
	site := SiteByName("worktreepoolAcquireTimeout")
	if site == nil {
		t.Fatal("Site worktreepoolAcquireTimeout missing")
	}
	if site.Package != "github.com/cbip-solutions/hades-system/internal/orchestrator/worktreepool" {
		t.Errorf("Package = %q", site.Package)
	}
}

func TestWorktreepoolAcquireTimeoutActivation(t *testing.T) {
	term := Term{Name: "worktreepoolAcquireTimeout", Mode: ModeSleep, Arg: "5s"}
	restore := Activate(term)
	defer restore()
	if !envContains(term.String()) {
		t.Errorf("env var missing %q after Activate", term)
	}
}

func TestAggregatorIndexCorruptionSiteRegistered(t *testing.T) {
	if SiteByName("aggregatorIndexCorruption") == nil {
		t.Fatal("Site aggregatorIndexCorruption missing")
	}
}

func TestAggregatorIndexCorruptionActivation(t *testing.T) {
	term := Term{Name: "aggregatorIndexCorruption", Mode: ModeReturn, Arg: `"corrupt"`}
	restore := Activate(term)
	defer restore()
	if !envContains(term.String()) {
		t.Errorf("env var missing %q after Activate", term)
	}
}

func TestCostLedgerRebuildSiteRegistered(t *testing.T) {
	if SiteByName("costLedgerRebuild") == nil {
		t.Fatal("Site costLedgerRebuild missing")
	}
}

func TestCostLedgerRebuildActivation(t *testing.T) {
	term := Term{Name: "costLedgerRebuild", Mode: ModeReturn, Arg: `"rebuild_failed"`}
	restore := Activate(term)
	defer restore()
	if !envContains(term.String()) {
		t.Errorf("env var missing %q after Activate", term)
	}
}

func TestDoctrineReloadRaceSiteRegistered(t *testing.T) {
	if SiteByName("doctrineReloadRace") == nil {
		t.Fatal("Site doctrineReloadRace missing")
	}
}

func TestDoctrineReloadRaceActivation(t *testing.T) {
	term := Term{Name: "doctrineReloadRace", Mode: ModeSleep, Arg: "10ms"}
	restore := Activate(term)
	defer restore()
	if !envContains(term.String()) {
		t.Errorf("env var missing %q after Activate", term)
	}
}

func TestPrivacyClassifierSidecarTimeoutSiteRegistered(t *testing.T) {
	if SiteByName("privacyClassifierSidecarTimeout") == nil {
		t.Fatal("Site privacyClassifierSidecarTimeout missing")
	}
}

func TestPrivacyClassifierSidecarTimeoutActivation(t *testing.T) {
	term := Term{Name: "privacyClassifierSidecarTimeout", Mode: ModeSleep, Arg: "100ms"}
	restore := Activate(term)
	defer restore()
	if !envContains(term.String()) {
		t.Errorf("env var missing %q after Activate", term)
	}
}

func TestOrchestratorFailpointsBatchActivation(t *testing.T) {
	terms := []Term{
		{Name: "mergeEngineApplyConflict", Mode: ModeReturn, Arg: `"conflict"`},
		{Name: "schedulerTickMiss", Mode: ModeSleep, Arg: "10ms"},
		{Name: "worktreepoolAcquireTimeout", Mode: ModeSleep, Arg: "5s"},
		{Name: "aggregatorIndexCorruption", Mode: ModeReturn, Arg: `"corrupt"`},
		{Name: "costLedgerRebuild", Mode: ModeReturn, Arg: `"rebuild_failed"`},
		{Name: "doctrineReloadRace", Mode: ModeSleep, Arg: "10ms"},
		{Name: "privacyClassifierSidecarTimeout", Mode: ModeSleep, Arg: "100ms"},
	}
	restore := ActivateAll(terms...)
	defer restore()
	for _, term := range terms {
		if !envContains(term.String()) {
			t.Errorf("env var missing %q after ActivateAll", term)
		}
	}
}

// TestAllFifteenSitesActivate pins the load-bearing matrix-completeness
// contract: every Site in the catalogue MUST round-trip through
// Activate cleanly. A new gofail site that fails to parse (typo,
// invalid char) lights up here.
func TestAllFifteenSitesActivate(t *testing.T) {
	sites := Sites()
	if len(sites) != CanonicalSiteCount {
		t.Fatalf("len(Sites()) = %d, want %d", len(sites), CanonicalSiteCount)
	}
	for _, s := range sites {
		s := s
		t.Run(s.Name, func(t *testing.T) {
			term := Term{Name: s.Name, Mode: ModeOff, Arg: ""}
			restore := Activate(term)
			defer restore()
			if !envContains(term.String()) {
				t.Errorf("env var missing %q", term)
			}
		})
	}
}
