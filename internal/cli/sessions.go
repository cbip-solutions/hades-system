// SPDX-License-Identifier: MIT
// Package cli — sessions.go (Plan 7 Phase C Task C-12).
//
// `zen sessions ls` lists all daemon-tracked tmux sessions in tabular
// form (5 columns: ALIAS, SHA8, STATUS, LAST-ATTACH, PANES).
//
// Cobra layout:
//
//	zen sessions
//	  ls    list known sessions + status + last attach time
//
// Format is stable across releases so scripts can grep / awk the output;
// changes will land via a new --json output format flag in Phase L,
// never by mutating the column set.
//
// Exit-code mapping (per spec §6.2):
//   - 0 success (any row count, including zero)
//   - 2 unrecoverable: transport, decode, daemon 5xx, daemon 503 Phase I gap
//
// Phase I gap: until the daemon ships GET /v1/sessions in Phase I, the
// route returns 503. The CLI surfaces 503 as exit 2.
package cli

import (
	"context"
	"errors"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

type SessionRow = client.SessionRow

type SessionsClient interface {
	ListSessions(ctx context.Context) ([]SessionRow, error)
}

type SessionsClientFactory func(cmd *cobra.Command) SessionsClient

const sessionsTimeout = 5 * time.Second

func NewSessionsCmd(factory SessionsClientFactory) *cobra.Command {
	root := &cobra.Command{
		Use:   "sessions",
		Short: "Manage HADES tmux sessions (ls)",
		Long: `Inspect or manage the per-project tmux sessions tracked by the daemon.

Currently only "ls" is registered; future subcommands (Plan 7+ — kill,
snapshot, restore) will extend this namespace without breaking the
existing surface. The plural form mirrors ` + "`zen projects`" + ` (cross-
fleet inspection); singular per-alias actions live on ` + "`zen attach`" + `
and ` + "`zen layout repaint`" + `.`,
		Example: `  # List every tracked tmux session across projects
  zen sessions ls`,
	}
	root.AddCommand(newSessionsLsCmd(factory))
	return root
}

func NewSessionsCmdProd() *cobra.Command {
	return NewSessionsCmd(func(cmd *cobra.Command) SessionsClient {
		return &productionSessionsClient{c: newClientFromCmd(cmd)}
	})
}

func newSessionsLsCmd(factory SessionsClientFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List tracked tmux sessions",
		Long: `List every per-project tmux session tracked in the daemon's
tmux_session_state table. Includes Idle, Active, Archived, and
Orphaned rows so operator can spot drift (Orphaned == daemon thinks
session active but tmux has-session disagrees).

Columns:
  ALIAS      project alias bound to the session
  STATE      Idle | Active | Archived | Orphaned
  TMUX-NAME  zen-<alias>-<sha8> (the actual tmux session name)
  WINDOWS    count of daemon-owned windows (orch/leads/workers/hra/logs)
  LAST-USE   relative time of last activation

Exit codes:
  0  success
  2  unrecoverable: transport, decode, daemon 5xx`,
		Example: `  # List every tracked session
  zen sessions ls

  # Spot orphaned sessions (drift candidates)
  zen sessions ls | grep Orphaned`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), sessionsTimeout)
			defer cancel()
			rows, err := c.ListSessions(ctx)
			if err != nil {
				if errors.Is(err, ErrRecoverable) {
					return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("sessions ls: %w", err))
				}
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("sessions ls: %w", err))
			}
			renderSessionsList(cmd.OutOrStdout(), rows)
			return nil
		},
	}
}

func renderSessionsList(w interface{ Write([]byte) (int, error) }, rows []SessionRow) {
	if len(rows) == 0 {
		fmt.Fprintln(w, "no active sessions")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ALIAS\tSHA8\tSTATUS\tLAST-ATTACH\tPANES")
	for _, r := range rows {
		attach := "never"
		if !r.LastAttach.IsZero() {
			attach = r.LastAttach.UTC().Format(time.RFC3339)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\n",
			r.Alias, r.Sha8, r.Status, attach, r.PaneCount)
	}
	_ = tw.Flush()
}

type productionSessionsClient struct {
	c *client.Client
}

func (p *productionSessionsClient) ListSessions(ctx context.Context) ([]SessionRow, error) {
	return p.c.ListSessions(ctx)
}
