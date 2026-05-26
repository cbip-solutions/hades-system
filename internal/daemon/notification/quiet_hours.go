// SPDX-License-Identifier: MIT
package notification

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type QuietHoursConfig struct {
	WeekdayStart         string
	WeekdayEnd           string
	WeekendStart         string
	WeekendEnd           string
	DuringQuiet          string
	FocusModeFollows     bool
	CriticalOverridePush bool
}

func IsQuiet(cfg QuietHoursConfig) (bool, error) {
	return false, zerrors.ErrNotImplementedPlan11
}

func AllowedDuringQuiet(cfg QuietHoursConfig, s Severity) (bool, error) {
	return false, zerrors.ErrNotImplementedPlan11
}
