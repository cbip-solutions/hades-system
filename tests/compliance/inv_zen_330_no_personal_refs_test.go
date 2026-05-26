// SPDX-License-Identifier: MIT

package compliance

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestInvZen330VerifyNoPersonalReferences(t *testing.T) {

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repo root not found (go.mod) walking up from %s", dir)
		}
		dir = parent
	}

	scriptPath := filepath.Join(dir, "scripts", "verify_no_personal_references.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("verify_no_personal_references.sh not found at %s: %v",
			scriptPath, err)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("verify_no_personal_references.sh failed (exit %v):\n%s",
			err, string(output))
	}
	t.Logf("scanner output: %s", string(output))
}

func TestInvZen330ScannerCatchesPlantedLeak(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repo root not found walking up from %s", dir)
		}
		dir = parent
	}

	tmpDir, err := os.MkdirTemp("", "inv-zen-330-leak-bite-check.*")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	leakPath := filepath.Join(tmpDir, "leak.go")
	leakContent := []byte(`package fake
const Path = "/Users/operator/projects/some-thing"
`)
	if err := os.WriteFile(leakPath, leakContent, 0o644); err != nil {
		t.Fatalf("write leak file: %v", err)
	}

	scriptPath := filepath.Join(dir, "scripts", "verify_no_personal_references.sh")
	cmd := exec.Command("bash", scriptPath, "--target-dir", tmpDir)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("scanner did NOT detect planted /Users/operator/ leak; output:\n%s",
			string(output))
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() != 1 {
			t.Errorf("scanner exited %d on planted leak; expected 1",
				exitErr.ExitCode())
		}
	}
}
