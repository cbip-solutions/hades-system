// SPDX-License-Identifier: MIT
// Package cli — attach.go.
//
// `hades attach <alias> [--window <name>]` is the operator-facing entry
// point for re-entering a per-project tmux session. If the session is
// Idle/Archived, the daemon spawns lazy (Q7 D TriggerExplicitAttach).
// Default window: orch (per spec §6.3).
//
// # Lifecycle
//
// 1. CLI validates --window against the canonical 6-window set
// (orch/leads/workers/hra/logs/scratch) BEFORE any daemon RTT.
// Defence-in-depth: the daemon also validates via tmuxlife.AllWindows.
// Client-side validation gives operator zero-RTT typo feedback.
//
// 2. CLI calls AttachClient.AttachSession(ctx, alias, window). Production
// wraps *client.Client → POST /v1/sessions/{alias}/attach → returns
// the daemon-rendered tmux command-line.
//
// 3. Under `go test` (isTestMode), the CLI echoes the returned command
// to stdout instead of exec-ing tmux (would replace the test process).
// Production replaces self via os/exec.Command + Run (the cobra layer
// surrenders the TTY to the new tmux client).
//
// Exit-code mapping:
// - 0 success
// - 1 operator-recoverable: --window invalid, daemon 404 alias-not-found
// or archived
// - 2 unrecoverable: transport, decode, daemon 5xx, daemon 503 gap
//
// gap: until the daemon ships POST /v1/sessions/{alias}/attach
// in, the route returns 503. The CLI surfaces 503 as exit 2
// (infra-issue, not operator-typo). This mirrors the release
// /v1/messages graceful-degradation pattern: client method shipped
// early, daemon route added in a follow-up phase.
package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/spf13/cobra"
)

type AttachClient interface {
	// AttachSession dispatches the request. Returns the exact tmux
	// command-line the CLI MUST exec (`tmux -S /tmp/hades-system.sock
	// attach -t hades-<alias>-<sha8>:<window>`), or an error.
	AttachSession(ctx context.Context, alias, window string) (tmuxCmd string, err error)
}

type AttachClientFactory func(cmd *cobra.Command) AttachClient

var validAttachWindows = map[string]bool{
	"orch": true, "leads": true, "workers": true, "hra": true, "logs": true, "scratch": true,
}

const attachTimeout = 5 * time.Second

func NewAttachCmd(factory AttachClientFactory) *cobra.Command {
	var window string
	cmd := &cobra.Command{
		Use:   "attach <alias>",
		Short: "Attach to a project's tmux session",
		Long: `Attach to the per-project tmux session named "hades-<alias>-<sha8>".

If the session is in Idle or Archived state, the daemon will lazily spawn
(or restore from snapshot) a fresh session. The default window is "orch";
override with --window. Valid windows: orch, leads, workers, hra, logs,
scratch (operator-owned per Q6 D + inv-hades-118).

The CLI execs ` + "`tmux attach-session`" + ` directly so the operator's terminal
takes over the tmux foreground. Inside the test harness (HADES_TEST_MODE=1)
the command echoes the resolved tmux invocation instead of execing so
unit tests can assert without taking over the test process.

Exit codes (spec §6.2):
  0  attached (operator now in tmux)
  1  alias not found OR --window invalid
  2  unrecoverable: transport, decode, daemon 5xx`,
		Example: " # Attach to the orch window (default)\n  hades attach internal-platform-x\n\n # Attach to the logs window\n  hades attach internal-platform-x --window logs\n\n # Attach to the operator-owned scratch window\n  hades attach internal-platform-x --window scratch",

		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			alias := args[0]

			if !validAttachWindows[window] {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("invalid --window %q; allowed: orch, leads, workers, hra, logs, scratch", window))
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), attachTimeout)
			defer cancel()
			tmuxCmd, err := c.AttachSession(ctx, alias, window)
			if err != nil {

				if errors.Is(err, ErrRecoverable) {
					return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("attach: %w", err))
				}
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("attach: %w", err))
			}
			// Daemon contract: tmuxCmd MUST be at least "tmux...". A
			// shorter / empty string is a daemon contract violation
			// ; surface as error rather than
			// exec a malformed command.
			fields := strings.Fields(tmuxCmd)
			if len(fields) < 2 {
				return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("attach: daemon returned malformed tmux command: %q", tmuxCmd))
			}
			out := cmd.OutOrStdout()

			if isTestMode() {
				fmt.Fprintln(out, tmuxCmd)
				return nil
			}
			return execAttach(fields)
		},
	}
	cmd.Flags().StringVar(&window, "window", "orch", "tmux window to attach to (orch|leads|workers|hra|logs|scratch)")
	return cmd
}

func NewAttachCmdProd() *cobra.Command {
	return NewAttachCmd(func(cmd *cobra.Command) AttachClient {
		return &productionAttachClient{c: newClientFromCmd(cmd)}
	})
}

type productionAttachClient struct {
	c *client.Client
}

func (p *productionAttachClient) AttachSession(ctx context.Context, alias, window string) (string, error) {
	tmuxCmd, err := p.c.AttachSession(ctx, alias, window)
	if err != nil {
		if client.IsHTTPStatus(err, http.StatusNotFound) {
			return "", recoverableWrap(err, "alias not found")
		}
		return "", err
	}
	return tmuxCmd, nil
}

func isTestMode() bool {
	a0 := testModeArg0()
	return strings.HasSuffix(a0, ".test") ||
		strings.Contains(a0, "/_test/") ||
		strings.Contains(a0, "/T/go-build")
}

var testModeArg0 = func() string {
	if len(os.Args) == 0 {
		return ""
	}
	return os.Args[0]
}

var execAttach = func(fields []string) error {

	c1 := exec.Command(fields[0], fields[1:]...)
	c1.Stdin = os.Stdin
	c1.Stdout = os.Stdout
	c1.Stderr = os.Stderr
	return c1.Run()
}
