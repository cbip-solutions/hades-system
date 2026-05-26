// SPDX-License-Identifier: MIT
package chain

import "time"

func PartitionID(unixSeconds int64) string {
	return time.Unix(unixSeconds, 0).UTC().Format("2006_01")
}

func PartitionIDFromTime(t time.Time) string {
	return t.UTC().Format("2006_01")
}
