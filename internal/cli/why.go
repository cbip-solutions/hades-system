// SPDX-License-Identifier: MIT
// Package cli — why.go.
//
// `hades why <symbol>` surfaces the architect's intent for a symbol: linked
// ADRs (+stale flag), semantic passages, and Lore-trailers from the
// symbol's commit history. Routes via the daemon /v1/mcpgateway/why route
// → the native Caronte engine GetWhy (single-egress preserved, invariant).
// Tests inject CaronteWhyClient fakes.
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

const whyTimeout = 15 * time.Second

type CaronteWhyClient interface {
	Why(ctx context.Context, req client.WhyRequest) (*client.WhyResponse, error)
}

type productionWhyClient struct{ c *client.Client }

func (p *productionWhyClient) Why(ctx context.Context, req client.WhyRequest) (*client.WhyResponse, error) {
	return p.c.Why(ctx, req)
}

type WhyFlags struct {
	Symbol  string
	Project string
	Format  string
}

func NewWhyCmd(factory func(cmd *cobra.Command) CaronteWhyClient) *cobra.Command {
	flags := WhyFlags{}
	cmd := &cobra.Command{
		Use:   "why <symbol>",
		Short: "Architect's intent for a symbol: linked ADRs (+stale), passages, Lore-trailers",
		Long:  "Surface WHY a symbol exists before you change it. Caronte links the\nsymbol to ADRs (explicit refs + coverage manifest + semantic similarity),\nflags stale links (the code changed since the ADR), and shows Lore-trailers\nfrom its commit history. Routes via the daemon (single-egress, invariant).",

		Example: `  hades why MergeEngine
  hades why internal/orchestrator/merge.Engine --format json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				flags.Symbol = args[0]
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), whyTimeout)
			defer cancel()
			return RunWhy(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.Project, "project", "", "scope to one project (alias)")
	cmd.Flags().StringVar(&flags.Format, "format", "text", "output format: text|json")
	return cmd
}

func NewWhyCmdProd() *cobra.Command {
	return NewWhyCmd(func(cmd *cobra.Command) CaronteWhyClient {
		return &productionWhyClient{c: newClientFromCmd(cmd)}
	})
}

func RunWhy(ctx context.Context, c CaronteWhyClient, flags WhyFlags, w io.Writer) error {
	if strings.TrimSpace(flags.Symbol) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("why: <symbol> is required"))
	}
	if flags.Format != "text" && flags.Format != "json" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--format %q must be text or json", flags.Format))
	}
	resp, err := c.Why(ctx, client.WhyRequest{Symbol: flags.Symbol, ProjectAlias: flags.Project})
	if err != nil {
		return classifyMCPGatewayError(err, "why")
	}
	if flags.Format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}
	fmt.Fprintf(w, "subject: %s\n", resp.Subject)
	if resp.Degraded {
		fmt.Fprintln(w, "(degraded — some intent layers unavailable)")
	}
	if len(resp.LinkedADRs) > 0 {
		fmt.Fprintln(w, "linked ADRs:")
		for _, a := range resp.LinkedADRs {
			staleTag := ""
			if a.Stale {
				staleTag = " [stale]"
			}
			fmt.Fprintf(w, "  - %s %s (%s, conf %.2f)%s\n", a.ADRID, a.ADRTitle, a.LinkKind, a.Confidence, staleTag)
		}
	}
	if len(resp.SemanticPassages) > 0 {
		fmt.Fprintln(w, "passages:")
		for _, s := range resp.SemanticPassages {
			fmt.Fprintf(w, "  - [%s %s] %s (%.2f)\n", s.SourceKind, s.SourceID, truncateLine(s.Text, 80), s.Score)
		}
	}
	if len(resp.LoreTrailers) > 0 {
		fmt.Fprintln(w, "lore:")
		for _, l := range resp.LoreTrailers {
			fmt.Fprintf(w, "  - %s %s: %s\n", shortSHA(l.CommitSHA), l.TrailerKind, truncateLine(l.Body, 80))
		}
	}
	if len(resp.LinkedADRs) == 0 && len(resp.SemanticPassages) == 0 && len(resp.LoreTrailers) == 0 {
		fmt.Fprintln(w, "(no intent links found — symbol may be undocumented)")
	}
	return nil
}

func truncateLine(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
