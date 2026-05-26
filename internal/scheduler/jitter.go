// SPDX-License-Identifier: MIT
package scheduler

import (
	"crypto/sha256"
	"encoding/binary"
	"time"
)

const (
	jitterRecurringCap = 15 * time.Minute
	jitterOneShotCap   = 90 * time.Second

	jitterRecurringThreshold = time.Hour
)

func ComputeJitter(routineID string, period time.Duration) time.Duration {
	if period <= 0 {
		return 0
	}

	sum := sha256.Sum256([]byte(routineID))
	h := binary.BigEndian.Uint64(sum[:8])

	bucket := period / 10
	if bucket <= 0 {
		bucket = 1
	}
	raw := time.Duration(h % uint64(bucket))

	maxOffset := jitterOneShotCap
	if period >= jitterRecurringThreshold {
		maxOffset = jitterRecurringCap
	}
	if raw > maxOffset {
		return maxOffset
	}
	return raw
}
