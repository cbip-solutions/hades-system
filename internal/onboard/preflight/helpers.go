// SPDX-License-Identifier: MIT
package preflight

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func CheckHermesInstalled(ctx context.Context) error {
	r := NewHermesCheck().Run(ctx)
	if r.Status != StatusPass {
		return fmt.Errorf("hermes preflight: %s", errorSummary(r))
	}
	return nil
}

func CheckBashInstalled(_ context.Context) error {
	if _, err := exec.LookPath("bash"); err != nil {
		return fmt.Errorf("bash preflight: bash binary not found on PATH (required for template hooks): %w", err)
	}
	return nil
}

func CheckPluginFormatRemnants(ctx context.Context, dirs ...string) error {
	var c *PluginFormatCheck
	if len(dirs) == 0 {
		c = NewPluginFormatCheck()
	} else {
		c = NewPluginFormatCheckForTest(dirs)
	}
	r := c.Run(ctx)
	if r.Status != StatusPass {
		return fmt.Errorf("plugin format preflight: %s", errorSummary(r))
	}
	return nil
}

func HermesCheck(ctx context.Context) (ok bool, version string, err error) {
	return hermesCheckWith(ctx, NewHermesCheck())
}

func hermesCheckWith(ctx context.Context, c *Hermes) (ok bool, version string, err error) {
	r := c.Run(ctx)
	switch r.Status {
	case StatusPass:

		v := parseVersionLine(r.Summary)
		return true, v, nil
	case StatusFail:

		if strings.Contains(r.Summary, "binary not found") {
			return false, "", nil
		}

		if strings.HasPrefix(r.Summary, "Hermes Agent ") && strings.Contains(r.Summary, "< required") {
			v := parseVersionLine(r.Summary)
			return false, v, nil
		}
		return false, "", fmt.Errorf("hermes preflight: %s", errorSummary(r))
	default:

		return false, "", fmt.Errorf("hermes preflight: unexpected status %s", r.Status)
	}
}

func CCDetect() (present bool, configRoot string, err error) {
	home, err := userHomeDir()
	if err != nil {
		return false, "", err
	}
	if home == "" {
		return false, "", errors.New("CCDetect: HOME unset and os.UserHomeDir empty")
	}
	p := filepath.Join(home, "local agent config")
	info, statErr := os.Stat(p)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return false, "", nil
		}
		return false, "", fmt.Errorf("CCDetect stat: %w", statErr)
	}
	if !info.IsDir() {
		return false, "", nil
	}

	for _, child := range []string{"settings.json", "commands", "skills"} {
		if _, err := os.Stat(filepath.Join(p, child)); err == nil {
			return true, p, nil
		}
	}
	return false, "", nil
}

func HermesVersion() (*Version, error) {
	bin, err := exec.LookPath(HermesBinary)
	if err != nil {
		return nil, fmt.Errorf("HermesVersion: %w", err)
	}
	out, err := exec.Command(bin, "--version").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("HermesVersion exec: %w", err)
	}
	got := parseVersionLine(string(out))
	if got == "" {
		return nil, fmt.Errorf("HermesVersion: unparseable --version output: %q", string(out))
	}
	v, ok := parseSemverString(got)
	if !ok {
		return nil, fmt.Errorf("HermesVersion: bad semver %q from --version", got)
	}
	return &v, nil
}

func userHomeDir() (string, error) {
	if h := os.Getenv("HOME"); h != "" {
		return h, nil
	}
	return os.UserHomeDir()
}

func errorSummary(r Result) string {
	if r.Summary != "" {
		return r.Summary
	}
	if r.Details != "" {
		return r.Details
	}
	return r.Status.String()
}
