package recognize

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestResultZeroValueIsUsable(t *testing.T) {
	var r Result
	if r.SchemaVersion != "" || r.PrimaryConfidence != 0 {
		t.Errorf("Result{}: non-zero fields: %+v", r)
	}
	if _, err := json.Marshal(r); err != nil {
		t.Errorf("Result{} json.Marshal: %v", err)
	}
}

func TestResultShimFieldsRoundTripJSON(t *testing.T) {
	in := Result{
		SchemaVersion:     "1.0",
		PrimaryLanguage:   "Go",
		PrimaryConfidence: 0.9,
		ManifestDeps:      map[string]string{"@prisma/client": "5.0.0"},
		EnvVars:           map[string]string{"LINEAR_API_KEY": "set"},
		ConfigFiles:       []string{"sentry.config.ts"},
		Doctrine:          "max-scope",
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var out Result
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round-trip mismatch:\nin =%+v\nout=%+v", in, out)
	}
}

func TestResultJSONFieldNamesCamelCase(t *testing.T) {
	r := Result{
		SchemaVersion:     "1.0",
		PrimaryLanguage:   "Go",
		PrimaryConfidence: 0.9,
		ManifestDeps:      map[string]string{"pg": "8"},
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	wantKeys := []string{
		`"schemaVersion"`,
		`"primaryLanguage"`,
		`"primaryConfidence"`,
		`"manifestDeps"`,
	}
	got := string(b)
	for _, k := range wantKeys {
		if !contains(got, k) {
			t.Errorf("Result JSON missing key %s; got %s", k, got)
		}
	}
}

func TestResultOmitemptyShimFields(t *testing.T) {
	r := Result{SchemaVersion: "1.0", PrimaryLanguage: "Go"}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := string(b)
	for _, omitted := range []string{`"manifestDeps"`, `"envVars"`, `"configFiles"`, `"doctrine"`} {
		if contains(got, omitted) {
			t.Errorf("Result JSON contains %s on zero-value; want omitempty: %s", omitted, got)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
