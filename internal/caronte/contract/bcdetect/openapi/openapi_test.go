package openapi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oasdiff/oasdiff/checker"

	br "github.com/cbip-solutions/hades-system/internal/caronte/contract/bcdetect"
)

type stubChange struct {
	level checker.Level
	id    string
}

func (s stubChange) GetLevel() checker.Level                           { return s.level }
func (s stubChange) GetId() string                                     { return s.id }
func (s stubChange) GetSection() string                                { return "" }
func (s stubChange) IsBreaking() bool                                  { return false }
func (s stubChange) GetText(_ checker.Localizer) string                { return "" }
func (s stubChange) GetArgs() []any                                    { return nil }
func (s stubChange) GetUncolorizedText(_ checker.Localizer) string     { return "" }
func (s stubChange) GetComment(_ checker.Localizer) string             { return "" }
func (s stubChange) GetOperation() string                              { return "" }
func (s stubChange) GetOperationId() string                            { return "" }
func (s stubChange) GetPath() string                                   { return "" }
func (s stubChange) GetSource() string                                 { return "" }
func (s stubChange) GetAttributes() map[string]any                     { return nil }
func (s stubChange) GetBaseSource() *checker.Source                    { return nil }
func (s stubChange) GetRevisionSource() *checker.Source                { return nil }
func (s stubChange) GetSourceFile() string                             { return "" }
func (s stubChange) GetSourceLine() int                                { return 0 }
func (s stubChange) GetSourceLineEnd() int                             { return 0 }
func (s stubChange) GetSourceColumn() int                              { return 0 }
func (s stubChange) GetSourceColumnEnd() int                           { return 0 }
func (s stubChange) MatchIgnore(_, _ string, _ checker.Localizer) bool { return false }
func (s stubChange) SingleLineError(_ checker.Localizer, _ checker.ColorMode) string {
	return ""
}
func (s stubChange) MultiLineError(_ checker.Localizer, _ checker.ColorMode) string {
	return ""
}

// TestOpenAPIDetectorID pins the master C-2 CHECK-constraint anchor: the
// DetectorID MUST be "oasdiff" verbatim. Drift surfaces as a CHECK
// violation at the federation store write, but this sister test catches
// it at compile-test time (faster feedback).
func TestOpenAPIDetectorID(t *testing.T) {
	d := NewOpenAPIDetector(br.DefaultParams())
	if d.DetectorID() != "oasdiff" {
		t.Errorf("DetectorID = %q; want \"oasdiff\" (master C-2 CHECK-constraint anchor)", d.DetectorID())
	}
}

func TestOpenAPIDetectParamAddedRequired(t *testing.T) {
	d := NewOpenAPIDetector(br.DefaultParams())
	oldB := mustRead(t, "fixtures/param_added_required.old.json")
	newB := mustRead(t, "fixtures/param_added_required.new.json")
	results, err := d.Detect(context.Background(), oldB, newB)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !hasBreaking(results) {
		t.Errorf("expected ≥1 SevBreaking finding for added-required-param; got %d results: %s", len(results), summarize(results))
	}

	for i, r := range results {
		assertResultShape(t, i, r)
	}
}

func TestOpenAPIDetectParamRemoved(t *testing.T) {
	d := NewOpenAPIDetector(br.DefaultParams())
	oldB := mustRead(t, "fixtures/param_removed.old.json")
	newB := mustRead(t, "fixtures/param_removed.new.json")
	results, err := d.Detect(context.Background(), oldB, newB)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	if len(results) == 0 {
		t.Errorf("expected ≥1 finding for removed-query-param; got 0 (silent classification drift)")
	}
	for i, r := range results {
		assertResultShape(t, i, r)
	}
}

func TestOpenAPIDetectTypeChanged(t *testing.T) {
	d := NewOpenAPIDetector(br.DefaultParams())
	oldB := mustRead(t, "fixtures/type_changed.old.json")
	newB := mustRead(t, "fixtures/type_changed.new.json")
	results, err := d.Detect(context.Background(), oldB, newB)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !hasBreaking(results) {
		t.Errorf("expected ≥1 SevBreaking finding for path-param type change; got %d: %s", len(results), summarize(results))
	}
}

func TestOpenAPIDetectPathChanged(t *testing.T) {
	d := NewOpenAPIDetector(br.DefaultParams())
	oldB := mustRead(t, "fixtures/path_changed.old.json")
	newB := mustRead(t, "fixtures/path_changed.new.json")
	results, err := d.Detect(context.Background(), oldB, newB)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !hasBreaking(results) {
		t.Errorf("expected ≥1 SevBreaking finding for path rename; got %d: %s", len(results), summarize(results))
	}
}

func TestOpenAPIDetectErrSpecTooLarge(t *testing.T) {
	p := br.DefaultParams()
	p.MaxSpecBytes = 64 * 1024
	d := NewOpenAPIDetector(p)
	big := make([]byte, 65*1024)
	_, err := d.Detect(context.Background(), big, big)
	if !errors.Is(err, br.ErrSpecTooLarge) {
		t.Errorf("err = %v; want ErrSpecTooLarge", err)
	}
}

func TestOpenAPIDetectErrInvalidSpec(t *testing.T) {
	d := NewOpenAPIDetector(br.DefaultParams())
	_, err := d.Detect(context.Background(), []byte("not valid json {"), []byte("{}"))
	if !errors.Is(err, br.ErrInvalidSpec) {
		t.Errorf("err = %v; want ErrInvalidSpec", err)
	}
}

func TestOpenAPIDetectErrInvalidSpecOnNew(t *testing.T) {
	d := NewOpenAPIDetector(br.DefaultParams())
	good := mustRead(t, "fixtures/path_changed.old.json")
	_, err := d.Detect(context.Background(), good, []byte("not valid json {"))
	if !errors.Is(err, br.ErrInvalidSpec) {
		t.Errorf("err = %v; want ErrInvalidSpec on malformed newSpec", err)
	}
}

func TestOpenAPIDetectNoChange(t *testing.T) {
	d := NewOpenAPIDetector(br.DefaultParams())
	spec := mustRead(t, "fixtures/param_added_required.old.json")
	results, err := d.Detect(context.Background(), spec, spec)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	for _, r := range results {
		if r.Severity == br.SevBreaking || r.Severity == br.SevDangerous {
			t.Errorf("unchanged spec produced %s finding: %+v", r.Severity, r)
		}
	}
}

func TestOpenAPIDetectorSatisfiesDetectorInterface(t *testing.T) {
	var _ br.Detector = (*OpenAPIDetector)(nil)
}

func TestOpenAPIDetectContextCancellation(t *testing.T) {
	d := NewOpenAPIDetector(br.DefaultParams())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	good := mustRead(t, "fixtures/path_changed.old.json")
	_, err := d.Detect(ctx, good, good)
	if err == nil {
		t.Fatal("expected context.Canceled; got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v; want context.Canceled", err)
	}
}

func TestTranslateLevelSkipsNoneAndInvalid(t *testing.T) {

	if got := translateLevel(0); got != "" {
		t.Errorf("translateLevel(NONE=0) = %q; want \"\"", got)
	}
	if got := translateLevel(-1); got != "" {
		t.Errorf("translateLevel(INVALID=-1) = %q; want \"\"", got)
	}
	if got := translateLevel(99); got != "" {
		t.Errorf("translateLevel(future-value=99) = %q; want \"\"", got)
	}
}

func TestTranslateChangesSkipsZeroLevel(t *testing.T) {
	// Construct a checker.Changes slice with a single NONE-level Change.
	// checker.CheckBackwardCompatibility never emits NONE in practice, but
	// the defensive skip-path MUST still work if upstream drifts to expose
	// new Level values without compiler updates.
	got := translateChanges(checker.Changes{stubChange{level: 0, id: "should-skip"}})
	if len(got) != 0 {
		t.Errorf("translateChanges with NONE-level change returned %d results; want 0", len(got))
	}
}

func assertResultShape(t *testing.T, idx int, r br.DiffResult) {
	t.Helper()
	if r.DetectorID != "oasdiff" {
		t.Errorf("result[%d].DetectorID = %q; want oasdiff", idx, r.DetectorID)
	}
	if r.Kind == "" {
		t.Errorf("result[%d].Kind empty", idx)
	}
	if r.Severity == "" {
		t.Errorf("result[%d].Severity empty", idx)
	}
	if len(r.Detail) == 0 {
		t.Errorf("result[%d].Detail empty (must carry canonical JSON of finding)", idx)
	}
}

func hasBreaking(results []br.DiffResult) bool {
	for _, r := range results {
		if r.Severity == br.SevBreaking {
			return true
		}
	}
	return false
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
