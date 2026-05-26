package inbox

import (
	"errors"
	"testing"
)

func TestSeverityConstants(t *testing.T) {
	cases := []struct {
		name string
		got  Severity
		want string
	}{
		{"urgent", SeverityUrgent, "urgent"},
		{"action-needed", SeverityActionNeeded, "action-needed"},
		{"info-immediate", SeverityInfoImmediate, "info-immediate"},
		{"info-digest", SeverityInfoDigest, "info-digest"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if string(c.got) != c.want {
				t.Errorf("Severity = %q, want %q", string(c.got), c.want)
			}
		})
	}
}

func TestValidSeverityAcceptsAllFour(t *testing.T) {
	for _, s := range []string{"urgent", "action-needed", "info-immediate", "info-digest"} {
		if !ValidSeverity(s) {
			t.Errorf("ValidSeverity(%q) = false, want true", s)
		}
	}
}

func TestValidSeverityRejectsInvalid(t *testing.T) {
	for _, s := range []string{"", "URGENT", "panic", "trace", "warn", "info", "debug", "Severity::Urgent"} {
		if ValidSeverity(s) {
			t.Errorf("ValidSeverity(%q) = true, want false", s)
		}
	}
}

func TestParseSeverityRoundTrip(t *testing.T) {
	for _, s := range []Severity{SeverityUrgent, SeverityActionNeeded, SeverityInfoImmediate, SeverityInfoDigest} {
		got, err := ParseSeverity(string(s))
		if err != nil {
			t.Fatalf("ParseSeverity(%q): %v", s, err)
		}
		if got != s {
			t.Errorf("ParseSeverity(%q) = %q, want %q", s, got, s)
		}
	}
}

func TestParseSeverityErrorOnInvalid(t *testing.T) {
	_, err := ParseSeverity("nope")
	if err == nil {
		t.Fatal("ParseSeverity(nope) returned nil error")
	}
	if !errors.Is(err, ErrInvalidSeverity) {
		t.Errorf("err is not ErrInvalidSeverity: %v", err)
	}
}

func TestSeverityEnumSentinelReturnsErr(t *testing.T) {
	if !errors.Is(severity4TierEnumSentinel(), ErrSeverity4TierAnchor) {
		t.Fatal("expected ErrSeverity4TierAnchor")
	}
}

func TestSeverityString(t *testing.T) {

	cases := []struct {
		s    Severity
		want string
	}{
		{SeverityUrgent, "urgent"},
		{SeverityActionNeeded, "action-needed"},
		{SeverityInfoImmediate, "info-immediate"},
		{SeverityInfoDigest, "info-digest"},
	}
	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			if got := c.s.String(); got != c.want {
				t.Errorf("Severity.String() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestAllSeveritiesCoversFour(t *testing.T) {
	got := AllSeverities()
	if len(got) != 4 {
		t.Fatalf("AllSeverities() len = %d, want 4", len(got))
	}
	want := map[Severity]bool{
		SeverityUrgent:        true,
		SeverityActionNeeded:  true,
		SeverityInfoImmediate: true,
		SeverityInfoDigest:    true,
	}
	for _, s := range got {
		if !want[s] {
			t.Errorf("AllSeverities() returned unexpected: %q", s)
		}
		delete(want, s)
	}
	if len(want) != 0 {
		t.Errorf("AllSeverities() missing: %v", want)
	}
}
