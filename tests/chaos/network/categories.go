//go:build chaos

// SPDX-License-Identifier: MIT

package network

import (
	"fmt"
	"sort"
)

// Category classifies a ToxicType by the shape of failure it produces
// on the wire. The taxonomy mirrors how the daemon's robustness paths
// branch: a CLOSE-class fault triggers the dial-error / circuit-breaker
// / retry path; a DEGRADE-class fault triggers the deadline / back-off
// / partial-read path; a CORRUPT-class fault triggers the response-
// validation / re-issue path.
//
// Per inv-zen-305 the 10-toxic taxonomy must be exhaustive — every
// canonical ToxicType MUST map to exactly one Category, and the union
// of categories MUST equal AllToxicTypes(). The test
// TestCategoriesPartitionAllToxicTypes pins this; a new toxic type
// added without a category assignment fails the test loudly.
type Category int

const (
	CategoryUnknown Category = iota

	CategoryClose

	CategoryDegrade

	CategoryCorrupt
)

func (c Category) String() string {
	switch c {
	case CategoryClose:
		return "close"
	case CategoryDegrade:
		return "degrade"
	case CategoryCorrupt:
		return "corrupt"
	default:
		return "unknown"
	}
}

func CategoryOf(t ToxicType) Category {
	switch t {
	case ToxicDown, ToxicTimeout, ToxicResetPeer:
		return CategoryClose
	case ToxicLatency, ToxicBandwidth, ToxicSlowClose, ToxicSlicer:
		return CategoryDegrade
	case ToxicLimitData, ToxicModifyBuffer, ToxicModifyRate:
		return CategoryCorrupt
	default:
		return CategoryUnknown
	}
}

func AllCategories() []Category {
	return []Category{CategoryClose, CategoryDegrade, CategoryCorrupt}
}

func ToxicsByCategory() map[Category][]ToxicType {
	out := make(map[Category][]ToxicType, len(AllCategories()))
	for _, tox := range AllToxicTypes() {
		cat := CategoryOf(tox)
		out[cat] = append(out[cat], tox)
	}
	for _, slice := range out {
		sort.Slice(slice, func(i, j int) bool { return slice[i] < slice[j] })
	}
	return out
}

func FilterByCategory(all []Scenario, cat Category) []Scenario {
	out := make([]Scenario, 0, len(all))
	for _, s := range all {
		if CategoryOf(s.Toxic) == cat {
			out = append(out, s)
		}
	}
	return out
}

func validateCategoryPartition() error {
	seen := make(map[ToxicType]Category, len(AllToxicTypes()))
	for _, tox := range AllToxicTypes() {
		cat := CategoryOf(tox)
		if cat == CategoryUnknown {
			return fmt.Errorf("toxic %q has CategoryUnknown — taxonomy gap", tox)
		}
		if prev, dup := seen[tox]; dup {
			return fmt.Errorf("toxic %q duplicated across categories %s, %s", tox, prev, cat)
		}
		seen[tox] = cat
	}
	if len(seen) != 10 {
		return fmt.Errorf("partition covers %d toxics, want 10", len(seen))
	}
	return nil
}

func init() {

	if err := validateCategoryPartition(); err != nil {
		panic("network: category partition broken: " + err.Error())
	}
}
