// SPDX-License-Identifier: MIT
//
// 30-CI-green gate per spec §7.3 (amendment §2.5 D-5). The "30" in the
// gate name is the MinSampleSize floor (denominator must be ≥30 before
// the gate is meaningful); the WindowSize is the lookback depth (50 by
// default). Implementation invariants:
//
// 1. Real-failure cap (MaxRealFails) is checked BEFORE the ratio gate.
// Rationale real_fail > MaxRealFails is a stricter signal than
// ratio violation; failing-fast on it yields better diagnostic.
// 2. Failures with empty Bucket are treated as "real" for safety
// (never silently excluded from the real-fail count).
// 3. Sample-size gate fires first to avoid divide-by-zero on tiny
// denominators.
//
// Coverage target ≥90% per project instructions security/correctness-critical list
// (analogue to validator/cost_ledger families).
package ci

import (
	"fmt"
)

const MinSampleSize = 30

type RollingWindow struct {
	WindowSize   int
	MinSuccess   int
	MaxRealFails int
}

func DefaultRollingWindow() RollingWindow {
	return RollingWindow{
		WindowSize:   50,
		MinSuccess:   45,
		MaxRealFails: 2,
	}
}

// Evaluate applies the rolling-window semantics to a classified commit
// list. Returns (pass, reason). Empty reason on pass; populated with
// operator-actionable detail when fail.
//
// Gate ordering:
// 1. Sample size (denom ≥ MinSampleSize)
// 2. Real-fail cap (real_fail ≤ MaxRealFails)
// 3. Success ratio (success / denom ≥ MinSuccess / WindowSize)
//
// Pre-condition: commits MUST have Bucket assigned via Classify (see
// classifier.go). Bucket="" + Status="failure" is treated as "real"
// for safety.
func (w RollingWindow) Evaluate(commits []CommitStatus) (bool, string) {
	successCount := 0
	realFail := 0
	infraFail := 0
	flakeFail := 0
	for _, c := range commits {
		switch {
		case c.Status == "success" || c.Bucket == "success":
			successCount++
		case c.Bucket == "infra":
			infraFail++
		case c.Bucket == "flake":
			flakeFail++
		case c.Bucket == "real":
			realFail++
		default:

			realFail++
		}
	}
	denom := successCount + realFail
	if denom < MinSampleSize {
		return false, fmt.Sprintf(
			"insufficient sample: %d classified commits (need ≥%d); infra=%d flake=%d",
			denom, MinSampleSize, infraFail, flakeFail,
		)
	}
	if realFail > w.MaxRealFails {
		return false, fmt.Sprintf(
			"real failures %d exceed max %d (success=%d infra=%d flake=%d)",
			realFail, w.MaxRealFails, successCount, infraFail, flakeFail,
		)
	}
	if w.WindowSize <= 0 {

		return false, fmt.Sprintf("invalid RollingWindow: WindowSize=%d (must be >0)", w.WindowSize)
	}
	ratio := float64(successCount) / float64(denom)
	threshold := float64(w.MinSuccess) / float64(w.WindowSize)
	if ratio < threshold {
		return false, fmt.Sprintf(
			"success ratio %.3f below threshold %.3f (success=%d denom=%d)",
			ratio, threshold, successCount, denom,
		)
	}
	return true, ""
}
