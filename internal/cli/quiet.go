// SPDX-License-Identifier: MIT
// Package cli — quiet.go.
//
// `zen quiet` is the operator-facing entry point for inspecting the
// quiet-hours config and managing the runtime urgent-pause window
// (spec §6.5).
//
// Three flag-driven actions, mutually exclusive:
//
// zen quiet [--list] # default: list config
// zen quiet --urgent-pause <duration> # disable urgent-bypass for N
// zen quiet --cancel # cancel active urgent-pause
//
// # Examples
//
// $ zen quiet --list
// Quiet hours (operator default): 21:00 — 09:00 (weekdays + extended weekends)
// Override (active): none
// Urgent severity bypass: enabled
//
// $ zen quiet --urgent-pause 30m
// Urgent bypass paused for 30m (resumes 2026-05-01 12:30 UTC)
//
// $ zen quiet --cancel
// Urgent pause cancelled
//
// The CLI does NOT directly write notifications.toml — operator edits
// that file manually (file-as-source-of-truth per spec §6.5); the CLI
// reads + renders, and manages only the runtime UrgentPauseUntil
// state via --urgent-pause / --cancel.
//
// Production wires through NewQuietCmdProd which adapts *client.Client
// → QuietClient. Tests inject a fake client via the QuietClientFactory
// parameter to NewQuietCmd; this mirrors the release D-13 schedule + E-12
// inbox dependency-injection pattern.
//
// Exit-code mapping (per spec §6.2; ErrRecoverable contract from
// ):
// - 0 success
// - 1 operator-recoverable: invalid duration on --urgent-pause,
// conflicting flags (e.g., --list + --cancel).
// - 2 unrecoverable: transport, decode, daemon 5xx, daemon 503
// (until SetQuietStore wires; mirrors release /v1/messages
// graceful-degradation pattern).
//
// File-as-source-of-truth: the persistent config (Default + PerProject)
// is operator-edited TOML and only RE-LOADED via daemon SIGHUP or
// restart. The runtime UrgentPauseUntil is purely in-memory at the
// daemon side; --urgent-pause sets it, --cancel clears it. The CLI
// renders both halves uniformly so the operator sees one screen.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/cbip-solutions/hades-system/internal/inbox"
)

type QuietClient interface {
	Get(ctx context.Context) (inbox.QuietConfig, error)
	SetUrgentPause(ctx context.Context, until time.Time) error
	CancelUrgentPause(ctx context.Context) error
}

type QuietClientFactory func(cmd *cobra.Command) QuietClient

const quietTimeout = 5 * time.Second

func NewQuietCmd(factory QuietClientFactory) *cobra.Command {
	var (
		listFlag        bool
		urgentPauseFlag string
		cancelFlag      bool
	)
	cmd := &cobra.Command{
		Use:   "quiet",
		Short: "Inspect quiet hours + manage urgent-pause window",
		Long: `Inspect the quiet-hours config (operator default + per-project
overrides + active urgent-pause) and manage the runtime urgent-pause
window.

Persistent config lives in ~/.config/zen-swarm/notifications.toml
(operator-edited). The CLI reads + renders that file via the daemon;
only the runtime UrgentPauseUntil state is mutated via
--urgent-pause / --cancel.

Mutually exclusive: pass exactly one of --list / --urgent-pause /
--cancel (bare invocation defaults to --list).`,
		Example: `  # Inspect current quiet-hours config + active pause
  zen quiet

  # Disable urgent-bypass for the next 30 minutes (e.g. during a meeting)
  zen quiet --urgent-pause 30m

  # Cancel an active urgent-pause early
  zen quiet --cancel`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {

			actions := 0
			if listFlag {
				actions++
			}
			if urgentPauseFlag != "" {
				actions++
			}
			if cancelFlag {
				actions++
			}
			if actions > 1 {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--list / --urgent-pause / --cancel are mutually exclusive"))
			}

			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), quietTimeout)
			defer cancel()

			now := time.Now().UTC()
			switch {
			case urgentPauseFlag != "":
				return RunQuietPause(ctx, c, urgentPauseFlag, now, cmd.OutOrStdout())
			case cancelFlag:
				return RunQuietCancel(ctx, c, cmd.OutOrStdout())
			default:

				return RunQuietList(ctx, c, cmd.OutOrStdout(), now)
			}
		},
	}
	cmd.Flags().BoolVar(&listFlag, "list", false, "render quiet config + active pause (default)")
	cmd.Flags().StringVar(&urgentPauseFlag, "urgent-pause", "", "disable urgent-bypass for the given Go duration (e.g., 30m, 8h)")
	cmd.Flags().BoolVar(&cancelFlag, "cancel", false, "cancel active urgent-pause")
	return cmd
}

func NewQuietCmdProd() *cobra.Command {
	return NewQuietCmd(func(cmd *cobra.Command) QuietClient {
		return &productionQuietClient{c: newClientFromCmd(cmd)}
	})
}

type productionQuietClient struct {
	c *client.Client
}

func (p *productionQuietClient) Get(ctx context.Context) (inbox.QuietConfig, error) {
	resp, err := p.c.QuietGet(ctx)
	if err != nil {
		return inbox.QuietConfig{}, err
	}
	cfg := inbox.QuietConfig{
		Default:    quietHoursFromWire(resp.Default),
		PerProject: make(map[string]inbox.QuietHours, len(resp.PerProject)),
	}
	for projectID, hours := range resp.PerProject {
		cfg.PerProject[projectID] = quietHoursFromWire(hours)
	}
	if resp.UrgentPauseUntil != nil {
		t := *resp.UrgentPauseUntil
		cfg.UrgentPauseUntil = &t
	}
	return cfg, nil
}

func (p *productionQuietClient) SetUrgentPause(ctx context.Context, until time.Time) error {
	return p.c.QuietUrgentPause(ctx, until)
}

func (p *productionQuietClient) CancelUrgentPause(ctx context.Context) error {
	return p.c.QuietCancel(ctx)
}

func quietHoursFromWire(w client.QuietHoursWire) inbox.QuietHours {
	return inbox.QuietHours{
		Start:           time.Duration(w.StartSec) * time.Second,
		End:             time.Duration(w.EndSec) * time.Second,
		WeekendExtended: w.WeekendExtended,
		UrgentBypass:    w.UrgentBypass,
	}
}

func RunQuietList(ctx context.Context, c QuietClient, w io.Writer, now time.Time) error {
	cfg, err := c.Get(ctx)
	if err != nil {
		return classifyQuietError(err, "list")
	}

	weekendNote := "weekdays only"
	if cfg.Default.WeekendExtended {
		weekendNote = "weekdays + extended weekends"
	}

	startHHMM := formatHHMM(cfg.Default.Start)
	endHHMM := formatHHMM(cfg.Default.End)

	fmt.Fprintf(w, "Quiet hours (operator default):  %s — %s (%s)\n",
		startHHMM, endHHMM, weekendNote)

	if cfg.UrgentPauseUntil != nil && cfg.UrgentPauseUntil.After(now) {
		remaining := cfg.UrgentPauseUntil.Sub(now).Round(time.Second)
		fmt.Fprintf(w, "Urgent pause active: resumes %s (in %s)\n",
			cfg.UrgentPauseUntil.Format(time.RFC3339), remaining)
	} else {
		fmt.Fprintln(w, "Override (active): none")
	}

	bypass := "enabled"
	if !cfg.Default.UrgentBypass {
		bypass = "disabled (operator opted out)"
	}
	fmt.Fprintf(w, "Urgent severity bypass: %s\n", bypass)

	return nil
}

func formatHHMM(d time.Duration) string {
	hours := int(d / time.Hour)
	mins := int((d % time.Hour) / time.Minute)
	return fmt.Sprintf("%02d:%02d", hours, mins)
}

func RunQuietPause(ctx context.Context, c QuietClient, durationSpec string, now time.Time, w io.Writer) error {
	d, err := time.ParseDuration(durationSpec)
	if err != nil {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err,
			fmt.Sprintf("--urgent-pause %q must be a Go duration like 30m, 8h, 24h", durationSpec)))
	}
	if d <= 0 {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--urgent-pause %q must be positive (got %s)", durationSpec, d))
	}
	until := now.Add(d).UTC()
	if err := c.SetUrgentPause(ctx, until); err != nil {
		return classifyQuietError(err, "urgent-pause")
	}
	fmt.Fprintf(w, "Urgent bypass paused for %s (resumes %s)\n",
		durationSpec, until.Format(time.RFC3339))
	return nil
}

func RunQuietCancel(ctx context.Context, c QuietClient, w io.Writer) error {
	if err := c.CancelUrgentPause(ctx); err != nil {
		return classifyQuietError(err, "cancel")
	}
	fmt.Fprintln(w, "Urgent pause cancelled")
	return nil
}

func classifyQuietError(err error, op string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrRecoverable) {
		return err
	}
	if client.IsHTTPStatus(err, http.StatusUnprocessableEntity) {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, fmt.Sprintf("quiet: %s: daemon rejected input", op)))
	}
	return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("quiet: %s: %w", op, err))
}
