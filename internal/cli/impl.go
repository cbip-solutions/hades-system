// SPDX-License-Identifier: MIT
// Package cli — impl.go (Plan 19 Phase K).
//
// `zen impl <interface>` lists the concrete implementations of an interface
// (static polymorphism resolution: VTA/CHA + types.Implements), each with a
// confidence tier + reachability flag. Routes via the daemon
// /v1/mcpgateway/impl route → engine GetImplementations (spec §11).
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

const implTimeout = 30 * time.Second

type CaronteImplClient interface {
	Impl(ctx context.Context, req client.ImplRequest) (*client.ImplResponse, error)
}

type productionImplClient struct{ c *client.Client }

func (p *productionImplClient) Impl(ctx context.Context, req client.ImplRequest) (*client.ImplResponse, error) {
	return p.c.Impl(ctx, req)
}

type ImplFlags struct {
	Interface string
	Project   string
	Format    string
}

func NewImplCmd(factory func(cmd *cobra.Command) CaronteImplClient) *cobra.Command {
	flags := ImplFlags{}
	cmd := &cobra.Command{
		Use:   "impl <interface>",
		Short: "Concrete implementations of an interface (VTA/CHA + confidence tier)",
		Long: `List the concrete types that implement <interface>, resolved by static
analysis (go/types Implements + VTA/CHA call graph). Each carries a confidence
tier (exact_vta / exact_cha / scip_impl / heuristic_name) and a reachability
flag. Routes via the daemon (single-egress, inv-zen-088).`,
		Example: `  zen impl io.Writer
  zen impl internal/providers.Backend --format json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				flags.Interface = args[0]
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), implTimeout)
			defer cancel()
			return RunImpl(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.Project, "project", "", "scope to one project")
	cmd.Flags().StringVar(&flags.Format, "format", "text", "output format: text|json")
	return cmd
}

func NewImplCmdProd() *cobra.Command {
	return NewImplCmd(func(cmd *cobra.Command) CaronteImplClient {
		return &productionImplClient{c: newClientFromCmd(cmd)}
	})
}

func RunImpl(ctx context.Context, c CaronteImplClient, flags ImplFlags, w io.Writer) error {
	if strings.TrimSpace(flags.Interface) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("impl: <interface> is required"))
	}
	if flags.Format != "text" && flags.Format != "json" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--format %q must be text or json", flags.Format))
	}
	resp, err := c.Impl(ctx, client.ImplRequest{Interface: flags.Interface, ProjectAlias: flags.Project})
	if err != nil {
		return classifyMCPGatewayError(err, "impl")
	}
	if flags.Format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}
	if len(resp.Implementations) == 0 {
		_, e := fmt.Fprintln(w, "(no implementations found — interface may be unimplemented or unresolvable)")
		return e
	}
	fmt.Fprintf(w, "implementations of %s:\n", resp.Interface)
	for _, im := range resp.Implementations {
		reach := "reachable"
		if !im.Reachable {
			reach = "unreachable"
		}
		fmt.Fprintf(w, "  %-50s %s (%s)\n", im.ImplID, im.Confidence, reach)
	}
	return nil
}
