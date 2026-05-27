// go:build test_helpers

// SPDX-License-Identifier: MIT

package v1

import "sync"

func SetTestRevertMetadata(s *Schema, rulePath, category string, threshold float64, window, cooldownH int) {
	setRuleMetadata(s, rulePath, "revert_category", category)
	setRuleMetadata(s, rulePath, "revert_threshold_pct", threshold)
	setRuleMetadata(s, rulePath, "revert_window_sessions", window)
	setRuleMetadata(s, rulePath, "revert_cooldown_hours", cooldownH)
}

// revertRuleMetaForTest backs SetTestRevertMetadata in test builds. Keyed
// by rule-path; values are merged into a per-path revertRuleMeta struct.
// Concurrent-safe so parallel-running tests can install fixtures without
// stepping on each other.
//
// Production builds (no -tags test_helpers) do NOT see this map — the
// `lookupRevertRuleMetaImpl` they consult lives in lookup_default.go and
// always returns zero, false. Test builds replace that production
// implementation via lookup_test_helpers.go (same build tag) which reads
// from this map.
var (
	revertRuleMetaForTestMu sync.RWMutex
	revertRuleMetaForTest   = map[string]*revertRuleMeta{}
)

func setRuleMetadata(s *Schema, rulePath, key string, value any) {
	revertRuleMetaForTestMu.Lock()
	defer revertRuleMetaForTestMu.Unlock()
	rule, ok := revertRuleMetaForTest[rulePath]
	if !ok {
		rule = &revertRuleMeta{}
		revertRuleMetaForTest[rulePath] = rule
	}
	switch key {
	case "revert_category":
		if v, ok := value.(string); ok {
			rule.Category = v
		}
	case "revert_threshold_pct":
		if v, ok := value.(float64); ok {
			rule.ThresholdPct = v
		}
	case "revert_window_sessions":
		if v, ok := value.(int); ok {
			rule.WindowSessions = v
		}
	case "revert_cooldown_hours":
		if v, ok := value.(int); ok {
			rule.CooldownH = v
		}
	}
}

func resetTestRevertMetadata() {
	revertRuleMetaForTestMu.Lock()
	defer revertRuleMetaForTestMu.Unlock()
	revertRuleMetaForTest = map[string]*revertRuleMeta{}
}
