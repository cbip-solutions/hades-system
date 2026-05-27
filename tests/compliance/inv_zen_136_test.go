package compliance_test

import (
	"errors"
	"testing"

	doctrineerrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

// TestInv136_TightenOnlyEnforced — invariant enforced for every direction
// class. Per-project overrides MUST be tighten-only relative to baseline;
// loosen attempts produce *TightenViolation entries joined under
// ErrTightenViolation so callers see every loosen in one pass.
func TestInv136_TightenOnlyEnforced(t *testing.T) {
	bs := minimalSchema()
	cases := []struct {
		name   string
		mutate func(*v1.Schema)
	}{
		{"decrease-loosen", func(s *v1.Schema) { s.Workforce.MaxDepth = 99 }},
		{"truth-violation", func(s *v1.Schema) { s.Transverse.NoStubs = false }},
		{"add-only-removal", func(s *v1.Schema) {
			s.Gates.TestTiers.Enabled = []string{"unit"}
		}},
		{"rank-loosen", func(s *v1.Schema) { s.Autonomy.Mode = "pure" }},
		{"increase-loosen", func(s *v1.Schema) { s.Workforce.MinDepth = 0 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ov := minimalSchema()
			c.mutate(&ov)
			err := ov.ValidateTighten(&bs)
			if err == nil {
				t.Fatal("expected ErrTightenViolation")
			}
			if !errors.Is(err, doctrineerrors.ErrTightenViolation) {
				t.Errorf("expected ErrTightenViolation; got %v", err)
			}
		})
	}
}

func TestInv136_IdenticalSchemasPass(t *testing.T) {
	bs := minimalSchema()
	ov := minimalSchema()
	if err := ov.ValidateTighten(&bs); err != nil {
		t.Fatalf("identical override must pass; got %v", err)
	}
}

func minimalSchema() v1.Schema {
	return v1.Schema{
		SchemaVersion:   "1.0",
		DoctrineVersion: "1.0.0",
		AutoUpgrade:     "patch",
		Workforce: v1.WorkforceConfig{
			MinDepth: 1, MaxDepth: 8, MaxWidthPerLayer: 10,
			Recovery: v1.WorkforceRecoveryConfig{
				TransientRetryBudget: 3, PermanentInfraEscalate: "operator-notify", DoctrineRetryBudget: 1,
			},
		},
		HRA: v1.HRAConfig{
			LayersEnabled: []int{1, 2, 3}, CadenceTacticalMin: 30, CadenceStrategicMin: 120,
			CadenceArchitecturalMin: 480, ReviewerToWorkerRatio: 3,
		},
		Research:   v1.ResearchConfig{Enabled: true, MaxBudgetPerSession: 10, SOTAOrchestratorEnforced: true},
		Gates:      v1.GatesConfig{TestTiers: v1.TestTiersConfig{Enabled: []string{"unit", "integration", "compliance"}}, CoverageMinPct: 90},
		Review:     v1.ReviewConfig{HiveCadenceMin: 60, RotateReviewerEvery: 5, RequireDualReview: true},
		Transverse: v1.TransverseExpected(),
		Autonomy: v1.AutonomyConfig{
			Mode: "assisted", CheckMode: "strict",
			ConfirmationPolicy: v1.ConfirmationPolicyConfig{
				BudgetBreachThreshold: "high", SpecAmendmentProposal: "high",
				InvariantViolation: "high", ArchitecturalReviewEscalation: "high",
			},
			Voting:             v1.VotingConfig{PluralityThresholdPct: 50, FMVEnable: true, EMSEnable: true},
			CostDegradation:    v1.CostDegradationConfig{SoftCheckUSD: 50, HardStopUSD: 100, DegradeStrategy: "downshift-tier"},
			AmendmentCooldownH: 24,
		},
		Merge: v1.MergeConfig{
			Mode:                "balanced",
			ScoringWeights:      v1.MergeScoringWeights{TestPass: 30, LintPass: 20, Coverage: 20, Diff: 15, Duration: 15},
			AnomalyThresholdPct: 80, AnomalyWindowMin: 60, MaxCandidates: 5,
		},
		Caronte: v1.CaronteConfig{BranchPolicy: "balanced", HRAReviewEnabled: true},
		Notifications: v1.NotificationsConfig{
			SeverityPerDoctrine: v1.SeverityPerDoctrineConfig{
				ActionNeededPromotesToUrgent: false, UrgentBypassesQuietHours: true, InfoImmediateDuringQuiet: "queue",
			},
			QuietHoursStart: "22:00", QuietHoursEnd: "08:00",
		},
		ZenDayCadence: v1.ZenDayCadenceConfig{
			MorningBriefCron: "0 8 * * 1-5", MorningBriefIfWithinHours: 2,
			EODDigestCron: "0 18 * * 1-5", EODDigestIfWithinHours: 2,
		},
		Quota:      v1.QuotaConfig{MaxConcurrentTasks: 8, MaxDailyBudgetUSD: 100, MaxStorageGB: 50},
		Tmux:       v1.TmuxConfig{IdleTTLMin: 30, AutoReap: true},
		Scheduling: v1.SchedulingConfig{MissPolicy: "catchup-bounded", MissCatchupMaxJobs: 5},
		WFQ:        v1.WFQConfig{ProjectWeightDefault: 10, StarvationGuardSec: 600, OvercommitPolicy: "queue"},
		Knowledge:  v1.KnowledgeConfig{HiveDocCadenceHours: 24, ObsidianVaultPath: "/tmp", CrossProjectAggr: true},
		Augmentation: v1.AugmentationConfig{
			Enable: true, MaxKGTokens: 10000, TimeoutMs: 1000,
			OnTimeout: "graceful_truncate", CrossProjectScope: "opt-in",
		},
	}
}
