// SPDX-License-Identifier: MIT
// Package cli — doctor_plan7.go
//
// subcommands. Each subcommand executes ONE per-subsystem probe (defined
// in doctor_<sys>.go from J-3..J-6) and renders the resulting probe slice
// with the legacy `flutter doctor` glyph layout (RenderProbes).
//
// The 4 subcommands share a small composition root (buildDoctorDeps) that
// resolves the daemon HTTP client from the inherited --uds flag and wires
// per-subsystem probers from the daemon HTTP responses ( lands the
// daemon-side HTTP probe endpoints; J-7 ships the CLI seam with
// nil probers, RunFullProbe gracefully emits Warn rows for nil sections).
//
// invariant boundary: NewDoctorKnowledgeCmd etc. live in internal/cli;
// they instantiate KnowledgeProber implementations through the daemon's
// HTTP layer (or, in pre-wiring, leave the prober nil and the
// helper emits a Warn no-op probe). Tests substitute a fake DoctorDeps
// directly via the exported RunXxxProbeWithDeps helpers — no daemon
// boot required.
//
// probe HTTP endpoints, buildDoctorDeps returns DoctorDeps with all four
// per-subsystem prober fields nil. RunFullProbe + invokeXxxProber emit one
// Warn ProbeResult per nil section ("prober not configured") so operators
// see the unwired surface rather than a silent gap.
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/spf13/cobra"
)

func buildDoctorDeps(udsPath string, strict bool) (DoctorDeps, error) {
	c := client.New(udsPath)
	return DoctorDeps{
		Client: c,
		Strict: strict,
	}, nil
}

func resolveDoctorFlags(cmd *cobra.Command) (udsPath string, strict bool) {
	udsPath, _ = cmd.Root().PersistentFlags().GetString("uds")
	if udsPath == "" {
		udsPath, _ = cmd.PersistentFlags().GetString("uds")
	}
	if udsPath == "" {

		for p := cmd; p != nil; p = p.Parent() {
			if v, _ := p.PersistentFlags().GetString("uds"); v != "" {
				udsPath = v
				break
			}
		}
	}
	if udsPath == "" {
		udsPath = "/tmp/hades-system.sock"
	}

	for p := cmd; p != nil; p = p.Parent() {
		if f := p.PersistentFlags().Lookup("strict"); f != nil {
			strict = f.Value.String() == "true"
			break
		}
	}
	return udsPath, strict
}

var runKnowledgeProbeWithDepsFunc = RunKnowledgeProbeWithDeps

var runSchedulerProbeWithDepsFunc = RunSchedulerProbeWithDeps

var runInboxProbeWithDepsFunc = RunInboxProbeWithDeps

var runTmuxProbeWithDepsFunc = RunTmuxProbeWithDeps

var buildDoctorDepsFunc = buildDoctorDeps

func NewDoctorKnowledgeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "knowledge",
		Short: "Diagnose knowledge subsystem (FTS5 index, indexer, watcher)",
		Long: `Run a 5-aspect probe of the knowledge subsystem (per spec §6.7):

  knowledge.index.integrity            PRAGMA integrity_check on the FTS5 db
  knowledge.index.last_indexed         staleness vs last fsnotify-driven write
  knowledge.indexer.cpu_budget         indexer CPU usage vs doctrine warn/fail
  knowledge.watcher.status             fsnotify watcher heartbeat freshness
  knowledge.extension_hooks.null_default Plan 9/14 hook columns NULL-by-default

Output uses the legacy ` + "`flutter doctor`" + ` glyph layout. The --strict
parent flag escalates Warn rows to non-zero exit so CI gates can fail
on early-warning thresholds.

Exit codes:
  0  every aspect OK (or only Warns without --strict)
  1  any aspect Fail OR (any Warn AND --strict)
  2  unrecoverable: prober wiring, transport`,
		Example: `  # Probe the knowledge subsystem
  hades doctor knowledge

  # CI gate: fail on Warn rows too
  hades doctor knowledge --strict`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			udsPath, strict := resolveDoctorFlags(cmd)
			deps, err := buildDoctorDepsFunc(udsPath, strict)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			probes, err := runKnowledgeProbeWithDepsFunc(ctx, deps)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Knowledge:")
			fmt.Fprint(out, RenderProbes(probes))
			if code := ExitCode(probes, strict); code != 0 {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("knowledge probe failed (exit %d)", code))
			}
			return nil
		},
	}
}

func NewDoctorSchedulerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scheduler",
		Short: "Diagnose scheduler subsystem (queue, missed fires, WFQ)",
		Long: `Run a 4-aspect probe of the scheduler subsystem (per spec §6.7):

  scheduler.queue.depth           pending fires across daemon
  scheduler.missed_fires.recent   MissedFire events in last 24h
  scheduler.wfq.saturation        max WFQ saturation across active queues
  scheduler.dispatcher.bound      Plan 3 dispatcher reachable (inv-hades-080,
                                  inv-hades-123)

The --strict parent flag escalates Warn rows to non-zero exit so CI
gates can fail on early-warning thresholds.

Exit codes:
  0  every aspect OK (or only Warns without --strict)
  1  any aspect Fail OR (any Warn AND --strict)
  2  unrecoverable: prober wiring, transport`,
		Example: `  # Probe the scheduler subsystem
  hades doctor scheduler

  # CI gate: fail on Warn rows too
  hades doctor scheduler --strict`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			udsPath, strict := resolveDoctorFlags(cmd)
			deps, err := buildDoctorDepsFunc(udsPath, strict)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			probes, err := runSchedulerProbeWithDepsFunc(ctx, deps)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Scheduler:")
			fmt.Fprint(out, RenderProbes(probes))
			if code := ExitCode(probes, strict); code != 0 {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("scheduler probe failed (exit %d)", code))
			}
			return nil
		},
	}
}

func NewDoctorInboxCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inbox",
		Short: "Diagnose inbox subsystem (aggregator cache, outbox, dedup)",
		Long: `Run a 4-aspect probe of the inbox subsystem (per spec §6.7):

  inbox.aggregator.cache.consistent   per-project counts == aggregator cache
                                      (inv-hades-113)
  inbox.outbox.queue.depth            undelivered notifications in outbox
  inbox.dedup.window.health           UNIQUE constraint violations (should be 0)
  inbox.severity.distribution         24h breakdown across the 4-tier enum
                                      (urgent / action-needed / info-immediate /
                                      info-digest; inv-hades-124)

The --strict parent flag escalates Warn rows to non-zero exit so CI
gates can fail on early-warning thresholds.

Exit codes:
  0  every aspect OK (or only Warns without --strict)
  1  any aspect Fail OR (any Warn AND --strict)
  2  unrecoverable: prober wiring, transport`,
		Example: `  # Probe the inbox subsystem
  hades doctor inbox

  # CI gate: fail on Warn rows too
  hades doctor inbox --strict`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			udsPath, strict := resolveDoctorFlags(cmd)
			deps, err := buildDoctorDepsFunc(udsPath, strict)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			probes, err := runInboxProbeWithDepsFunc(ctx, deps)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Inbox:")
			fmt.Fprint(out, RenderProbes(probes))
			if code := ExitCode(probes, strict); code != 0 {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("inbox probe failed (exit %d)", code))
			}
			return nil
		},
	}
}

func NewDoctorTmuxCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tmux",
		Short: "Diagnose tmux subsystem (binary, server, sessions, drift)",
		Long: `Run a 5-aspect probe of the tmux subsystem (per spec §6.7):

  tmux.binary.installed     tmux on PATH + version ≥ 3.4 (spec §1 Q5)
  tmux.server.reachable     /tmp/hades-system.sock responsive (inv-hades-117)
  tmux.session.count        active sessions in daemon.db
  tmux.drift.count          orphaned sessions (db says active but tmux disagrees)
  tmux.socket.permissions   /tmp/hades-system.sock mode == 0600 (spec §7.3)

Inv-hades-117 anchor: the prober delegates to the live tmux adapter which
enforces the dedicated -S socket flag (forbids the default
/tmp/tmux-<uid>) so the probe never races operator's personal tmux
server.

The --strict parent flag escalates Warn rows to non-zero exit so CI
gates can fail on early-warning thresholds.

Exit codes:
  0  every aspect OK (or only Warns without --strict)
  1  any aspect Fail OR (any Warn AND --strict)
  2  unrecoverable: prober wiring, transport`,
		Example: `  # Probe the tmux subsystem
  hades doctor tmux

  # CI gate: fail on Warn rows too
  hades doctor tmux --strict`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			udsPath, strict := resolveDoctorFlags(cmd)
			deps, err := buildDoctorDepsFunc(udsPath, strict)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			probes, err := runTmuxProbeWithDepsFunc(ctx, deps)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Tmux:")
			fmt.Fprint(out, RenderProbes(probes))
			if code := ExitCode(probes, strict); code != 0 {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("tmux probe failed (exit %d)", code))
			}
			return nil
		},
	}
}
