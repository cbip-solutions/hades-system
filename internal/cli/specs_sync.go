// SPDX-License-Identifier: MIT
// specs_sync.go — Task F-5 subcommand `zen specs sync`.
//
// Calls POST /v1/knowledge/ecosystem/specs-sync to re-index openspec/specs/
// into the ecosystem.db RAG store. Renders the daemon's chunks/specs
// counts + elapsed-ms summary.
//
// The daemon-side handler is wired in ; ships the CLI
// surface so operators can plug into the unified `zen specs *` family
// before the route lands. Calling against a daemon that does not yet
// register the route returns 404 — classifySpecsError maps that to
// ErrRecoverable with an operator-facing hint.
//
// Exit-code mapping (per spec §6.2; ErrRecoverable contract):
// - 0 success
// - 1 operator-recoverable: daemon 422 (validation), 404 (route not
// yet wired in ).
// - 2 unrecoverable: transport, decode, daemon 5xx.
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
)

const specsSyncTimeout = 5 * time.Minute

type SpecsSyncFlags struct {
	Full     bool
	SpecsDir string
}

type SpecsDaemonClient interface {
	SpecsSync(ctx context.Context, req client.SpecsSyncRequest) (*client.SpecsSyncResponse, error)
}

type productionSpecsDaemonClient struct {
	c *client.Client
}

func (p *productionSpecsDaemonClient) SpecsSync(ctx context.Context, req client.SpecsSyncRequest) (*client.SpecsSyncResponse, error) {
	return p.c.SpecsSync(ctx, req)
}

func RunSpecsSync(ctx context.Context, c SpecsDaemonClient, flags SpecsSyncFlags, w io.Writer) error {
	resp, err := c.SpecsSync(ctx, client.SpecsSyncRequest{
		Full:     flags.Full,
		SpecsDir: flags.SpecsDir,
	})
	if err != nil {
		return classifySpecsError(err, "sync")
	}
	if resp.Message != "" {
		fmt.Fprintf(w, "specs sync: %s\n", resp.Message)
	}
	fmt.Fprintf(w, "specs sync complete: specs=%d chunks=%d elapsed=%dms\n",
		resp.SpecsScanned, resp.ChunksIndexed, resp.ElapsedMs)
	return nil
}

func newSpecsSyncCmd(factory SpecsDaemonClientFactory) *cobra.Command {
	flags := SpecsSyncFlags{}
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Re-index openspec/specs/ into ecosystem.db RAG store",
		Long: `Trigger the daemon to re-index openspec/specs/ markdown files
into the ecosystem.db RAG store. Delta sweep by default (sha-based
changed-only); --full forces a complete rebuild.

Phase F ships the CLI surface; the daemon-side handler is wired in
Phase G. Against a daemon without the route, sync returns exit 1 with
a roadmap pointer (operator-recoverable).`,
		Example: `  zen specs sync
  zen specs sync --full`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), specsSyncTimeout)
			defer cancel()
			return RunSpecsSync(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVar(&flags.Full, "full", false, "Force full re-index (default: delta sweep)")
	cmd.Flags().StringVar(&flags.SpecsDir, "specs-dir", "", "Override openspec/specs/ path (daemon-side resolution)")
	return cmd
}

func classifySpecsError(err error, op string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrRecoverable) {
		return err
	}
	if client.IsHTTPStatus(err, http.StatusNotFound) {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, fmt.Sprintf("specs: %s: daemon route not yet wired (phase g)", op)))
	}
	if client.IsHTTPStatus(err, http.StatusUnprocessableEntity) {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, fmt.Sprintf("specs: %s: daemon rejected input", op)))
	}
	return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("specs: %s: %w", op, err))
}
