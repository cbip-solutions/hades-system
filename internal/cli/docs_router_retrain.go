// SPDX-License-Identifier: MIT
// Package cli — docs_router_retrain.go.
//
// `hades docs router-retrain` invokes the daemon endpoint that retrains
// the router's local logistic classifier (per design contract=A; D-2
// scaffold in cmd/hades/docs/router_retrain.go).
//
// The daemon-side handler wires the D-2 trainer pipeline:
// it resolves the corpus + output path from configuration and invokes
// cmd/hades/docs.RunRouterRetrainWithOptions(...). The CLI surface here
// is a thin transport call — no flag plumbing for corpus / output path
// at this layer; that configuration is daemon-resident so a single
// retrain invocation produces a deterministic checkpoint regardless of
// which host the CLI runs on.
//
// Output a single confirmation line with checkpoint path + accuracy +
// elapsed wall-clock. This is the same shape used by other one-shot
// daemon operations (hades audit verify, hades knowledge promote).
//
// Exit codes (per design contract):
//
// 0 success
// 1 recoverable: daemon 404 / 422
// 2 unrecoverable: training failure (5xx), transport, decode
package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"
)

const docsRouterRetrainTimeout = 10 * time.Minute

func NewDocsRouterRetrainCmd(factory DocsClientFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "router-retrain",
		Short: "Retrain the router's local logistic classifier",
		Long: `Invoke the daemon-side router-retrain pipeline. The daemon
resolves the corpus + output path from its configuration and runs the D-2
training pipeline (cmd/hades/docs.RunRouterRetrainWithOptions).

On success prints the persisted checkpoint path, held-out accuracy, and
total elapsed wall-clock.`,
		Example: `  hades docs router-retrain`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), docsRouterRetrainTimeout)
			defer cancel()
			return RunDocsRouterRetrain(ctx, c, cmd.OutOrStdout())
		},
	}
	return cmd
}

func RunDocsRouterRetrain(ctx context.Context, c DocsClient, w io.Writer) error {
	resp, err := c.DocsRouterRetrain(ctx)
	if err != nil {
		return classifyDocsError(err, "router-retrain")
	}
	fmt.Fprintf(w, "router-retrain ok: checkpoint=%s accuracy=%.3f elapsed=%dms\n",
		resp.CheckpointPath, resp.Accuracy, resp.ElapsedMs)
	return nil
}
