// SPDX-License-Identifier: MIT

package compliance_test

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func repoRootInvZen295(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("inv-zen-295: getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("inv-zen-295: go.mod not found walking up from %s", dir)
		}
		dir = parent
	}
}

type invZen295Sign struct {
	ID        string   `yaml:"id"`
	Cmd       string   `yaml:"cmd"`
	Args      []string `yaml:"args"`
	IDs       []string `yaml:"ids"`
	Artifacts string   `yaml:"artifacts"`
	If        string   `yaml:"if"`
	Signature string   `yaml:"signature"`
}

type invZen295Config struct {
	Signs []invZen295Sign `yaml:"signs"`
}

func TestInvZen295_GoreleaserMacosAdHocSignBlock(t *testing.T) {
	root := repoRootInvZen295(t)
	data, err := os.ReadFile(filepath.Join(root, ".goreleaser.yml"))
	if err != nil {
		t.Fatalf("inv-zen-295 VIOLATED: .goreleaser.yml missing: %v", err)
	}
	var cfg invZen295Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("inv-zen-295: parse: %v", err)
	}
	var macSign *invZen295Sign
	for i := range cfg.Signs {
		if cfg.Signs[i].ID == "macos-ad-hoc" {
			macSign = &cfg.Signs[i]
			break
		}
	}
	if macSign == nil {
		t.Fatal("inv-zen-295 VIOLATED: .goreleaser.yml signs: missing 'macos-ad-hoc' entry")
	}
	if macSign.Cmd != "codesign" {
		t.Errorf("inv-zen-295 VIOLATED: macos-ad-hoc cmd=%q, want 'codesign'", macSign.Cmd)
	}
	if macSign.Artifacts != "binary" {
		t.Errorf("inv-zen-295 VIOLATED: macos-ad-hoc artifacts=%q, want 'binary' "+
			"(codesign needs Mach-O input, not archive)", macSign.Artifacts)
	}
	if macSign.If == "" {
		t.Errorf("inv-zen-295 VIOLATED: macos-ad-hoc missing if: filter (would attempt codesign on linux)")
	}

	requiredArgs := map[string]bool{
		"--sign":            false,
		"-":                 false,
		"--force":           false,
		"--options=runtime": false,
		"${artifact}":       false,
	}
	for _, arg := range macSign.Args {
		if _, ok := requiredArgs[arg]; ok {
			requiredArgs[arg] = true
		}
	}
	for arg, found := range requiredArgs {
		if !found {
			t.Errorf("inv-zen-295 VIOLATED: macos-ad-hoc args missing %q in %v",
				arg, macSign.Args)
		}
	}

	wantIDs := map[string]bool{"zen": false, "zen-swarm-ctld": false}
	for _, id := range macSign.IDs {
		if _, ok := wantIDs[id]; ok {
			wantIDs[id] = true
		}
	}
	for id, found := range wantIDs {
		if !found {
			t.Errorf("inv-zen-295 VIOLATED: macos-ad-hoc ids missing %q", id)
		}
	}
}

func TestInvZen295_VerifyHelperPresentAndExecutable(t *testing.T) {
	root := repoRootInvZen295(t)
	helperPath := filepath.Join(root, "scripts", "release-gates", "verify_macos_codesign.sh")
	info, err := os.Stat(helperPath)
	if err != nil {
		t.Fatalf("inv-zen-295 VIOLATED: verify_macos_codesign.sh missing: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("inv-zen-295 VIOLATED: verify_macos_codesign.sh not executable; mode=%v",
			info.Mode())
	}
}
