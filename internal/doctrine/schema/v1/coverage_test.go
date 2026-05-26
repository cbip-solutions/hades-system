package v1_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	doctrineerrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

func TestErrorIsMethods(t *testing.T) {
	cases := []struct {
		name string
		err  error
		hit  error
		miss error
	}{
		{"RequiredFieldMissing", &v1.RequiredFieldMissing{Field: "X"}, v1.ErrValidationFailed, v1.ErrTightenViolation},
		{"RangeViolation", &v1.RangeViolation{Field: "X", Got: 0, MinAllow: 1}, v1.ErrValidationFailed, v1.ErrTightenViolation},
		{"EnumViolation", &v1.EnumViolation{Field: "X", Got: "y", Allowed: []string{"a"}}, v1.ErrValidationFailed, v1.ErrTightenViolation},
		{"TransverseMutationViolation", &v1.TransverseMutationViolation{}, v1.ErrValidationFailed, v1.ErrTightenViolation},
		{"CrossFieldViolation", &v1.CrossFieldViolation{InvariantID: "X", Detail: "y"}, v1.ErrValidationFailed, v1.ErrTightenViolation},
		{"TightenViolation", &v1.TightenViolation{RulePath: "X"}, v1.ErrTightenViolation, v1.ErrValidationFailed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !errors.Is(c.err, c.hit) {
				t.Errorf("expected errors.Is(%T, %v) = true", c.err, c.hit)
			}
			if errors.Is(c.err, c.miss) {
				t.Errorf("expected errors.Is(%T, %v) = false", c.err, c.miss)
			}

			if msg := c.err.Error(); msg == "" {
				t.Errorf("Error() empty for %T", c.err)
			}
		})
	}
}

func TestRequiresOperatorConfirmation_Error(t *testing.T) {
	e := &v1.RequiresOperatorConfirmation{RulePath: "Foo.Bar", Reason: "operator-test"}
	got := e.Error()
	for _, want := range []string{"Foo.Bar", "operator-test", "operator confirmation"} {
		if !strings.Contains(got, want) {
			t.Errorf("Error() = %q; want substring %q", got, want)
		}
	}
}

func TestTightenDirectionString(t *testing.T) {
	cases := []struct {
		dir  v1.TightenDirection
		want string
	}{
		{v1.TightenDirSkip, "skip"},
		{v1.TightenDirDecrease, "decrease"},
		{v1.TightenDirIncrease, "increase"},
		{v1.TightenDirTruth, "truth"},
		{v1.TightenDirAddOnly, "add-only"},
		{v1.TightenDirBidirectional, "bidirectional"},
		{v1.TightenDirRank, "rank"},
		{v1.TightenDirection(999), "unknown(999)"},
	}
	for _, c := range cases {
		if got := c.dir.String(); got != c.want {
			t.Errorf("TightenDirection(%d).String() = %q; want %q", c.dir, got, c.want)
		}
	}
}

func TestTransverseSourceString(t *testing.T) {
	cases := []struct {
		src  v1.TransverseSource
		want string
	}{
		{v1.SourceEmbed, "embed"},
		{v1.SourceUserBaseline, "user-baseline"},
		{v1.SourceUserOverride, "user-override"},
		{v1.TransverseSource(999), "unknown(999)"},
	}
	for _, c := range cases {
		if got := c.src.String(); got != c.want {
			t.Errorf("TransverseSource(%d).String() = %q; want %q", c.src, got, c.want)
		}
	}
}

func TestTransverseFields(t *testing.T) {
	got := v1.TransverseFields()
	want := map[string]bool{
		"no_tech_debt":        true,
		"no_stubs":            true,
		"build_final_product": true,
		"no_defer":            true,
	}
	if len(got) != 4 {
		t.Fatalf("TransverseFields() len = %d; want 4", len(got))
	}
	for _, f := range got {
		if !want[f] {
			t.Errorf("unexpected field %q in TransverseFields()", f)
		}
	}

	got[0] = "MUTATED"
	got2 := v1.TransverseFields()
	if got2[0] == "MUTATED" {
		t.Error("TransverseFields() leaked internal slice (mutation visible across calls)")
	}
}

func TestGetRevertCooldownHours_NilSchema(t *testing.T) {
	if got := v1.GetRevertCooldownHours(nil, "Workforce.MaxDepth"); got != 0 {
		t.Errorf("GetRevertCooldownHours(nil) = %d; want 0", got)
	}
}

func TestGetRevertCooldownHours_UnknownPath(t *testing.T) {
	s := goodSchema()
	if got := v1.GetRevertCooldownHours(&s, "Nonexistent.Rule"); got != 0 {
		t.Errorf("GetRevertCooldownHours(unknown) = %d; want 0", got)
	}
}

func TestValidate_NilSchema(t *testing.T) {
	var s *v1.Schema
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error on nil receiver")
	}
	if !errors.Is(err, v1.ErrValidationFailed) {
		t.Errorf("expected ErrValidationFailed; got %v", err)
	}
}

func TestValidateTighten_NilArgs(t *testing.T) {
	s := goodSchema()
	if err := s.ValidateTighten(nil); err == nil {
		t.Error("expected error when baseline nil")
	}
	var nilOverride *v1.Schema
	if err := nilOverride.ValidateTighten(&s); err == nil {
		t.Error("expected error when override (receiver) nil")
	}
}

func TestValidate_RangeViolation_AllBranches(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*v1.Schema)
		want   string
	}{
		{"Workforce.MaxDepth>32", func(s *v1.Schema) { s.Workforce.MaxDepth = 33 }, "Workforce.MaxDepth"},
		{"Workforce.MaxWidthPerLayer<1", func(s *v1.Schema) { s.Workforce.MaxWidthPerLayer = 0 }, "Workforce.MaxWidthPerLayer"},
		{"Workforce.Recovery.TransientRetryBudget<0", func(s *v1.Schema) { s.Workforce.Recovery.TransientRetryBudget = -1 }, "TransientRetryBudget"},
		{"Workforce.Recovery.DoctrineRetryBudget<0", func(s *v1.Schema) { s.Workforce.Recovery.DoctrineRetryBudget = -1 }, "DoctrineRetryBudget"},
		{"HRA.CadenceTacticalMin<1", func(s *v1.Schema) { s.HRA.CadenceTacticalMin = 0 }, "HRA.CadenceTacticalMin"},
		{"HRA.CadenceStrategicMin<1", func(s *v1.Schema) { s.HRA.CadenceStrategicMin = 0 }, "HRA.CadenceStrategicMin"},
		{"HRA.CadenceArchitecturalMin<1", func(s *v1.Schema) { s.HRA.CadenceArchitecturalMin = 0 }, "HRA.CadenceArchitecturalMin"},
		{"HRA.ReviewerToWorkerRatio<1", func(s *v1.Schema) { s.HRA.ReviewerToWorkerRatio = 0 }, "HRA.ReviewerToWorkerRatio"},
		{"Research.MaxBudgetPerSession<0", func(s *v1.Schema) { s.Research.MaxBudgetPerSession = -1 }, "Research.MaxBudgetPerSession"},
		{"Gates.CoverageMinPct<0", func(s *v1.Schema) { s.Gates.CoverageMinPct = -1 }, "Gates.CoverageMinPct"},
		{"Gates.CoverageMinPct>100", func(s *v1.Schema) { s.Gates.CoverageMinPct = 101 }, "Gates.CoverageMinPct"},
		{"Review.HiveCadenceMin<1", func(s *v1.Schema) { s.Review.HiveCadenceMin = 0 }, "Review.HiveCadenceMin"},
		{"Review.RotateReviewerEvery<1", func(s *v1.Schema) { s.Review.RotateReviewerEvery = 0 }, "Review.RotateReviewerEvery"},
		{"Autonomy.Voting.PluralityThresholdPct<1", func(s *v1.Schema) { s.Autonomy.Voting.PluralityThresholdPct = 0 }, "PluralityThresholdPct"},
		{"Autonomy.Voting.PluralityThresholdPct>100", func(s *v1.Schema) { s.Autonomy.Voting.PluralityThresholdPct = 101 }, "PluralityThresholdPct"},
		{"Autonomy.AmendmentCooldownH<0", func(s *v1.Schema) { s.Autonomy.AmendmentCooldownH = -1 }, "AmendmentCooldownH"},
		{"Autonomy.CostDegradation.SoftCheckUSD<0", func(s *v1.Schema) { s.Autonomy.CostDegradation.SoftCheckUSD = -1 }, "SoftCheckUSD"},
		{"Merge.AnomalyThresholdPct<0", func(s *v1.Schema) { s.Merge.AnomalyThresholdPct = -1 }, "Merge.AnomalyThresholdPct"},
		{"Merge.AnomalyThresholdPct>100", func(s *v1.Schema) { s.Merge.AnomalyThresholdPct = 101 }, "Merge.AnomalyThresholdPct"},
		{"Merge.AnomalyWindowMin<1", func(s *v1.Schema) { s.Merge.AnomalyWindowMin = 0 }, "Merge.AnomalyWindowMin"},
		{"Merge.MaxCandidates<1", func(s *v1.Schema) { s.Merge.MaxCandidates = 0 }, "Merge.MaxCandidates"},
		{"ZenDayCadence.MorningBriefIfWithinHours<0", func(s *v1.Schema) { s.ZenDayCadence.MorningBriefIfWithinHours = -1 }, "MorningBriefIfWithinHours"},
		{"ZenDayCadence.EODDigestIfWithinHours<0", func(s *v1.Schema) { s.ZenDayCadence.EODDigestIfWithinHours = -1 }, "EODDigestIfWithinHours"},
		{"Quota.MaxConcurrentTasks<1", func(s *v1.Schema) { s.Quota.MaxConcurrentTasks = 0 }, "Quota.MaxConcurrentTasks"},
		{"Quota.MaxDailyBudgetUSD<0", func(s *v1.Schema) { s.Quota.MaxDailyBudgetUSD = -1 }, "Quota.MaxDailyBudgetUSD"},
		{"Quota.MaxStorageGB<0", func(s *v1.Schema) { s.Quota.MaxStorageGB = -1 }, "Quota.MaxStorageGB"},
		{"Tmux.IdleTTLMin<1", func(s *v1.Schema) { s.Tmux.IdleTTLMin = 0 }, "Tmux.IdleTTLMin"},
		{"Scheduling.MissCatchupMaxJobs<0", func(s *v1.Schema) { s.Scheduling.MissCatchupMaxJobs = -1 }, "Scheduling.MissCatchupMaxJobs"},
		{"WFQ.ProjectWeightDefault<1", func(s *v1.Schema) { s.WFQ.ProjectWeightDefault = 0 }, "WFQ.ProjectWeightDefault"},
		{"WFQ.StarvationGuardSec<1", func(s *v1.Schema) { s.WFQ.StarvationGuardSec = 0 }, "WFQ.StarvationGuardSec"},
		{"Knowledge.HiveDocCadenceHours<1", func(s *v1.Schema) { s.Knowledge.HiveDocCadenceHours = 0 }, "Knowledge.HiveDocCadenceHours"},
		{"Merge.ScoringWeights.TestPass>100", func(s *v1.Schema) { s.Merge.ScoringWeights.TestPass = 101 }, "Merge.ScoringWeights.TestPass"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := goodSchema()
			c.mutate(&s)
			err := s.Validate()
			if err == nil {
				t.Fatalf("expected error citing %q", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error message missing %q; got %q", c.want, err.Error())
			}
		})
	}
}

func TestValidate_DoctrineVersionMissing(t *testing.T) {
	s := goodSchema()
	s.DoctrineVersion = ""
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error when DoctrineVersion empty")
	}
	if !strings.Contains(err.Error(), "DoctrineVersion") {
		t.Errorf("error message must cite DoctrineVersion; got %q", err.Error())
	}
}

func TestRangeViolation_ErrorBranches(t *testing.T) {
	withMax := &v1.RangeViolation{Field: "X", Got: 5, MinAllow: 0, MaxAllow: 100, Reason: "r"}
	if !strings.Contains(withMax.Error(), "[0,100]") {
		t.Errorf("expected upper-bound bracket; got %q", withMax.Error())
	}
	withoutMax := &v1.RangeViolation{Field: "Y", Got: 5, MinAllow: 1, MaxAllow: 0, Reason: "r"}
	if !strings.Contains(withoutMax.Error(), "+inf") {
		t.Errorf("expected open-end bracket; got %q", withoutMax.Error())
	}
}

func TestTightenViolation_ErrorBranches(t *testing.T) {
	withDetail := &v1.TightenViolation{RulePath: "X", Direction: "d", AttemptedValue: 1, BaselineValue: 2, Detail: "extra"}
	if !strings.Contains(withDetail.Error(), "extra") {
		t.Errorf("expected detail; got %q", withDetail.Error())
	}
	withoutDetail := &v1.TightenViolation{RulePath: "Y", Direction: "d", AttemptedValue: 1, BaselineValue: 2}
	if got := withoutDetail.Error(); strings.Contains(got, "; ") {
		t.Errorf("no-detail variant must not have semicolon prefix; got %q", got)
	}
}

func TestSemverGreaterOrEqual_AllBranches(t *testing.T) {

	bs := goodSchema()
	bs.DoctrineVersion = "1.0.0"
	ov := goodSchema()
	ov.DoctrineVersion = "1.0.0"
	if err := ov.ValidateTighten(&bs); err != nil {
		t.Errorf("equal version must pass; got %v", err)
	}

	ov2 := goodSchema()
	ov2.DoctrineVersion = "2.0.0"
	if err := ov2.ValidateTighten(&bs); err != nil {
		t.Errorf("major bump must pass; got %v", err)
	}

	ov3 := goodSchema()
	ov3.DoctrineVersion = "1.1.0"
	if err := ov3.ValidateTighten(&bs); err != nil {
		t.Errorf("minor bump must pass; got %v", err)
	}
}

func TestValidateSchemaVersion_WhitespaceRejected(t *testing.T) {
	err := v1.ValidateSchemaVersion(" 1.0 ")
	if err == nil {
		t.Fatal("whitespace-padded version must NOT be accepted (no trim)")
	}
}

func TestTightenRegistry_TransverseAllPresent(t *testing.T) {
	reg := v1.TightenRegistry()
	for _, path := range []string{
		"Transverse.NoTechDebt", "Transverse.NoStubs",
		"Transverse.BuildFinalProduct", "Transverse.NoDefer",
	} {
		if _, ok := reg[path]; !ok {
			t.Errorf("missing transverse leaf %q in registry", path)
		}
	}
}

func TestEnumViolation_FromValidate(t *testing.T) {
	s := goodSchema()
	s.Autonomy.CostDegradation.DegradeStrategy = "garbage"
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error")
	}
	var enum *v1.EnumViolation
	if !errors.As(err, &enum) {
		t.Fatalf("expected *EnumViolation; got %v", err)
	}
	if enum.Field != "Autonomy.CostDegradation.DegradeStrategy" {
		t.Errorf("unexpected field %q", enum.Field)
	}
	if enum.Got != "garbage" {
		t.Errorf("unexpected got %q", enum.Got)
	}
}

func TestRequiredFieldMissing_FromValidate(t *testing.T) {
	s := goodSchema()
	s.SchemaVersion = ""
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error")
	}
	var req *v1.RequiredFieldMissing
	if !errors.As(err, &req) {
		t.Fatalf("expected *RequiredFieldMissing; got %v", err)
	}
	if req.Field != "SchemaVersion" {
		t.Errorf("unexpected field %q", req.Field)
	}
}

func TestTransverseMutationViolation_FromValidate(t *testing.T) {
	s := goodSchema()
	s.Transverse.NoStubs = false
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error")
	}
	var trans *v1.TransverseMutationViolation
	if !errors.As(err, &trans) {
		t.Fatalf("expected *TransverseMutationViolation; got %v", err)
	}
	if trans.Got.NoStubs != false {
		t.Errorf("Got.NoStubs = %v; want false (mutated)", trans.Got.NoStubs)
	}
}

func TestRejectTransverseOverride_NonTransverseKeysOnly(t *testing.T) {
	raw := map[string]any{"some_other_key": "value", "another_key": 123}
	for _, src := range []v1.TransverseSource{v1.SourceUserBaseline, v1.SourceUserOverride, v1.SourceEmbed} {
		err := v1.RejectTransverseOverride(src, raw)
		if err != nil {
			t.Errorf("source=%v: non-transverse keys must not flag; got %v", src, err)
		}
	}
}

func TestTransverseOverrideAttempt_TypeAlias(t *testing.T) {
	var attempt *v1.TransverseOverrideAttempt = &doctrineerrors.TransverseOverrideAttempt{
		Source: "x", Section: "y", Fields: []string{"a"},
	}
	if attempt.Source != "x" {
		t.Errorf("Source = %q; want x", attempt.Source)
	}

	if !errors.Is(attempt, doctrineerrors.ErrTransverseOverrideAttempted) {
		t.Error("alias must satisfy errors.Is to canonical sentinel")
	}
}

func TestLookupField_NonexistentPath(t *testing.T) {

	s := goodSchema()
	if err := s.ValidateTighten(&s); err != nil {
		t.Errorf("self-tighten must pass; got %v", err)
	}
}

func TestIntValue_DefensiveBehaviour(t *testing.T) {

	s := goodSchema()
	if err := s.ValidateTighten(&s); err != nil {
		t.Errorf("self-tighten must pass; got %v", err)
	}

	reg := v1.TightenRegistry()
	if len(reg) == 0 {
		t.Fatal("registry empty")
	}
}

func TestCheckAddOnly_SameSetPasses(t *testing.T) {
	bs := goodSchema()
	bs.Gates.TestTiers.Enabled = []string{"unit", "integration"}
	ov := goodSchema()
	ov.Gates.TestTiers.Enabled = []string{"integration", "unit"}
	if err := ov.ValidateTighten(&bs); err != nil {
		t.Errorf("same-set add-only must pass; got %v", err)
	}
}

func TestCheckRank_OutOfRangeValue(t *testing.T) {
	bs := goodSchema()
	bs.Autonomy.Mode = "garbage"
	ov := goodSchema()
	ov.Autonomy.Mode = "assisted"
	err := ov.ValidateTighten(&bs)
	if err == nil {
		t.Fatal("expected error when baseline rank value is invalid")
	}
	if !strings.Contains(err.Error(), "Autonomy.Mode") {
		t.Errorf("expected Autonomy.Mode in error; got %q", err.Error())
	}
}

func TestCompareDottedVersion_DifferentLengths(t *testing.T) {

	err := v1.ValidateSchemaVersion("0.5.0")
	if err == nil {
		t.Fatal("expected error for 0.5.0")
	}
	if !errors.Is(err, doctrineerrors.ErrSchemaVersionTooOld) {
		t.Errorf("expected too-old; got %v", err)
	}
}

func TestTightenRegistry_BuildSucceeds(t *testing.T) {

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()
	if reg := v1.TightenRegistry(); len(reg) == 0 {
		t.Fatal("registry empty")
	}
}

func TestWalkRankFields_NoSyntheticMarkers(t *testing.T) {
	s := goodSchema()
	if err := s.Validate(); err != nil {
		t.Fatalf("good schema must validate; if you see <non-string field type=> in this output, a rank: tag landed on a non-string field: %v", err)
	}
}

// Ensure the package's reflection-based walkers do not fall over on an
// extreme but valid schema: all-zero numeric fields produce many violations
// but no panics.
func TestValidate_AllZeroSchema(t *testing.T) {
	var s v1.Schema
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error on all-zero schema (Required + Range + Enum violations)")
	}

	if !errors.Is(err, v1.ErrValidationFailed) {
		t.Errorf("expected ErrValidationFailed; got %v", err)
	}
}

func TestRegistryFieldType(t *testing.T) {
	reg := v1.TightenRegistry()
	cases := []struct {
		path string
		kind reflect.Kind
	}{
		{"Workforce.MaxDepth", reflect.Int},
		{"Transverse.NoStubs", reflect.Bool},
		{"Autonomy.Mode", reflect.String},
		{"HRA.LayersEnabled", reflect.Slice},
		{"Gates.TestTiers.Enabled", reflect.Slice},
	}
	for _, c := range cases {
		rule, ok := reg[c.path]
		if !ok {
			t.Errorf("registry missing %s", c.path)
			continue
		}
		if rule.FieldType != c.kind {
			t.Errorf("%s FieldType = %v; want %v", c.path, rule.FieldType, c.kind)
		}
	}
}
