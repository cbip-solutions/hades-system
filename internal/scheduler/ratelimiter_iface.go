// SPDX-License-Identifier: MIT
package scheduler

import (
	"context"
	"time"
)

type RateLimiter interface {
	Allow(ctx context.Context, projectAlias string, now time.Time) bool
}
