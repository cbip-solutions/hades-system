package v1

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseTightenTag_AllBranches(t *testing.T) {
	intKind := reflect.TypeOf(int(0))
	stringKind := reflect.TypeOf("")
	boolKind := reflect.TypeOf(false)
	sliceKind := reflect.TypeOf([]string{})

	cases := []struct {
		tag    string
		ft     reflect.Type
		wantOK bool
		wantD  TightenDirection
	}{
		{"-", intKind, true, TightenDirSkip},
		{"decrease", intKind, true, TightenDirDecrease},
		{"increase", intKind, true, TightenDirIncrease},
		{"truth", boolKind, true, TightenDirTruth},
		{"add-only", sliceKind, true, TightenDirAddOnly},
		{"bidirectional", stringKind, true, TightenDirBidirectional},
		{"bidirectional,requires-operator", stringKind, true, TightenDirBidirectional},
		{"rank:a,b,c", stringKind, true, TightenDirRank},

		{"add-only", intKind, false, 0},
		{"bidirectional,foo", stringKind, false, 0},
		{"rank:", stringKind, false, 0},
		{"rank:a,,c", stringKind, false, 0},
		{"unknown-direction", stringKind, false, 0},
	}
	for _, c := range cases {
		rule, err := parseTightenTag(c.tag, c.ft)
		if c.wantOK {
			if err != nil {
				t.Errorf("tag=%q: unexpected error %v", c.tag, err)
				continue
			}
			if rule.Direction != c.wantD {
				t.Errorf("tag=%q: Direction = %v; want %v", c.tag, rule.Direction, c.wantD)
			}
		} else {
			if err == nil {
				t.Errorf("tag=%q: expected error; got nil", c.tag)
			}
		}
	}
}

func TestParseTightenTag_RequiresOperatorSet(t *testing.T) {
	rule, err := parseTightenTag("bidirectional,requires-operator", reflect.TypeOf(""))
	if err != nil {
		t.Fatal(err)
	}
	if !rule.RequiresOperator {
		t.Error("expected RequiresOperator=true for ',requires-operator' suffix")
	}

	rule2, err := parseTightenTag("bidirectional", reflect.TypeOf(""))
	if err != nil {
		t.Fatal(err)
	}
	if rule2.RequiresOperator {
		t.Error("expected RequiresOperator=false for plain 'bidirectional'")
	}
}

func TestIntValue_AllKinds(t *testing.T) {
	cases := []struct {
		v      reflect.Value
		want   int64
		wantOK bool
	}{
		{reflect.ValueOf(int(7)), 7, true},
		{reflect.ValueOf(int8(8)), 8, true},
		{reflect.ValueOf(int16(16)), 16, true},
		{reflect.ValueOf(int32(32)), 32, true},
		{reflect.ValueOf(int64(64)), 64, true},
		{reflect.ValueOf(uint(7)), 7, true},
		{reflect.ValueOf(uint8(8)), 8, true},
		{reflect.ValueOf(uint16(16)), 16, true},
		{reflect.ValueOf(uint32(32)), 32, true},
		{reflect.ValueOf(uint64(64)), 64, true},
		{reflect.ValueOf("string"), 0, false},
		{reflect.ValueOf(true), 0, false},
		{reflect.ValueOf(3.14), 0, false},
	}
	for _, c := range cases {
		got, ok := intValue(c.v)
		if ok != c.wantOK {
			t.Errorf("intValue(%v) ok = %v; want %v", c.v.Interface(), ok, c.wantOK)
		}
		if got != c.want {
			t.Errorf("intValue(%v) = %d; want %d", c.v.Interface(), got, c.want)
		}
	}
}

func TestCheckNumericDecrease_NonNumeric(t *testing.T) {
	if v := checkNumericDecrease("X", reflect.ValueOf("foo"), reflect.ValueOf(int(1))); v != nil {
		t.Errorf("expected nil for non-numeric baseline; got %v", v)
	}
	if v := checkNumericDecrease("X", reflect.ValueOf(int(1)), reflect.ValueOf("bar")); v != nil {
		t.Errorf("expected nil for non-numeric override; got %v", v)
	}
}

func TestCheckNumericIncrease_NonNumeric(t *testing.T) {
	if v := checkNumericIncrease("X", reflect.ValueOf("foo"), reflect.ValueOf(int(1))); v != nil {
		t.Errorf("expected nil for non-numeric baseline; got %v", v)
	}
	if v := checkNumericIncrease("X", reflect.ValueOf(int(1)), reflect.ValueOf("bar")); v != nil {
		t.Errorf("expected nil for non-numeric override; got %v", v)
	}
}

func TestLookupField_BogusPath(t *testing.T) {
	s := Schema{SchemaVersion: "1.0"}
	v := reflect.ValueOf(s)

	if got := lookupField(v, "Bogus"); got.IsValid() {
		t.Error("expected invalid Value for non-existent top-level path")
	}

	if got := lookupField(v, "Workforce.Bogus"); got.IsValid() {
		t.Error("expected invalid Value for non-existent nested path")
	}

	if got := lookupField(v, "SchemaVersion.Sub"); got.IsValid() {
		t.Error("expected invalid Value for descend-through-non-struct path")
	}
}

func TestCheckAddOnly_NonSlice(t *testing.T) {
	if v := checkAddOnly("X", reflect.ValueOf(int(1)), reflect.ValueOf(int(2))); v != nil {
		t.Errorf("expected nil for non-slice; got %v", v)
	}
}

func TestSemverGreaterOrEqualStrict_HappyPath(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.0.0", "1.0.0", true},
		{"1.0.1", "1.0.0", true},
		{"1.0.0", "1.0.1", false},
		{"2.0.0", "1.99.99", true},
		{"1.10.0", "1.9.0", true},
		{"0.0.0", "0.0.0", true},
		{"100.200.300", "100.200.299", true},
	}
	for _, c := range cases {
		got, err := semverGreaterOrEqualStrict(c.a, c.b)
		if err != nil {
			t.Errorf("semverGreaterOrEqualStrict(%q, %q) unexpected err: %v", c.a, c.b, err)
			continue
		}
		if got != c.want {
			t.Errorf("semverGreaterOrEqualStrict(%q, %q) = %v; want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestSemverGreaterOrEqualStrict_RejectGarbage(t *testing.T) {
	cases := []struct {
		a, b string
		desc string
	}{
		{"abc", "def", "non-numeric and 1-segment"},
		{"abc.def.ghi", "1.2.3", "non-numeric segments"},
		{"1.2.3.4", "1.2.3", "extra segment in a"},
		{"1.2.3", "1.2.3.4", "extra segment in b"},
		{"1.2", "1.2.3", "missing segment in a"},
		{"1.2.3", "1.2", "missing segment in b"},
		{"", "0.0.0", "empty a"},
		{"0.0.0", "", "empty b"},
		{"1.0.0-alpha", "1.0.0", "pre-release suffix in a"},
		{"-1.0.0", "0.0.0", "negative segment"},
		{"+1.0.0", "0.0.0", "leading-plus segment"},
		{"1..3", "0.0.0", "empty middle segment"},
	}
	for _, c := range cases {
		got, err := semverGreaterOrEqualStrict(c.a, c.b)
		if err == nil {
			t.Errorf("%s — semverGreaterOrEqualStrict(%q, %q) = %v; expected error", c.desc, c.a, c.b, got)
		}
		if got {
			t.Errorf("%s — semverGreaterOrEqualStrict(%q, %q) returned ok=true on parse error; must be false", c.desc, c.a, c.b)
		}
	}
}

func TestSemverGreaterOrEqualStrict_EqualEdge(t *testing.T) {
	got, err := semverGreaterOrEqualStrict("1.2.3", "1.2.3")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !got {
		t.Error("equal versions must compare >= true")
	}
}

func TestParseStrictSemver_Decompose(t *testing.T) {
	major, minor, patch, err := parseStrictSemver("3.14.159")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if major != 3 || minor != 14 || patch != 159 {
		t.Errorf("decompose = (%d,%d,%d); want (3,14,159)", major, minor, patch)
	}
}

func TestCompareDottedVersion_DirectEdgeCases(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0", "1.0", 0},
		{"0.9", "1.0", -1},
		{"1.1", "1.0", 1},
		{"1.0.0", "1.0", 0},
		{"1.0", "1.0.0", 0},
		{"1.0.1", "1.0", 1},
	}
	for _, c := range cases {
		if got := compareDottedVersion(c.a, c.b); got != c.want {
			t.Errorf("compareDottedVersion(%q, %q) = %d; want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestSplitDots_EdgeCases(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"a", 1},
		{"a.b", 2},
		{"a.b.c", 3},
		{"a..b", 3},
	}
	for _, c := range cases {
		got := splitDots(c.in)
		if len(got) != c.want {
			t.Errorf("splitDots(%q) len = %d; want %d (got %v)", c.in, len(got), c.want, got)
		}
	}
}

func TestIndexOf_EdgeCases(t *testing.T) {
	xs := []string{"a", "b", "c"}
	if got := indexOf(xs, "b"); got != 1 {
		t.Errorf("indexOf=%d; want 1", got)
	}
	if got := indexOf(xs, "missing"); got != -1 {
		t.Errorf("indexOf(miss)=%d; want -1", got)
	}
	if got := indexOf(nil, "x"); got != -1 {
		t.Errorf("indexOf(nil)=%d; want -1", got)
	}
}

func TestContains_EdgeCases(t *testing.T) {
	if !contains([]string{"a", "b"}, "a") {
		t.Error("expected true")
	}
	if contains([]string{"a", "b"}, "z") {
		t.Error("expected false")
	}
	if contains(nil, "x") {
		t.Error("nil contains anything is false")
	}
}

func TestLookupRevertRuleMeta_DefaultEmpty(t *testing.T) {
	rule, ok := lookupRevertRuleMeta("Workforce.MaxDepth")
	if ok {
		t.Errorf("default (no fixture) lookup must return ok=false; got rule=%+v", rule)
	}
	if rule != (revertRuleMeta{}) {
		t.Errorf("default lookup must return zero revertRuleMeta; got %+v", rule)
	}
}

// TestBuildTightenRegistry_PanicOnMalformedTag — defensive: registry build
// MUST panic if a tag is malformed at struct-tag declaration time. We use a
// local helper struct (not the production Schema) to drive the panic via
// the same walker.
type malformedTagStruct struct {
	Field string `tighten:"this-is-not-a-valid-direction"`
}

func TestWalkTightenFields_DriveMalformed(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on malformed tag")
		} else if !strings.Contains(strings.Join([]string{"x", "x"}, ""), "x") {

		}
	}()

	walkTightenFields(reflect.TypeOf(malformedTagStruct{}), "", func(path, tag string, ft reflect.Type) {
		_, err := parseTightenTag(tag, ft)
		if err != nil {
			panic("doctrine: malformed tighten tag at " + path + ": " + err.Error())
		}
	})
}

type pointerStruct struct {
	Inner *innerStruct `tighten:"-"`
}

type innerStruct struct {
	Leaf string `tighten:"truth"`
}

func TestWalkTightenFields_PointerStructDescent(t *testing.T) {
	visited := []string{}
	walkTightenFields(reflect.TypeOf(pointerStruct{}), "", func(path, tag string, ft reflect.Type) {
		visited = append(visited, path)
	})

	found := false
	for _, p := range visited {
		if p == "Inner.Leaf" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Inner.Leaf in visited paths; got %v", visited)
	}
}

type missingTagStruct struct {
	Leaf string
}

func TestWalkTightenFields_MissingTagPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when leaf has no tighten tag")
		}
	}()
	walkTightenFields(reflect.TypeOf(missingTagStruct{}), "", func(path, tag string, ft reflect.Type) {

	})
}

func TestWalkTightenFields_NonStructInput(t *testing.T) {
	called := false
	walkTightenFields(reflect.TypeOf("string"), "", func(path, tag string, ft reflect.Type) {
		called = true
	})
	if called {
		t.Error("expected no callback on non-struct type")
	}
}

type rankPointerStruct struct {
	Inner *rankInnerStruct `tighten:"-"`
}
type rankInnerStruct struct {
	Mode string `tighten:"rank:a,b,c"`
}

func TestWalkRankFields_PointerDeref(t *testing.T) {
	v := rankPointerStruct{Inner: &rankInnerStruct{Mode: "a"}}
	visited := map[string]string{}
	walkRankFields(reflect.ValueOf(v), reflect.TypeOf(v), "", func(fpath string, allowed []string, got string) {
		visited[fpath] = got
	})
	if visited["Inner.Mode"] != "a" {
		t.Errorf("expected Inner.Mode=a; got %q (full visited: %v)", visited["Inner.Mode"], visited)
	}
}

func TestWalkRankFields_NilPointerSkipped(t *testing.T) {
	v := rankPointerStruct{Inner: nil}
	visited := map[string]string{}
	walkRankFields(reflect.ValueOf(v), reflect.TypeOf(v), "", func(fpath string, allowed []string, got string) {
		visited[fpath] = got
	})
	if _, present := visited["Inner.Mode"]; present {
		t.Errorf("nil pointer must be skipped; got visited[Inner.Mode]=%q", visited["Inner.Mode"])
	}
}

func TestValidateRanges_MergeScoringWeights_DeterministicOrder(t *testing.T) {
	const N = 50
	for i := 0; i < N; i++ {
		s := goodSchemaUnexported()
		s.Merge.ScoringWeights.TestPass = 200
		s.Merge.ScoringWeights.LintPass = 200
		s.Merge.ScoringWeights.Coverage = 200
		s.Merge.ScoringWeights.Diff = 200
		s.Merge.ScoringWeights.Duration = 200
		errs := validateRanges(&s)

		got := []string{}
		for _, e := range errs {
			var rv *RangeViolation
			if reflect.TypeOf(e) == reflect.TypeOf(rv) {
				rv = e.(*RangeViolation)
				if reflect.DeepEqual(rv.Field, "Merge.ScoringWeights.TestPass") ||
					reflect.DeepEqual(rv.Field, "Merge.ScoringWeights.LintPass") ||
					reflect.DeepEqual(rv.Field, "Merge.ScoringWeights.Coverage") ||
					reflect.DeepEqual(rv.Field, "Merge.ScoringWeights.Diff") ||
					reflect.DeepEqual(rv.Field, "Merge.ScoringWeights.Duration") {
					got = append(got, rv.Field)
				}
			}
		}
		want := []string{
			"Merge.ScoringWeights.TestPass",
			"Merge.ScoringWeights.LintPass",
			"Merge.ScoringWeights.Coverage",
			"Merge.ScoringWeights.Diff",
			"Merge.ScoringWeights.Duration",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("iteration %d: order = %v; want %v", i, got, want)
		}
	}
}

func TestValidateRanges_HardStopBelowSoft(t *testing.T) {
	s := goodSchemaUnexported()
	s.Autonomy.CostDegradation.SoftCheckUSD = 200
	s.Autonomy.CostDegradation.HardStopUSD = 100
	errs := validateRanges(&s)
	found := false
	for _, e := range errs {
		var rv *RangeViolation
		if reflect.TypeOf(e) == reflect.TypeOf(rv) {
			rv = e.(*RangeViolation)
			if rv.Field == "Autonomy.CostDegradation.HardStopUSD" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected HardStopUSD < SoftCheckUSD violation; got %v", errs)
	}
}

func goodSchemaUnexported() Schema {
	return Schema{
		SchemaVersion:   "1.0",
		DoctrineVersion: "1.0.0",
		AutoUpgrade:     "patch",
		Workforce: WorkforceConfig{
			MinDepth: 1, MaxDepth: 8, MaxWidthPerLayer: 10,
			Recovery: WorkforceRecoveryConfig{
				TransientRetryBudget: 3, PermanentInfraEscalate: "operator-notify", DoctrineRetryBudget: 1,
			},
		},
		HRA: HRAConfig{
			LayersEnabled: []int{1, 2, 3}, CadenceTacticalMin: 30, CadenceStrategicMin: 120,
			CadenceArchitecturalMin: 480, ReviewerToWorkerRatio: 3,
		},
		Research:   ResearchConfig{Enabled: true, MaxBudgetPerSession: 10, SOTAOrchestratorEnforced: true},
		Gates:      GatesConfig{TestTiers: TestTiersConfig{Enabled: []string{"unit"}}, CoverageMinPct: 90},
		Review:     ReviewConfig{HiveCadenceMin: 60, RotateReviewerEvery: 5, RequireDualReview: true},
		Transverse: TransverseExpected(),
		Autonomy: AutonomyConfig{
			Mode: "assisted", CheckMode: "strict",
			ConfirmationPolicy: ConfirmationPolicyConfig{
				BudgetBreachThreshold: "high", SpecAmendmentProposal: "high",
				InvariantViolation: "high", ArchitecturalReviewEscalation: "high",
			},
			Voting:             VotingConfig{PluralityThresholdPct: 50, FMVEnable: true, EMSEnable: true},
			CostDegradation:    CostDegradationConfig{SoftCheckUSD: 50, HardStopUSD: 100, DegradeStrategy: "downshift-tier"},
			AmendmentCooldownH: 24,
		},
		Merge: MergeConfig{
			Mode:                "balanced",
			ScoringWeights:      MergeScoringWeights{TestPass: 30, LintPass: 20, Coverage: 20, Diff: 15, Duration: 15},
			AnomalyThresholdPct: 80, AnomalyWindowMin: 60, MaxCandidates: 5,
		},
		Caronte: CaronteConfig{BranchPolicy: "balanced", HRAReviewEnabled: true},
		Notifications: NotificationsConfig{
			SeverityPerDoctrine: SeverityPerDoctrineConfig{
				ActionNeededPromotesToUrgent: false, UrgentBypassesQuietHours: true, InfoImmediateDuringQuiet: "queue",
			},
			QuietHoursStart: "22:00", QuietHoursEnd: "08:00",
		},
		ZenDayCadence: ZenDayCadenceConfig{
			MorningBriefCron: "0 8 * * 1-5", MorningBriefIfWithinHours: 2,
			EODDigestCron: "0 18 * * 1-5", EODDigestIfWithinHours: 2,
		},
		Quota:      QuotaConfig{MaxConcurrentTasks: 8, MaxDailyBudgetUSD: 100, MaxStorageGB: 50},
		Tmux:       TmuxConfig{IdleTTLMin: 30, AutoReap: true},
		Scheduling: SchedulingConfig{MissPolicy: "catchup-bounded", MissCatchupMaxJobs: 5},
		WFQ:        WFQConfig{ProjectWeightDefault: 10, StarvationGuardSec: 600, OvercommitPolicy: "queue"},
		Knowledge:  KnowledgeConfig{HiveDocCadenceHours: 24, ObsidianVaultPath: "/tmp", CrossProjectAggr: true},
		Augmentation: AugmentationConfig{
			Enable: true, MaxKGTokens: 10000, TimeoutMs: 1000,
			OnTimeout: "graceful_truncate", CrossProjectScope: "opt-in",
		},
	}
}
