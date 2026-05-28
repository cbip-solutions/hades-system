// SPDX-License-Identifier: MIT
// Package cli — inbox.go.
//
// `hades inbox <subcommand>` is the operator-facing entry point for the
// per-project inbox storage + cross-project aggregator cache (spec §6.5).
//
// Cobra layout (3 leaves under 1 root):
//
// hades inbox # list (default)
// ack <id> # set AckedAt = now
// snooze <id> --until <duration> # set SnoozedUntil
//
// # Examples
//
// $ hades inbox
// ID SEVERITY PROJECT EVENT AGE
// #234 urgent internal-platform-x hra.l4_alert 1h
//
// $ hades inbox --severity urgent --since 24h --limit 10
// $ hades inbox --format json | jq '.[].notification_id'
//
// $ hades inbox ack 234
// ✓ Acked notification #234
//
// $ hades inbox snooze 230 --until 8h
// ✓ Snoozed #230 until 2026-05-07T20:00:00Z
//
// All subcommands lazily resolve a daemon HTTP client at RunE time via
// newClientFromCmd (mirrors the HADES design C-12 attach/sessions/layout +
// D-13 schedule pattern). Tests inject a fake client via the
// InboxClientFactory parameter to NewInboxCmd; production wires through
// NewInboxCmdProd which adapts *client.Client → InboxClient.
//
// Exit-code mapping (per design contract; ErrRecoverable contract from
// ):
// - 0 success
// - 1 operator-recoverable: invalid --severity, malformed --since,
// malformed --until, daemon 404 (notification id not found).
// - 2 unrecoverable: transport, decode, daemon 5xx, daemon 503
// (until SetInboxStore wires; mirrors HADES design /v1/messages
// graceful-degradation pattern).
//
// Severity flag validates client-side against the 4-tier enum
// (inbox.ParseSeverity); invalid value returns inbox.ErrInvalidSeverity
// wrapped recoverable so the typo path never hits the daemon.
//
// Snooze accepts Go duration syntax ("30m", "8h", "24h"); named forms
// ("9am-tomorrow") are deferred to a future enhancement — rejecting
// non-duration input keeps the CLI predictable.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/cbip-solutions/hades-system/internal/inbox"
)

type InboxClient interface {
	List(ctx context.Context, filter inbox.ListFilter) ([]inbox.CacheRow, error)
	Ack(ctx context.Context, id int64) error
	Snooze(ctx context.Context, id int64, until time.Time) error
}

type InboxClientFactory func(cmd *cobra.Command) InboxClient

const inboxTimeout = 5 * time.Second

const defaultInboxLimit = 20

type InboxListFlags struct {
	Severity string
	Project  string
	Since    string
	Limit    int
	Format   string
	Unacked  bool
}

func NewInboxCmd(factory InboxClientFactory) *cobra.Command {
	root := newInboxListCmd(factory)
	root.AddCommand(newInboxAckCmd(factory))
	root.AddCommand(newInboxSnoozeCmd(factory))
	return root
}

func NewInboxCmdProd() *cobra.Command {
	return NewInboxCmd(func(cmd *cobra.Command) InboxClient {
		return &productionInboxClient{c: newClientFromCmd(cmd)}
	})
}

type productionInboxClient struct {
	c *client.Client
}

func (p *productionInboxClient) List(ctx context.Context, filter inbox.ListFilter) ([]inbox.CacheRow, error) {
	req := client.InboxListRequest{
		Project:      filter.ProjectID,
		Limit:        filter.Limit,
		IncludeAcked: filter.IncludeAcked,
	}
	if filter.Severity != nil {
		req.Severity = string(*filter.Severity)
	}
	if filter.Since != nil {
		req.SinceUnix = filter.Since.Unix()
	}
	wireRows, err := p.c.InboxList(ctx, req)
	if err != nil {
		return nil, err
	}
	out := make([]inbox.CacheRow, 0, len(wireRows))
	for _, r := range wireRows {

		sev, sevErr := inbox.ParseSeverity(r.Severity)
		if sevErr != nil {
			continue
		}
		out = append(out, inbox.CacheRow{
			CacheID:        r.CacheID,
			ProjectID:      r.ProjectID,
			ProjectAlias:   r.ProjectAlias,
			NotificationID: r.NotificationID,
			Severity:       sev,
			EventType:      r.EventType,
			ContentHash:    r.ContentHash,
			CreatedAt:      r.CreatedAt,
			AckedAt:        r.AckedAt,
		})
	}
	return out, nil
}

func (p *productionInboxClient) Ack(ctx context.Context, id int64) error {
	return p.c.InboxAck(ctx, id)
}

func (p *productionInboxClient) Snooze(ctx context.Context, id int64, until time.Time) error {
	return p.c.InboxSnooze(ctx, id, until)
}

func newInboxListCmd(factory InboxClientFactory) *cobra.Command {
	flags := InboxListFlags{}
	cmd := &cobra.Command{
		Use:   "inbox",
		Short: "List operator notifications across projects",
		Long: `List notifications surfaced to the operator inbox. Default
behaviour shows the most recent unacked notifications across all
projects ordered by creation time DESC, capped at 20 rows.

Filter via:
  --severity   urgent | action-needed | info-immediate | info-digest
  --project    project alias (single)
  --since      lookback window (Go duration; 24h, 7d, etc.)
  --limit      max rows
  --format     text (default) or json (operator-pipeable)

Subcommands:
  ack <id>     mark a notification as acked (sets AckedAt = now)
  snooze <id>  hide a notification until a future time`,
		Example: " # Bare list (most recent unacked, all projects, top 20)\n  hades inbox\n\n # Filter by urgent severity in the last 24h\n  hades inbox --severity urgent --since 24h\n\n # JSON output for jq pipelines\n  hades inbox --format json | jq '.[] | select(.Severity==\"urgent\")'",

		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), inboxTimeout)
			defer cancel()
			return RunInboxList(ctx, c, flags, cmd.OutOrStdout(), time.Now().UTC())
		},
	}
	cmd.Flags().StringVar(&flags.Severity, "severity", "", "filter by severity (urgent | action-needed | info-immediate | info-digest)")
	cmd.Flags().StringVar(&flags.Project, "project", "", "filter by project alias")
	cmd.Flags().StringVar(&flags.Since, "since", "", "lookback window (Go duration; e.g. 24h, 7d)")
	cmd.Flags().IntVar(&flags.Limit, "limit", 0, "max rows (default 20)")
	cmd.Flags().StringVar(&flags.Format, "format", "text", "output format: text | json")
	cmd.Flags().BoolVar(&flags.Unacked, "unacked", false, "show only unacked (default; flag is preserved for discoverability)")
	return cmd
}

func RunInboxList(ctx context.Context, c InboxClient, flags InboxListFlags, w io.Writer, now time.Time) error {
	filter := inbox.ListFilter{ProjectID: flags.Project}
	if flags.Severity != "" {
		sev, err := inbox.ParseSeverity(flags.Severity)
		if err != nil {

			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("%w: --severity rejected: %w", ErrRecoverable, err))
		}
		filter.Severity = &sev
	}
	if flags.Since != "" {
		d, err := time.ParseDuration(flags.Since)
		if err != nil {
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, fmt.Sprintf("--since %q", flags.Since)))
		}
		t := now.Add(-d)
		filter.Since = &t
	}
	filter.Limit = flags.Limit
	if filter.Limit == 0 {
		filter.Limit = defaultInboxLimit
	}

	filter.IncludeAcked = false

	rows, err := c.List(ctx, filter)
	if err != nil {
		return classifyInboxError(err, "list")
	}

	if filter.Severity != nil {
		kept := rows[:0]
		for _, r := range rows {
			if r.Severity == *filter.Severity {
				kept = append(kept, r)
			}
		}
		rows = kept
	}

	switch flags.Format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	default:
		if len(rows) == 0 {
			fmt.Fprintln(w, "(no notifications)")
			return nil
		}
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tSEVERITY\tPROJECT\tEVENT\tAGE")
		for _, r := range rows {
			fmt.Fprintln(tw, renderInboxRow(r, now))
		}
		return tw.Flush()
	}
}

func renderInboxRow(r inbox.CacheRow, now time.Time) string {
	age := humanizeAge(now.Sub(r.CreatedAt))
	return fmt.Sprintf("#%d\t%s\t%s\t%s\t%s",
		r.NotificationID, r.Severity, r.ProjectAlias, r.EventType, age)
}

func humanizeAge(d time.Duration) string {
	if d < 0 {
		return "now"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func newInboxAckCmd(factory InboxClientFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "ack <id>",
		Short: "Acknowledge a notification (sets AckedAt = now)",
		Long: `Acknowledge a single notification by its numeric ID. The daemon
sets AckedAt = now() so the row is excluded from the default unacked
view. The notification body remains in the per-project inbox table for
audit / digest consumption.

The id argument matches the "#NN" column in ` + "`hades inbox`" + ` output
(without the # prefix); pass the bare integer.

Exit codes (spec §6.5):
  0  acked
  1  invalid id (not numeric, ≤0) OR id not found
  2  unrecoverable: transport, decode, daemon 5xx`,
		Example: " # Ack notification #42\n  hades inbox ack 42",

		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, fmt.Sprintf("invalid id %q", args[0])))
			}
			if id <= 0 {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("id must be positive (got %d)", id))
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), inboxTimeout)
			defer cancel()
			return RunInboxAck(ctx, c, id, cmd.OutOrStdout())
		},
	}
}

func RunInboxAck(ctx context.Context, c InboxClient, id int64, w io.Writer) error {
	if err := c.Ack(ctx, id); err != nil {
		return classifyInboxError(err, fmt.Sprintf("ack %d", id))
	}
	fmt.Fprintf(w, "Acked notification #%d\n", id)
	return nil
}

func newInboxSnoozeCmd(factory InboxClientFactory) *cobra.Command {
	var until string
	cmd := &cobra.Command{
		Use:   "snooze <id>",
		Short: "Snooze a notification until a future time",
		Long: `Snooze a notification until a future time. The row is hidden
from the default ` + "`hades inbox`" + ` view until --until elapses, then
re-surfaces automatically. Accepts Go duration syntax for --until:
30m, 8h, 24h, 7d, etc. Named forms (9am-tomorrow) are deferred to a
future enhancement.

The snooze does NOT ack the notification — operator still needs to
ack (or let the snooze re-surface and ack later). Snooze is a "deal
with this later" lever, not an "I dealt with this" lever.

Exit codes:
  0  snoozed
  1  invalid id, missing --until, malformed duration, OR id not found
  2  unrecoverable: transport, decode, daemon 5xx`,
		Example: " # Snooze notification 42 for 30 minutes\n  hades inbox snooze 42 --until 30m\n\n # Snooze until tomorrow morning (8h after midnight UTC)\n  hades inbox snooze 42 --until 24h",

		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if until == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--until is required (Go duration: 30m, 8h, 24h)"))
			}
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, fmt.Sprintf("invalid id %q", args[0])))
			}
			if id <= 0 {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("id must be positive (got %d)", id))
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), inboxTimeout)
			defer cancel()
			return RunInboxSnooze(ctx, c, id, until, time.Now().UTC(), cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&until, "until", "", "Go duration like 30m, 8h, 24h (required)")
	return cmd
}

func RunInboxSnooze(ctx context.Context, c InboxClient, id int64, untilSpec string, now time.Time, w io.Writer) error {
	d, err := time.ParseDuration(untilSpec)
	if err != nil {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err,
			fmt.Sprintf("--until %q must be a Go duration like 30m, 8h, 24h", untilSpec)))
	}
	until := now.Add(d).UTC()
	if err := c.Snooze(ctx, id, until); err != nil {
		return classifyInboxError(err, fmt.Sprintf("snooze %d", id))
	}
	fmt.Fprintf(w, "Snoozed #%d until %s\n", id, until.Format(time.RFC3339))
	return nil
}

func classifyInboxError(err error, op string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrRecoverable) {
		return err
	}
	if client.IsHTTPStatus(err, http.StatusNotFound) {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, fmt.Sprintf("inbox: %s: notification not found", op)))
	}
	if client.IsHTTPStatus(err, http.StatusUnprocessableEntity) {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, fmt.Sprintf("inbox: %s: daemon rejected input", op)))
	}
	return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("inbox: %s: %w", op, err))
}
