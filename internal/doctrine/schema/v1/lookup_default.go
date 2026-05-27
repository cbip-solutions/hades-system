//go:build !test_helpers

// SPDX-License-Identifier: MIT

package v1

func lookupRevertRuleMetaImpl(rulePath string) (revertRuleMeta, bool) {
	return revertRuleMeta{}, false
}
