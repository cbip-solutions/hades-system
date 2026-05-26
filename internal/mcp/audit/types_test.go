package audit

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClassificationEnum(t *testing.T) {
	values := []Classification{
		ClassificationClean,
		ClassificationMinor,
		ClassificationMajor,
		ClassificationReject,
	}
	seen := map[string]bool{}
	for _, c := range values {
		s := string(c)
		if s == "" {
			t.Errorf("Classification has empty string value")
		}
		if seen[s] {
			t.Errorf("duplicate Classification string %q", s)
		}
		seen[s] = true

		b, err := json.Marshal(c)
		if err != nil {
			t.Fatalf("marshal %q: %v", s, err)
		}
		var got Classification
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal %q: %v", s, err)
		}
		if got != c {
			t.Errorf("roundtrip: got %q, want %q", got, c)
		}
	}
}

func TestClassificationFromString(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  Classification
		err   bool
	}{
		{"clean", ClassificationClean, false},
		{"minor", ClassificationMinor, false},
		{"major", ClassificationMajor, false},
		{"reject", ClassificationReject, false},
		{"CLEAN", ClassificationClean, false},
		{"REJECT", ClassificationReject, false},
		{"", ClassificationClean, true},
		{"unknown", ClassificationClean, true},
		{"approved", ClassificationClean, true},
	} {
		got, err := ParseClassification(tc.input)
		if tc.err {
			if err == nil {
				t.Errorf("ParseClassification(%q) want error, got nil (value=%q)", tc.input, got)
			}
		} else {
			if err != nil {
				t.Errorf("ParseClassification(%q) unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ParseClassification(%q) = %q, want %q", tc.input, got, tc.want)
			}
		}
	}
}

func TestVerdictJSONRoundtrip(t *testing.T) {
	v := Verdict{
		Classification:   ClassificationMinor,
		Concerns:         []string{"missing nil check on line 42", "error not propagated"},
		Suggestions:      []string{"add nil guard", "return fmt.Errorf(...)"},
		ReviewerProvider: "google",
		ReviewerModel:    "gemini-2.6-pro",
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Verdict
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Classification != v.Classification {
		t.Errorf("Classification: got %q, want %q", got.Classification, v.Classification)
	}
	if len(got.Concerns) != len(v.Concerns) {
		t.Errorf("Concerns len: got %d, want %d", len(got.Concerns), len(v.Concerns))
	}
	if got.ReviewerProvider != v.ReviewerProvider {
		t.Errorf("ReviewerProvider: got %q, want %q", got.ReviewerProvider, v.ReviewerProvider)
	}
	if got.ReviewerModel != v.ReviewerModel {
		t.Errorf("ReviewerModel: got %q, want %q", got.ReviewerModel, v.ReviewerModel)
	}
}

func TestAuditRequestValidation(t *testing.T) {
	base := AuditRequest{
		Diff:                    "--- a/foo.go\n+++ b/foo.go\n@@ -1 +1 @@\n+func main() {}",
		CriteriaName:            "default",
		GeneratorProviderFamily: "anthropic",
	}
	if err := base.Validate(); err != nil {
		t.Errorf("valid base request rejected: %v", err)
	}

	r := base
	r.Diff = ""
	if err := r.Validate(); err == nil {
		t.Error("expected error for empty diff, got nil")
	}

	r = base
	r.CriteriaName = ""
	if err := r.Validate(); err == nil {
		t.Error("expected error for empty criteria name, got nil")
	}

	r = base
	r.GeneratorProviderFamily = ""
	if err := r.Validate(); err == nil {
		t.Error("expected error for empty generator family, got nil")
	}

	r = base
	r.Diff = "   \n\t  "
	if err := r.Validate(); err == nil {
		t.Error("expected error for whitespace-only diff, got nil")
	}
}

func TestAuditResponseContainsAllFields(t *testing.T) {
	resp := AuditResponse{
		Verdict:          Verdict{Classification: ClassificationClean},
		CriteriaUsed:     "security",
		CriteriaResolved: true,
		GeneratorFamily:  "anthropic",
	}
	b, _ := json.Marshal(resp)
	if !strings.Contains(string(b), `"classification"`) {
		t.Error("AuditResponse JSON missing classification field")
	}
	if !strings.Contains(string(b), `"criteria_used"`) {
		t.Error("AuditResponse JSON missing criteria_used field")
	}
	if !strings.Contains(string(b), `"criteria_resolved"`) {
		t.Error("AuditResponse JSON missing criteria_resolved field (review S-7)")
	}
	if !strings.Contains(string(b), `"generator_family"`) {
		t.Error("AuditResponse JSON missing generator_family field")
	}
}
