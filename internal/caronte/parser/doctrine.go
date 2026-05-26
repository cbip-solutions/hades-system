// SPDX-License-Identifier: MIT
package parser

import "time"

type DoctrineAccessor interface {
	WatcherCPUBudget(projectID string) float64

	WatcherCadence(projectID string) time.Duration
}

const DefaultDebounce = 3 * time.Second

const MaxCPUBudgetPct = 25.0
