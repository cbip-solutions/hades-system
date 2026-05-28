// SPDX-License-Identifier: MIT
// Package cli — schedule.go.
//
// `hades schedule <subcommand>` is the operator-facing entry point for
// the 3-tier scheduler (Routine / Task / Loop) per spec §1 Q8 D + §6.2.
//
// Cobra layout (8 leaves under 1 root):
//
// hades schedule
// routine
// create --project --action --trigger --cron --repo --branch
// --miss-policy --miss-lookback
// list [--project|--all]
// delete <id>
// run <id>
// task --project --in <DUR> <action>
// loop --project --interval <DUR> <action>
// history --id <id> [--since <DUR>]
// queue
//
// All subcommands lazily resolve a daemon HTTP client at RunE time via
// newClientFromCmd (mirrors the release C-12 attach/sessions/layout
// pattern). Tests inject a fake client via TestOnlyClientFactory at
// the package level + a per-test ScheduleClient interface.
//
// Exit-code mapping (per spec §6.2; ErrRecoverable contract from
// ):
// - 0 success
// - 1 operator-recoverable: validation reject (missing flag, bogus
// duration, --interval below 1min floor), daemon 404 (id not
// found / alias not in registry), daemon 422 (cron malformed,
// miss-policy unknown).
// - 2 unrecoverable: transport, decode, daemon 5xx, daemon 503
// .
//
// gap: until the daemon mounts the Run dispatch substrate in
// , `hades schedule routine run <id>` surfaces 503 → exit 2
// (infra-issue, not operator-typo). Mirrors the release /v1/messages
// graceful-degradation pattern.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/spf13/cobra"
)

type ScheduleClient interface {
	ScheduleCreate(ctx context.Context, req client.CreateRoutineRequest) (*client.CreateRoutineResponse, error)
	ScheduleCreateTask(ctx context.Context, req client.CreateTaskRequest) (*client.CreateTaskResponse, error)
	ScheduleCreateLoop(ctx context.Context, req client.CreateLoopRequest) (*client.CreateLoopResponse, error)
	ScheduleList(ctx context.Context, alias string) ([]client.RoutineRow, error)
	ScheduleDelete(ctx context.Context, id string) error
	ScheduleRun(ctx context.Context, id string) (*client.RunRoutineResponse, error)
	ScheduleHistory(ctx context.Context, id string, from, to time.Time) ([]client.HistoryRow, error)
	ScheduleQueue(ctx context.Context) ([]client.QueueRow, error)
}

type ScheduleClientFactory func(cmd *cobra.Command) ScheduleClient

const scheduleTimeout = 10 * time.Second

const defaultScheduleHistorySince = 24 * time.Hour

const defaultScheduleLoopInterval = 5 * time.Minute

const defaultMissLookback = 7 * 24 * time.Hour

func NewScheduleCmd(factory ScheduleClientFactory) *cobra.Command {
	root := &cobra.Command{
		Use:   "schedule",
		Short: "Manage the 3-tier scheduler (routine / task / loop)",
		Long: `Operator surface for the HADES scheduler:

  routine    durable cron / http / git-poll triggered schedules
  task       ephemeral one-shot fire after duration
  loop       session-bound polling at fixed interval
  history    fire history for a schedule
  queue      next-24h fire queue across all projects

The scheduler runs inside the HADES daemon (hades-ctld); this command is a thin
HTTP client. All real logic lives in internal/scheduler +
internal/daemon/scheduleradapter. The 3-tier split (routine / task /
loop) lets operator pick the right durability + binding for each
need without re-implementing cron logic per use case.`,
		Example: " # List every routine across every project\n  hades schedule routine list --all\n\n # Inspect the next 24h of scheduled fires\n  hades schedule queue\n\n # See history for one routine\n  hades schedule history --id <id> --since 7d",
	}
	root.AddCommand(newScheduleRoutineCmd(factory))
	root.AddCommand(newScheduleTaskCmd(factory))
	root.AddCommand(newScheduleLoopCmd(factory))
	root.AddCommand(newScheduleHistoryCmd(factory))
	root.AddCommand(newScheduleQueueCmd(factory))
	return root
}

func NewScheduleCmdProd() *cobra.Command {
	return NewScheduleCmd(func(cmd *cobra.Command) ScheduleClient {
		return &productionScheduleClient{c: newClientFromCmd(cmd)}
	})
}

type productionScheduleClient struct {
	c *client.Client
}

func (p *productionScheduleClient) ScheduleCreate(ctx context.Context, req client.CreateRoutineRequest) (*client.CreateRoutineResponse, error) {
	return p.c.ScheduleCreate(ctx, req)
}

func (p *productionScheduleClient) ScheduleCreateTask(ctx context.Context, req client.CreateTaskRequest) (*client.CreateTaskResponse, error) {
	return p.c.ScheduleCreateTask(ctx, req)
}

func (p *productionScheduleClient) ScheduleCreateLoop(ctx context.Context, req client.CreateLoopRequest) (*client.CreateLoopResponse, error) {
	return p.c.ScheduleCreateLoop(ctx, req)
}

func (p *productionScheduleClient) ScheduleList(ctx context.Context, alias string) ([]client.RoutineRow, error) {
	return p.c.ScheduleList(ctx, alias)
}

func (p *productionScheduleClient) ScheduleDelete(ctx context.Context, id string) error {
	return p.c.ScheduleDelete(ctx, id)
}

func (p *productionScheduleClient) ScheduleRun(ctx context.Context, id string) (*client.RunRoutineResponse, error) {
	return p.c.ScheduleRun(ctx, id)
}

func (p *productionScheduleClient) ScheduleHistory(ctx context.Context, id string, from, to time.Time) ([]client.HistoryRow, error) {
	return p.c.ScheduleHistory(ctx, id, from, to)
}

func (p *productionScheduleClient) ScheduleQueue(ctx context.Context) ([]client.QueueRow, error) {
	return p.c.ScheduleQueue(ctx)
}

func newScheduleRoutineCmd(factory ScheduleClientFactory) *cobra.Command {
	root := &cobra.Command{
		Use:   "routine",
		Short: "Manage durable scheduled routines (cron / http / git-poll)",
		Long: `Manage durable routines — schedules that survive daemon restart
and fire repeatedly per their trigger. Three trigger types:

  cron      5-field vixie expression (e.g. "0 8 * * 1-5")
  http      bearer-token-gated POST /v1/schedules/{id}/fire
  git-poll  local 'gh' CLI poll of repo/branch (privacy-by-default)

Subcommands:
  create   define a new routine + (for http) print the bearer token ONCE
  list     filter by --project or --all
  delete   soft-delete (Disabled then DELETE)
  run      manually trigger a routine (Phase I substrate)`,
		Example: " # Daily 8am cron routine\n  hades schedule routine create --project internal-platform-x --action morning-brief \\\n      --trigger cron --cron \"0 8 * * 1-5\"\n\n # List routines for one project\n  hades schedule routine list --project internal-platform-x\n\n # Delete a routine by id\n  hades schedule routine delete <id>",
	}
	root.AddCommand(newScheduleRoutineCreateCmd(factory))
	root.AddCommand(newScheduleRoutineListCmd(factory))
	root.AddCommand(newScheduleRoutineDeleteCmd(factory))
	root.AddCommand(newScheduleRoutineRunCmd(factory))
	return root
}

func newScheduleRoutineCreateCmd(factory ScheduleClientFactory) *cobra.Command {
	var (
		project    string
		action     string
		trigger    string
		cronExpr   string
		repo       string
		branch     string
		missPolicy string
		missLookbk time.Duration
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a durable routine schedule",
		Long: `Create a durable routine schedule. Trigger types:

  cron      5-field vixie expression (e.g. "0 8 * * 1-5")
  http      bearer-token-gated POST /v1/schedules/{id}/fire
  git-poll  local 'gh' CLI poll of repo/branch (privacy-by-default)

Examples:
    hades schedule routine create --project internal-platform-x --action morning-brief \
        --trigger cron --cron "0 8 * * 1-5"

    hades schedule routine create --project internal-platform-x --action webhook \
        --trigger http       # surfaces raw bearer token ONCE

    hades schedule routine create --project internal-platform-x --action git-watcher \
        --trigger git-poll --repo https://github.com/owner/repo --branch main`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(project) == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--project is required"))
			}
			if strings.TrimSpace(action) == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--action is required"))
			}
			if trigger == "cron" && strings.TrimSpace(cronExpr) == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--cron is required when --trigger=cron"))
			}
			if trigger == "git-poll" && strings.TrimSpace(repo) == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--repo is required when --trigger=git-poll"))
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), scheduleTimeout)
			defer cancel()
			resp, err := c.ScheduleCreate(ctx, client.CreateRoutineRequest{
				ProjectAlias:  project,
				Action:        action,
				Trigger:       trigger,
				CronExpr:      cronExpr,
				RepoURL:       repo,
				Branch:        branch,
				MissPolicyStr: missPolicy,
				MissLookback:  missLookbk,
			})
			if err != nil {
				return classifyScheduleError(err)
			}
			renderRoutineCreate(cmd.OutOrStdout(), resp)
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project alias (required)")
	cmd.Flags().StringVar(&action, "action", "", "action key (required, e.g. morning-brief)")
	cmd.Flags().StringVar(&trigger, "trigger", "cron", "cron|http|git-poll")
	cmd.Flags().StringVar(&cronExpr, "cron", "", "5-field vixie cron expression (for trigger=cron)")
	cmd.Flags().StringVar(&repo, "repo", "", "github repo URL (for trigger=git-poll)")
	cmd.Flags().StringVar(&branch, "branch", "main", "git branch (for trigger=git-poll)")
	cmd.Flags().StringVar(&missPolicy, "miss-policy", "doctrine",
		"skip|catch-up-bounded|coalesce|notify-only|doctrine (default: per doctrine)")
	cmd.Flags().DurationVar(&missLookbk, "miss-lookback", defaultMissLookback,
		"bounded catch-up lookback")
	return cmd
}

func newScheduleRoutineListCmd(factory ScheduleClientFactory) *cobra.Command {
	var (
		project string
		all     bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List schedules (optionally filtered by --project)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !all && strings.TrimSpace(project) == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--project or --all is required"))
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), scheduleTimeout)
			defer cancel()
			alias := project
			if all {
				alias = ""
			}
			rows, err := c.ScheduleList(ctx, alias)
			if err != nil {
				return classifyScheduleError(err)
			}
			renderRoutineList(cmd.OutOrStdout(), rows)
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "filter by project alias")
	cmd.Flags().BoolVar(&all, "all", false, "list across all projects")
	return cmd
}

func newScheduleRoutineDeleteCmd(factory ScheduleClientFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Soft-delete a schedule (Disabled then DELETE)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			if id == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("schedule id is empty"))
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), scheduleTimeout)
			defer cancel()
			if err := c.ScheduleDelete(ctx, id); err != nil {
				return classifyScheduleError(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Routine %s deleted.\n", id)
			return nil
		},
	}
}

func newScheduleRoutineRunCmd(factory ScheduleClientFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "run <id>",
		Short: "Manually trigger a schedule (Phase I substrate)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			if id == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("schedule id is empty"))
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), scheduleTimeout)
			defer cancel()
			resp, err := c.ScheduleRun(ctx, id)
			if err != nil {
				return classifyScheduleError(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"Routine %s fired manually. Outcome=%s. Cost=$%.4f. Duration=%dms.\n",
				id, resp.Outcome, resp.CostUSD, resp.DurationMs)
			return nil
		},
	}
}

func newScheduleTaskCmd(factory ScheduleClientFactory) *cobra.Command {
	var (
		project string
		in      time.Duration
	)
	cmd := &cobra.Command{
		Use:   "task <action>",
		Short: "Schedule a one-shot task to fire after a duration",
		Long: `Schedule a one-shot ephemeral task that fires once after the
supplied --in duration, then auto-deletes from the schedules table.

Tasks are NOT durable across daemon restart by design — they are the
"remind me in 30 minutes" primitive, not the "every weekday at 8am"
primitive. Use ` + "`routine`" + ` for durable cron / http / git-poll
schedules and ` + "`loop`" + ` for session-bound polling.

Multi-word actions don't need quoting — the CLI joins positional args
with spaces. Action strings carry no daemon-side semantics; they are
passed verbatim to the dispatcher.

Exit codes:
  0  task scheduled
  1  validation reject (missing --project, missing --in, --in <= 0)
  2  unrecoverable: transport, decode, daemon 5xx`,
		Example: " # Fire after 30 minutes\n  hades schedule task --project internal-platform-x --in 30m send daily report\n\n # Fire 4 hours from now\n  hades schedule task --project internal-platform-x --in 4h check ci status",

		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(project) == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--project is required"))
			}
			if in <= 0 {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--in <duration> is required (e.g. 30m, 4h)"))
			}
			action := strings.Join(args, " ")
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), scheduleTimeout)
			defer cancel()
			resp, err := c.ScheduleCreateTask(ctx, client.CreateTaskRequest{
				ProjectAlias: project,
				Action:       action,
				In:           in,
			})
			if err != nil {
				return classifyScheduleError(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"Task %s scheduled. Fires at %s.\n",
				resp.ID, resp.NextRunAt.UTC().Format(time.RFC3339))
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project alias (required)")
	cmd.Flags().DurationVar(&in, "in", 0, "fire after this duration (e.g. 30m)")
	return cmd
}

func newScheduleLoopCmd(factory ScheduleClientFactory) *cobra.Command {
	var (
		project  string
		interval time.Duration
	)
	cmd := &cobra.Command{
		Use:   "loop <action>",
		Short: "Schedule a session-bound polling loop",
		Long: `Schedule a session-bound polling loop that fires the supplied
action every --interval until the bound tmux session ends. Loops are
the "keep checking until I'm done" primitive — useful for monitoring
long-running builds, watching CI, or keeping a research-cache warm
during an exploration session.

Loops auto-tear-down when the bound tmux session ends, so operator
doesn't have to remember to delete them. The 1-minute --interval
floor enforces operator-doctrine pacing (faster polling burns the
LLM tier without proportional value).

Exit codes:
  0  loop scheduled
  1  validation reject (missing --project, --interval below 1min floor)
  2  unrecoverable: transport, decode, daemon 5xx`,
		Example: " # Poll every 5 minutes (default interval)\n  hades schedule loop --project internal-platform-x watch ci builds\n\n # Poll every 2 minutes for an active monitoring window\n  hades schedule loop --project internal-platform-x --interval 2m tail logs",

		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(project) == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--project is required"))
			}
			if interval < time.Minute {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--interval %v below 1min floor", interval))
			}
			action := strings.Join(args, " ")
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), scheduleTimeout)
			defer cancel()
			resp, err := c.ScheduleCreateLoop(ctx, client.CreateLoopRequest{
				ProjectAlias: project,
				Action:       action,
				Interval:     interval,
			})
			if err != nil {
				return classifyScheduleError(err)
			}
			session := resp.SessionID
			if session == "" {
				session = "(unbound)"
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"Loop %s bound to session %s. Polls every %s.\n",
				resp.ID, session, interval)
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project alias (required)")
	cmd.Flags().DurationVar(&interval, "interval", defaultScheduleLoopInterval,
		"polling interval (≥1min)")
	return cmd
}

func newScheduleHistoryCmd(factory ScheduleClientFactory) *cobra.Command {
	var (
		id    string
		since time.Duration
	)
	cmd := &cobra.Command{
		Use:   "history",
		Short: "Show fire history for a schedule",
		Long: `Show the fire history for a single schedule (routine / task /
loop) within a look-back window. Each row records:

  FIRED-AT      RFC3339 UTC timestamp of the fire
  OUTCOME       success | failed | skipped | rate-limited
  COST          USD cost of the LLM tier reached during the fire
  DURATION-MS   wall-clock latency from fire-start to fire-finish
  REASON        miss-policy / failure-class / skip-reason text

Default window is 24 hours; widen via --since (Go duration: 7d, 30d,
etc.). Rows render in fired_at ASC order so a tail-style read flows
top-to-bottom.

Exit codes:
  0  history fetched (zero rows is success)
  1  --id missing OR id not found in schedules table
  2  unrecoverable: transport, decode, daemon 5xx`,
		Example: " # Default 24h history\n  hades schedule history --id <id>\n\n # Widen to 7 days\n  hades schedule history --id <id> --since 7d",

		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(id) == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--id is required"))
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), scheduleTimeout)
			defer cancel()
			now := time.Now().UTC()
			rows, err := c.ScheduleHistory(ctx, id, now.Add(-since), now)
			if err != nil {
				return classifyScheduleError(err)
			}
			renderScheduleHistory(cmd.OutOrStdout(), rows)
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "schedule ID (required)")
	cmd.Flags().DurationVar(&since, "since", defaultScheduleHistorySince,
		"history window (look-back from now)")
	return cmd
}

func newScheduleQueueCmd(factory ScheduleClientFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "queue",
		Short: "Show the next-24h scheduled fire queue",
		Long: `Show the next 24h of scheduled fires across every project. Each
row reports:

  NEXT-RUN-AT  RFC3339 UTC timestamp of the upcoming fire
  PROJECT      alias the schedule is bound to
  ACTION       action key the dispatcher will hand off
  IN           wall-clock distance from now (e.g. "2h15m")

The IN column is computed at render time so the operator's view
matches their wall clock; the daemon never pre-computes the gap.
Empty queue surfaces as "no scheduled fires in next 24h" rather than
a header-only table (header-only output misleads).

Exit codes:
  0  queue rendered (zero rows is success)
  2  unrecoverable: transport, decode, daemon 5xx`,
		Example: " # Inspect the next-24h horizon\n  hades schedule queue",

		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), scheduleTimeout)
			defer cancel()
			rows, err := c.ScheduleQueue(ctx)
			if err != nil {
				return classifyScheduleError(err)
			}
			renderScheduleQueue(cmd.OutOrStdout(), rows)
			return nil
		},
	}
}

func renderRoutineCreate(w io.Writer, resp *client.CreateRoutineResponse) {
	tier := resp.Tier
	if tier == "" {
		tier = "routine"
	}
	nextAt := "(none)"
	if !resp.NextRunAt.IsZero() {
		nextAt = resp.NextRunAt.UTC().Format(time.RFC3339)
	}
	fmt.Fprintf(w, "Routine %s created. Tier=%s. NextRunAt=%s\n",
		resp.ID, tier, nextAt)
	if resp.RawBearerToken != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Bearer token (save NOW; not shown again):")
		fmt.Fprintln(w, "  "+resp.RawBearerToken)
		fmt.Fprintln(w, "Activate via:")
		fmt.Fprintf(w, "  curl -X POST http://localhost:7867/v1/schedules/%s/fire \\\n", resp.ID)
		fmt.Fprintf(w, "    -H 'Authorization: Bearer %s'\n", resp.RawBearerToken)
	}
}

func renderRoutineList(w io.Writer, rows []client.RoutineRow) {
	if len(rows) == 0 {
		fmt.Fprintln(w, "no schedules")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID-SHORT\tPROJECT\tACTION\tTIER\tSTATUS\tNEXT")
	for _, r := range rows {
		idShort := r.ID
		if len(idShort) > 8 {
			idShort = idShort[:8]
		}
		next := "(none)"
		if !r.NextRunAt.IsZero() {
			next = r.NextRunAt.UTC().Format(time.RFC3339)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			idShort, r.ProjectAlias, r.Action, r.Tier, r.Status, next)
	}
	_ = tw.Flush()
}

func renderScheduleHistory(w io.Writer, rows []client.HistoryRow) {
	if len(rows) == 0 {
		fmt.Fprintln(w, "no history rows")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FIRED-AT\tOUTCOME\tCOST\tDURATION-MS\tREASON")
	for _, h := range rows {
		fmt.Fprintf(tw, "%s\t%s\t$%.4f\t%d\t%s\n",
			h.FiredAt.UTC().Format(time.RFC3339),
			scheduleOutcomeStr(h.Outcome),
			h.CostUSD,
			h.DurationMs,
			truncateScheduleReason(h.Reason, 60))
	}
	_ = tw.Flush()
}

func renderScheduleQueue(w io.Writer, rows []client.QueueRow) {
	if len(rows) == 0 {
		fmt.Fprintln(w, "no scheduled fires in next 24h")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NEXT-RUN-AT\tPROJECT\tACTION\tIN")
	now := time.Now().UTC()
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			r.NextRunAt.UTC().Format(time.RFC3339),
			r.ProjectAlias,
			r.Action,
			r.NextRunAt.Sub(now).Round(time.Second))
	}
	_ = tw.Flush()
}

func scheduleOutcomeStr(o int) string {
	switch o {
	case 0:
		return "success"
	case 1:
		return "failed"
	case 2:
		return "skipped"
	case 3:
		return "rate-limited"
	default:
		return fmt.Sprintf("outcome(%d)", o)
	}
}

func truncateScheduleReason(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n < 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}

func classifyScheduleError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrRecoverable) {
		return err
	}
	if client.IsHTTPStatus(err, http.StatusNotFound) {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, "schedule not found"))
	}
	if client.IsHTTPStatus(err, http.StatusUnprocessableEntity) {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, "daemon rejected input"))
	}
	return ierrors.Wrap(ierrors.Code("daemon.unreachable"), err)
}
