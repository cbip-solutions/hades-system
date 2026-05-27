// go:build test_helpers

// SPDX-License-Identifier: MIT

package v1

func lookupRevertRuleMetaImpl(rulePath string) (revertRuleMeta, bool) {
	revertRuleMetaForTestMu.RLock()
	defer revertRuleMetaForTestMu.RUnlock()
	if rule, ok := revertRuleMetaForTest[rulePath]; ok {
		return *rule, true
	}
	return revertRuleMeta{}, false
}
