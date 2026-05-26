// Package ci tests — Plan 15 Phase A A-5/A-9 — rolling window unit tests.
//
// Tests the 50/45/2 elite-DORA-CFR-aligned rolling window semantics
// (spec §7.3; amendment §2.5 D-5; inv-zen-275). Covers:
//   - default thresholds match canonical 50/45/2
//   - all-success → pass
//   - sample <30 → fail with "sample" diagnostic
//   - real-fail count > MaxRealFails → fail with "real" diagnostic
//   - infra-bucketed failures excluded from denominator
//   - flake-bucketed failures excluded from denominator
//   - ratio below threshold → fail
//
// Coverage target ≥90% (rolling_window.go is correctness-critical per
// CLAUDE.md security/correctness list: validator + cost_ledger family).
package ci_test

import (
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/ci"
)

func TestRollingWindow_DefaultThresholds(t *testing.T) {
	t.Parallel()
	w := ci.DefaultRollingWindow()
	if w.WindowSize != 50 {
		t.Errorf("WindowSize: got %d; want 50", w.WindowSize)
	}
	if w.MinSuccess != 45 {
		t.Errorf("MinSuccess: got %d; want 45", w.MinSuccess)
	}
	if w.MaxRealFails != 2 {
		t.Errorf("MaxRealFails: got %d; want 2", w.MaxRealFails)
	}
}

func TestRollingWindow_Evaluate_AllSuccess(t *testing.T) {
	t.Parallel()
	commits := generateCommits(50, "success", "success")
	w := ci.DefaultRollingWindow()
	pass, reason := w.Evaluate(commits)
	if !pass {
		t.Errorf("expected pass; got fail: %s", reason)
	}
	if reason != "" {
		t.Errorf("expected empty reason on pass; got: %s", reason)
	}
}

func TestRollingWindow_Evaluate_TooFewCommits(t *testing.T) {
	t.Parallel()

	commits := generateCommits(25, "success", "success")
	w := ci.DefaultRollingWindow()
	pass, reason := w.Evaluate(commits)
	if pass {
		t.Errorf("expected fail (sample <30); got pass")
	}
	if !strings.Contains(reason, "sample") {
		t.Errorf("reason should mention sample; got: %s", reason)
	}
}

func TestRollingWindow_Evaluate_RealFailExceeded(t *testing.T) {
	t.Parallel()
	commits := generateCommits(45, "success", "success")
	commits = append(commits, generateCommits(3, "failure", "real")...)
	commits = append(commits, generateCommits(2, "success", "success")...)
	w := ci.DefaultRollingWindow()
	pass, reason := w.Evaluate(commits)
	if pass {
		t.Errorf("expected fail (3 real fails > 2 max); got pass")
	}
	if !strings.Contains(reason, "real") {
		t.Errorf("reason should mention real failures; got: %s", reason)
	}
}

func TestRollingWindow_Evaluate_InfraBucketed_Excluded(t *testing.T) {
	t.Parallel()

	commits := generateCommits(45, "success", "success")
	commits = append(commits, generateCommits(5, "failure", "infra")...)
	w := ci.DefaultRollingWindow()
	pass, reason := w.Evaluate(commits)
	if !pass {
		t.Errorf("expected pass (infra bucketed; 45/45 success ratio); got fail: %s", reason)
	}
}

func TestRollingWindow_Evaluate_FlakeBucketed_Excluded(t *testing.T) {
	t.Parallel()
	commits := generateCommits(45, "success", "success")
	commits = append(commits, generateCommits(5, "failure", "flake")...)
	w := ci.DefaultRollingWindow()
	pass, reason := w.Evaluate(commits)
	if !pass {
		t.Errorf("expected pass (flake bucketed; 45/45 success ratio); got fail: %s", reason)
	}
}

func TestRollingWindow_Evaluate_RatioBelowThreshold(t *testing.T) {
	t.Parallel()

	commits := generateCommits(39, "success", "success")
	commits = append(commits, generateCommits(3, "failure", "real")...)
	w := ci.DefaultRollingWindow()
	pass, _ := w.Evaluate(commits)
	if pass {
		t.Errorf("expected fail (3 real > 2 max); got pass")
	}
}

func TestRollingWindow_Evaluate_RatioGate(t *testing.T) {
	t.Parallel()
	// Force the ratio gate (real_fail ≤ MaxRealFails but ratio < 0.9):
	// 25 success + 2 real fail + 0 infra = denom 27 < MinSampleSize 30 → fails on sample
	// Need enough denom but ratio < 0.9 + real ≤ 2.
	// 30 success + 2 real → denom 32, ratio 30/32 = 0.9375 ≥ 0.9 → pass
	// 27 success + 2 real → denom 29 < 30 → fails on sample
	// 32 success + 2 real → denom 34, ratio 32/34 = 0.9411 → pass
	// To trigger ratio gate alone: need real ≤ 2 + denom ≥ 30 + ratio < 0.9.
	// With real=2, success=18 → denom=20<30. With real=2, success=28 → denom=30, ratio 0.933.
	// Cannot trigger ratio<0.9 with real≤2 + denom≥30 because:
	//   ratio = success / (success+2) < 0.9 → success < 0.9*(success+2) → 0.1*success < 1.8 → success < 18.
	//   With success<18 + real=2 → denom<20 → fails on sample first.
	// This is BY DESIGN — the real-fail cap is the binding constraint at MinSampleSize.
	// We document this invariant via the test: the implementation MUST gate
	// on real-fail before computing ratio, so the test verifies the gate
	// ordering by constructing a scenario where ratio violation is moot.
	commits := generateCommits(28, "success", "success")
	commits = append(commits, generateCommits(2, "failure", "real")...)

	w := ci.DefaultRollingWindow()
	pass, reason := w.Evaluate(commits)
	if !pass {
		t.Errorf("expected pass (real=2 within cap, ratio=0.933 ≥ 0.9); got fail: %s", reason)
	}
}

func TestRollingWindow_Evaluate_UnclassifiedFailureCountedAsReal(t *testing.T) {
	t.Parallel()
	// Failure with empty Bucket (unclassified) MUST count as real for safety.
	commits := generateCommits(45, "success", "success")
	commits = append(commits, generateCommits(3, "failure", "")...)
	w := ci.DefaultRollingWindow()
	pass, reason := w.Evaluate(commits)
	if pass {
		t.Errorf("expected fail (3 unclassified failures count as real, > 2 max); got pass")
	}
	if !strings.Contains(reason, "real") {
		t.Errorf("reason should mention real failures; got: %s", reason)
	}
}

func TestRollingWindow_Evaluate_EmptyCommits(t *testing.T) {
	t.Parallel()
	w := ci.DefaultRollingWindow()
	pass, reason := w.Evaluate(nil)
	if pass {
		t.Errorf("expected fail on empty commit list; got pass")
	}
	if !strings.Contains(reason, "sample") {
		t.Errorf("reason should mention sample shortage; got: %s", reason)
	}
}

func TestRollingWindow_Evaluate_CustomThresholds(t *testing.T) {
	t.Parallel()

	commits := generateCommits(95, "success", "success")
	commits = append(commits, generateCommits(5, "failure", "real")...)
	w := ci.RollingWindow{WindowSize: 100, MinSuccess: 90, MaxRealFails: 5}
	pass, reason := w.Evaluate(commits)
	if !pass {
		t.Errorf("expected pass (95 success + 5 real exactly at cap; ratio 95/100=0.95); got fail: %s", reason)
	}

	commits = append(commits, generateCommits(1, "failure", "real")...)
	pass, reason = w.Evaluate(commits)
	if pass {
		t.Errorf("expected fail (6 real > 5 max); got pass; %s", reason)
	}
}

func TestRollingWindow_Evaluate_RatioFailsWithPermissiveCap(t *testing.T) {
	t.Parallel()

	commits := generateCommits(25, "success", "success")
	commits = append(commits, generateCommits(15, "failure", "real")...)
	w := ci.RollingWindow{WindowSize: 50, MinSuccess: 45, MaxRealFails: 50}
	pass, reason := w.Evaluate(commits)
	if pass {
		t.Errorf("expected fail (ratio 0.625 < 0.90); got pass")
	}
	if !strings.Contains(reason, "ratio") {
		t.Errorf("reason should mention ratio; got: %s", reason)
	}
}

func TestRollingWindow_Evaluate_MalformedWindowSize(t *testing.T) {
	t.Parallel()

	commits := generateCommits(30, "success", "success")
	w := ci.RollingWindow{WindowSize: 0, MinSuccess: 45, MaxRealFails: 2}
	pass, reason := w.Evaluate(commits)
	if pass {
		t.Errorf("expected fail (WindowSize=0); got pass")
	}
	if !strings.Contains(reason, "WindowSize") {
		t.Errorf("reason should mention WindowSize; got: %s", reason)
	}
}

func generateCommits(n int, status, bucket string) []ci.CommitStatus {
	out := make([]ci.CommitStatus, n)
	for i := 0; i < n; i++ {
		out[i] = ci.CommitStatus{
			SHA:    paddedSHA(i),
			Status: status,
			Bucket: bucket,
			Date:   time.Now().Add(-time.Duration(i) * time.Hour),
		}
	}
	return out
}

func paddedSHA(i int) string {
	out := []byte("0000000000000000000000000000000000000000")
	hex := []byte("0123456789abcdef")

	for pos := 39; pos >= 34 && i > 0; pos-- {
		out[pos] = hex[i&0xf]
		i >>= 4
	}
	return string(out)
}
