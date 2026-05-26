// SPDX-License-Identifier: MIT
// Package cli — cochange.go (Plan 19 Phase K).
//
// `zen cochange <file>` lists files historically co-changed with <file>
// (code-maat coupling). Surfaces invisible coupling so a worker does not
// break it before the compiler catches it (spec §8). Routes via the daemon
// /v1/mcpgateway/cochange route → engine GetCoChange.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

const cochangeTimeout = 15 * time.Second

type CaronteCochangeClient interface {
	CoChange(ctx context.Context, req client.CoChangeRequest) (*client.CoChangeResponse, error)
}

type productionCochangeClient struct{ c *client.Client }

func (p *productionCochangeClient) CoChange(ctx context.Context, req client.CoChangeRequest) (*client.CoChangeResponse, error) {
	return p.c.CoChange(ctx, req)
}

type CochangeFlags struct {
	File    string
	Project string
	Format  string
}

func NewCochangeCmd(factory func(cmd *cobra.Command) CaronteCochangeClient) *cobra.Command {
	flags := CochangeFlags{}
	cmd := &cobra.Command{
		Use:   "cochange <file>",
		Short: "Files historically co-changed with <file> (invisible-coupling guard)",
		Long: `List files that change together with <file> across git history
(code-maat coupling_degree). High coupling means editing one likely needs the
other — surfaced before the compiler catches the break (spec §8). Below the
cold-start gate (insufficient history) the engine returns no peers. Routes via
the daemon (single-egress, inv-zen-088).`,
		Example: `  zen cochange internal/orchestrator/merge/engine.go
  zen cochange internal/daemon/server.go --format json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				flags.File = args[0]
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), cochangeTimeout)
			defer cancel()
			return RunCochange(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.Project, "project", "", "scope to one project")
	cmd.Flags().StringVar(&flags.Format, "format", "text", "output format: text|json")
	return cmd
}

func NewCochangeCmdProd() *cobra.Command {
	return NewCochangeCmd(func(cmd *cobra.Command) CaronteCochangeClient {
		return &productionCochangeClient{c: newClientFromCmd(cmd)}
	})
}

func RunCochange(ctx context.Context, c CaronteCochangeClient, flags CochangeFlags, w io.Writer) error {
	if strings.TrimSpace(flags.File) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("cochange: <file> is required"))
	}
	if flags.Format != "text" && flags.Format != "json" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--format %q must be text or json", flags.Format))
	}
	resp, err := c.CoChange(ctx, client.CoChangeRequest{File: flags.File, ProjectAlias: flags.Project})
	if err != nil {
		return classifyMCPGatewayError(err, "cochange")
	}
	if flags.Format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}
	if len(resp.Peers) == 0 {
		_, e := fmt.Fprintln(w, "(no co-change peers — insufficient history or isolated file)")
		return e
	}
	fmt.Fprintf(w, "co-changed with %s:\n", resp.File)
	for _, p := range resp.Peers {
		fmt.Fprintf(w, "  %-50s %5.0f%% (%d shared / %dd)\n", p.Path, p.CouplingPercent, p.SharedRevs, p.WindowDays)
	}
	return nil
}
