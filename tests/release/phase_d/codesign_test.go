// SPDX-License-Identifier: MIT

package phase_d_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func snapshotForCodesign(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	cmd := exec.Command("goreleaser", "release",
		"--snapshot", "--clean",
		"--skip=publish",
		"--skip=docker",
		"--skip=sign=cosign-keyless",
		"--skip=sign=cosign-keyless-checksum",
		"--skip=sign=cosign-keyless-sbom",
	)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"GORELEASER_CURRENT_TAG=v0.0.0-test",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("goreleaser snapshot (codesign only): %v\noutput:\n%s", err, out)
	}
	return filepath.Join(root, "dist")
}

func TestCodesignAdHocMarker(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("codesign verification only meaningful on darwin")
	}
	if testing.Short() {
		t.Skip("requires goreleaser + codesign; skip in -short mode")
	}
	if os.Getenv("GORELEASER_CODESIGN_TEST") != "1" {
		t.Skip("set GORELEASER_CODESIGN_TEST=1 to run the codesign snapshot (slow)")
	}
	if _, err := exec.LookPath("goreleaser"); err != nil {
		t.Skip("goreleaser not in PATH")
	}
	if _, err := exec.LookPath("codesign"); err != nil {
		t.Skip("codesign not in PATH")
	}

	distDir := snapshotForCodesign(t)

	pairs := []struct {
		binaryGlob string
	}{
		{filepath.Join(distDir, "zen_darwin_arm64*", "zen")},
		{filepath.Join(distDir, "zen-swarm-ctld_darwin_arm64*", "zen-swarm-ctld")},
	}
	for _, p := range pairs {
		matches, err := filepath.Glob(p.binaryGlob)
		if err != nil || len(matches) == 0 {
			t.Errorf("inv-zen-295 VIOLATED: no binaries match %s: %v", p.binaryGlob, err)
			continue
		}
		for _, bin := range matches {
			verifyCmd := exec.Command("codesign", "--verify", bin)
			if vOut, vErr := verifyCmd.CombinedOutput(); vErr != nil {
				t.Errorf("inv-zen-295 VIOLATED: codesign --verify %s failed: %v\n%s",
					bin, vErr, vOut)
			}
			detailCmd := exec.Command("codesign", "-dvvv", bin)
			detailOut, _ := detailCmd.CombinedOutput()
			if !strings.Contains(string(detailOut), "Signature=adhoc") {
				t.Errorf("inv-zen-295 VIOLATED: codesign -dvvv %s missing 'Signature=adhoc' marker\n%s",
					bin, detailOut)
			}
		}
	}
}

func TestCodesignOptionsRuntime(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("codesign verification only meaningful on darwin")
	}
	if testing.Short() {
		t.Skip("requires codesign; skip in -short mode")
	}
	if _, err := exec.LookPath("codesign"); err != nil {
		t.Skip("codesign not in PATH")
	}
	root := repoRoot(t)
	matches, _ := filepath.Glob(filepath.Join(root, "dist", "zen_darwin_arm64*", "zen"))
	if len(matches) == 0 {
		t.Skip("no darwin-arm64 zen binary in dist/; run TestCodesignAdHocMarker first " +
			"(GORELEASER_CODESIGN_TEST=1)")
	}
	for _, bin := range matches {
		detailCmd := exec.Command("codesign", "-dvvv", bin)
		detailOut, _ := detailCmd.CombinedOutput()
		text := string(detailOut)
		if !strings.Contains(text, "CodeDirectory") {
			t.Errorf("inv-zen-295 VIOLATED: codesign -dvvv %s missing CodeDirectory\n%s",
				bin, text)
		}

		if !strings.Contains(text, "runtime") {
			t.Errorf("inv-zen-295 VIOLATED: codesign -dvvv %s missing 'runtime' flag (hardened runtime not enabled)\n%s",
				bin, text)
		}
	}
}
