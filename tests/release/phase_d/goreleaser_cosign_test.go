// SPDX-License-Identifier: MIT

package phase_d_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type cosignSignsConfig struct {
	Signs []struct {
		ID        string   `yaml:"id"`
		Cmd       string   `yaml:"cmd"`
		Args      []string `yaml:"args"`
		Artifacts string   `yaml:"artifacts"`
		Env       []string `yaml:"env"`
	} `yaml:"signs"`
}

func TestCosignSignBlobBlocksDeclared(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".goreleaser.yml"))
	if err != nil {
		t.Fatalf(".goreleaser.yml missing: %v", err)
	}
	var cfg cosignSignsConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse: %v", err)
	}
	wantBlocks := map[string]string{
		"cosign-keyless":          "archive",
		"cosign-keyless-checksum": "checksum",
		"cosign-keyless-sbom":     "sbom",
	}
	gotBlocks := make(map[string]string)
	for _, s := range cfg.Signs {
		if want, ok := wantBlocks[s.ID]; ok {
			gotBlocks[s.ID] = s.Artifacts
			if s.Cmd != "cosign" {
				t.Errorf("inv-zen-296 VIOLATED: %s cmd=%q, want 'cosign'", s.ID, s.Cmd)
			}
			if s.Artifacts != want {
				t.Errorf("inv-zen-296 VIOLATED: %s artifacts=%q, want %q",
					s.ID, s.Artifacts, want)
			}

			required := map[string]bool{
				"sign-blob":            false,
				"--yes":                false,
				"${signature}":         false,
				"${certificate}":       false,
				"${artifact}":          false,
				"--output-signature":   false,
				"--output-certificate": false,
			}
			for _, arg := range s.Args {
				if _, ok := required[arg]; ok {
					required[arg] = true
				}
			}
			for arg, found := range required {
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
	}
	for id := range wantBlocks {
		if _, ok := gotBlocks[id]; !ok {
			t.Errorf("inv-zen-296 VIOLATED: signs: missing %q block", id)
		}
	}
}

func TestCosignSignBlobCoverageSnapshot(t *testing.T) {
	if testing.Short() {
		t.Skip("requires goreleaser + cosign; skip in -short mode")
	}
	if os.Getenv("GORELEASER_COSIGN_TEST") != "1" {
		t.Skip("set GORELEASER_COSIGN_TEST=1 to run the cosign snapshot (slow + requires keyless)")
	}
	if _, err := exec.LookPath("goreleaser"); err != nil {
		t.Skip("goreleaser not in PATH")
	}
	if _, err := exec.LookPath("cosign"); err != nil {
		t.Skip("cosign not in PATH")
	}
	root := repoRoot(t)
	cmd := exec.Command("goreleaser", "release",
		"--snapshot", "--clean",
		"--skip=publish",
		"--skip=docker",
		"--skip=sign=macos-ad-hoc",
	)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"GORELEASER_CURRENT_TAG=v0.0.0-test",
		"COSIGN_EXPERIMENTAL=1",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("goreleaser snapshot (cosign only): %v\noutput:\n%s", err, out)
	}
	distDir := filepath.Join(root, "dist")
	sigs, _ := filepath.Glob(filepath.Join(distDir, "*.sig"))
	pems, _ := filepath.Glob(filepath.Join(distDir, "*.pem"))
	if len(sigs) != len(pems) {
		t.Errorf("inv-zen-296 VIOLATED: sigstore file pair count mismatch: %d .sig / %d .pem",
			len(sigs), len(pems))
	}
	if len(sigs) < 7 {
		t.Errorf("inv-zen-296 VIOLATED: expected >=7 sigstore signature pairs "+
			"(3 archives + 1 checksums + 3 SBOMs); got %d", len(sigs))
	}

	for _, sig := range sigs {
		base := sig[:len(sig)-len(".sig")]
		wantPem := base + ".pem"
		var found bool
		for _, pem := range pems {
			if pem == wantPem {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("inv-zen-296 VIOLATED: signature %s has no matching certificate (.pem)",
				sig)
		}
	}
}

func TestCosignVerifyBlobIdentityRegex(t *testing.T) {
	root := repoRoot(t)
	helperPath := filepath.Join(root, "scripts", "release-gates", "verify_cosign_signature.sh")
	data, err := os.ReadFile(helperPath)
	if err != nil {
		t.Fatalf("verify_cosign_signature.sh missing: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "certificate-identity-regexp") {
		t.Error("inv-zen-296 VIOLATED: helper missing certificate-identity-regexp pin")
	}
	if !strings.Contains(text, "certificate-oidc-issuer") {
		t.Error("inv-zen-296 VIOLATED: helper missing certificate-oidc-issuer pin")
	}
	if !strings.Contains(text, "token.actions.githubusercontent.com") {
		t.Error("inv-zen-296 VIOLATED: helper missing GH OIDC issuer literal " +
			"(token.actions.githubusercontent.com)")
	}
	if !strings.Contains(text, "hades-system") {
		t.Error("inv-zen-296 VIOLATED: helper missing hades-system identity literal")
	}
}
