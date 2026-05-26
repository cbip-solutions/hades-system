// SPDX-License-Identifier: MIT

// tests/compliance/inv_zen_174_extended_test.go
//
// existing surface tests with the C-8 umbrella contract.
//
// inv-zen-174 (widened, MIT-canonical 4-redundant per decisión 15):
//  1. LICENSE                MIT canonical text + "Ika el Zur" copyright
//  2. per-file SPDX headers  SPDX-License-Identifier: MIT
//  3. supporting docs        README.md + INSTALL.md + THIRD_PARTY_LICENSES.md
//  4. brew Formula           Formula/hades.rb license "MIT"
//
// inv-zen-286: LICENSE canonical content sentinels.
// inv-zen-290: composition contract — scripts/verify_license_compliance.sh
//
//	exits 0 with the canonical success banner.
//
// W4C scope extension: NOTICE file is shipped (minimal mode per
// decisión 15-optional-best-practice path); content sentinels asserted
// here so the W4C override is captured as a load-bearing gate.
//
// All assertions are repo-local (no network); the umbrella shell-out
// gate is gated by the script's executable bit.
package compliance

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func findRepoRootInvZen174(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("findRepoRootInvZen174: could not locate go.mod from %v", dir)
	return ""
}

func TestInvZen174_LicensePresent(t *testing.T) {
	root := findRepoRootInvZen174(t)
	data, err := os.ReadFile(filepath.Join(root, "LICENSE"))
	if err != nil {
		t.Fatalf("inv-zen-286: LICENSE missing: %v", err)
	}
	content := string(data)
	wantSentinels := []string{
		"MIT License",
		"Copyright (c) 2026 Ika el Zur",
		"Permission is hereby granted",
		`THE SOFTWARE IS PROVIDED "AS IS"`,
	}
	for _, s := range wantSentinels {
		if !strings.Contains(content, s) {
			t.Errorf("inv-zen-286: LICENSE missing sentinel %q", s)
		}
	}

	if strings.Contains(content, "Apache License") {
		t.Error("inv-zen-286: LICENSE contains 'Apache License' marker; decisión 15 mandates MIT")
	}
}

func TestInvZen174_FourRedundantSurfaces(t *testing.T) {
	root := findRepoRootInvZen174(t)
	cases := []struct {
		name      string
		path      string
		sentinels []string
	}{
		{
			name:      "Surface 1 LICENSE",
			path:      "LICENSE",
			sentinels: []string{"MIT License", "Ika el Zur"},
		},
		{
			name:      "Surface 3a README License section",
			path:      "README.md",
			sentinels: []string{"## License", "MIT"},
		},
		{
			name:      "Surface 3b INSTALL.md Installing HADES entrypoint",
			path:      "INSTALL.md",
			sentinels: []string{"Installing HADES"},
		},
		{
			name:      "Surface 3c THIRD_PARTY_LICENSES.md inbound inventory",
			path:      "THIRD_PARTY_LICENSES.md",
			sentinels: []string{"hermes-agent", "smacker/go-tree-sitter", "sqlite-vec"},
		},
		{
			name:      "Surface 4 Formula/hades.rb brew MIT field",
			path:      "Formula/hades.rb",
			sentinels: []string{`license "MIT"`},
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, c.path))
			if err != nil {
				t.Fatalf("inv-zen-174: surface %s missing at %s: %v", c.name, c.path, err)
			}
			content := string(data)
			for _, s := range c.sentinels {
				if !strings.Contains(content, s) {
					t.Errorf("inv-zen-174: surface %s missing sentinel %q", c.name, s)
				}
			}
		})
	}
}

func TestInvZen174_NoticeOptionalSentinels(t *testing.T) {
	root := findRepoRootInvZen174(t)
	noticePath := filepath.Join(root, "NOTICE")
	data, err := os.ReadFile(noticePath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Logf("NOTICE absent (plan default; OK under decisión 15 — MIT does not require NOTICE)")
			return
		}
		t.Fatalf("inv-zen-174: stat NOTICE: %v", err)
	}
	content := string(data)

	wantSentinels := []string{
		"HADES",
		"MIT License",
		"Apache-2.0",
		"THIRD_PARTY_LICENSES.md",
	}
	for _, s := range wantSentinels {
		if !strings.Contains(content, s) {
			t.Errorf("inv-zen-174: NOTICE present (W4C C-3 scope) but missing sentinel %q", s)
		}
	}
}

func TestInvZen290_UmbrellaScriptExitsZero(t *testing.T) {
	root := findRepoRootInvZen174(t)
	scriptPath := filepath.Join(root, "scripts", "verify_license_compliance.sh")
	if info, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("inv-zen-290: umbrella script missing at %s: %v", scriptPath, err)
	} else if info.Mode()&0o111 == 0 {
		t.Fatalf("inv-zen-290: umbrella script %s is not executable", scriptPath)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("inv-zen-290: umbrella exited non-zero: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(string(out), "ALL LICENSE-COMPLIANCE GATES PASSED") {
		t.Errorf("inv-zen-290: umbrella missing canonical success banner; got:\n%s", out)
	}
	if !strings.Contains(string(out), "MIT-canonical per decisión 15") {
		t.Errorf("inv-zen-290: umbrella missing 'MIT-canonical per decisión 15' marker; got:\n%s", out)
	}
}
