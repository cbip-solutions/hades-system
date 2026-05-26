// SPDX-License-Identifier: MIT
package scheduler

import (
	"context"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
	"github.com/cbip-solutions/hades-system/internal/quota"
)

type QuotaPreFlightChecker interface {
	PreFlight(ctx context.Context, alias string, d doctrine.Name) (quota.PreFlightDecision, error)
}
