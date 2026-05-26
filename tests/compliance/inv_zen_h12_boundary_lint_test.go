// SPDX-License-Identifier: MIT
//
// boundary consolidation is in place + the boundary lint script catches
// regressions.
//
// Composes the inv-zen-322 boundary lint into the standard compliance
// suite so `make test` exercises the gate without depending on
// `make verify-hermes-boundary` being chained into the developer's local
// workflow. Belt + suspenders per defense-in-depth.

package compliance

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvZenH12_BoundaryPackagePresent(t *testing.T) {
	t.Parallel()
	root := findRepoRootInvZenH12(t)
	pkgRoot := filepath.Join(root, "internal", "hermes", "boundary")

	expected := []string{
		"doc.go",
		"surface.go",
		"adapter.go",
		"transport.go",
		"hooks.go",
		"mcp_envelope.go",
		"feature_detect.go",
	}
	for _, name := range expected {
		path := filepath.Join(pkgRoot, name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected file missing: %s (%v)", path, err)
		}
	}
}

func TestInvZenH12_BoundaryLintScriptExecutable(t *testing.T) {
	t.Parallel()
	root := findRepoRootInvZenH12(t)
	path := filepath.Join(root, "scripts", "verify_no_direct_hermes_imports.sh")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("boundary lint script missing: %v", err)
	}
	mode := info.Mode()
	if mode&0o111 == 0 {
		t.Errorf("boundary lint script not executable: mode %v", mode)
	}
}

func TestInvZenH12_BoundaryLintScriptPasses(t *testing.T) {

	root := findRepoRootInvZenH12(t)
	cmd := exec.Command("bash", filepath.Join(root, "scripts", "verify_no_direct_hermes_imports.sh"))
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("boundary lint failed (exit %v):\n%s", err, string(out))
	}
	if !strings.Contains(string(out), "PASS:") {
		t.Errorf("boundary lint output missing PASS marker:\n%s", string(out))
	}
}

func TestInvZenH12_BoundaryLintScriptCatchesViolation(t *testing.T) {

	root := findRepoRootInvZenH12(t)
	scriptPath := filepath.Join(root, "scripts", "verify_no_direct_hermes_imports.sh")

	scratch := t.TempDir()
	for _, d := range []string{"internal", "cmd", "tests/violator"} {
		if err := os.MkdirAll(filepath.Join(scratch, d), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	hermesName := "hermes" + "_" + "cli"
	violation := "// SPDX-License-Identifier: MIT\n" +
		"package violator\n" +
		"\n" +
		"import (\n" +
		"\t\"" + hermesName + "\"\n" +
		")\n" +
		"\n" +
		"var _ = " + hermesName + ".Foo\n"
	violationPath := filepath.Join(scratch, "tests/violator/v.go")
	if err := os.WriteFile(violationPath, []byte(violation), 0o644); err != nil {
		t.Fatalf("write violation file: %v", err)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = scratch
	cmd.Env = append(os.Environ(), "REPO_ROOT="+scratch)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Errorf("boundary lint should FAIL on synthetic violation; got exit 0 with:\n%s", string(out))
	}
	if !strings.Contains(string(out), "FAIL:") {
		t.Errorf("boundary lint output missing FAIL marker on violation:\n%s", string(out))
	}
}

func findRepoRootInvZenH12(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	for dir := wd; dir != "/"; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
	}
	t.Fatal("go.mod not found walking up from " + wd)
	return ""
}
