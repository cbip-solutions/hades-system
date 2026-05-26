// SPDX-License-Identifier: MIT
package notification

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type AggregationRule struct {
	Pattern      string
	Window       int
	MaxPerWindow int
	Coalesce     bool
}

type Aggregator struct{}

func (a *Aggregator) Submit(e Event) (bool, error) {
	return false, zerrors.ErrNotImplementedPlan11
}
