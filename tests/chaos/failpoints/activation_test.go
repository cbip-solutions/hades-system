// go:build chaos

package failpoints

import (
	"os"
	"strings"
	"testing"
)

func TestParseTermReturn(t *testing.T) {
	term, err := ParseTerm(`auditWALFsync=return("err")`)
	if err != nil {
		t.Fatalf("ParseTerm: %v", err)
	}
	if term.Name != "auditWALFsync" {
		t.Errorf("name = %q, want auditWALFsync", term.Name)
	}
	if term.Mode != ModeReturn {
		t.Errorf("mode = %v, want ModeReturn", term.Mode)
	}
	if term.Arg != `"err"` {
		t.Errorf("arg = %q, want \"err\"", term.Arg)
	}
}

func TestParseTermSleep(t *testing.T) {
	term, err := ParseTerm("worktreepoolAcquireTimeout=sleep(5s)")
	if err != nil {
		t.Fatalf("ParseTerm: %v", err)
	}
	if term.Mode != ModeSleep {
		t.Errorf("mode = %v, want ModeSleep", term.Mode)
	}
	if term.Arg != "5s" {
		t.Errorf("arg = %q, want 5s", term.Arg)
	}
}

func TestParseTermPanic(t *testing.T) {
	term, err := ParseTerm("dispatcherCancelMidFlight=panic(abort)")
	if err != nil {
		t.Fatalf("ParseTerm: %v", err)
	}
	if term.Mode != ModePanic {
		t.Errorf("mode = %v, want ModePanic", term.Mode)
	}
}

func TestParseTermOff(t *testing.T) {
	term, err := ParseTerm("auditWALFsync=off()")
	if err != nil {
		t.Fatalf("ParseTerm: %v", err)
	}
	if term.Mode != ModeOff {
		t.Errorf("mode = %v, want ModeOff", term.Mode)
	}
}

func TestParseTermMalformed(t *testing.T) {
	cases := []string{
		"no-equals",
		"name=unknown(arg)",
		"name=return",
		"name=sleep(arg",
		"=return(x)",
		"NaMe=return(x",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			_, err := ParseTerm(in)
			if err == nil {
				t.Errorf("ParseTerm(%q) succeeded; want error", in)
			}
		})
	}
}

func TestModeStringStable(t *testing.T) {
	cases := []struct {
		m    Mode
		want string
	}{
		{ModeUnknown, "unknown"},
		{ModeReturn, "return"},
		{ModeSleep, "sleep"},
		{ModePanic, "panic"},
		{ModeOff, "off"},
	}
	for _, c := range cases {
		if got := c.m.String(); got != c.want {
			t.Errorf("Mode(%d).String() = %q, want %q", c.m, got, c.want)
		}
	}
}

func TestTermStringRoundTrip(t *testing.T) {
	terms := []Term{
		{Name: "auditWALFsync", Mode: ModeReturn, Arg: `"err"`},
		{Name: "worktreepoolAcquireTimeout", Mode: ModeSleep, Arg: "5s"},
		{Name: "dispatcherCancelMidFlight", Mode: ModePanic, Arg: "abort"},
		{Name: "auditWALFsync", Mode: ModeOff, Arg: ""},
	}
	for _, want := range terms {
		t.Run(want.Name+"_"+want.Mode.String(), func(t *testing.T) {
			s := want.String()
			got, err := ParseTerm(s)
			if err != nil {
				t.Fatalf("ParseTerm(%q): %v", s, err)
			}
			if got.Name != want.Name || got.Mode != want.Mode || got.Arg != want.Arg {
				t.Errorf("round-trip drift: got=%+v want=%+v (via %q)", got, want, s)
			}
		})
	}
}

func TestActivateSetsEnv(t *testing.T) {
	prevSet := os.Getenv("GOFAIL_FAILPOINTS") != ""
	t.Cleanup(func() {
		if !prevSet {
			_ = os.Unsetenv("GOFAIL_FAILPOINTS")
		}
	})
	term := Term{Name: "auditWALFsync", Mode: ModeReturn, Arg: `"err"`}
	restore := Activate(term)
	got := os.Getenv("GOFAIL_FAILPOINTS")
	if got != term.String() {
		t.Errorf("env = %q, want %q", got, term.String())
	}
	restore()
}

func TestActivateRestoresPreviousValue(t *testing.T) {
	t.Setenv("GOFAIL_FAILPOINTS", "preexisting=return(prev)")
	term := Term{Name: "auditWALFsync", Mode: ModeReturn, Arg: `"err"`}
	restore := Activate(term)
	if got := os.Getenv("GOFAIL_FAILPOINTS"); got != term.String() {
		t.Errorf("during Activate: env=%q, want %q", got, term.String())
	}
	restore()
	if got := os.Getenv("GOFAIL_FAILPOINTS"); got != "preexisting=return(prev)" {
		t.Errorf("post-restore: env=%q, want preserved value", got)
	}
}

func TestActivateRestoresUnsetWhenNoPrior(t *testing.T) {
	_ = os.Unsetenv("GOFAIL_FAILPOINTS")
	term := Term{Name: "auditWALFsync", Mode: ModeOff, Arg: ""}
	restore := Activate(term)
	restore()
	if _, ok := os.LookupEnv("GOFAIL_FAILPOINTS"); ok {
		t.Error("post-restore: env var still set; want unset")
	}
}

func TestActivateAllJoinsTerms(t *testing.T) {
	_ = os.Unsetenv("GOFAIL_FAILPOINTS")
	a := Term{Name: "auditWALFsync", Mode: ModeReturn, Arg: `"err"`}
	b := Term{Name: "dispatcherCancelMidFlight", Mode: ModeSleep, Arg: "5s"}
	restore := ActivateAll(a, b)
	got := os.Getenv("GOFAIL_FAILPOINTS")
	want := a.String() + "," + b.String()
	if got != want {
		t.Errorf("ActivateAll env = %q, want %q", got, want)
	}
	restore()
	if _, ok := os.LookupEnv("GOFAIL_FAILPOINTS"); ok {
		t.Error("post-restore: env still set")
	}
}

func TestCanaryPathResolves(t *testing.T) {
	path := canaryPath(t)
	if !strings.HasSuffix(path, "internal/audit/chain/seal.go") {
		t.Errorf("canary path = %q, want suffix internal/audit/chain/seal.go", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read canary: %v", err)
	}
	if !strings.Contains(string(data), "auditWALFsync") {
		t.Error("canary file missing auditWALFsync reference; F-2 injection drift?")
	}
}

func TestRequireGofailEnabledSkipsOnDisabledTree(t *testing.T) {
	// Drive a child T so we can observe the skip without aborting
	// the parent test. testing.T has no public Skipped() check
	// inside the same test, so we use a child via t.Run and pin
	// the documented invariant: when canary contains the literal
	// disabled marker, requireGofailEnabled MUST skip.
	canary := canaryPath(t)
	data, err := os.ReadFile(canary)
	if err != nil {
		t.Fatalf("read canary: %v", err)
	}
	const literal = "// gofail: var auditWALFsync"
	contains := strings.Contains(string(data), literal)
	subSkipped := false
	t.Run("probe", func(child *testing.T) {
		defer func() { subSkipped = child.Skipped() }()
		requireGofailEnabled(child)
	})
	if contains && !subSkipped {
		t.Error("canary contains disabled marker but probe did not Skip")
	}
	if !contains && subSkipped {
		t.Error("canary lacks disabled marker but probe Skipped (unexpected)")
	}
}
