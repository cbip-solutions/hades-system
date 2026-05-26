package bcdetect

import (
	"testing"
)

func TestSeverityValueSetIsExactlyFour(t *testing.T) {
	want := []Severity{SevBreaking, SevDangerous, SevNonBreaking, SevInsufficient}
	wantStrs := []string{"BREAKING", "DANGEROUS", "NON_BREAKING", "INSUFFICIENT"}
	for i, s := range want {
		if string(s) != wantStrs[i] {
			t.Errorf("Severity[%d] = %q; want %q", i, string(s), wantStrs[i])
		}
	}
}

func TestDiffResultFieldSet(t *testing.T) {
	d := DiffResult{
		DetectorID: "oasdiff",
		Kind:       "param_added_required",
		Severity:   SevBreaking,
		Detail:     []byte(`{"x":1}`),
	}
	if d.DetectorID == "" || d.Kind == "" || d.Severity == "" || len(d.Detail) == 0 {
		t.Errorf("DiffResult{} zero-value drift detected: %+v", d)
	}
}

func TestBreakingEventFieldSet(t *testing.T) {
	e := BreakingEvent{
		ChangeID:      "ch-1",
		WorkspaceID:   "ws-1",
		EndpointID:    "ep-1",
		EndpointRepo:  "backend",
		Kind:          "param_added_required",
		Severity:      SevBreaking,
		DetectorID:    "oasdiff",
		Detail:        []byte(`{}`),
		DetectedAt:    1700000000,
		ConsumerCount: 3,
	}
	if e.ChangeID == "" || e.WorkspaceID == "" || e.EndpointID == "" ||
		e.EndpointRepo == "" || e.Kind == "" || e.Severity == "" ||
		e.DetectorID == "" || len(e.Detail) == 0 || e.DetectedAt == 0 ||
		e.ConsumerCount == 0 {
		t.Errorf("BreakingEvent{} drift detected: %+v", e)
	}
}

func TestSeverityIsString(t *testing.T) {
	var s Severity = "BREAKING"
	if got := string(s); got != "BREAKING" {
		t.Errorf("Severity not a string: got %q", got)
	}
}
