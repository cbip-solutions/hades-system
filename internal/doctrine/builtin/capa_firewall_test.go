package builtin_test

import (
	"reflect"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctrine/builtin"
)

// TestCapaFirewallAutoUpgradeNoneHardGuard verifies invariant
// generalized: capa-firewall MUST have auto_upgrade="none" (cannot
// auto-upgrade ever).
func TestCapaFirewallAutoUpgradeNoneHardGuard(t *testing.T) {
	s := builtin.CapaFirewall()
	if s == nil {
		t.Fatal("CapaFirewall() returned nil")
	}
	if s.AutoUpgrade != "none" {
		t.Errorf("AutoUpgrade = %q, want %q (inv-zen-100 hard guard)",
			s.AutoUpgrade, "none")
	}
}

// TestCapaFirewallAutonomyAssistedMandatory covers Q11 C hard guard:
// capa-firewall.mode MUST be assisted (operator gates ALL substantive
// decisions); CheckMode MUST be strict.
func TestCapaFirewallAutonomyAssistedMandatory(t *testing.T) {
	s := builtin.CapaFirewall()
	if s.Autonomy.Mode != "assisted" {
		t.Errorf("Autonomy.Mode = %q, want assisted (Plan 5 Q11 C hard guard)",
			s.Autonomy.Mode)
	}
	if s.Autonomy.CheckMode != "strict" {
		t.Errorf("Autonomy.CheckMode = %q, want strict", s.Autonomy.CheckMode)
	}
}

func TestCapaFirewallTransverseAxiomsAllTrue(t *testing.T) {
	s := builtin.CapaFirewall()
	if !s.Transverse.NoTechDebt || !s.Transverse.NoStubs ||
		!s.Transverse.BuildFinalProduct || !s.Transverse.NoDefer {
		t.Errorf("transverse axioms must all be true")
	}
}

func TestCapaFirewallR1ConfirmationPolicyTight(t *testing.T) {
	s := builtin.CapaFirewall()
	cp := s.Autonomy.ConfirmationPolicy
	wants := []struct {
		name, value string
	}{
		{"BudgetBreachThreshold", "high"},
		{"SpecAmendmentProposal", "high"},
		{"InvariantViolation", "high"},
		{"ArchitecturalReviewEscalation", "low"},
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

func TestCapaFirewallR2VotingFullConsensus(t *testing.T) {
	s := builtin.CapaFirewall()
	v := s.Autonomy.Voting
	if v.PluralityThresholdPct != 100 {
		t.Errorf("Voting.PluralityThresholdPct = %d, want 100", v.PluralityThresholdPct)
	}
	if !v.FMVEnable {
		t.Errorf("Voting.FMVEnable = false, want true")
	}
	if v.EMSEnable {
		t.Errorf("Voting.EMSEnable = true, want false (never early-stop)")
	}
}

func TestCapaFirewallR3TestTiersStrict(t *testing.T) {
	s := builtin.CapaFirewall()
	want := []string{"unit", "integration", "compliance", "adversarial", "chaos", "analysistest"}
	got := s.Gates.TestTiers.Enabled
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Gates.TestTiers.Enabled = %v, want %v", got, want)
	}
}

func TestCapaFirewallR4SeverityActionNeededPromotedToUrgent(t *testing.T) {
	s := builtin.CapaFirewall()
	sev := s.Notifications.SeverityPerDoctrine
	if !sev.ActionNeededPromotesToUrgent {
		t.Errorf("ActionNeededPromotesToUrgent = false, want true (Pulido)")
	}
	if !sev.UrgentBypassesQuietHours {
		t.Errorf("UrgentBypassesQuietHours = false, want true")
	}
	if sev.InfoImmediateDuringQuiet != "deliver" {
		t.Errorf("InfoImmediateDuringQuiet = %q, want deliver",
			sev.InfoImmediateDuringQuiet)
	}
}

func TestCapaFirewallR5ZenDayCadenceDailyTight(t *testing.T) {
	s := builtin.CapaFirewall()
	cad := s.ZenDayCadence
	if cad.MorningBriefCron != "0 9 * * 1-7" {
		t.Errorf("MorningBriefCron = %q, want %q", cad.MorningBriefCron, "0 9 * * 1-7")
	}
	if cad.MorningBriefIfWithinHours != 1 {
		t.Errorf("MorningBriefIfWithinHours = %d, want 1", cad.MorningBriefIfWithinHours)
	}
	if cad.EODDigestCron != "0 17 * * 1-7" {
		t.Errorf("EODDigestCron = %q, want %q", cad.EODDigestCron, "0 17 * * 1-7")
	}
	if cad.EODDigestIfWithinHours != 1 {
		t.Errorf("EODDigestIfWithinHours = %d, want 1", cad.EODDigestIfWithinHours)
	}
}

func TestCapaFirewallNoRetries(t *testing.T) {
	s := builtin.CapaFirewall()
	r := s.Workforce.Recovery
	if r.TransientRetryBudget != 0 {
		t.Errorf("Workforce.Recovery.TransientRetryBudget = %d, want 0 (Pulido strict)",
			r.TransientRetryBudget)
	}
	if r.PermanentInfraEscalate != "abort" {
		t.Errorf("Workforce.Recovery.PermanentInfraEscalate = %q, want abort",
			r.PermanentInfraEscalate)
	}
	if r.DoctrineRetryBudget != 0 {
		t.Errorf("Workforce.Recovery.DoctrineRetryBudget = %d, want 0",
			r.DoctrineRetryBudget)
	}
}

func TestCapaFirewallQuotaTight(t *testing.T) {
	s := builtin.CapaFirewall()
	q := s.Quota
	cap := s.Workforce.MaxDepth * s.Workforce.MaxWidthPerLayer
	if q.MaxConcurrentTasks > cap {
		t.Errorf("Quota.MaxConcurrentTasks = %d > pool capacity %d (CFI-QuotaWithinPoolCapacity)",
			q.MaxConcurrentTasks, cap)
	}
	if q.MaxConcurrentTasks != 4 {
		t.Errorf("Quota.MaxConcurrentTasks = %d, want 4", q.MaxConcurrentTasks)
	}
	if q.MaxDailyBudgetUSD != 20 {
		t.Errorf("Quota.MaxDailyBudgetUSD = %d, want 20 (tight)", q.MaxDailyBudgetUSD)
	}
}

func TestCapaFirewallTmuxIdleTTLShort(t *testing.T) {
	s := builtin.CapaFirewall()
	if s.Tmux.IdleTTLMin != 240 {
		t.Errorf("Tmux.IdleTTLMin = %d, want 240 (4h)", s.Tmux.IdleTTLMin)
	}
}

func TestCapaFirewallMissPolicySkip(t *testing.T) {
	s := builtin.CapaFirewall()
	if s.Scheduling.MissPolicy != "skip" {
		t.Errorf("Scheduling.MissPolicy = %q, want skip", s.Scheduling.MissPolicy)
	}
	if s.Scheduling.MissCatchupMaxJobs != 0 {
		t.Errorf("Scheduling.MissCatchupMaxJobs = %d, want 0", s.Scheduling.MissCatchupMaxJobs)
	}
}

func TestCapaFirewallWFQReject(t *testing.T) {
	s := builtin.CapaFirewall()
	if s.WFQ.ProjectWeightDefault != 30 {
		t.Errorf("WFQ.ProjectWeightDefault = %d, want 30", s.WFQ.ProjectWeightDefault)
	}
	if s.WFQ.OvercommitPolicy != "reject" {
		t.Errorf("WFQ.OvercommitPolicy = %q, want reject", s.WFQ.OvercommitPolicy)
	}
	if s.Quota.MaxConcurrentTasks > 256 {
		t.Errorf("CFI-RejectOvercommitImpliesBoundedQuota: quota=%d > 256",
			s.Quota.MaxConcurrentTasks)
	}
}

func TestCapaFirewallAmendmentCooldown168h(t *testing.T) {
	s := builtin.CapaFirewall()
	if s.Autonomy.AmendmentCooldownH != 168 {
		t.Errorf("Autonomy.AmendmentCooldownH = %d, want 168 (1 week)",
			s.Autonomy.AmendmentCooldownH)
	}
}

func TestCapaFirewallMergeStrictMode(t *testing.T) {
	s := builtin.CapaFirewall()
	if s.Merge.Mode != "strict" {
		t.Errorf("Merge.Mode = %q, want strict", s.Merge.Mode)
	}
	if s.Merge.MaxCandidates > 2 {
		t.Errorf("Merge.MaxCandidates = %d, want <= 2 (capa-firewall preference)",
			s.Merge.MaxCandidates)
	}
}

func TestCapaFirewallMergeScoringSum100(t *testing.T) {
	s := builtin.CapaFirewall()
	w := s.Merge.ScoringWeights
	sum := w.TestPass + w.LintPass + w.Coverage + w.Diff + w.Duration
	if sum != 100 {
		t.Errorf("Merge.ScoringWeights sum = %d, want 100", sum)
	}
}

func TestCapaFirewallCostDegradationAbort(t *testing.T) {
	s := builtin.CapaFirewall()
	cd := s.Autonomy.CostDegradation
	if cd.DegradeStrategy != "abort" {
		t.Errorf("Autonomy.CostDegradation.DegradeStrategy = %q, want abort",
			cd.DegradeStrategy)
	}
}

func TestCapaFirewallCaronteStrict(t *testing.T) {
	s := builtin.CapaFirewall()
	if s.Caronte.BranchPolicy != "strict" {
		t.Errorf("Caronte.BranchPolicy = %q, want strict", s.Caronte.BranchPolicy)
	}
}

func TestCapaFirewallReviewMandatory(t *testing.T) {
	s := builtin.CapaFirewall()
	if !s.Review.RequireDualReview {
		t.Errorf("Review.RequireDualReview = false, want true (capa-firewall mandatory)")
	}
	if s.Review.RotateReviewerEvery != 1 {
		t.Errorf("Review.RotateReviewerEvery = %d, want 1", s.Review.RotateReviewerEvery)
	}
}

func TestCapaFirewallHRAFullLayers(t *testing.T) {
	s := builtin.CapaFirewall()
	if !reflect.DeepEqual(s.HRA.LayersEnabled, []int{1, 2, 3}) {
		t.Errorf("HRA.LayersEnabled = %v, want [1 2 3]", s.HRA.LayersEnabled)
	}
	if s.HRA.ReviewerToWorkerRatio != 1 {
		t.Errorf("HRA.ReviewerToWorkerRatio = %d, want 1", s.HRA.ReviewerToWorkerRatio)
	}
}

func TestCapaFirewallWorkforceNarrowest(t *testing.T) {
	c := builtin.CapaFirewall()
	d := builtin.Default()
	m := builtin.MaxScope()
	cCap := c.Workforce.MaxDepth * c.Workforce.MaxWidthPerLayer
	dCap := d.Workforce.MaxDepth * d.Workforce.MaxWidthPerLayer
	mCap := m.Workforce.MaxDepth * m.Workforce.MaxWidthPerLayer
	if cCap >= dCap {
		t.Errorf("capa-firewall capacity %d >= default %d; capa-firewall must be narrowest",
			cCap, dCap)
	}
	if cCap >= mCap {
		t.Errorf("capa-firewall capacity %d >= max-scope %d", cCap, mCap)
	}
}
