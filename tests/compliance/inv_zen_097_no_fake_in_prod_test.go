// tests/compliance/inv_zen_097_no_fake_in_prod_test.go
//
// Compliance test for inv-zen-097: MergeEngineFake (apply package) is
// gated by `//go:build test` AND the runtime mustBeTestRun() panic
// guard, so the fake never reaches a production binary.
//
// Four enforcement layers exercised here:
//
//  1. TestInvZen097_BuildTagOnFakeFile — `go list -json -f '{{.GoFiles}}'`
//     comparison: merge_fake.go MUST be invisible in the default build
//     and visible under `-tags test`.
//  2. TestInvZen097_NoFakeSymbolInProdBinary — build the daemon
//     (no `-tags test`), run `go tool nm`, assert the symbol
//     `MergeEngineFake` is absent.
//  3. TestInvZen097_IsTestRunFalseInProdBinary — build a tiny standalone
//     program that calls apply.IsTestRun() and exits with the result;
//     run it (NOT under `go test`) and assert exit code reports false.
//  4. TestInvZen097_NoFakeReferenceInProdSources — `git grep` for the
//     literal `MergeEngineFake` in non-test, non-tag sources; the only
//     allowed locations are merge_fake*.go + this file + docs/.
//
// Spec §6.3 inv-zen-097. Plan 5 Phase J ships the contract; Plan 6
// implements the real MergeEngine and removes the fake guards once
// MergeEngineRealEngine is wired.
package compliance

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestInvZen097_BuildTagOnFakeFile asserts the //go:build test
// constraint via `go list -json -f '{{.GoFiles}}'` (default build) vs
// `-tags test` (test build). The fake file MUST appear in the test
// build's GoFiles set and MUST NOT appear in the default build's
// GoFiles set.
func TestInvZen097_BuildTagOnFakeFile(t *testing.T) {
	defaultFiles := goListGoFiles(t, "")
	testFiles := goListGoFiles(t, "test")

	if hasFile(defaultFiles, "merge_fake.go") {
		t.Fatalf("merge_fake.go visible in DEFAULT build (inv-zen-097 violated): %v", defaultFiles)
	}
	if !hasFile(testFiles, "merge_fake.go") {
		t.Fatalf("merge_fake.go MISSING from -tags test build: %v", testFiles)
	}
}

func TestInvZen097_NoFakeSymbolInProdBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("go tool nm output format varies on Windows; skip")
	}
	if testing.Short() {
		t.Skip("skipping prod-binary scan in -short mode")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go binary not in PATH (CI sandboxing)")
	}

	root, err := repoRoot097()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "zen-swarm-ctld")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/zen-swarm-ctld")
	cmd.Dir = root
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build ./cmd/zen-swarm-ctld: %v", err)
	}
	out, err := exec.Command("go", "tool", "nm", bin).Output()
	if err != nil {
		t.Fatalf("go tool nm: %v", err)
	}
	if bytes.Contains(out, []byte("MergeEngineFake")) {
		var hits []string
		for _, ln := range strings.Split(string(out), "\n") {
			if strings.Contains(ln, "MergeEngineFake") {
				hits = append(hits, ln)
			}
		}
		t.Fatalf("MergeEngineFake found in production binary (inv-zen-097):\n%s",
			strings.Join(hits, "\n"))
	}
}

func TestInvZen097_IsTestRunFalseInProdBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping prod-binary subprocess in -short mode")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go binary not in PATH (CI sandboxing)")
	}

	root, err := repoRoot097()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}

	probeDir := filepath.Join(root, "tests", "compliance", "_probe097")
	if err := os.MkdirAll(probeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(probeDir) })

	src := `package main

import (
	"fmt"
	"os"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/apply"
)

func main() {
	if apply.IsTestRun() {
		fmt.Fprintln(os.Stderr, "FAIL: IsTestRun() returned true in standalone binary (inv-zen-097)")
		os.Exit(1)
	}
	os.Exit(0)
}
`
	if err := os.WriteFile(filepath.Join(probeDir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	binPath := filepath.Join(t.TempDir(), "is-test-run-probe")
	build := exec.Command("go", "build", "-o", binPath, "./tests/compliance/_probe097")
	build.Dir = root
	build.Env = append(os.Environ(), "GOFLAGS=")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("go build probe: %v", err)
	}

	run := exec.Command(binPath)
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("probe exited non-zero (IsTestRun returned true outside go test): %v\nstdout/stderr: %s", err, out)
	}
}

func TestInvZen097_NoFakeReferenceInProdSources(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	root, err := repoRoot097()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}

	patterns := []string{
		`MergeEngineFake{`,
		`\*MergeEngineFake`,
		`MergeEngineFake\)`,
	}
	for _, pat := range patterns {
		cmd := exec.Command("git", "grep", "-l", "-E", pat, "--",
			":(exclude)tests/compliance/inv_zen_097_no_fake_in_prod_test.go",
			":(exclude)internal/orchestrator/apply/merge_fake.go",
			":(exclude)internal/orchestrator/apply/merge_fake_test.go",
			":(exclude)docs/")
		cmd.Dir = root
		out, err := cmd.CombinedOutput()

		if err == nil && len(bytes.TrimSpace(out)) > 0 {
			t.Errorf("MergeEngineFake code-reference (pattern %q) found outside test-tagged files (inv-zen-097):\n%s", pat, out)
		}
	}
}

func goListGoFiles(t *testing.T, tag string) []string {
	t.Helper()
	args := []string{"list", "-json"}
	if tag != "" {
		args = append(args, "-tags", tag)
	}
	args = append(args, "github.com/cbip-solutions/hades-system/internal/orchestrator/apply")
	cmd := exec.Command("go", args...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go %v: %v", args, err)
	}
	var pkg struct {
		GoFiles []string
	}
	if err := json.Unmarshal(out, &pkg); err != nil {
		t.Fatalf("unmarshal go list: %v", err)
	}
	return pkg.GoFiles
}

func hasFile(files []string, name string) bool {
	for _, f := range files {
		if filepath.Base(f) == name {
			return true
		}
	}
	return false
}

func repoRoot097() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
