// SPDX-License-Identifier: MIT
package inbox

import "errors"

var ErrCrossProjectInboxLeakAnchor = errors.New("inbox: no cross-project leak anchor (inv-hades-113)")

var ErrQuietHoursUrgentBypassAnchor = errors.New("inbox: urgent quiet-hours bypass anchor (inv-hades-125)")

func severity4TierEnumSentinel() error {

	for _, s := range AllSeverities() {
		_ = s
	}

	_, _ = ParseSeverity(string(SeverityUrgent))
	return ErrSeverity4TierAnchor
}
