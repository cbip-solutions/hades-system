// SPDX-License-Identifier: MIT
package preflight

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

const HermesMinVersion = "0.13.0"

const HermesBinary = "hermes"

type Version struct {
	Major int
	Minor int
	Patch int

	Pre string
}

func (v Version) String() string {
	out := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Pre != "" {
		out += "-" + v.Pre
	}
	return out
}

func (v Version) GreaterOrEqual(other Version) bool {
	if v.Major != other.Major {
		return v.Major > other.Major
	}
	if v.Minor != other.Minor {
		return v.Minor > other.Minor
	}
	return v.Patch >= other.Patch
}

type Hermes struct {
	lookPath   func(string) (string, error)
	runVersion func(ctx context.Context, bin string) (string, error)
}

func NewHermesCheck() *Hermes {
	return &Hermes{
		lookPath: exec.LookPath,
		runVersion: func(ctx context.Context, bin string) (string, error) {
			out, err := exec.CommandContext(ctx, bin, "--version").CombinedOutput()
			if err != nil {
				return "", fmt.Errorf("hermes --version: %w", err)
			}
			return string(out), nil
		},
	}
}

func NewHermesCheckForTest(lookPath func(string) (string, error), runVersion func(context.Context, string) (string, error)) *Hermes {
	return &Hermes{lookPath: lookPath, runVersion: runVersion}
}

func (c *Hermes) Name() string { return "hermes" }

func (c *Hermes) Run(ctx context.Context) Result {
	if c.lookPath == nil {
		return Result{
			Name:     c.Name(),
			Status:   StatusFail,
			Summary:  "internal: HermesCheck.lookPath nil",
			Details:  "programmer error: NewHermesCheckForTest received nil lookPath",
			ExitCode: 3,
		}
	}
	bin, err := c.lookPath(HermesBinary)
	if err != nil {
		return Result{
			Name:            c.Name(),
			Status:          StatusFail,
			Summary:         "Hermes Agent binary not found in PATH",
			Details:         "inv-hades-175 requires Hermes Agent >=" + HermesMinVersion + ". The binary `" + HermesBinary + "` was not found in $PATH.",
			RemediationHint: "Install Hermes Agent: `brew install hermes-agent` (macOS) or follow https://hermes-agent.dev/install (other platforms). Then re-run.",
			ExitCode:        3,
		}
	}
	if c.runVersion == nil {
		return Result{
			Name:     c.Name(),
			Status:   StatusFail,
			Summary:  "internal: HermesCheck.runVersion nil",
			Details:  "programmer error: NewHermesCheckForTest received nil runVersion",
			ExitCode: 3,
		}
	}
	rawVersion, err := c.runVersion(ctx, bin)
	if err != nil {
		return Result{
			Name:            c.Name(),
			Status:          StatusFail,
			Summary:         "Hermes Agent --version invocation failed",
			Details:         fmt.Sprintf("`%s --version` returned error: %v", bin, err),
			RemediationHint: "Verify Hermes binary integrity: `which hermes && hermes --version`. Reinstall if corrupt.",
			ExitCode:        3,
		}
	}
	got := parseVersionLine(rawVersion)
	if got == "" {
		return Result{
			Name:     c.Name(),
			Status:   StatusFail,
			Summary:  "Hermes Agent --version output not parseable",
			Details:  fmt.Sprintf("Could not extract semver from output: %q. Expected MAJOR.MINOR.PATCH.", rawVersion),
			ExitCode: 3,
		}
	}
	if !compareVersionGE(got, HermesMinVersion) {
		return Result{
			Name:            c.Name(),
			Status:          StatusFail,
			Summary:         fmt.Sprintf("Hermes Agent %s < required %s", got, HermesMinVersion),
			Details:         "inv-hades-175 enforces Hermes Agent >=" + HermesMinVersion + ". Found older version.",
			RemediationHint: "Upgrade Hermes Agent: `brew upgrade hermes-agent` (macOS) or follow https://hermes-agent.dev/install.",
			ExitCode:        3,
		}
	}
	return Result{
		Name:    c.Name(),
		Status:  StatusPass,
		Summary: fmt.Sprintf("Hermes Agent %s present at %s", got, bin),
	}
}

var versionRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

func parseVersionLine(raw string) string {
	raw = strings.TrimSpace(raw)
	if m := versionRe.FindString(raw); m != "" {
		return m
	}
	return ""
}

func compareVersionGE(got, min string) bool {
	gv, ok1 := parseSemverString(got)
	mv, ok2 := parseSemverString(min)
	if !ok1 || !ok2 {
		return false
	}
	return gv.GreaterOrEqual(mv)
}

func parseSemverString(v string) (Version, bool) {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) != 3 {
		return Version{}, false
	}
	var out Version
	for i, p := range parts {
		if p == "" {
			return Version{}, false
		}
		if p[0] == '-' || p[0] == '+' {
			return Version{}, false
		}

		if i == 2 {
			if idx := strings.IndexAny(p, "-+"); idx >= 0 {
				out.Pre = p[idx+1:]
				p = p[:idx]
			}
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return Version{}, false
		}
		switch i {
		case 0:
			out.Major = n
		case 1:
			out.Minor = n
		case 2:
			out.Patch = n
		}
	}
	return out, true
}

var _ Check = (*Hermes)(nil)
