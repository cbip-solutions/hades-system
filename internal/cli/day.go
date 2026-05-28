// SPDX-License-Identifier: MIT
// Package cli — day.go.
//
// `hades day [--force | --eod | --check-pending]` dispatches to the
// daemon-side hades day generator façade and renders the returned
// BriefDoc to stdout via hadesday.Render. Default invocation = morning
// brief. --eod runs the EOD digest; --check-pending runs the
// introspection preview; --force overrides today's-archive
// idempotency.
//
// Pre-HADES design history: this file previously hosted the HADES design
// morning-brief composer that issued multiple GET /v1/*/summary calls
// from the CLI side and stitched the markdown locally. HADES design
// consolidates the brief into a single daemon-side composer
// (hadesday.Generator, F-7..F-9) so the CLI is now a thin dispatcher:
// one POST round-trip + Render. The HADES design sections (workforce,
// research, audit, budget, sshexec, health, autonomy, merge,
// notifications, bypass) are now sourced server-side via
// hadesday.Collect's parallel fan-out.
//
// --include-bypass is preserved as a no-op flag (with a stderr
// deprecation notice) for backward compat with operators who scripted
// the HADES design form. The deprecation message points at the new behaviour
// — the brief always consolidates every source as of HADES design — so
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
	"github.com/cbip-solutions/hades-system/internal/hadesday"
)

const dayClientTimeout = 30 * time.Second

type HadesDayClient interface {
	GenerateMorning(ctx context.Context, force bool) (hadesday.BriefDoc, error)
	GenerateEOD(ctx context.Context, force bool) (hadesday.BriefDoc, error)
	CheckPending(ctx context.Context) (hadesday.BriefDoc, error)
}

type httpDayClient struct {
	c *client.Client
}

func (h *httpDayClient) GenerateMorning(ctx context.Context, force bool) (hadesday.BriefDoc, error) {
	return h.c.DayMorning(ctx, force)
}

func (h *httpDayClient) GenerateEOD(ctx context.Context, force bool) (hadesday.BriefDoc, error) {
	return h.c.DayEOD(ctx, force)
}

func (h *httpDayClient) CheckPending(ctx context.Context) (hadesday.BriefDoc, error) {
	return h.c.DayCheckPending(ctx)
}

func dayClientFromHTTP(c *client.Client) HadesDayClient {
	return &httpDayClient{c: c}
}

// NewDayCmd builds `hades day`. Registered by internal/cli/root.go.
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
		Short: "Show today's hades day brief (morning), EOD digest, or check-pending preview",
		Long:  "hades day composes today's morning brief, EOD digest, or check-pending\nintrospection per design contract\nmorning brief. --eod runs the EOD digest; --check-pending shows\nnext-fire + pending-since-last counts. --force overrides today's-\narchive idempotency for morning / eod (no effect on check-pending).\n\nThe brief honours the 7-item hard cap (invariant) and canonical\nleverage rank order (invariant). Truncation marker\n\"+ N more in hades inbox --since 24h\" appears when more items exist\nthan the cap allows.\n\n--include-bypass is a legacy HADES design flag preserved as a no-op for\nbackward compat with scripted callers; the brief now consolidates\nall sources (bypass health is rendered alongside the other sections\nby the daemon-side composer).",

		Example: " # Default: today's morning brief (idempotent)\n  hades day\n\n # Force regeneration even if today's brief is already archived\n  hades day --force\n\n # End-of-day digest\n  hades day --eod\n\n # Check what would fire next + pending-since-last counts (read-only)\n  hades day --check-pending",

		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), dayClientTimeout)
			defer cancel()

			if includeBypass {
				fmt.Fprintln(cmd.ErrOrStderr(),
					"warn: --include-bypass is a legacy HADES design no-op; the brief now consolidates every source")
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
		"legacy HADES design no-op flag; the brief always consolidates every source")
	return cmd
}

func runDay(ctx context.Context, c HadesDayClient, out io.Writer, force, eod, checkPending bool) error {
	doc, err := dispatchDay(ctx, c, force, eod, checkPending)
	if err != nil {
		return classifyDayError(err, dayPathFor(eod, checkPending))
	}
	fmt.Fprint(out, hadesday.Render(doc))
	return nil
}

func dispatchDay(ctx context.Context, c HadesDayClient, force, eod, checkPending bool) (hadesday.BriefDoc, error) {
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
			fmt.Sprintf("hades day %s: today's brief already exists (use --force to overwrite)", path)))
	}
	if client.IsHTTPStatus(err, http.StatusServiceUnavailable) {
		return ierrors.Wrap(ierrors.Code("daemon.not-running"), recoverableWrap(err,
			fmt.Sprintf("hades day %s: daemon not ready (start it with: hades daemon start)", path)))
	}
	return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("hades day %s: %w", path, err))
}
