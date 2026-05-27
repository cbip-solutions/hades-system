// SPDX-License-Identifier: MIT
// Package hadesday — `RegisterDefaultRoutines` cron registration helper +
// `~/.config/hades-system/hades-day.toml` operator override.
//
// Per spec §1 Q13, hades day owns two default schedules:
//
// - Morning brief: `0 8 * * 1-5` (Mon-Fri 08:00 local) with a 2h
// `if_within` catch-up window (MissPolicyCatchUpBounded).
// - EOD digest: `0 18 * * 1-5` (Mon-Fri 18:00 local) with a 4h
// `if_within` catch-up window.
//
// Both schedules are persisted as `scheduler.TierRoutine` rows under
// project alias `_global` (the cross-project flow loop, distinct from
// any individual project alias). The Action field carries the well-known
// dispatch token (`morning-brief` or `eod-digest`) `scheduler.Fire`
// routes to the hadesday composer.
//
// invariant boundary: this file declares its only persistence
// dependency as the package-local view of `scheduler.Store` — the same
// 9-method interface scheduler/ uses internally and that the daemon
// satisfies via `internal/daemon/scheduleradapter.Adapter`. hadesday
// never imports `internal/store`.
//
// Idempotency contract (Q13 verbatim Clawpilot): RegisterDefaultRoutines
// runs at every daemon boot. When a row with the same ID + cron
// expression already exists, the call is a no-op (a single Get + zero
// mutations). When the operator edits hades-day.toml between boots and
// the new cron differs, the existing row is dropped + re-inserted with
// the new expression. This avoids partial-update edge cases (e.g.
// stale NextRunAt under a freshly edited cron) at the cost of resetting
// the cron cursor — acceptable for routine briefs whose phase is fixed
// to wall-clock 08:00 / 18:00.
//
// Operator override file shape (hades-day.toml):
//
// [morning_brief]
// cron = "0 9 * * *" # default "0 8 * * 1-5"
// if_within_hours = 3 # default 2
// enabled = true # default true; false skips registration
//
// [eod_digest]
// cron = "0 19 * * *" # default "0 18 * * 1-5"
// if_within_hours = 5 # default 4
// enabled = true # default true; false skips registration
//
// Missing file → defaults; missing fields per section → defaults for
// just those fields (see LoadHadesDayConfig).
package hadesday

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/cbip-solutions/hades-system/internal/scheduler"
)

const (
	DefaultMorningCron = "0 8 * * 1-5"

	DefaultEODCron = "0 18 * * 1-5"

	DefaultMorningIfWithinHours = 2

	DefaultEODIfWithinHours = 4

	MorningRoutineID = "hadesday-morning"

	EODRoutineID = "hadesday-eod"

	MorningActionToken = "morning-brief"

	EODActionToken = "eod-digest"

	GlobalProjectAlias = "_global"
)

type cronRegistrationStore interface {
	Insert(ctx context.Context, s *scheduler.Schedule) error
	Get(ctx context.Context, id string) (*scheduler.Schedule, error)
	Delete(ctx context.Context, id string) error
}

type HadesDayConfig struct {
	MorningBrief BriefScheduleConfig `toml:"morning_brief"`
	EODDigest    BriefScheduleConfig `toml:"eod_digest"`
}

type BriefScheduleConfig struct {
	Cron          string `toml:"cron"`
	IfWithinHours int    `toml:"if_within_hours"`
	Enabled       bool   `toml:"enabled"`
}

var configRelPath = filepath.Join(".config", "hades-system", "hades-day.toml")

func LoadHadesDayConfig(homeDir string) (HadesDayConfig, error) {
	cfg := HadesDayConfig{
		MorningBrief: BriefScheduleConfig{
			Cron:          DefaultMorningCron,
			IfWithinHours: DefaultMorningIfWithinHours,
			Enabled:       true,
		},
		EODDigest: BriefScheduleConfig{
			Cron:          DefaultEODCron,
			IfWithinHours: DefaultEODIfWithinHours,
			Enabled:       true,
		},
	}
	path := filepath.Join(homeDir, configRelPath)
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("hadesday: read %q: %w", path, err)
	}
	if _, err := toml.Decode(string(body), &cfg); err != nil {
		return cfg, fmt.Errorf("hadesday: parse %q: %w", path, err)
	}

	if cfg.MorningBrief.Cron == "" {
		cfg.MorningBrief.Cron = DefaultMorningCron
	}
	if cfg.EODDigest.Cron == "" {
		cfg.EODDigest.Cron = DefaultEODCron
	}
	return cfg, nil
}

func RegisterDefaultRoutines(ctx context.Context, store scheduler.Store, homeDir string, now time.Time) error {
	cfg, err := LoadHadesDayConfig(homeDir)
	if err != nil {
		return err
	}

	type entry struct {
		id     string
		action string
		cfg    BriefScheduleConfig
	}
	entries := []entry{
		{id: MorningRoutineID, action: MorningActionToken, cfg: cfg.MorningBrief},
		{id: EODRoutineID, action: EODActionToken, cfg: cfg.EODDigest},
	}
	for _, e := range entries {
		if !e.cfg.Enabled {
			continue
		}
		if err := upsertRoutine(ctx, store, e.id, e.action, e.cfg, now); err != nil {
			return fmt.Errorf("hadesday: %s routine: %w", e.id, err)
		}
	}
	return nil
}

func upsertRoutine(ctx context.Context, store cronRegistrationStore, id, action string, cfg BriefScheduleConfig, now time.Time) error {
	existing, err := store.Get(ctx, id)
	if err == nil && existing != nil {
		if existing.TriggerConfig.CronExpr == cfg.Cron {

			return nil
		}

		if err := store.Delete(ctx, id); err != nil {
			return fmt.Errorf("delete stale row: %w", err)
		}
	} else if err != nil && !errors.Is(err, scheduler.ErrNotFound) {

		return fmt.Errorf("probe existing row: %w", err)
	}
	s := &scheduler.Schedule{
		ID:           id,
		Tier:         scheduler.TierRoutine,
		ProjectAlias: GlobalProjectAlias,
		Action:       action,
		TriggerType:  scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{
			CronExpr: cfg.Cron,
		},
		MissPolicy:   scheduler.MissPolicyCatchUpBounded,
		MissLookback: time.Duration(cfg.IfWithinHours) * time.Hour,
		Status:       scheduler.StatusEnabled,
		CreatedAt:    now,
	}
	return store.Insert(ctx, s)
}
