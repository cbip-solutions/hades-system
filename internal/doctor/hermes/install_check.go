// SPDX-License-Identifier: MIT
// Package hermes ships HADES design doctor checks for Hermes
// substrate verification: install + plugin-format. Each check satisfies
// the internal/doctor/check.Check interface.
//
// Boundary (invariant): hermes package consumes ONLY internal/doctor/check
// + the OS exec/PATH facilities (via the Detector seam); MUST NOT import
// internal/store.
//
// Cross-platform: exec.LookPath honours $PATH/$PATHEXT per-OS. The
// Detector seam allows tests to substitute deterministic stubs without
// shelling out, ensuring CI determinism.
package hermes

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/doctor/check"
	"github.com/cbip-solutions/hades-system/internal/doctor/fix"
)

const MinimumHermesVersion = "0.13.0"

var ErrNotFound = errors.New("hermes: binary not found on PATH")

var ErrVersionProbeFailed = errors.New("hermes: version probe failed")

type Detector interface {
	Detect(ctx context.Context) (path, version string, err error)
}

type LiveDetector struct{}

func (LiveDetector) Detect(ctx context.Context) (path, version string, err error) {
	path, err = exec.LookPath("hermes")
	if err != nil {
		return "", "", ErrNotFound
	}
	cmd := exec.CommandContext(ctx, path, "--version")
	out, err := cmd.Output()
	if err != nil {
		return path, "", fmt.Errorf("%w: %v", ErrVersionProbeFailed, err)
	}

	tokens := strings.Fields(string(out))
	for _, tok := range tokens {
		if _, perr := check.ParseSemver(tok); perr == nil {
			return path, tok, nil
		}
	}
	return path, "", fmt.Errorf("%w: no semver token in output %q", ErrVersionProbeFailed, string(out))
}

type InstallCheck struct {
	detector   Detector
	fixApplier fix.Applier
	emitter    fix.Emitter
}

type InstallCheckConfig struct {
	Detector   Detector
	FixApplier fix.Applier
	Emitter    fix.Emitter
}

func NewInstallCheck(cfg InstallCheckConfig) *InstallCheck {
	d := cfg.Detector
	if d == nil {
		d = LiveDetector{}
	}
	return &InstallCheck{
		detector:   d,
		fixApplier: cfg.FixApplier,
		emitter:    cfg.Emitter,
	}
}

func (c *InstallCheck) Name() string { return "hermes.install" }

func (c *InstallCheck) Category() check.Category { return check.CategoryPreflight }

func (c *InstallCheck) Description() string {
	return "Hermes binary presence + version ≥" + MinimumHermesVersion + " (invariant)"
}

func (c *InstallCheck) IsDestructive() bool { return false }

func (c *InstallCheck) Fix(ctx context.Context, mode check.FixMode) error {
	if c.fixApplier == nil {
		return nil
	}
	return fix.Apply(ctx, c.fixApplier, mode, c.emitter)
}

func (c *InstallCheck) Run(ctx context.Context) check.DiagnosticResult {
	d := check.DiagnosticResult{Name: c.Name()}
	path, version, err := c.detector.Detect(ctx)
	if errors.Is(err, ErrNotFound) {
		d.Status = check.StatusFail
		d.Message = "hermes binary not found on PATH"
		d.Hint = "brew install hermes-agent  (or download from https://hermes-agent.dev/install)"
		return d
	}
	if err != nil {
		d.Status = check.StatusFail
		d.Message = fmt.Sprintf("hermes --version failed: %v", err)
		d.Hint = "check `hermes --version` directly; report version-parsing issue if output non-standard"
		return d
	}
	got, perr := check.ParseSemver(version)
	if perr != nil {
		d.Status = check.StatusFail
		d.Message = fmt.Sprintf("hermes version unparseable: %q at %s", version, path)
		d.Hint = "verify hermes installation; expected 'X.Y.Z' semver"
		return d
	}

	want, mperr := check.ParseSemver(MinimumHermesVersion)
	if mperr != nil {

		d.Status = check.StatusFail
		d.Message = fmt.Sprintf("MinimumHermesVersion constant %q unparseable: %v", MinimumHermesVersion, mperr)
		return d
	}
	if check.CompareVersions(got, want) < 0 {
		d.Status = check.StatusWarn
		d.Message = fmt.Sprintf("hermes %s at %s; want ≥%s", version, path, MinimumHermesVersion)
		d.Hint = "brew upgrade hermes-agent  (HADES design requires ≥" + MinimumHermesVersion + " plugin format from stage)"
		return d
	}
	d.Status = check.StatusPass
	d.Message = fmt.Sprintf("hermes %s at %s (≥%s)", version, path, MinimumHermesVersion)
	return d
}
