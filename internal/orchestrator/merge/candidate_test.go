package merge_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

func TestCandidateFailureTypeStringer(t *testing.T) {
	cases := []struct {
		f    merge.CandidateFailureType
		want string
	}{
		{merge.CandidateFailureTimeout, "Timeout"},
		{merge.CandidateFailurePanic, "Panic"},
		{merge.CandidateFailureCrash, "Crash"},
		{merge.CandidateFailureBaselineBreaker, "BaselineBreaker"},
		{merge.CandidateFailurePatchRejected, "PatchRejected"},
		{merge.CandidateFailureGitTransient, "GitTransient"},
	}
	for _, c := range cases {
		if got := c.f.String(); got != c.want {
			t.Errorf("CandidateFailureType(%d).String() = %q want %q", int(c.f), got, c.want)
		}
	}
}

func TestCandidateFailureTypeUnknown(t *testing.T) {
	if got := merge.CandidateFailureType(9999).String(); got != "Unknown" {
		t.Errorf("CandidateFailureType(9999).String() = %q want Unknown", got)
	}
}

func TestAllCandidateFailureTypes(t *testing.T) {
	all := merge.AllCandidateFailureTypes()
	if len(all) != 6 {
		t.Fatalf("AllCandidateFailureTypes len = %d want 6", len(all))
	}
	seen := make(map[merge.CandidateFailureType]bool)
	for _, f := range all {
		if seen[f] {
			t.Errorf("duplicate %v", f)
		}
		seen[f] = true
	}
}

func TestCandidateOutcomeFieldSet(t *testing.T) {

	wantFields := map[string]string{
		"Candidate":      "merge.MergeCandidate",
		"TestPassCount":  "int",
		"TestFailCount":  "int",
		"FlakeCount":     "int",
		"HardRejected":   "bool",
		"PatchSizeLines": "int",
		"Reason":         "string",
		"PassingSet":     "merge.PassingSet",
		"Stderr":         "string",
		"Duration":       "time.Duration",
		"BlastRadius":    "float64",
	}
	rt := reflect.TypeOf(merge.CandidateOutcome{})
	if rt.NumField() != len(wantFields) {
		t.Fatalf("NumField = %d want %d (field-set drift)", rt.NumField(), len(wantFields))
	}
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		want, ok := wantFields[f.Name]
		if !ok {
			t.Errorf("unexpected field %s (cross-phase contract drift)", f.Name)
			continue
		}
		got := f.Type.String()
		if !strings.HasSuffix(got, strings.TrimPrefix(want, "merge.")) && got != want {
			t.Errorf("field %s type = %q want %q", f.Name, got, want)
		}
	}
}

func TestCandidateStartedPayloadJSONRoundTrip(t *testing.T) {
	in := merge.CandidateStartedPayload{
		CandidateID:    "abc123",
		Branch:         "feat-A",
		Mode:           "Normal",
		PatchSizeBytes: 256,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out merge.CandidateStartedPayload
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round-trip mismatch: %+v vs %+v", in, out)
	}
}

func TestCandidateCompletePayloadJSONRoundTrip(t *testing.T) {
	in := merge.CandidateCompletePayload{
		CandidateID:    "abc",
		TestPassCount:  10,
		TestFailCount:  2,
		FlakeCount:     1,
		HardRejected:   false,
		PatchSizeLines: 50,
		PassingSetHash: "deadbeef",
		DurationMs:     12345,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out merge.CandidateCompletePayload
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round-trip mismatch")
	}
}

func TestCandidateFailedPayloadJSONRoundTrip(t *testing.T) {
	in := merge.CandidateFailedPayload{
		CandidateID: "abc",
		FailureType: merge.CandidateFailureTimeout.String(),
		Reason:      "timed out at 5min",
		ExitCode:    -1,
		Stderr:      "...",
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out merge.CandidateFailedPayload
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if in.FailureType != out.FailureType {
		t.Errorf("FailureType drift")
	}
}

func TestFlakeRerunStartedPayloadJSONRoundTrip(t *testing.T) {
	in := merge.FlakeRerunStartedPayload{
		CandidateID: "abc",
		RetryN:      2,
		TestID:      "test_x",
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var out merge.FlakeRerunStartedPayload
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round-trip mismatch")
	}
}

func TestRunResultZeroValue(t *testing.T) {
	var r merge.RunResult
	if r.ExitCode != 0 || r.Stdout != "" || r.Stderr != "" {
		t.Errorf("zero RunResult not pristine: %+v", r)
	}
}

func TestCandidateOutcomeDurationField(t *testing.T) {
	o := merge.CandidateOutcome{Duration: 5 * time.Second}
	if o.Duration != 5*time.Second {
		t.Errorf("Duration round-trip drift: %v", o.Duration)
	}
}
