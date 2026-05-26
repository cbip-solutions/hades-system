// SPDX-License-Identifier: MIT
package zenday

import "errors"

var ErrZenDayCap7Anchor = errors.New(
	"zenday: 7-item hard cap invariant anchor (inv-zen-126); MaxBriefItems = 7",
)

func zenDayCap7Sentinel() error {
	return ErrZenDayCap7Anchor
}

func Cap7SentinelForTest() error {
	return zenDayCap7Sentinel()
}

var ErrZenDayLeverageSortAnchor = errors.New(
	"zenday: canonical leverage rank invariant anchor (inv-zen-127); LeverageRank 1..7",
)

func zenDayLeverageSortSentinel() error {
	return ErrZenDayLeverageSortAnchor
}

func LeverageSortSentinelForTest() error {
	return zenDayLeverageSortSentinel()
}

var _ = zenDayCap7Sentinel()
var _ = zenDayLeverageSortSentinel()
