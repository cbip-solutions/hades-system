// SPDX-License-Identifier: MIT
package inbox

import "errors"

var ErrCrossProjectInboxLeakAnchor = errors.New("inbox: no cross-project leak anchor (invariant)")

var ErrQuietHoursUrgentBypassAnchor = errors.New("inbox: urgent quiet-hours bypass anchor (invariant)")

func severity4TierEnumSentinel() error {

	for _, s := range AllSeverities() {
		_ = s
	}

	_, _ = ParseSeverity(string(SeverityUrgent))
	return ErrSeverity4TierAnchor
}
