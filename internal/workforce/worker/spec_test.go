package worker_test

import (
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/workforce/worker"
)

func TestVariantStringer(t *testing.T) {
	cases := map[worker.Variant]string{
		worker.VariantWorker:     "worker",
		worker.VariantTeamLead:   "teamlead",
		worker.VariantReviewerL2: "reviewer-l2",
		worker.VariantReviewerL3: "reviewer-l3",
		worker.VariantReviewerL4: "reviewer-l4",
	}
	for got, want := range cases {
		if got.String() != want {
			t.Errorf("Variant(%d).String() = %q, want %q", got, got.String(), want)
		}
	}
}

func TestVariantStringerUnknown(t *testing.T) {
	v := worker.Variant(99)
	if got := v.String(); !strings.Contains(got, "unknown_variant") {
		t.Errorf("Variant(99).String() = %q, want substring 'unknown_variant'", got)
	}
}

func TestVariantParse(t *testing.T) {
	cases := map[string]worker.Variant{
		"worker":      worker.VariantWorker,
		"teamlead":    worker.VariantTeamLead,
		"reviewer-l2": worker.VariantReviewerL2,
		"reviewer-l3": worker.VariantReviewerL3,
		"reviewer-l4": worker.VariantReviewerL4,
	}
	for s, want := range cases {
		got, err := worker.ParseVariant(s)
		if err != nil {
			t.Fatalf("ParseVariant(%q): %v", s, err)
		}
		if got != want {
			t.Errorf("ParseVariant(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestVariantParseRejectsUnknown(t *testing.T) {
	if _, err := worker.ParseVariant("UNKNOWN"); err == nil {
		t.Fatal("expected error for unknown variant")
	}
	if _, err := worker.ParseVariant(""); err == nil {
		t.Fatal("expected error for empty variant")
	}
}

func TestVariantPersistent(t *testing.T) {
	cases := map[worker.Variant]bool{
		worker.VariantWorker:     false,
		worker.VariantTeamLead:   true,
		worker.VariantReviewerL2: false,
		worker.VariantReviewerL3: true,
		worker.VariantReviewerL4: true,
	}
	for v, want := range cases {
		if got := v.Persistent(); got != want {
			t.Errorf("Variant(%v).Persistent() = %v, want %v", v, got, want)
		}
	}
}

func TestVariantPersistentUnknown(t *testing.T) {
	v := worker.Variant(99)
	if v.Persistent() {
		t.Error("Variant(99).Persistent() = true; expected false (conservative default)")
	}
}

func TestTaskTierStringer(t *testing.T) {
	cases := map[worker.TaskTier]string{
		worker.TierTrivial: "trivial",
		worker.TierSimple:  "simple",
		worker.TierMedium:  "medium",
		worker.TierComplex: "complex",
	}
	for got, want := range cases {
		if got.String() != want {
			t.Errorf("TaskTier(%d).String() = %q, want %q", got, got.String(), want)
		}
	}
}

func TestTaskTierStringerUnknown(t *testing.T) {
	tt := worker.TaskTier(99)
	if got := tt.String(); !strings.Contains(got, "unknown_task_tier") {
		t.Errorf("TaskTier(99).String() = %q, want substring 'unknown_task_tier'", got)
	}
}

func TestTaskTierParse(t *testing.T) {
	cases := map[string]worker.TaskTier{
		"trivial": worker.TierTrivial,
		"simple":  worker.TierSimple,
		"medium":  worker.TierMedium,
		"complex": worker.TierComplex,
	}
	for s, want := range cases {
		got, err := worker.ParseTaskTier(s)
		if err != nil {
			t.Fatalf("ParseTaskTier(%q): %v", s, err)
		}
		if got != want {
			t.Errorf("ParseTaskTier(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestTaskTierParseRejectsUnknown(t *testing.T) {
	if _, err := worker.ParseTaskTier("ZILLION"); err == nil {
		t.Fatal("expected error for unknown tier")
	}
	if _, err := worker.ParseTaskTier(""); err == nil {
		t.Fatal("expected error for empty tier")
	}
}

func TestRecoveryPolicyStringer(t *testing.T) {
	cases := map[worker.RecoveryPolicy]string{
		worker.RecoveryAutoRespawn:   "auto-respawn",
		worker.RecoveryManual:        "manual",
		worker.RecoveryDoctrineBound: "doctrine-bound",
	}
	for got, want := range cases {
		if got.String() != want {
			t.Errorf("RecoveryPolicy(%d).String() = %q, want %q", got, got.String(), want)
		}
	}
}

func TestRecoveryPolicyStringerUnknown(t *testing.T) {
	rp := worker.RecoveryPolicy(99)
	if got := rp.String(); !strings.Contains(got, "unknown_recovery_policy") {
		t.Errorf("RecoveryPolicy(99).String() = %q, want substring 'unknown_recovery_policy'", got)
	}
}

func TestRecoveryPolicyParse(t *testing.T) {
	cases := map[string]worker.RecoveryPolicy{
		"auto-respawn":   worker.RecoveryAutoRespawn,
		"manual":         worker.RecoveryManual,
		"doctrine-bound": worker.RecoveryDoctrineBound,
	}
	for s, want := range cases {
		got, err := worker.ParseRecoveryPolicy(s)
		if err != nil {
			t.Fatalf("ParseRecoveryPolicy(%q): %v", s, err)
		}
		if got != want {
			t.Errorf("ParseRecoveryPolicy(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestRecoveryPolicyParseRejectsUnknown(t *testing.T) {
	if _, err := worker.ParseRecoveryPolicy("MAGIC"); err == nil {
		t.Fatal("expected error for unknown recovery policy")
	}
	if _, err := worker.ParseRecoveryPolicy(""); err == nil {
		t.Fatal("expected error for empty recovery policy")
	}
}

func TestQuotaZeroValueRejected(t *testing.T) {
	q := worker.Quota{}
	if err := q.Validate(); err == nil {
		t.Fatal("expected zero-value Quota to fail Validate()")
	}
}

func TestQuotaPositiveValuesAccepted(t *testing.T) {
	q := worker.Quota{
		MaxTokens:   50000,
		MaxCostUSD:  1.50,
		MaxDuration: 10 * time.Minute,
	}
	if err := q.Validate(); err != nil {
		t.Errorf("Validate(positive): %v", err)
	}
}

func TestQuotaNegativeRejected(t *testing.T) {
	cases := []worker.Quota{
		{MaxTokens: -1, MaxCostUSD: 1.0, MaxDuration: time.Minute},
		{MaxTokens: 100, MaxCostUSD: -0.01, MaxDuration: time.Minute},
		{MaxTokens: 100, MaxCostUSD: 1.0, MaxDuration: -time.Minute},
	}
	for i, q := range cases {
		if err := q.Validate(); err == nil {
			t.Errorf("Validate(case %d, %+v): expected error", i, q)
		}
	}
}

func TestQuotaZeroFields(t *testing.T) {
	cases := []worker.Quota{
		{MaxTokens: 0, MaxCostUSD: 1.0, MaxDuration: time.Minute},
		{MaxTokens: 100, MaxCostUSD: 0, MaxDuration: time.Minute},
		{MaxTokens: 100, MaxCostUSD: 1.0, MaxDuration: 0},
	}
	for i, q := range cases {
		if err := q.Validate(); err == nil {
			t.Errorf("Validate(zero-field case %d, %+v): expected error", i, q)
		}
	}
}

func TestWorkerSpecValidate(t *testing.T) {
	good := worker.WorkerSpec{
		ID:             "spec-medium-worker-1",
		Variant:        worker.VariantWorker,
		TaskTier:       worker.TierMedium,
		ModelClass:     "tier-medium",
		Tools:          []string{"research_dispatch", "ssh_exec"},
		Quota:          worker.Quota{MaxTokens: 50000, MaxCostUSD: 1.0, MaxDuration: 5 * time.Minute},
		RecoveryPolicy: worker.RecoveryAutoRespawn,
		DoctrineName:   "max-scope",
		ProjectID:      "internal-platform-x",
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("Validate(good): %v", err)
	}
}

func TestWorkerSpecValidateRejectsEmptyID(t *testing.T) {
	bad := worker.WorkerSpec{
		ID:             "",
		Variant:        worker.VariantWorker,
		TaskTier:       worker.TierMedium,
		ModelClass:     "tier-medium",
		Tools:          []string{"research_dispatch"},
		Quota:          worker.Quota{MaxTokens: 100, MaxCostUSD: 1.0, MaxDuration: time.Minute},
		RecoveryPolicy: worker.RecoveryAutoRespawn,
		DoctrineName:   "default",
		ProjectID:      "internal-platform-x",
	}
	err := bad.Validate()
	if err == nil {
		t.Fatal("expected error for empty ID")
	}
	if !strings.Contains(err.Error(), "ID") {
		t.Errorf("err = %v, want substring 'ID'", err)
	}
}

func TestWorkerSpecValidateRejectsEmptyModelClass(t *testing.T) {
	bad := worker.WorkerSpec{
		ID:             "x",
		Variant:        worker.VariantWorker,
		TaskTier:       worker.TierMedium,
		ModelClass:     "",
		Tools:          []string{"x"},
		Quota:          worker.Quota{MaxTokens: 100, MaxCostUSD: 1.0, MaxDuration: time.Minute},
		RecoveryPolicy: worker.RecoveryAutoRespawn,
		DoctrineName:   "max-scope",
		ProjectID:      "internal-platform-x",
	}
	if err := bad.Validate(); err == nil {
		t.Fatal("expected error for empty ModelClass")
	}
}

func TestWorkerSpecValidateRejectsEmptyDoctrine(t *testing.T) {
	bad := worker.WorkerSpec{
		ID:             "x",
		Variant:        worker.VariantWorker,
		TaskTier:       worker.TierMedium,
		ModelClass:     "tier-medium",
		Tools:          []string{"x"},
		Quota:          worker.Quota{MaxTokens: 100, MaxCostUSD: 1.0, MaxDuration: time.Minute},
		RecoveryPolicy: worker.RecoveryAutoRespawn,
		DoctrineName:   "",
		ProjectID:      "internal-platform-x",
	}
	if err := bad.Validate(); err == nil {
		t.Fatal("expected error for empty DoctrineName")
	}
}

func TestWorkerSpecValidateRejectsEmptyProjectID(t *testing.T) {
	bad := worker.WorkerSpec{
		ID:             "x",
		Variant:        worker.VariantWorker,
		TaskTier:       worker.TierMedium,
		ModelClass:     "tier-medium",
		Tools:          []string{"x"},
		Quota:          worker.Quota{MaxTokens: 100, MaxCostUSD: 1.0, MaxDuration: time.Minute},
		RecoveryPolicy: worker.RecoveryAutoRespawn,
		DoctrineName:   "max-scope",
		ProjectID:      "",
	}
	if err := bad.Validate(); err == nil {
		t.Fatal("expected error for empty ProjectID")
	}
}

func TestWorkerSpecValidateRejectsEmptyTools(t *testing.T) {
	bad := worker.WorkerSpec{
		ID:             "x",
		Variant:        worker.VariantWorker,
		TaskTier:       worker.TierMedium,
		ModelClass:     "tier-medium",
		Tools:          nil,
		Quota:          worker.Quota{MaxTokens: 100, MaxCostUSD: 1.0, MaxDuration: time.Minute},
		RecoveryPolicy: worker.RecoveryAutoRespawn,
		DoctrineName:   "max-scope",
		ProjectID:      "internal-platform-x",
	}
	if err := bad.Validate(); err == nil {
		t.Fatal("expected error for nil Tools")
	}
}

func TestWorkerSpecValidatePropagatesQuotaError(t *testing.T) {
	bad := worker.WorkerSpec{
		ID:             "x",
		Variant:        worker.VariantWorker,
		TaskTier:       worker.TierMedium,
		ModelClass:     "tier-medium",
		Tools:          []string{"x"},
		Quota:          worker.Quota{},
		RecoveryPolicy: worker.RecoveryAutoRespawn,
		DoctrineName:   "max-scope",
		ProjectID:      "internal-platform-x",
	}
	if err := bad.Validate(); err == nil {
		t.Fatal("expected error for zero Quota")
	}
}

func TestNewSpecConstructor(t *testing.T) {
	spec, err := worker.NewSpec(worker.SpecOptions{
		ID:             "spec-1",
		Variant:        worker.VariantWorker,
		TaskTier:       worker.TierMedium,
		ModelClass:     "tier-medium",
		Tools:          []string{"research_dispatch"},
		Quota:          worker.Quota{MaxTokens: 100, MaxCostUSD: 1.0, MaxDuration: time.Minute},
		RecoveryPolicy: worker.RecoveryAutoRespawn,
		DoctrineName:   "max-scope",
		ProjectID:      "internal-platform-x",
	})
	if err != nil {
		t.Fatalf("NewSpec: %v", err)
	}
	if spec.ID != "spec-1" {
		t.Errorf("ID = %q", spec.ID)
	}
}

func TestNewSpecToolsDefensiveCopy(t *testing.T) {
	originalTools := []string{"research_dispatch"}
	spec, err := worker.NewSpec(worker.SpecOptions{
		ID:             "spec-defensive",
		Variant:        worker.VariantWorker,
		TaskTier:       worker.TierMedium,
		ModelClass:     "tier-medium",
		Tools:          originalTools,
		Quota:          worker.Quota{MaxTokens: 100, MaxCostUSD: 1.0, MaxDuration: time.Minute},
		RecoveryPolicy: worker.RecoveryAutoRespawn,
		DoctrineName:   "max-scope",
		ProjectID:      "internal-platform-x",
	})
	if err != nil {
		t.Fatalf("NewSpec: %v", err)
	}

	originalTools[0] = "CALLER-MUTATED"
	if spec.Tools[0] == "CALLER-MUTATED" {
		t.Error("input Tools slice not defensively copied; caller mutation leaked into spec")
	}
}

func TestNewSpecRejectsInvalid(t *testing.T) {
	_, err := worker.NewSpec(worker.SpecOptions{
		ID: "",
	})
	if err == nil {
		t.Fatal("expected NewSpec to reject zero-value SpecOptions")
	}
}

func TestNewSpecNilToolsHandled(t *testing.T) {
	_, err := worker.NewSpec(worker.SpecOptions{
		ID:             "x",
		Variant:        worker.VariantWorker,
		TaskTier:       worker.TierMedium,
		ModelClass:     "tier-medium",
		Tools:          nil,
		Quota:          worker.Quota{MaxTokens: 100, MaxCostUSD: 1.0, MaxDuration: time.Minute},
		RecoveryPolicy: worker.RecoveryAutoRespawn,
		DoctrineName:   "max-scope",
		ProjectID:      "internal-platform-x",
	})
	if err == nil {
		t.Fatal("expected NewSpec to reject nil Tools")
	}
}
