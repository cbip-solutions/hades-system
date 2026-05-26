// SPDX-License-Identifier: MIT
package inbox

import (
	"fmt"
	"time"
)

const BucketSeconds = 300

func DedupBucket(t time.Time) int64 {
	return t.UTC().Unix() / int64(BucketSeconds)
}

func BucketBoundary(bucket int64) time.Time {
	return time.Unix(bucket*int64(BucketSeconds), 0).UTC()
}

func ComputeDedupKey(eventType, contentHash string, t time.Time) string {
	return fmt.Sprintf("%s|%s|%d", eventType, contentHash, DedupBucket(t))
}
