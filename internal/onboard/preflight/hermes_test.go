package preflight

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCompareVersionGreaterEqual(t *testing.T) {
	cases := []struct {
		got, min string
		ok       bool
	}{
		{"0.13.0", "0.13.0", true},
		{"0.13.1", "0.13.0", true},
		{"0.14.0", "0.13.0", true},
		{"1.0.0", "0.13.0", true},
		{"0.12.99", "0.13.0", false},
		{"0.12.0", "0.13.0", false},
		{"0.0.0", "0.13.0", false},

		{"0.13.0-beta3", "0.13.0", true},
	}
	for _, tc := range cases {
		got := compareVersionGE(tc.got, tc.min)
		if got != tc.ok {
			t.Errorf("compareVersionGE(%s, %s) = %v, want %v", tc.got, tc.min, got, tc.ok)
		}
	}
}

func TestCompareVersionGEMalformed(t *testing.T) {
	if compareVersionGE("garbage", "0.13.0") {
		t.Error("compareVersionGE(garbage, 0.13.0) returned true; want false on malformed input")
	}
	if compareVersionGE("0.13.0", "also-garbage") {
		t.Error("compareVersionGE returned true with malformed minimum")
	}
	if compareVersionGE("", "0.13.0") {
		t.Error("compareVersionGE empty got returned true")
	}
	if compareVersionGE("0.13", "0.13.0") {
		t.Error("compareVersionGE 2-segment got returned true")
	}
	if compareVersionGE("-1.0.0", "0.13.0") {
		t.Error("compareVersionGE sign-prefix returned true")
	}
	if compareVersionGE("+1.0.0", "0.13.0") {
		t.Error("compareVersionGE plus-prefix returned true")
	}
	if compareVersionGE("a.b.c", "0.13.0") {
		t.Error("compareVersionGE non-numeric returned true")
	}
}

func TestParseVersionLine(t *testing.T) {
	cases := []struct {
		raw, want string
	}{
		{"hermes 0.13.0", "0.13.0"},
		{"hermes-agent v0.14.2", "0.14.2"},
		{"Hermes Agent 1.0.0-beta3", "1.0.0"},
		{"0.13.0", "0.13.0"},
		{"  0.13.0  ", "0.13.0"},
		{"no version here", ""},
		{"", ""},

		{"hermes-agent (1.2.3) revision abc", "1.2.3"},
	}
	for _, tc := range cases {
		got := parseVersionLine(tc.raw)
		if got != tc.want {
			t.Errorf("parseVersionLine(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestParseSemverString(t *testing.T) {
	cases := []struct {
		in   string
		want Version
		ok   bool
	}{
		{"0.13.0", Version{Major: 0, Minor: 13, Patch: 0}, true},
		{"1.2.3", Version{Major: 1, Minor: 2, Patch: 3}, true},
		{"0.13.0-beta3", Version{Major: 0, Minor: 13, Patch: 0, Pre: "beta3"}, true},
		{"1.0.0+build5", Version{Major: 1, Minor: 0, Patch: 0, Pre: "build5"}, true},

		{"", Version{}, false},
		{"0.13", Version{}, false},
		{"a.b.c", Version{}, false},
		{"-1.0.0", Version{}, false},
		{"0..0", Version{}, false},
	}
	for _, tc := range cases {
		got, ok := parseSemverString(tc.in)
		if ok != tc.ok {
			t.Errorf("parseSemverString(%q) ok = %v, want %v", tc.in, ok, tc.ok)
			continue
		}
		if ok && got != tc.want {
			t.Errorf("parseSemverString(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

func TestVersionStringRoundtrip(t *testing.T) {
	cases := []struct {
		v    Version
		want string
	}{
		{Version{Major: 0, Minor: 13, Patch: 0}, "0.13.0"},
		{Version{Major: 1, Minor: 2, Patch: 3}, "1.2.3"},
		{Version{Major: 0, Minor: 13, Patch: 0, Pre: "beta3"}, "0.13.0-beta3"},
	}
	for _, tc := range cases {
		if got := tc.v.String(); got != tc.want {
			t.Errorf("Version{%+v}.String() = %q, want %q", tc.v, got, tc.want)
		}
	}
}

func TestVersionGreaterOrEqual(t *testing.T) {
	a := Version{Major: 0, Minor: 13, Patch: 1}
	b := Version{Major: 0, Minor: 13, Patch: 0}
	if !a.GreaterOrEqual(b) {
		t.Error("0.13.1 GreaterOrEqual 0.13.0: false, want true")
	}
	if b.GreaterOrEqual(a) {
		t.Error("0.13.0 GreaterOrEqual 0.13.1: true, want false")
	}

	if !a.GreaterOrEqual(a) {
		t.Error("equal versions: false, want true")
	}

	c := Version{Major: 1, Minor: 0, Patch: 0}
	if !c.GreaterOrEqual(a) {
		t.Error("1.0.0 GreaterOrEqual 0.13.1: false, want true")
	}
}

func TestCheckHermesMissingExits3(t *testing.T) {
	c := &Hermes{
		lookPath: func(_ string) (string, error) { return "", errExecNotFound },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := c.Run(ctx)
	if r.Status != StatusFail {
		t.Errorf("Hermes missing: got Status=%v, want StatusFail", r.Status)
	}
	if r.ExitCode != 3 {
		t.Errorf("Hermes missing: ExitCode = %d, want 3 (preflight failure)", r.ExitCode)
	}
	if r.RemediationHint == "" {
		t.Errorf("Hermes missing: RemediationHint empty; expected install URL or brew hint")
	}
	if r.Name != "hermes" {
		t.Errorf("Hermes missing: Name = %q, want hermes", r.Name)
	}
}

func TestCheckHermesPresentValidVersion(t *testing.T) {
	c := &Hermes{
		lookPath: func(_ string) (string, error) { return "/usr/local/bin/hermes", nil },
		runVersion: func(_ context.Context, _ string) (string, error) {
			return "hermes 0.13.0", nil
		},
	}
	r := c.Run(context.Background())
	if r.Status != StatusPass {
		t.Errorf("Hermes valid: Status = %v, want StatusPass; result=%+v", r.Status, r)
	}
	if r.ExitCode != 0 {
		t.Errorf("Hermes valid: ExitCode = %d, want 0", r.ExitCode)
	}
}

func TestCheckHermesPresentTooOld(t *testing.T) {
	c := &Hermes{
		lookPath: func(_ string) (string, error) { return "/usr/local/bin/hermes", nil },
		runVersion: func(_ context.Context, _ string) (string, error) {
			return "hermes 0.12.0", nil
		},
	}
	r := c.Run(context.Background())
	if r.Status != StatusFail {
		t.Errorf("Hermes too old: Status = %v, want StatusFail", r.Status)
	}
	if r.ExitCode != 3 {
		t.Errorf("Hermes too old: ExitCode = %d, want 3", r.ExitCode)
	}
	if r.RemediationHint == "" {
		t.Errorf("Hermes too old: RemediationHint empty")
	}
}

func TestCheckHermesPresentNewer(t *testing.T) {
	c := &Hermes{
		lookPath: func(_ string) (string, error) { return "/usr/local/bin/hermes", nil },
		runVersion: func(_ context.Context, _ string) (string, error) {
			return "hermes-agent v1.0.0", nil
		},
	}
	r := c.Run(context.Background())
	if r.Status != StatusPass {
		t.Errorf("Hermes newer: Status = %v, want StatusPass", r.Status)
	}
}

func TestCheckHermesRunVersionFails(t *testing.T) {
	c := &Hermes{
		lookPath: func(_ string) (string, error) { return "/usr/local/bin/hermes", nil },
		runVersion: func(_ context.Context, _ string) (string, error) {
			return "", errors.New("exec failed: signal 9")
		},
	}
	r := c.Run(context.Background())
	if r.Status != StatusFail {
		t.Errorf("Hermes exec fail: Status = %v, want StatusFail", r.Status)
	}
	if r.ExitCode != 3 {
		t.Errorf("Hermes exec fail: ExitCode = %d, want 3", r.ExitCode)
	}
}

func TestCheckHermesUnparseable(t *testing.T) {
	c := &Hermes{
		lookPath: func(_ string) (string, error) { return "/usr/local/bin/hermes", nil },
		runVersion: func(_ context.Context, _ string) (string, error) {
			return "garbage output no version", nil
		},
	}
	r := c.Run(context.Background())
	if r.Status != StatusFail {
		t.Errorf("Hermes unparseable: Status = %v, want StatusFail", r.Status)
	}
	if r.ExitCode != 3 {
		t.Errorf("Hermes unparseable: ExitCode = %d, want 3", r.ExitCode)
	}
}

func TestCheckHermesNilLookPath(t *testing.T) {
	c := &Hermes{lookPath: nil}
	r := c.Run(context.Background())
	if r.Status != StatusFail {
		t.Errorf("nil lookPath: Status = %v, want StatusFail (programmer error)", r.Status)
	}
}

func TestCheckHermesNilRunVersion(t *testing.T) {
	c := &Hermes{
		lookPath:   func(_ string) (string, error) { return "/usr/local/bin/hermes", nil },
		runVersion: nil,
	}
	r := c.Run(context.Background())
	if r.Status != StatusFail {
		t.Errorf("nil runVersion: Status = %v, want StatusFail (programmer error)", r.Status)
	}
}

func TestNewHermesCheckForTestExport(t *testing.T) {

	c := NewHermesCheckForTest(
		func(_ string) (string, error) { return "", errExecNotFound },
		nil,
	)
	if c == nil {
		t.Fatal("NewHermesCheckForTest returned nil")
	}
	r := c.Run(context.Background())
	if r.Status != StatusFail {
		t.Errorf("exported test ctor missing: Status = %v, want StatusFail", r.Status)
	}
}

func TestHermesCheckProductionConstructor(t *testing.T) {

	c := NewHermesCheck()
	if c == nil {
		t.Fatal("NewHermesCheck returned nil")
	}
	if c.Name() != "hermes" {
		t.Errorf("Name = %q, want hermes", c.Name())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_ = c.Run(ctx)
}
