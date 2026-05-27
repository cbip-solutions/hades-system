// SPDX-License-Identifier: MIT
package hadesday

import "errors"

var ErrHadesDayCap7Anchor = errors.New(
	"hadesday: 7-item hard cap invariant anchor (inv-hades-126); MaxBriefItems = 7",
)

func zenDayCap7Sentinel() error {
	return ErrHadesDayCap7Anchor
}

func Cap7SentinelForTest() error {
	return zenDayCap7Sentinel()
}

var ErrHadesDayLeverageSortAnchor = errors.New(
	"hadesday: canonical leverage rank invariant anchor (inv-hades-127); LeverageRank 1..7",
)

func zenDayLeverageSortSentinel() error {
	return ErrHadesDayLeverageSortAnchor
}

func LeverageSortSentinelForTest() error {
	return zenDayLeverageSortSentinel()
}

var _ = zenDayCap7Sentinel()
var _ = zenDayLeverageSortSentinel()
