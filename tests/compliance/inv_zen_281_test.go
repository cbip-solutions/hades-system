// tests/compliance/inv_zen_281_test.go
//
// Compliance gate for invariant (v0.20.1 fix #1): `zen doctor` /
// `zen doctor caronte` auto-resolve the current working directory to
// a registered project's canonical alias when neither --project flag
// nor ZEN_PROJECT_ID env is set. The resolution drives the
// X-Zen-Project-ID header on every caronte probe so per-project state
// (NodeCount, LastIndexed, etc.) renders correctly in the doctor
// output for the operator's repo.
//
// Why: before v0.20.1, every `zen doctor caronte` probe against a
// registered project returned `project_id required` because the CLI
// did not infer the project from cwd. Operators had to pass
// `--project` or set ZEN_PROJECT_ID every invocation — undermining
// the documented UX of "doctor just works inside a registered repo".
// The new resolution path (doctor_caronte.go::resolveCaronteAliasViaCwd
// + runCaronteChecks rewiring) calls /v1/projects/doctor with cwd and
// uses the canonical alias from the response.
//
// Four source-regex anchors:
//
// 1. `resolveCaronteAliasViaCwd(` helper function declared in
// internal/cli/doctor_caronte.go.
// 2. The helper invokes `c.ProjectDoctor(cctx, "", cwd, false)` — the
// daemon's cwd→alias seam. Empty alias arg signals "resolve from
// cwd"; rebind=false because the doctor section MUST be a read-
// only probe (no side-effect on path_history).
// 3. The runCaronteChecks no-alias entry composes
// resolveCaronteAliasViaCwd into the alias chain when
// ZEN_PROJECT_ID is empty (the auto-resolve fallback layer).
// 4. Graceful fallback: errors from ProjectDoctor return "" so the
// section renders against daemon-default-project — pinned by the
// `err != nil || resp == nil` early-return.
//
// Sister-test bite check: revert any of the four anchors; this test
// MUST fail. Behavioural tests live in
// internal/cli/doctor_caronte_test.go
// (TestRunCaronteChecks_AutoResolvesProjectFromCwd +
// TestRunCaronteChecks_GracefulFallbackOnCwdResolveFailure).
//
// invariant (v0.20.1 fix #1).
package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvZen281SourceRegex_HelperExists(t *testing.T) {
	src := readDoctorCaronteSource(t)
	const needle = `func resolveCaronteAliasViaCwd(`
	if !strings.Contains(src, needle) {
		t.Errorf("inv-zen-281 violated: %q helper missing from internal/cli/doctor_caronte.go; cwd auto-resolution may have been removed", needle)
	}
}

func TestInvZen281SourceRegex_ProjectDoctorInvocation(t *testing.T) {
	src := readDoctorCaronteSource(t)
	const needle = `c.ProjectDoctor(cctx, "", cwd, false)`
	if !strings.Contains(src, needle) {
		t.Errorf("inv-zen-281 violated: %q call shape missing; the cwd auto-resolve may have been redirected to a different daemon endpoint or changed its alias signal", needle)
	}
}

func TestInvZen281SourceRegex_RunCaronteChecksComposesCwdResolve(t *testing.T) {
	src := readDoctorCaronteSource(t)
	const needle = `alias = resolveCaronteAliasViaCwd(ctx, c)`
	if !strings.Contains(src, needle) {
		t.Errorf("inv-zen-281 violated: %q composition missing in runCaronteChecks; the cwd auto-resolve is not wired into the no-alias entry", needle)
	}
}

func TestInvZen281SourceRegex_GracefulFallback(t *testing.T) {
	src := readDoctorCaronteSource(t)
	const needle = `if err != nil || resp == nil`
	if !strings.Contains(src, needle) {
		t.Errorf("inv-zen-281 violated: %q graceful-fallback guard missing; cwd-resolve failures may now bubble up and break the doctor section", needle)
	}
}

func TestInvZen281SourceRegex_DoctorCaronteCmdDispatch(t *testing.T) {
	src := readDoctorCaronteSource(t)

	const needleExplicit = `if explicit != ""`
	const needleFallthrough = `return runCaronteChecks(ctx, c)`
	if !strings.Contains(src, needleExplicit) {
		t.Errorf("inv-zen-281 violated: %q explicit-alias guard missing in doctorCaronteCmd dispatch", needleExplicit)
	}
	if !strings.Contains(src, needleFallthrough) {
		t.Errorf("inv-zen-281 violated: %q fallthrough to runCaronteChecks (cwd auto-resolve) missing from doctorCaronteCmd dispatch", needleFallthrough)
	}
}

func readDoctorCaronteSource(t *testing.T) string {
	t.Helper()
	rel := filepath.Join("..", "..", "internal", "cli", "doctor_caronte.go")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve doctor_caronte.go: %v", err)
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read %s: %v", abs, err)
	}
	return string(b)
}
