// SPDX-License-Identifier: MIT
package scheduler

import "context"

type DispatchInput struct {
	ProjectAlias string

	Action string

	BackfillWindow *BackfillWindow

	Metadata map[string]string
}

type DispatchResult struct {
	CostUSD float64

	DurationMs int64

	Tier string
}

type Dispatcher interface {
	Dispatch(ctx context.Context, in DispatchInput) (DispatchResult, error)
}
