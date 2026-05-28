// SPDX-License-Identifier: MIT
package preflight

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type PluginFormatCheck struct {
	roots []string
}

func NewPluginFormatCheck() *PluginFormatCheck {
	return &PluginFormatCheck{roots: defaultPluginScanRoots()}
}

func NewPluginFormatCheckForTest(roots []string) *PluginFormatCheck {
	return &PluginFormatCheck{roots: roots}
}

func defaultPluginScanRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	return []string{
		filepath.Join(home, "local agent config"),
		filepath.Join(home, ".openclaude"),
		filepath.Join(cwd, ".hades"),
	}
}

func (c *PluginFormatCheck) Name() string { return "plugin_format" }

func (c *PluginFormatCheck) Run(ctx context.Context) Result {
	if migrationAcknowledged() {
		return Result{
			Name:    c.Name(),
			Status:  StatusPass,
			Summary: "migrate claude-code artifact present; CC install treated as acknowledged coexistence",
		}
	}
	for _, root := range c.roots {
		if err := ctx.Err(); err != nil {
			return Result{
				Name:     c.Name(),
				Status:   StatusFail,
				Summary:  "context cancelled mid-scan",
				Details:  err.Error(),
				ExitCode: 3,
			}
		}
		if found, kind, evidence := scanForRemnants(root); found {
			return Result{
				Name:            c.Name(),
				Status:          StatusFail,
				Summary:         fmt.Sprintf("%s format remnant detected at %s", kind, root),
				Details:         fmt.Sprintf("invariant halts on legacy plugin-format remnants. Evidence: %s. stage (commit 8bd84187) made the Hermes plugin format canonical; legacy CC/OpenClaude installs require migration before HADES onboarding.", evidence),
				RemediationHint: hintForKind(kind),
				ExitCode:        3,
			}
		}
	}
	return Result{
		Name:    c.Name(),
		Status:  StatusPass,
		Summary: "No CC/OpenClaude format remnants detected",
	}
}

func migrationAcknowledged() bool {
	cfgRoot := os.Getenv("XDG_CONFIG_HOME")
	if cfgRoot == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		cfgRoot = filepath.Join(home, ".config")
	}
	artifact := filepath.Join(cfgRoot, "hades-system", "doctrines", "imported-from-claude-code.toml")
	_, err := os.Stat(artifact)
	return err == nil
}

var ccPluginMarkers = []string{
	"plugin.js",
	"manifest.json",
}

const ccPluginHooksMarker = "hooks/hooks.json"

var ccRootMarkers = []string{
	"settings.json",
	"skills",
	"commands",
	"hooks",
	"memory",
}

func scanForRemnants(root string) (bool, string, string) {
	if root == "" {
		return false, "", ""
	}
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		return false, "", ""
	}

	if entries, err := os.ReadDir(root); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			sub := filepath.Join(root, e.Name())
			for _, m := range ccPluginMarkers {
				if _, err := os.Stat(filepath.Join(sub, m)); err == nil {
					return true, "claude-code", filepath.Join(sub, m)
				}
			}
			if _, err := os.Stat(filepath.Join(sub, ccPluginHooksMarker)); err == nil {
				return true, "claude-code", filepath.Join(sub, ccPluginHooksMarker)
			}

			if e.Name() == "plugins" {
				if nested, err := os.ReadDir(sub); err == nil {
					for _, n := range nested {
						if !n.IsDir() {
							continue
						}
						np := filepath.Join(sub, n.Name())
						for _, m := range ccPluginMarkers {
							if _, err := os.Stat(filepath.Join(np, m)); err == nil {
								return true, "claude-code", filepath.Join(np, m)
							}
						}
						if _, err := os.Stat(filepath.Join(np, ccPluginHooksMarker)); err == nil {
							return true, "claude-code", filepath.Join(np, ccPluginHooksMarker)
						}
					}
				}
			}
		}
	}

	for _, m := range ccRootMarkers {
		path := filepath.Join(root, m)
		if _, err := os.Stat(path); err == nil {
			return true, "claude-code", path
		}
	}

	if _, err := os.Stat(filepath.Join(root, "openclaude.toml")); err == nil {
		return true, "openclaude", filepath.Join(root, "openclaude.toml")
	}
	if strings.HasSuffix(root, ".openclaude") || strings.HasSuffix(root, ".openclaude"+string(filepath.Separator)) {
		entries, _ := os.ReadDir(root)
		if len(entries) > 0 {
			return true, "openclaude", root
		}
	}

	return false, "", ""
}

func hintForKind(kind string) string {
	switch kind {
	case "claude-code":
		return "Run `hades migrate claude-code --dry-run` to preview migration; then `hades migrate claude-code --apply` to import. Or move the legacy install out of the scan paths (local agent memory/, ./.hades/) if intentionally preserved offline."
	case "openclaude":
		return "Run `hermes claw migrate` (Hermes-provided OpenClaude importer) to convert to Hermes plugin format. Or move ~/.openclaude/ out of the scan paths."
	default:
		return "Remove or relocate the detected remnant; consult docs/operations/migrate.md."
	}
}

var _ Check = (*PluginFormatCheck)(nil)
