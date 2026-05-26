// SPDX-License-Identifier: MIT
package inbox

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

type QuietHours struct {
	Start           time.Duration
	End             time.Duration
	WeekendExtended bool
	UrgentBypass    bool
}

type QuietConfig struct {
	Default          QuietHours
	PerProject       map[string]QuietHours
	UrgentPauseUntil *time.Time
}

var ErrInvalidQuietConfig = errors.New("inbox: invalid quiet config")

func ShouldEmit(n Notification, cfg QuietConfig, now time.Time) bool {
	hours := cfg.Default
	if cfg.PerProject != nil {
		if override, ok := cfg.PerProject[n.ProjectID]; ok {
			hours = override
		}
	}

	if n.Severity == SeverityUrgent {

		if !hours.UrgentBypass {

			if InQuietHours(hours, now) {
				return false
			}
			return true
		}

		if cfg.UrgentPauseUntil != nil && now.Before(*cfg.UrgentPauseUntil) {
			return false
		}
		return true
	}

	if InQuietHours(hours, now) {
		return false
	}
	return true
}

func InQuietHours(hours QuietHours, now time.Time) bool {
	if hours.WeekendExtended {
		switch now.Weekday() {
		case time.Saturday, time.Sunday:
			return true
		}
	}

	if hours.Start == hours.End {
		return false
	}

	dayDur := time.Duration(now.Hour())*time.Hour +
		time.Duration(now.Minute())*time.Minute +
		time.Duration(now.Second())*time.Second

	if hours.Start < hours.End {

		return dayDur >= hours.Start && dayDur < hours.End
	}

	return dayDur >= hours.Start || dayDur < hours.End
}

type notificationsTOML struct {
	QuietHours struct {
		Start           string `toml:"start"`
		End             string `toml:"end"`
		WeekendExtended bool   `toml:"weekend_extended"`
		UrgentBypass    bool   `toml:"urgent_bypass"`
	} `toml:"quiet_hours"`
}

func LoadQuietConfig(path string) (QuietConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return QuietConfig{}, fmt.Errorf("%w: read %q: %v", ErrInvalidQuietConfig, path, err)
	}
	var t notificationsTOML
	if _, err := toml.Decode(string(raw), &t); err != nil {
		return QuietConfig{}, fmt.Errorf("%w: parse %q: %v", ErrInvalidQuietConfig, path, err)
	}

	start, err := parseHHMM(t.QuietHours.Start)
	if err != nil {
		return QuietConfig{}, fmt.Errorf("%w: start %q: %v", ErrInvalidQuietConfig, t.QuietHours.Start, err)
	}
	end, err := parseHHMM(t.QuietHours.End)
	if err != nil {
		return QuietConfig{}, fmt.Errorf("%w: end %q: %v", ErrInvalidQuietConfig, t.QuietHours.End, err)
	}

	return QuietConfig{
		Default: QuietHours{
			Start:           start,
			End:             end,
			WeekendExtended: t.QuietHours.WeekendExtended,
			UrgentBypass:    t.QuietHours.UrgentBypass,
		},
		PerProject: make(map[string]QuietHours),
	}, nil
}

func parseHHMM(s string) (time.Duration, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, err
	}
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute, nil
}

func writeFileImpl(path, contents string) error {
	return os.WriteFile(path, []byte(contents), 0o600)
}

func quietHoursUrgentBypassSentinel() error {
	cfg := QuietConfig{
		Default: QuietHours{
			Start:        21 * time.Hour,
			End:          9 * time.Hour,
			UrgentBypass: true,
		},
	}
	now := time.Unix(0, 0).UTC()
	n := Notification{
		ProjectID:   "a" + "0000000000000000000000000000000000000000000000000000000000000",
		Severity:    SeverityUrgent,
		EventType:   "anchor.event",
		ContentHash: "0000000000000000000000000000000000000000000000000000000000000000",
		CreatedAt:   now,
	}
	_ = ShouldEmit(n, cfg, now)
	return ErrQuietHoursUrgentBypassAnchor
}
