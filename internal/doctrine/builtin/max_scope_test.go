package builtin_test

import (
	"reflect"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctrine/builtin"
)

func TestMaxScopeWorkforceBoundsPerSystemDesign(t *testing.T) {
	s := builtin.MaxScope()
	if s == nil {
		t.Fatal("MaxScope() returned nil")
	}
	if s.Workforce.MinDepth != 1 {
		t.Errorf("Workforce.MinDepth = %d, want 1", s.Workforce.MinDepth)
	}
	if s.Workforce.MaxDepth != 4 {
		t.Errorf("Workforce.MaxDepth = %d, want 4", s.Workforce.MaxDepth)
	}
	if s.Workforce.MaxWidthPerLayer != 8 {
		t.Errorf("Workforce.MaxWidthPerLayer = %d, want 8", s.Workforce.MaxWidthPerLayer)
	}
}

func TestMaxScopeWorkforceRecovery(t *testing.T) {
	s := builtin.MaxScope()
	r := s.Workforce.Recovery
	if r.TransientRetryBudget != 5 {
		t.Errorf("Workforce.Recovery.TransientRetryBudget = %d, want 5", r.TransientRetryBudget)
	}
	if r.PermanentInfraEscalate != "operator-notify" {
		t.Errorf("Workforce.Recovery.PermanentInfraEscalate = %q, want operator-notify",
			r.PermanentInfraEscalate)
	}
	if r.DoctrineRetryBudget != 3 {
		t.Errorf("Workforce.Recovery.DoctrineRetryBudget = %d, want 3", r.DoctrineRetryBudget)
	}
}

func TestMaxScopeAutonomyMode(t *testing.T) {
	s := builtin.MaxScope()
	if s.Autonomy.Mode != "agent" {
		t.Errorf("Autonomy.Mode = %q, want %q", s.Autonomy.Mode, "agent")
	}
	if s.Autonomy.CheckMode != "strict" {
		t.Errorf("Autonomy.CheckMode = %q, want strict", s.Autonomy.CheckMode)
	}
}

func TestMaxScopeTransverseAxiomsAllTrue(t *testing.T) {
	s := builtin.MaxScope()
	if !s.Transverse.NoTechDebt {
		t.Errorf("Transverse.NoTechDebt = false, want true")
	}
	if !s.Transverse.NoStubs {
		t.Errorf("Transverse.NoStubs = false, want true")
	}
	if !s.Transverse.BuildFinalProduct {
		t.Errorf("Transverse.BuildFinalProduct = false, want true")
	}
	if !s.Transverse.NoDefer {
		t.Errorf("Transverse.NoDefer = false, want true")
	}
}

func TestMaxScopeAutoUpgradePatchDefault(t *testing.T) {
	s := builtin.MaxScope()
	if s.AutoUpgrade != "patch" {
		t.Errorf("AutoUpgrade = %q, want %q", s.AutoUpgrade, "patch")
	}
}

func TestMaxScopeR1ConfirmationPolicy(t *testing.T) {
	s := builtin.MaxScope()
	cp := s.Autonomy.ConfirmationPolicy
	wants := map[string]string{
		"BudgetBreachThreshold":         "high",
		"SpecAmendmentProposal":         "high",
		"InvariantViolation":            "high",
		"ArchitecturalReviewEscalation": "high",
	}
	for field, want := range wants {
		var got string
		switch field {
		case "BudgetBreachThreshold":
			got = cp.BudgetBreachThreshold
		case "SpecAmendmentProposal":
			got = cp.SpecAmendmentProposal
		case "InvariantViolation":
			got = cp.InvariantViolation
		case "ArchitecturalReviewEscalation":
			got = cp.ArchitecturalReviewEscalation
		}
		if got != want {
			t.Errorf("ConfirmationPolicy.%s = %q, want %q", field, got, want)
		}
	}
}

func TestMaxScopeR2VotingFullStack(t *testing.T) {
	s := builtin.MaxScope()
	v := s.Autonomy.Voting
	if v.PluralityThresholdPct != 50 {
		t.Errorf("Voting.PluralityThresholdPct = %d, want 50", v.PluralityThresholdPct)
	}
	if !v.FMVEnable {
		t.Errorf("Voting.FMVEnable = false, want true")
	}
	if !v.EMSEnable {
		t.Errorf("Voting.EMSEnable = false, want true")
	}
}

func TestMaxScopeR3TestTiersAll10(t *testing.T) {
	s := builtin.MaxScope()
	want := []string{
		"unit", "integration", "adversarial", "chaos", "realworld",
		"compliance", "replay", "timeaccel", "orchestrator_chaos", "analysistest",
	}
	got := s.Gates.TestTiers.Enabled
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Gates.TestTiers.Enabled = %v, want %v", got, want)
	}
}

func TestMaxScopeR4SeverityStrict(t *testing.T) {
	s := builtin.MaxScope()
	sev := s.Notifications.SeverityPerDoctrine
	if sev.ActionNeededPromotesToUrgent {
		t.Errorf("ActionNeededPromotesToUrgent = true, want false")
	}
	if !sev.UrgentBypassesQuietHours {
		t.Errorf("UrgentBypassesQuietHours = false, want true")
	}
	if sev.InfoImmediateDuringQuiet != "queue" {
		t.Errorf("InfoImmediateDuringQuiet = %q, want %q",
			sev.InfoImmediateDuringQuiet, "queue")
	}
}

func TestMaxScopeR5ZenDayCadenceTwiceDaily(t *testing.T) {
	s := builtin.MaxScope()
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
	if cad.EODDigestIfWithinHours != 2 {
		t.Errorf("EODDigestIfWithinHours = %d, want 2", cad.EODDigestIfWithinHours)
	}
}

func TestMaxScopeMergeScoringWeightsSum100(t *testing.T) {
	s := builtin.MaxScope()
	w := s.Merge.ScoringWeights
	sum := w.TestPass + w.LintPass + w.Coverage + w.Diff + w.Duration
	if sum != 100 {
		t.Errorf("Merge.ScoringWeights sum = %d, want 100", sum)
	}
	if w.TestPass <= 0 {
		t.Errorf("Merge.ScoringWeights.TestPass = %d, want > 0", w.TestPass)
	}
	if w.Coverage <= 0 {
		t.Errorf("Merge.ScoringWeights.Coverage = %d, want > 0", w.Coverage)
	}
}

func TestMaxScopeMergeMode(t *testing.T) {
	s := builtin.MaxScope()
	if s.Merge.Mode != "balanced" {
		t.Errorf("Merge.Mode = %q, want balanced", s.Merge.Mode)
	}
	if s.Merge.MaxCandidates != 5 {
		t.Errorf("Merge.MaxCandidates = %d, want 5", s.Merge.MaxCandidates)
	}
}

func TestMaxScopeQuotaBounds(t *testing.T) {
	s := builtin.MaxScope()
	q := s.Quota
	cap := s.Workforce.MaxDepth * s.Workforce.MaxWidthPerLayer
	if q.MaxConcurrentTasks > cap {
		t.Errorf("Quota.MaxConcurrentTasks = %d > pool capacity %d (CFI-QuotaWithinPoolCapacity)",
			q.MaxConcurrentTasks, cap)
	}
	if q.MaxConcurrentTasks != 32 {
		t.Errorf("Quota.MaxConcurrentTasks = %d, want 32", q.MaxConcurrentTasks)
	}
	if q.MaxDailyBudgetUSD != 200 {
		t.Errorf("Quota.MaxDailyBudgetUSD = %d, want 200", q.MaxDailyBudgetUSD)
	}
}

func TestMaxScopeTmuxIdleTTLLong(t *testing.T) {
	s := builtin.MaxScope()
	if s.Tmux.IdleTTLMin != 1440 {
		t.Errorf("Tmux.IdleTTLMin = %d, want 1440 (24h)", s.Tmux.IdleTTLMin)
	}
	if !s.Tmux.AutoReap {
		t.Errorf("Tmux.AutoReap = false, want true")
	}
}

func TestMaxScopeMissPolicyCatchupBounded(t *testing.T) {
	s := builtin.MaxScope()
	if s.Scheduling.MissPolicy != "catchup-bounded" {
		t.Errorf("Scheduling.MissPolicy = %q, want catchup-bounded",
			s.Scheduling.MissPolicy)
	}
	if s.Scheduling.MissCatchupMaxJobs <= 0 {
		t.Errorf("Scheduling.MissCatchupMaxJobs = %d, want > 0",
			s.Scheduling.MissCatchupMaxJobs)
	}
}

func TestMaxScopeWFQWeights(t *testing.T) {
	s := builtin.MaxScope()
	if s.WFQ.ProjectWeightDefault != 100 {
		t.Errorf("WFQ.ProjectWeightDefault = %d, want 100", s.WFQ.ProjectWeightDefault)
	}
	if s.WFQ.OvercommitPolicy != "degrade" {
		t.Errorf("WFQ.OvercommitPolicy = %q, want degrade", s.WFQ.OvercommitPolicy)
	}
}

func TestMaxScopeAmendmentCooldown(t *testing.T) {
	s := builtin.MaxScope()
	if s.Autonomy.AmendmentCooldownH != 24 {
		t.Errorf("Autonomy.AmendmentCooldownH = %d, want 24",
			s.Autonomy.AmendmentCooldownH)
	}
}

func TestMaxScopeHRAFullCoverage(t *testing.T) {
	s := builtin.MaxScope()
	if !reflect.DeepEqual(s.HRA.LayersEnabled, []int{1, 2, 3}) {
		t.Errorf("HRA.LayersEnabled = %v, want [1 2 3]", s.HRA.LayersEnabled)
	}
	if s.HRA.ReviewerToWorkerRatio != 1 {
		t.Errorf("HRA.ReviewerToWorkerRatio = %d, want 1", s.HRA.ReviewerToWorkerRatio)
	}
}

func TestMaxScopeResearchEnabled(t *testing.T) {
	s := builtin.MaxScope()
	if !s.Research.Enabled {
		t.Errorf("Research.Enabled = false, want true")
	}
	if !s.Research.SOTAOrchestratorEnforced {
		t.Errorf("Research.SOTAOrchestratorEnforced = false, want true")
	}
	if s.Research.MaxBudgetPerSession <= 0 {
		t.Errorf("Research.MaxBudgetPerSession = %d, want > 0",
			s.Research.MaxBudgetPerSession)
	}
}

func TestMaxScopeGatesCoverage(t *testing.T) {
	s := builtin.MaxScope()
	if s.Gates.CoverageMinPct < 90 {
		t.Errorf("Gates.CoverageMinPct = %d, want >= 90", s.Gates.CoverageMinPct)
	}
}
