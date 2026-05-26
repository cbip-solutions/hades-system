// SPDX-License-Identifier: MIT
package zenday

import "sort"

type ByLeverage []BriefItem

func (b ByLeverage) Len() int { return len(b) }

func (b ByLeverage) Less(i, j int) bool {
	if b[i].Rank != b[j].Rank {
		return b[i].Rank < b[j].Rank
	}

	return b[i].CreatedAt.After(b[j].CreatedAt)
}

func (b ByLeverage) Swap(i, j int) {
	b[i], b[j] = b[j], b[i]
}

func SortByLeverage(items []BriefItem) {
	sort.Stable(ByLeverage(items))
}

func IsSorted(items []BriefItem) bool {
	return sort.IsSorted(ByLeverage(items))
}
