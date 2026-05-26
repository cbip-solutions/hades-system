//go:build chaos

// SPDX-License-Identifier: MIT

package failpoints

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

type Mode int

const (
	ModeUnknown Mode = iota

	ModeReturn

	ModeSleep

	ModePanic

	ModeOff
)

func (m Mode) String() string {
	switch m {
	case ModeReturn:
		return "return"
	case ModeSleep:
		return "sleep"
	case ModePanic:
		return "panic"
	case ModeOff:
		return "off"
	default:
		return "unknown"
	}
}

// Term is a parsed GOFAIL_FAILPOINTS term — one `<name>=<mode>(<arg>)`
// entry. Name MUST match an injected gofail variable across the 15
// canonical sites (see Makefile GOFAIL_PKGS); ParseTerm validates the
// shape but cannot validate the name resolves to a real site in the
// rewritten tree (that surfaces at runtime activation).
type Term struct {
	Name string
	Mode Mode
	Arg  string
}

func (t Term) String() string {
	return fmt.Sprintf("%s=%s(%s)", t.Name, t.Mode, t.Arg)
}

var termRe = regexp.MustCompile(`^([a-zA-Z0-9_\-]+)=(return|sleep|panic|off)\(([^)]*)\)$`)

func ParseTerm(s string) (Term, error) {
	if !strings.Contains(s, "=") {
		return Term{}, fmt.Errorf("ParseTerm: missing '='; got %q", s)
	}
	m := termRe.FindStringSubmatch(s)
	if m == nil {
		return Term{}, fmt.Errorf("ParseTerm: grammar mismatch; got %q", s)
	}
	var mode Mode
	switch m[2] {
	case "return":
		mode = ModeReturn
	case "sleep":
		mode = ModeSleep
	case "panic":
		mode = ModePanic
	case "off":
		mode = ModeOff
	default:
		return Term{}, fmt.Errorf("ParseTerm: unknown mode %q", m[2])
	}
	return Term{Name: m[1], Mode: mode, Arg: m[3]}, nil
}

func Activate(term Term) func() {
	prev, hadPrev := os.LookupEnv("GOFAIL_FAILPOINTS")
	_ = os.Setenv("GOFAIL_FAILPOINTS", term.String())
	return func() {
		if hadPrev {
			_ = os.Setenv("GOFAIL_FAILPOINTS", prev)
		} else {
			_ = os.Unsetenv("GOFAIL_FAILPOINTS")
		}
	}
}

func ActivateAll(terms ...Term) func() {
	parts := make([]string, len(terms))
	for i, t := range terms {
		parts[i] = t.String()
	}
	joined := strings.Join(parts, ",")
	prev, hadPrev := os.LookupEnv("GOFAIL_FAILPOINTS")
	_ = os.Setenv("GOFAIL_FAILPOINTS", joined)
	return func() {
		if hadPrev {
			_ = os.Setenv("GOFAIL_FAILPOINTS", prev)
		} else {
			_ = os.Unsetenv("GOFAIL_FAILPOINTS")
		}
	}
}

func requireGofailEnabled(t *testing.T) {
	t.Helper()
	canary := canaryPath(t)
	data, err := os.ReadFile(canary)
	if err != nil {
		t.Skipf("gofail canary probe: cannot read %s: %v", canary, err)
	}
	if strings.Contains(string(data), "// gofail: var auditWALFsync") {
		t.Skip("gofail-disabled source tree (canonical commit state); run `make gofail-enable` to activate")
	}
}

func canaryPath(t *testing.T) string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("canaryPath: runtime.Caller failed")
	}

	root := strings.TrimSuffix(here, "tests/chaos/failpoints/activation.go")
	return root + "internal/audit/chain/seal.go"
}

var ErrSkipped = errors.New("failpoints: gofail-disabled tree")
