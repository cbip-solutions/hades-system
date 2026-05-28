// SPDX-License-Identifier: MIT
// Package main — subsystem_snapshot.go
//
// Every 5 minutes, invoke each HADES design subsystem's prober via
// Server.SubsystemProbe and emit a structured slog line with the
// per-status counts ("ok", "warn", "fail"). The 5-min cadence is the
// canonical interval declared in spec §J-7 + §6.7; faster cadences burn
// SQLite read budget without giving operators meaningfully fresher data
// (per-subsystem state changes on minute timescales at the fastest).
//
// The slog logger is the daemon's existing default (constructed in main()
// via slog.NewTextHandler over os.Stderr). The snapshot logger uses
// `slog.With("subsystem", name)` to scope each subsystem's log line so
// operators grepping `subsystem=knowledge` see only knowledge events.
// This is the HADES design application of the "structured logs are queryable
// substrate" doctrine — the snapshot timeline is the substrate HADES design
// hash-chain extension hooks will later anchor (per design choice D).
package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon"
)

const snapshotInterval = 5 * time.Minute

var snapshotSubsystems = []string{"knowledge", "scheduler", "inbox", "tmux"}

func runSubsystemSnapshotLogger(ctx context.Context, srv *daemon.Server, logger *slog.Logger) {
	ticker := time.NewTicker(snapshotInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, name := range snapshotSubsystems {
				snapshotSubsystem(ctx, srv, name, logger)
			}
		}
	}
}

func snapshotSubsystem(ctx context.Context, srv *daemon.Server, name string, logger *slog.Logger) {
	subLogger := logger.With(slog.String("subsystem", name))
	probes, err := srv.SubsystemProbe(ctx, name)
	if err != nil {
		subLogger.Warn("subsystem probe error",
			slog.String("err", err.Error()))
		return
	}
	if len(probes) == 0 {

		subLogger.Info("subsystem unwired",
			slog.Int("ok", 0), slog.Int("warn", 0), slog.Int("fail", 0), slog.Int("total", 0))
		return
	}
	okCount, warnCount, failCount := 0, 0, 0
	for _, p := range probes {
		switch p.Status {
		case "ok":
			okCount++
		case "warn":
			warnCount++
		case "fail":
			failCount++
		}
	}
	level := slog.LevelInfo
	if failCount > 0 {
		level = slog.LevelError
	} else if warnCount > 0 {
		level = slog.LevelWarn
	}
	subLogger.Log(ctx, level, "subsystem health snapshot",
		slog.Int("ok", okCount),
		slog.Int("warn", warnCount),
		slog.Int("fail", failCount),
		slog.Int("total", len(probes)))
}
