package v1_test

import (
	"errors"
	"strings"
	"testing"

	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

func goodSchema() v1.Schema {
	return v1.Schema{
		SchemaVersion:   "1.0",
		DoctrineVersion: "1.0.0",
		AutoUpgrade:     "patch",
		Workforce: v1.WorkforceConfig{
			MinDepth:         1,
			MaxDepth:         8,
			MaxWidthPerLayer: 10,
			Recovery: v1.WorkforceRecoveryConfig{
				TransientRetryBudget:   3,
				PermanentInfraEscalate: "operator-notify",
				DoctrineRetryBudget:    1,
			},
		},
		HRA: v1.HRAConfig{
			LayersEnabled:           []int{1, 2, 3},
			CadenceTacticalMin:      30,
			CadenceStrategicMin:     120,
			CadenceArchitecturalMin: 480,
			ReviewerToWorkerRatio:   3,
		},
		Research: v1.ResearchConfig{
			Enabled:                  true,
			MaxBudgetPerSession:      10,
			SOTAOrchestratorEnforced: true,
		},
		Gates: v1.GatesConfig{
			TestTiers: v1.TestTiersConfig{
				Enabled: []string{"unit", "integration", "compliance", "analysistest"},
			},
			CoverageMinPct: 90,
		},
		Review: v1.ReviewConfig{
			HiveCadenceMin:      60,
			RotateReviewerEvery: 5,
			RequireDualReview:   true,
		},
		Transverse: v1.TransverseExpected(),
		Autonomy: v1.AutonomyConfig{
			Mode:      "assisted",
			CheckMode: "strict",
			ConfirmationPolicy: v1.ConfirmationPolicyConfig{
				BudgetBreachThreshold:         "high",
				SpecAmendmentProposal:         "high",
				InvariantViolation:            "high",
				ArchitecturalReviewEscalation: "high",
			},
			Voting: v1.VotingConfig{
				PluralityThresholdPct: 50,
				FMVEnable:             true,
				EMSEnable:             true,
			},
			CostDegradation: v1.CostDegradationConfig{
				SoftCheckUSD:    50,
				HardStopUSD:     100,
				DegradeStrategy: "downshift-tier",
			},
			AmendmentCooldownH: 24,
		},
		Merge: v1.MergeConfig{
			Mode: "balanced",
			ScoringWeights: v1.MergeScoringWeights{
				TestPass: 30, LintPass: 20, Coverage: 20, Diff: 15, Duration: 15,
			},
			AnomalyThresholdPct: 80,
			AnomalyWindowMin:    60,
			MaxCandidates:       5,
		},
		Caronte: v1.CaronteConfig{
			BranchPolicy:     "balanced",
			HRAReviewEnabled: true,
		},
		Notifications: v1.NotificationsConfig{
			SeverityPerDoctrine: v1.SeverityPerDoctrineConfig{
				ActionNeededPromotesToUrgent: false,
				UrgentBypassesQuietHours:     true,
				InfoImmediateDuringQuiet:     "queue",
			},
			QuietHoursStart: "22:00",
			QuietHoursEnd:   "08:00",
		},
		ZenDayCadence: v1.ZenDayCadenceConfig{
			MorningBriefCron:          "0 8 * * 1-5",
			MorningBriefIfWithinHours: 2,
			EODDigestCron:             "0 18 * * 1-5",
			EODDigestIfWithinHours:    2,
		},
		Quota: v1.QuotaConfig{
			MaxConcurrentTasks: 8,
			MaxDailyBudgetUSD:  100,
			MaxStorageGB:       50,
		},
		Tmux: v1.TmuxConfig{
			IdleTTLMin: 30,
			AutoReap:   true,
		},
		Scheduling: v1.SchedulingConfig{
			MissPolicy:         "catchup-bounded",
			MissCatchupMaxJobs: 5,
		},
		WFQ: v1.WFQConfig{
			ProjectWeightDefault: 10,
			StarvationGuardSec:   600,
			OvercommitPolicy:     "queue",
		},
		Knowledge: v1.KnowledgeConfig{
			HiveDocCadenceHours: 24,
			ObsidianVaultPath:   "/path/to/home/Obsidian/zen-swarm",
			CrossProjectAggr:    true,
		},
		Augmentation: v1.AugmentationConfig{
			Enable:            true,
			MaxKGTokens:       10000,
			TimeoutMs:         1000,
			OnTimeout:         "graceful_truncate",
			CrossProjectScope: "opt-in",
		},
	}
}

func TestValidate_Good(t *testing.T) {
	s := goodSchema()
	if err := s.Validate(); err != nil {
		t.Fatalf("good schema must validate; got %v", err)
	}
}

func TestValidate_RequiredFieldMissing(t *testing.T) {
	s := goodSchema()
	s.SchemaVersion = ""
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error when SchemaVersion empty")
	}
	if !errors.Is(err, v1.ErrValidationFailed) {
		t.Errorf("expected ErrValidationFailed wrap; got %v", err)
	}
	if !strings.Contains(err.Error(), "SchemaVersion") {
		t.Errorf("error message must cite SchemaVersion; got %q", err.Error())
	}
}

func TestValidate_RangeViolation_MaxDepthLessThanMin(t *testing.T) {
	s := goodSchema()
	s.Workforce.MinDepth = 5
	s.Workforce.MaxDepth = 3
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error when MaxDepth < MinDepth")
	}
	if !errors.Is(err, v1.ErrValidationFailed) {
		t.Errorf("expected ErrValidationFailed; got %v", err)
	}
}

func TestValidate_EnumViolation_AutonomyMode(t *testing.T) {
	s := goodSchema()
	s.Autonomy.Mode = "yolo"
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for invalid Autonomy.Mode")
	}
	if !errors.Is(err, v1.ErrValidationFailed) {
		t.Errorf("expected ErrValidationFailed; got %v", err)
	}
	if !strings.Contains(err.Error(), "yolo") {
		t.Errorf("error must cite the bad value; got %q", err.Error())
	}
}

func TestValidate_EnumViolation_AutoUpgrade(t *testing.T) {
	s := goodSchema()
	s.AutoUpgrade = "alpha"
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for invalid AutoUpgrade")
	}
}

func TestValidate_TransverseMutated_Rejected(t *testing.T) {
	s := goodSchema()
	s.Transverse.NoStubs = false
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error when transverse mutated")
	}
	if !strings.Contains(err.Error(), "transverse") {
		t.Errorf("error must cite transverse; got %q", err.Error())
	}
	if !errors.Is(err, v1.ErrValidationFailed) {
		t.Errorf("expected ErrValidationFailed; got %v", err)
	}
}

func TestValidate_MultipleViolations_AllReported(t *testing.T) {
	s := goodSchema()
	s.SchemaVersion = ""
	s.Autonomy.Mode = "yolo"
	s.Workforce.MaxDepth = -1
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{"SchemaVersion", "yolo", "MaxDepth"} {
		if !strings.Contains(msg, want) {
			t.Errorf("multi-violation message missing %q; got %q", want, msg)
		}
	}
}

// TestValidate_GarbageDoctrineVersion_Rejected — Phase H amendment Apply
// path can construct a candidate Schema in-memory bypassing the parser, so
// Validate() MUST itself enforce ValidateDoctrineVersion (reviewer
// IMPORTANT #1). A non-semver value must surface as ErrValidationFailed.
func TestValidate_GarbageDoctrineVersion_Rejected(t *testing.T) {
	s := goodSchema()
	s.DoctrineVersion = "abc-not-semver"
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error on non-semver DoctrineVersion")
	}
	if !errors.Is(err, v1.ErrValidationFailed) {
		t.Errorf("expected ErrValidationFailed; got %v", err)
	}
	if !strings.Contains(err.Error(), "abc-not-semver") {
		t.Errorf("error must cite the bad value; got %q", err.Error())
	}
}

func TestValidate_GarbageSchemaVersion_Rejected(t *testing.T) {
	s := goodSchema()
	s.SchemaVersion = "not.a.version"
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error on garbage SchemaVersion")
	}
	if !errors.Is(err, v1.ErrValidationFailed) {
		t.Errorf("expected ErrValidationFailed; got %v", err)
	}
}

func TestValidate_AlphaSuffix_Rejected(t *testing.T) {
	s := goodSchema()
	s.DoctrineVersion = "1.0.0-alpha"
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error on pre-release suffix")
	}
	if !errors.Is(err, v1.ErrValidationFailed) {
		t.Errorf("expected ErrValidationFailed; got %v", err)
	}
}

// TestValidate_SuccessSetsValidatedTrue — per spec line 872 (inv-zen-140
// applierMustValidateTighten): Validate() MUST set Schema.Validated = true
// on success. Phase L analyzer reads this flag to enforce that the
// amendment Apply path called Validate before ValidateTighten.
func TestValidate_SuccessSetsValidatedTrue(t *testing.T) {
	s := goodSchema()
	if s.Validated {
		t.Fatal("goodSchema baseline must start with Validated=false")
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("good schema must validate; got %v", err)
	}
	if !s.Validated {
		t.Error("Validate() success must set Validated=true")
	}
}

func TestValidate_FailureLeavesValidatedFalse(t *testing.T) {
	s := goodSchema()
	s.Workforce.MaxDepth = -1
	if err := s.Validate(); err == nil {
		t.Fatal("expected error")
	}
	if s.Validated {
		t.Error("Validate() failure must leave Validated=false")
	}
}

// TestValidate_FailureAfterPriorSuccess_ResetsValidated — guards the
// state-machine drift surfaced by Plan 8 Phase A code re-review: a Schema
// that passes Validate() (Validated=true), is then mutated invalid, and
// re-validated MUST have Validated reset to false. Without an explicit
// reset on the failure path the field would lie to the Phase H amendment
// Apply path's runtime precondition check.
func TestValidate_FailureAfterPriorSuccess_ResetsValidated(t *testing.T) {
	s := goodSchema()
	if err := s.Validate(); err != nil {
		t.Fatalf("first Validate must pass on goodSchema: %v", err)
	}
	if !s.Validated {
		t.Fatal("expected Validated=true after first successful Validate")
	}

	s.Workforce.MaxDepth = -1
	if err := s.Validate(); err == nil {
		t.Fatal("second Validate must fail after invalidating mutation")
	}
	if s.Validated {
		t.Error("Validate() failure must reset Validated=false even after prior success " +
			"(stale-true would mislead Phase H amendment Apply path runtime check)")
	}
}
