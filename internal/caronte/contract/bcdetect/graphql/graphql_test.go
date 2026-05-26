package graphql

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	br "github.com/cbip-solutions/hades-system/internal/caronte/contract/bcdetect"
)

func TestGraphQLDetectorID(t *testing.T) {
	d := NewGraphQLDetector(br.DefaultParams())
	if d.DetectorID() != "gqlparser" {
		t.Errorf("DetectorID = %q; want gqlparser", d.DetectorID())
	}
}

func TestGraphQLDetectorSatisfiesInterface(t *testing.T) {
	var _ br.Detector = (*GraphQLDetector)(nil)
}

func TestCanonicalRulesIsExactlySix(t *testing.T) {
	got := CanonicalRules()
	want := []Rule{
		RuleFieldRemoved, RuleFieldArgumentTypeChanged, RuleTypeRemoved,
		RuleEnumValueRemoved, RuleInputFieldAddedRequired, RuleDirectiveUsageRemoved,
	}
	if len(got) != len(want) {
		t.Fatalf("CanonicalRules len = %d; want %d", len(got), len(want))
	}
	for i, r := range want {
		if got[i] != r {
			t.Errorf("CanonicalRules[%d] = %q; want %q", i, got[i], r)
		}
	}
}

func TestGraphQLDetectFieldRemoved(t *testing.T) {
	d := NewGraphQLDetector(br.DefaultParams())
	oldB := mustRead(t, "fixtures/field_removed.old.gql")
	newB := mustRead(t, "fixtures/field_removed.new.gql")
	results, err := d.Detect(context.Background(), oldB, newB)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !hasKind(results, string(RuleFieldRemoved)) {
		t.Errorf("expected %s finding; got %d: %s", RuleFieldRemoved, len(results), summarize(results))
	}
	for i, r := range results {
		assertResultShape(t, i, r)
	}
}

func TestGraphQLDetectFieldArgumentTypeChanged(t *testing.T) {
	d := NewGraphQLDetector(br.DefaultParams())
	oldB := mustRead(t, "fixtures/field_argument_type_changed.old.gql")
	newB := mustRead(t, "fixtures/field_argument_type_changed.new.gql")
	results, err := d.Detect(context.Background(), oldB, newB)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !hasKind(results, string(RuleFieldArgumentTypeChanged)) {
		t.Errorf("expected %s finding; got %d: %s", RuleFieldArgumentTypeChanged, len(results), summarize(results))
	}
}

func TestGraphQLDetectTypeRemoved(t *testing.T) {
	d := NewGraphQLDetector(br.DefaultParams())
	oldB := mustRead(t, "fixtures/type_removed.old.gql")
	newB := mustRead(t, "fixtures/type_removed.new.gql")
	results, err := d.Detect(context.Background(), oldB, newB)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !hasKind(results, string(RuleTypeRemoved)) {
		t.Errorf("expected %s finding; got %d: %s", RuleTypeRemoved, len(results), summarize(results))
	}
}

func TestGraphQLDetectEnumValueRemoved(t *testing.T) {
	d := NewGraphQLDetector(br.DefaultParams())
	oldB := mustRead(t, "fixtures/enum_value_removed.old.gql")
	newB := mustRead(t, "fixtures/enum_value_removed.new.gql")
	results, err := d.Detect(context.Background(), oldB, newB)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !hasKind(results, string(RuleEnumValueRemoved)) {
		t.Errorf("expected %s finding; got %d: %s", RuleEnumValueRemoved, len(results), summarize(results))
	}
}

func TestGraphQLDetectInputFieldAddedRequired(t *testing.T) {
	d := NewGraphQLDetector(br.DefaultParams())
	oldB := mustRead(t, "fixtures/input_field_added_required.old.gql")
	newB := mustRead(t, "fixtures/input_field_added_required.new.gql")
	results, err := d.Detect(context.Background(), oldB, newB)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !hasKind(results, string(RuleInputFieldAddedRequired)) {
		t.Errorf("expected %s finding; got %d: %s", RuleInputFieldAddedRequired, len(results), summarize(results))
	}
}

func TestGraphQLDetectDirectiveUsageRemoved(t *testing.T) {
	d := NewGraphQLDetector(br.DefaultParams())
	oldB := mustRead(t, "fixtures/directive_usage_removed.old.gql")
	newB := mustRead(t, "fixtures/directive_usage_removed.new.gql")
	results, err := d.Detect(context.Background(), oldB, newB)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !hasKind(results, string(RuleDirectiveUsageRemoved)) {
		t.Errorf("expected %s finding; got %d: %s", RuleDirectiveUsageRemoved, len(results), summarize(results))
	}
}

// TestGraphQLDetectSevInsufficient pins the load-bearing Stage-0 divergence
// #3 signal: when the Go SDL diff hits a rule class OUTSIDE the canonical
// six (a custom directive's argument-list extension here), the result MUST
// carry SevInsufficient with a Kind naming the unclassified rule. This is
// the deterministic trigger for the Node fallback under the inv-zen-272
// opt-in gate.
func TestGraphQLDetectSevInsufficient(t *testing.T) {
	d := NewGraphQLDetector(br.DefaultParams())
	oldB := mustRead(t, "fixtures/insufficient_custom_directive.old.gql")
	newB := mustRead(t, "fixtures/insufficient_custom_directive.new.gql")
	results, err := d.Detect(context.Background(), oldB, newB)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !hasSeverity(results, br.SevInsufficient) {
		t.Errorf("expected ≥1 SevInsufficient finding for custom directive argument extension; got %d: %s", len(results), summarize(results))
	}
}

func TestGraphQLDetectErrSpecTooLarge(t *testing.T) {
	p := br.DefaultParams()
	p.MaxSpecBytes = 64 * 1024
	d := NewGraphQLDetector(p)
	big := make([]byte, 65*1024)
	_, err := d.Detect(context.Background(), big, big)
	if !errors.Is(err, br.ErrSpecTooLarge) {
		t.Errorf("err = %v; want ErrSpecTooLarge", err)
	}
}

func TestGraphQLDetectErrInvalidSpec(t *testing.T) {
	d := NewGraphQLDetector(br.DefaultParams())
	_, err := d.Detect(context.Background(), []byte("type {}{{}}"), []byte("type Q { x: Int }"))
	if !errors.Is(err, br.ErrInvalidSpec) {
		t.Errorf("err = %v; want ErrInvalidSpec", err)
	}
}

func TestGraphQLDetectErrInvalidSpecOnNew(t *testing.T) {
	d := NewGraphQLDetector(br.DefaultParams())
	good := mustRead(t, "fixtures/type_removed.old.gql")
	_, err := d.Detect(context.Background(), good, []byte("type {{}}"))
	if !errors.Is(err, br.ErrInvalidSpec) {
		t.Errorf("err = %v; want ErrInvalidSpec on bad newSpec", err)
	}
}

func TestGraphQLDetectNoChange(t *testing.T) {
	d := NewGraphQLDetector(br.DefaultParams())
	spec := mustRead(t, "fixtures/type_removed.old.gql")
	results, err := d.Detect(context.Background(), spec, spec)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	for _, r := range results {
		if r.Severity == br.SevBreaking || r.Severity == br.SevDangerous {
			t.Errorf("unchanged SDL produced %s finding: %+v", r.Severity, r)
		}
	}
}

func TestGraphQLDetectContextCancellation(t *testing.T) {
	d := NewGraphQLDetector(br.DefaultParams())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	good := mustRead(t, "fixtures/type_removed.old.gql")
	_, err := d.Detect(ctx, good, good)
	if err == nil {
		t.Fatal("expected context.Canceled; got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v; want context.Canceled", err)
	}
}

func hasKind(results []br.DiffResult, kind string) bool {
	for _, r := range results {
		if r.Kind == kind {
			return true
		}
	}
	return false
}

func hasSeverity(results []br.DiffResult, sev br.Severity) bool {
	for _, r := range results {
		if r.Severity == sev {
			return true
		}
	}
	return false
}

func assertResultShape(t *testing.T, idx int, r br.DiffResult) {
	t.Helper()
	if r.DetectorID != "gqlparser" {
		t.Errorf("result[%d].DetectorID = %q; want gqlparser", idx, r.DetectorID)
	}
	if r.Kind == "" {
		t.Errorf("result[%d].Kind empty", idx)
	}
	if r.Severity == "" {
		t.Errorf("result[%d].Severity empty", idx)
	}
	if len(r.Detail) == 0 {
		t.Errorf("result[%d].Detail empty", idx)
	}
}

func summarize(results []br.DiffResult) string {
	parts := make([]string, 0, len(results))
	for _, r := range results {
		parts = append(parts, r.Kind+"="+string(r.Severity))
	}
	return strings.Join(parts, ", ")
}

func mustRead(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.FromSlash(rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return b
}
