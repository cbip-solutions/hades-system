// SPDX-License-Identifier: MIT
// Package cli — layout.go.
//
// `hades layout repaint <alias>` is the operator-invoked recovery primitive:
// re-construct the 5 daemon-owned tmux windows (orch / leads / workers /
// hra / logs) from logical state in daemon.db. Preserves the
// operator-owned scratch window per inv-hades-118.
//
// Use case: after `hades day` surfaces a TmuxLayoutDriftDetected event
// (the drift poller's read-only forensic side-channel — Q6 D), the
// operator can manually trigger a re-paint to converge the live tmux
// server back to the daemon's expected layout.
//
// Cobra layout:
//
// hades layout
// repaint <alias> re-construct daemon-owned windows
//
// Exit-code mapping (per spec §6.2):
// - 0 success
// - 1 operator-recoverable: daemon 404 (alias not found / archived)
// - 2 unrecoverable: transport, decode, daemon 5xx, daemon 503 gap
//
// gap: until the daemon ships POST /v1/sessions/{alias}/layout/repaint
// in, the route returns 503. The CLI surfaces 503 as exit 2.
package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/spf13/cobra"
)

type LayoutClient interface {
	RepaintLayout(ctx context.Context, alias string) error
}

type LayoutClientFactory func(cmd *cobra.Command) LayoutClient

const layoutTimeout = 10 * time.Second

func NewLayoutCmd(factory LayoutClientFactory) *cobra.Command {
	root := &cobra.Command{
		Use:   "layout",
		Short: "Recover tmux layout (after drift)",
		Long: `Operate on per-project tmux layout.

Currently only "repaint" is registered; the namespace is reserved for
future Plan 7+ recovery primitives (e.g. snapshot, kill-orphaned). The
canonical workflow is: ` + "`hades day`" + ` surfaces a TmuxLayoutDriftDetected
event in the inbox, operator runs ` + "`hades layout repaint <alias>`" + ` to
re-construct the daemon-owned windows in place.`,
		Example: `  # Repaint the daemon-owned windows for a specific session
  hades layout repaint internal-platform-x`,
	}
	root.AddCommand(newLayoutRepaintCmd(factory))
	return root
}

func NewLayoutCmdProd() *cobra.Command {
	return NewLayoutCmd(func(cmd *cobra.Command) LayoutClient {
		return &productionLayoutClient{c: newClientFromCmd(cmd)}
	})
}

func newLayoutRepaintCmd(factory LayoutClientFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "repaint <alias>",
		Short: "Re-construct daemon-owned windows for a session",
		Long: `Re-construct the 5 daemon-owned windows (orch, leads, workers, hra,
logs) for the per-project tmux session bound to <alias>. Preserves the
operator-owned scratch window (Q6 D + inv-hades-118).

Use this after ` + "`hades day`" + ` surfaces a TmuxLayoutDriftDetected event,
or whenever ` + "`hades sessions ls`" + ` shows a row whose WINDOWS count is
below the expected 5. The repaint is idempotent: running it on a
session whose layout is already correct is a no-op.

Exit codes:
  0  repainted (or no-op when layout already correct)
  1  alias not found
  2  unrecoverable: transport, decode, daemon 5xx, tmux server stuck`,
		Example: `  # Repaint after drift
  hades layout repaint internal-platform-x`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			alias := args[0]
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), layoutTimeout)
			defer cancel()
			if err := c.RepaintLayout(ctx, alias); err != nil {
				if errors.Is(err, ErrRecoverable) {
					return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("layout repaint: %w", err))
				}
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("layout repaint: %w", err))
			}
			fmt.Fprintln(cmd.OutOrStdout(), "ok")
			return nil
		},
	}
}

type productionLayoutClient struct {
	c *client.Client
}

func (p *productionLayoutClient) RepaintLayout(ctx context.Context, alias string) error {
	if err := p.c.RepaintLayout(ctx, alias); err != nil {
		if client.IsHTTPStatus(err, http.StatusNotFound) {
			return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, "alias not found"))
		}
		return err
	}
	return nil
}
