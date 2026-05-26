package builtin_test

import (
	"reflect"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctrine/builtin"
)

func TestDefaultWorkforceBoundsTighter(t *testing.T) {
	s := builtin.Default()
	if s == nil {
		t.Fatal("Default() returned nil")
	}
	if s.Workforce.MaxDepth != 2 {
		t.Errorf("Workforce.MaxDepth = %d, want 2", s.Workforce.MaxDepth)
	}
	if s.Workforce.MaxWidthPerLayer != 4 {
		t.Errorf("Workforce.MaxWidthPerLayer = %d, want 4", s.Workforce.MaxWidthPerLayer)
	}
}

func TestDefaultTighterThanMaxScope(t *testing.T) {
	d := builtin.Default()
	m := builtin.MaxScope()
	defaultCap := d.Workforce.MaxDepth * d.Workforce.MaxWidthPerLayer
	maxCap := m.Workforce.MaxDepth * m.Workforce.MaxWidthPerLayer
	if defaultCap >= maxCap {
		t.Errorf("default workforce capacity %d >= max-scope %d; default must be tighter",
			defaultCap, maxCap)
	}
}

func TestDefaultAutonomyAssistedMode(t *testing.T) {
	s := builtin.Default()
	if s.Autonomy.Mode != "assisted" {
		t.Errorf("Autonomy.Mode = %q, want %q", s.Autonomy.Mode, "assisted")
	}
}

func TestDefaultTransverseAxiomsAllTrue(t *testing.T) {
	s := builtin.Default()
	if !s.Transverse.NoTechDebt || !s.Transverse.NoStubs ||
		!s.Transverse.BuildFinalProduct || !s.Transverse.NoDefer {
		t.Errorf("transverse axioms must all be true")
	}
}

func TestDefaultR1ConfirmationPolicyMedium(t *testing.T) {
	s := builtin.Default()
	cp := s.Autonomy.ConfirmationPolicy
	wants := []struct {
		name, value string
	}{
		{"BudgetBreachThreshold", "medium"},
		{"SpecAmendmentProposal", "medium"},
		{"InvariantViolation", "medium"},
		{"ArchitecturalReviewEscalation", "medium"},
	}
	for _, w := range wants {
		var got string
		switch w.name {
		case "BudgetBreachThreshold":
			got = cp.BudgetBreachThreshold
		case "SpecAmendmentProposal":
			got = cp.SpecAmendmentProposal
		case "InvariantViolation":
			got = cp.InvariantViolation
		case "ArchitecturalReviewEscalation":
			got = cp.ArchitecturalReviewEscalation
		}
		if got != w.value {
			t.Errorf("ConfirmationPolicy.%s = %q, want %q", w.name, got, w.value)
		}
	}
}

func TestDefaultR2VotingPluralityOnly(t *testing.T) {
	s := builtin.Default()
	v := s.Autonomy.Voting
	if v.PluralityThresholdPct != 50 {
		t.Errorf("Voting.PluralityThresholdPct = %d, want 50", v.PluralityThresholdPct)
	}
	if v.FMVEnable {
		t.Errorf("Voting.FMVEnable = true, want false")
	}
	if !v.EMSEnable {
		t.Errorf("Voting.EMSEnable = false, want true")
	}
}

func TestDefaultR3TestTiersMinimumViable(t *testing.T) {
	s := builtin.Default()
	want := []string{"unit", "integration", "compliance", "analysistest"}
	got := s.Gates.TestTiers.Enabled
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Gates.TestTiers.Enabled = %v, want %v", got, want)
	}
}

func TestDefaultR4SeverityStandard(t *testing.T) {
	s := builtin.Default()
	sev := s.Notifications.SeverityPerDoctrine
	if sev.ActionNeededPromotesToUrgent {
		t.Errorf("ActionNeededPromotesToUrgent = true, want false")
	}
	if !sev.UrgentBypassesQuietHours {
		t.Errorf("UrgentBypassesQuietHours = false, want true")
	}
	if sev.InfoImmediateDuringQuiet != "queue" {
		t.Errorf("InfoImmediateDuringQuiet = %q, want queue", sev.InfoImmediateDuringQuiet)
	}
}

func TestDefaultR5ZenDayCadencePlan7Verbatim(t *testing.T) {
	s := builtin.Default()
	cad := s.ZenDayCadence
	if cad.MorningBriefCron != "0 8 * * 1-5" {
		t.Errorf("MorningBriefCron = %q, want %q", cad.MorningBriefCron, "0 8 * * 1-5")
	}
	if cad.MorningBriefIfWithinHours != 2 {
		t.Errorf("MorningBriefIfWithinHours = %d, want 2", cad.MorningBriefIfWithinHours)
	}
	if cad.EODDigestCron != "0 18 * * 1-5" {
		t.Errorf("EODDigestCron = %q, want %q", cad.EODDigestCron, "0 18 * * 1-5")
	}
	if cad.EODDigestIfWithinHours != 4 {
		t.Errorf("EODDigestIfWithinHours = %d, want 4", cad.EODDigestIfWithinHours)
	}
}

func TestDefaultQuotaBounded(t *testing.T) {
	s := builtin.Default()
	q := s.Quota
	cap := s.Workforce.MaxDepth * s.Workforce.MaxWidthPerLayer
	if q.MaxConcurrentTasks > cap {
		t.Errorf("Quota.MaxConcurrentTasks = %d > pool capacity %d", q.MaxConcurrentTasks, cap)
	}
	if q.MaxConcurrentTasks != 8 {
		t.Errorf("Quota.MaxConcurrentTasks = %d, want 8", q.MaxConcurrentTasks)
	}
}

func TestDefaultTmuxIdleTTL(t *testing.T) {
	s := builtin.Default()
	if s.Tmux.IdleTTLMin != 480 {
		t.Errorf("Tmux.IdleTTLMin = %d, want 480 (8h)", s.Tmux.IdleTTLMin)
	}
	if !s.Tmux.AutoReap {
		t.Errorf("Tmux.AutoReap = false, want true")
	}
}

func TestDefaultMissPolicySkip(t *testing.T) {
	s := builtin.Default()
	if s.Scheduling.MissPolicy != "skip" {
		t.Errorf("Scheduling.MissPolicy = %q, want skip", s.Scheduling.MissPolicy)
	}
}

func TestDefaultWFQBaseline(t *testing.T) {
	s := builtin.Default()
	if s.WFQ.ProjectWeightDefault != 50 {
		t.Errorf("WFQ.ProjectWeightDefault = %d, want 50", s.WFQ.ProjectWeightDefault)
	}
	if s.WFQ.OvercommitPolicy != "queue" {
		t.Errorf("WFQ.OvercommitPolicy = %q, want queue", s.WFQ.OvercommitPolicy)
	}
}

func TestDefaultAmendmentCooldown72h(t *testing.T) {
	s := builtin.Default()
	if s.Autonomy.AmendmentCooldownH != 72 {
		t.Errorf("Autonomy.AmendmentCooldownH = %d, want 72",
			s.Autonomy.AmendmentCooldownH)
	}
}

func TestDefaultMergeScoringSum100(t *testing.T) {
	s := builtin.Default()
	w := s.Merge.ScoringWeights
	sum := w.TestPass + w.LintPass + w.Coverage + w.Diff + w.Duration
	if sum != 100 {
		t.Errorf("Merge.ScoringWeights sum = %d, want 100", sum)
	}
}

func TestDefaultMergeNarrowerCandidates(t *testing.T) {
	d := builtin.Default()
	m := builtin.MaxScope()
	if d.Merge.MaxCandidates >= m.Merge.MaxCandidates {
		t.Errorf("default Merge.MaxCandidates = %d >= max-scope %d; default must be narrower",
			d.Merge.MaxCandidates, m.Merge.MaxCandidates)
	}
}

func TestDefaultHRAFewerLayers(t *testing.T) {
	s := builtin.Default()
	if !reflect.DeepEqual(s.HRA.LayersEnabled, []int{1, 2}) {
		t.Errorf("HRA.LayersEnabled = %v, want [1 2]", s.HRA.LayersEnabled)
	}
}

func TestDefaultCaronteBalanced(t *testing.T) {
	s := builtin.Default()
	if s.Caronte.BranchPolicy != "balanced" {
		t.Errorf("Caronte.BranchPolicy = %q, want balanced", s.Caronte.BranchPolicy)
	}
}

func TestDefaultReviewRelaxed(t *testing.T) {
	s := builtin.Default()
	if s.Review.RequireDualReview {
		t.Errorf("Review.RequireDualReview = true, want false (default doctrine)")
	}
	if s.Review.RotateReviewerEvery != 3 {
		t.Errorf("Review.RotateReviewerEvery = %d, want 3", s.Review.RotateReviewerEvery)
	}
}
