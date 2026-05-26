// SPDX-License-Identifier: MIT

package compliance_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type invZen296CosignConfig struct {
	Signs []struct {
		ID        string   `yaml:"id"`
		Cmd       string   `yaml:"cmd"`
		Args      []string `yaml:"args"`
		Artifacts string   `yaml:"artifacts"`
		Env       []string `yaml:"env"`
	} `yaml:"signs"`
}

func TestInvZen296_VerifyCosignHelperPresentAndExecutable(t *testing.T) {
	root := repoRootInvZen296(t)
	helperPath := filepath.Join(root, "scripts", "release-gates", "verify_cosign_signature.sh")
	info, err := os.Stat(helperPath)
	if err != nil {
		t.Fatalf("inv-zen-296 VIOLATED: verify_cosign_signature.sh missing: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("inv-zen-296 VIOLATED: verify_cosign_signature.sh not executable; mode=%v",
			info.Mode())
	}
	data, err := os.ReadFile(helperPath)
	if err != nil {
		t.Fatalf("read helper: %v", err)
	}
	text := string(data)
	requiredPins := []string{
		"certificate-identity-regexp",
		"certificate-oidc-issuer",
		"token.actions.githubusercontent.com",
		"hades-system",
	}
	for _, pin := range requiredPins {
		if !strings.Contains(text, pin) {
			t.Errorf("inv-zen-296 VIOLATED: verify_cosign_signature.sh missing pin %q",
				pin)
		}
	}
}

func TestInvZen296_GoreleaserCosignKeylessBlocks(t *testing.T) {
	root := repoRootInvZen296(t)
	data, err := os.ReadFile(filepath.Join(root, ".goreleaser.yml"))
	if err != nil {
		t.Fatalf("inv-zen-296 VIOLATED: .goreleaser.yml missing: %v", err)
	}
	var cfg invZen296CosignConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("inv-zen-296: parse: %v", err)
	}
	wantBlocks := map[string]string{
		"cosign-keyless":          "archive",
		"cosign-keyless-checksum": "checksum",
		"cosign-keyless-sbom":     "sbom",
	}
	gotBlocks := make(map[string]bool)
	for _, s := range cfg.Signs {
		wantArtifacts, ok := wantBlocks[s.ID]
		if !ok {
			continue
		}
		gotBlocks[s.ID] = true
		if s.Cmd != "cosign" {
			t.Errorf("inv-zen-296 VIOLATED: %s cmd=%q, want 'cosign'", s.ID, s.Cmd)
		}
		if s.Artifacts != wantArtifacts {
			t.Errorf("inv-zen-296 VIOLATED: %s artifacts=%q, want %q",
				s.ID, s.Artifacts, wantArtifacts)
		}
		requiredArgs := map[string]bool{
			"sign-blob":            false,
			"--yes":                false,
			"--output-signature":   false,
			"--output-certificate": false,
			"${signature}":         false,
			"${certificate}":       false,
			"${artifact}":          false,
		}
		for _, arg := range s.Args {
			if _, ok := requiredArgs[arg]; ok {
				requiredArgs[arg] = true
			}
		}
		for arg, found := range requiredArgs {
			if !found {
				t.Errorf("inv-zen-296 VIOLATED: %s args missing %q in %v",
					s.ID, arg, s.Args)
			}
		}
		var foundEnv bool
		for _, e := range s.Env {
			if e == "COSIGN_EXPERIMENTAL=1" {
				foundEnv = true
			}
		}
		if !foundEnv {
			t.Errorf("inv-zen-296 VIOLATED: %s env missing COSIGN_EXPERIMENTAL=1", s.ID)
		}
	}
	for id := range wantBlocks {
		if !gotBlocks[id] {
			t.Errorf("inv-zen-296 VIOLATED: .goreleaser.yml signs: missing %q block", id)
		}
	}
}

func TestInvZen296_CosignKeylessNotOverScopedToNFPM(t *testing.T) {
	root := repoRootInvZen296(t)
	data, err := os.ReadFile(filepath.Join(root, ".goreleaser.yml"))
	if err != nil {
		t.Fatalf("inv-zen-296: %v", err)
	}
	var cfg invZen296CosignConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("inv-zen-296: parse: %v", err)
	}
	for _, s := range cfg.Signs {
		if strings.HasPrefix(s.ID, "cosign-") && s.Artifacts == "all" {
			t.Errorf("inv-zen-296 VIOLATED: cosign sign block %q has artifacts: all "+
				"(would over-sign nfpm packages; .deb/.rpm have their own GPG standard)", s.ID)
		}
	}
}
