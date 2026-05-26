// SPDX-License-Identifier: MIT
package config

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type NotificationsConfig struct {
	QuietHours       QuietHoursConfig
	FocusMode        FocusModeConfig
	CriticalOverride CriticalOverrideConfig
	Channels         map[string]ChannelConfig
	RateLimit        RateLimitConfig
	Aggregation      AggregationConfig
}

type QuietHoursConfig struct {
	WeekdayStart string
	WeekdayEnd   string
	WeekendStart string
	WeekendEnd   string
	DuringQuiet  string
}

type FocusModeConfig struct {
	FollowMacOS    bool
	ManualOverride bool
}

type CriticalOverrideConfig struct {
	PushDuringQuiet bool
}

type ChannelConfig struct {
	Enabled bool
	Topic   string
	To      string
}

type RateLimitConfig struct {
	MaxPerHourOutsideDashboard int
}

type AggregationConfig struct {
	SameSwarmWindowSeconds     int
	ProviderErrorWindowSeconds int
	ProviderErrorThreshold     int
	BudgetThresholdsPercent    []int
	DedupWindowSeconds         int
}

func LoadNotifications(path string) (*NotificationsConfig, error) {
	return nil, zerrors.ErrNotImplementedPlan11
}
