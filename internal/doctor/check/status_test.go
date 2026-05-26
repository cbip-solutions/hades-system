package check_test

import (
	"encoding/json"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctor/check"
)

func TestStatusOrdering(t *testing.T) {
	if check.StatusPass != 0 {
		t.Errorf("StatusPass = %d, want 0", check.StatusPass)
	}
	if check.StatusWarn != 1 {
		t.Errorf("StatusWarn = %d, want 1", check.StatusWarn)
	}
	if check.StatusFail != 2 {
		t.Errorf("StatusFail = %d, want 2", check.StatusFail)
	}
	if check.StatusSkip != 3 {
		t.Errorf("StatusSkip = %d, want 3", check.StatusSkip)
	}

	if !(check.StatusPass < check.StatusWarn && check.StatusWarn < check.StatusFail && check.StatusFail < check.StatusSkip) {
		t.Errorf("Status enum ordering broken: pass=%d warn=%d fail=%d skip=%d",
			check.StatusPass, check.StatusWarn, check.StatusFail, check.StatusSkip)
	}
}

func TestStatusString(t *testing.T) {
	tests := []struct {
		s    check.Status
		want string
	}{
		{check.StatusPass, "pass"},
		{check.StatusWarn, "warn"},
		{check.StatusFail, "fail"},
		{check.StatusSkip, "skip"},
		{check.Status(99), "unknown"},
	}
	for _, tc := range tests {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("Status(%d).String() = %q, want %q", tc.s, got, tc.want)
		}
	}
}

func TestStatusGlyphUnicode(t *testing.T) {
	tests := []struct {
		s    check.Status
		want string
	}{
		{check.StatusPass, "✓"},
		{check.StatusWarn, "⚠"},
		{check.StatusFail, "✗"},
		{check.StatusSkip, "⊝"},
		{check.Status(99), "?"},
	}
	for _, tc := range tests {
		if got := tc.s.Glyph(false); got != tc.want {
			t.Errorf("Status(%d).Glyph(ascii=false) = %q, want %q", tc.s, got, tc.want)
		}
	}
}

func TestStatusGlyphASCII(t *testing.T) {
	tests := []struct {
		s    check.Status
		want string
	}{
		{check.StatusPass, "OK"},
		{check.StatusWarn, "WARN"},
		{check.StatusFail, "FAIL"},
		{check.StatusSkip, "SKIP"},
		{check.Status(99), "??"},
	}
	for _, tc := range tests {
		if got := tc.s.Glyph(true); got != tc.want {
			t.Errorf("Status(%d).Glyph(ascii=true) = %q, want %q", tc.s, got, tc.want)
		}
	}
}

func TestStatusJSONMarshal(t *testing.T) {
	tests := []struct {
		s    check.Status
		want string
	}{
		{check.StatusPass, `"pass"`},
		{check.StatusWarn, `"warn"`},
		{check.StatusFail, `"fail"`},
		{check.StatusSkip, `"skip"`},
	}
	for _, tc := range tests {
		b, err := json.Marshal(tc.s)
		if err != nil {
			t.Fatalf("Marshal(%v): %v", tc.s, err)
		}
		if string(b) != tc.want {
			t.Errorf("Marshal(%v) = %q, want %q", tc.s, string(b), tc.want)
		}
	}
}

func TestStatusJSONUnmarshal(t *testing.T) {
	tests := []struct {
		input string
		want  check.Status
	}{
		{`"pass"`, check.StatusPass},
		{`"warn"`, check.StatusWarn},
		{`"fail"`, check.StatusFail},
		{`"skip"`, check.StatusSkip},
		{`"garbage"`, check.StatusSkip},
	}
	for _, tc := range tests {
		var got check.Status
		if err := json.Unmarshal([]byte(tc.input), &got); err != nil {
			t.Fatalf("Unmarshal(%q): %v", tc.input, err)
		}
		if got != tc.want {
			t.Errorf("Unmarshal(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestStatusJSONRoundtripInsideStruct(t *testing.T) {
	original := check.DiagnosticResult{
		Name:    "test.example",
		Status:  check.StatusWarn,
		Message: "warning",
	}
	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got check.DiagnosticResult
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Status != check.StatusWarn {
		t.Errorf("roundtrip Status = %v, want StatusWarn", got.Status)
	}
	if got.Name != "test.example" || got.Message != "warning" {
		t.Errorf("roundtrip lost fields: %+v", got)
	}
}

func TestFixModeOrdering(t *testing.T) {
	if check.FixModeReadOnly != 0 {
		t.Errorf("FixModeReadOnly = %d, want 0", check.FixModeReadOnly)
	}
	if check.FixModeInteractive != 1 {
		t.Errorf("FixModeInteractive = %d, want 1", check.FixModeInteractive)
	}
	if check.FixModeAutoSafe != 2 {
		t.Errorf("FixModeAutoSafe = %d, want 2", check.FixModeAutoSafe)
	}
	if check.FixModeYes != 3 {
		t.Errorf("FixModeYes = %d, want 3", check.FixModeYes)
	}
}

func TestFixModeString(t *testing.T) {
	tests := []struct {
		f    check.FixMode
		want string
	}{
		{check.FixModeReadOnly, "read-only"},
		{check.FixModeInteractive, "interactive"},
		{check.FixModeAutoSafe, "auto-safe"},
		{check.FixModeYes, "yes"},
		{check.FixMode(99), "unknown"},
	}
	for _, tc := range tests {
		if got := tc.f.String(); got != tc.want {
			t.Errorf("FixMode(%d).String() = %q, want %q", tc.f, got, tc.want)
		}
	}
}
