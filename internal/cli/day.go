// SPDX-License-Identifier: MIT
// Package cli — day.go.
//
// `zen day [--force | --eod | --check-pending]` dispatches to the
// daemon-side zen day generator façade and renders the returned
// BriefDoc to stdout via zenday.Render. Default invocation = morning
// brief. --eod runs the EOD digest; --check-pending runs the
// introspection preview; --force overrides today's-archive
// idempotency.
//
// Pre-release history: this file previously hosted the release-6
// morning-brief composer that issued multiple GET /v1/*/summary calls
// from the CLI side and stitched the markdown locally. release
// consolidates the brief into a single daemon-side composer
// (zenday.Generator, F-7..F-9) so the CLI is now a thin dispatcher:
// one POST round-trip + Render. The release-6 sections (workforce,
// research, audit, budget, sshexec, health, autonomy, merge,
// notifications, bypass) are now sourced server-side via
// zenday.Collect's parallel fan-out.
//
// --include-bypass is preserved as a no-op flag (with a stderr
// deprecation notice) for backward compat with operators who scripted
// the release form. The deprecation message points at the new behaviour
// — the brief always consolidates every source as of release — so
// removing the flag in the next major can happen without surprise.
//
// Exit-code contract via cmd.RunE / IsRecoverable:
//
// - 0 — brief rendered to stdout.
// - 1 — operator-recoverable (e.g. 409 idempotency without --force,
// daemon UDS missing); cli/main maps via IsRecoverable.
// - 2 — unrecoverable (network, decode, daemon 5xx).
package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/cbip-solutions/hades-system/internal/zenday"
)

const dayClientTimeout = 30 * time.Second

type ZenDayClient interface {
	GenerateMorning(ctx context.Context, force bool) (zenday.BriefDoc, error)
	GenerateEOD(ctx context.Context, force bool) (zenday.BriefDoc, error)
	CheckPending(ctx context.Context) (zenday.BriefDoc, error)
}

type httpDayClient struct {
	c *client.Client
}

func (h *httpDayClient) GenerateMorning(ctx context.Context, force bool) (zenday.BriefDoc, error) {
	return h.c.DayMorning(ctx, force)
}

func (h *httpDayClient) GenerateEOD(ctx context.Context, force bool) (zenday.BriefDoc, error) {
	return h.c.DayEOD(ctx, force)
}

func (h *httpDayClient) CheckPending(ctx context.Context) (zenday.BriefDoc, error) {
	return h.c.DayCheckPending(ctx)
}

func dayClientFromHTTP(c *client.Client) ZenDayClient {
	return &httpDayClient{c: c}
}

// NewDayCmd builds `zen day`. Registered by internal/cli/root.go.
//
// Flag precedence (locked by tests in day_test.go):
//
// - --check-pending wins over --eod and --force (introspection is
// read-only; force is irrelevant).
// - --eod wins over default morning when both flags would otherwise
// match (no flag = morning).
// - --force applies to whichever path is selected (morning / eod);
// no effect on check-pending.
// - --include-bypass is a no-op with a stderr deprecation warning.
//
// The function signature is preserved across the F-10 rewrite so
// root.go's `root.AddCommand(NewDayCmd())` registration line is
// unchanged.
func NewDayCmd() *cobra.Command {
	var (
		force         bool
		eod           bool
		checkPending  bool
		includeBypass bool
	)

	cmd := &cobra.Command{
		Use:   "day",
		Short: "Show today's zen day brief (morning), EOD digest, or check-pending preview",
		Long: `zen day composes today's morning brief, EOD digest, or check-pending
introspection per spec §1 Q13/Q14/Q15. Default invocation is the
morning brief. --eod runs the EOD digest; --check-pending shows
next-fire + pending-since-last counts. --force overrides today's-
archive idempotency for morning / eod (no effect on check-pending).

The brief honours the 7-item hard cap (inv-zen-126) and canonical
leverage rank order (inv-zen-127). Truncation marker
"+ N more in zen inbox --since 24h" appears when more items exist
than the cap allows.

--include-bypass is a legacy Plan 2 flag preserved as a no-op for
backward compat with scripted callers; the brief now consolidates
all sources (bypass health is rendered alongside the other sections
by the daemon-side composer).`,
		Example: `  # Default: today's morning brief (idempotent)
  zen day

  # Force regeneration even if today's brief is already archived
  zen day --force

  # End-of-day digest
  zen day --eod

  # Check what would fire next + pending-since-last counts (read-only)
  zen day --check-pending`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), dayClientTimeout)
			defer cancel()

			if includeBypass {
				fmt.Fprintln(cmd.ErrOrStderr(),
					"warn: --include-bypass is a legacy Plan 2 no-op; the brief now consolidates every source")
			}

			c := dayClientFromHTTP(newClientFromCmd(cmd))
			return runDay(ctx, c, cmd.OutOrStdout(), force, eod, checkPending)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "regenerate even if today's brief already exists")
	cmd.Flags().BoolVar(&eod, "eod", false, "run EOD digest instead of morning brief")
	cmd.Flags().BoolVar(&checkPending, "check-pending", false,
		"show next-fire + pending-since-last counts (read-only introspection)")
	cmd.Flags().BoolVar(&includeBypass, "include-bypass", false,
		"legacy Plan 2 no-op flag; the brief always consolidates every source")
	return cmd
}

func runDay(ctx context.Context, c ZenDayClient, out io.Writer, force, eod, checkPending bool) error {
	doc, err := dispatchDay(ctx, c, force, eod, checkPending)
	if err != nil {
		return classifyDayError(err, dayPathFor(eod, checkPending))
	}
	fmt.Fprint(out, zenday.Render(doc))
	return nil
}

func dispatchDay(ctx context.Context, c ZenDayClient, force, eod, checkPending bool) (zenday.BriefDoc, error) {
	switch {
	case checkPending:
		return c.CheckPending(ctx)
	case eod:
		return c.GenerateEOD(ctx, force)
	default:
		return c.GenerateMorning(ctx, force)
	}
}

func dayPathFor(eod, checkPending bool) string {
	switch {
	case checkPending:
		return "check-pending"
	case eod:
		return "eod"
	default:
		return "morning"
	}
}

func classifyDayError(err error, path string) error {
	if err == nil {
		return nil
	}
	if client.IsHTTPStatus(err, http.StatusConflict) {
		return ierrors.Wrap(ierrors.Code("daemon.unreachable"), recoverableWrap(err,
			fmt.Sprintf("zen day %s: today's brief already exists (use --force to overwrite)", path)))
	}
	if client.IsHTTPStatus(err, http.StatusServiceUnavailable) {
		return ierrors.Wrap(ierrors.Code("daemon.not-running"), recoverableWrap(err,
			fmt.Sprintf("zen day %s: daemon not ready (start it with: zen daemon start)", path)))
	}
	return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("zen day %s: %w", path, err))
}
