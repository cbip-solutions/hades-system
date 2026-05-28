//go:build !cgo
// +build !cgo

// SPDX-License-Identifier: MIT
package aggregator

import (
	"context"
	"fmt"
)

func (a *Aggregator) RebuildPinnedEmbeddings(ctx context.Context, projectID string) (int, error) {
	_ = a
	_ = ctx
	_ = projectID
	return 0, fmt.Errorf("aggregator: RebuildPinnedEmbeddings: %w", ErrCGODisabled)
}
