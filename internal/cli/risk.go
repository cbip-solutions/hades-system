// SPDX-License-Identifier: MIT
// Package cli — risk.go.
//
// `zen risk <symbols-or-files…>` computes the blast-radius of a change set:
// a composite risk score (cone + coreness + churn + coupling) graded
// low|medium|high with the most-affected downstream symbols. Routes via the
// daemon /v1/mcpgateway/risk route → engine BlastRadius. Variadic args are
// classified into changed_files (path-like) vs changed_symbols (DECISION K-6);
// --file/--symbol override the heuristic.
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

const riskTimeout = 30 * time.Second

type CaronteRiskClient interface {
	Risk(ctx context.Context, req client.RiskRequest) (*client.RiskResponse, error)
}

type productionRiskClient struct{ c *client.Client }

func (p *productionRiskClient) Risk(ctx context.Context, req client.RiskRequest) (*client.RiskResponse, error) {
	return p.c.Risk(ctx, req)
}

type RiskFlags struct {
	Args    []string
	Files   []string
	Symbols []string
	Project string
	Format  string
}

func NewRiskCmd(factory func(cmd *cobra.Command) CaronteRiskClient) *cobra.Command {
	flags := RiskFlags{}
	cmd := &cobra.Command{
		Use:   "risk <symbols-or-files...>",
		Short: "Blast-radius risk score for a change set (low|medium|high + affected symbols)",
		Long: `Compute the blast radius of changing one or more symbols/files. Each
positional arg is classified as a file (path-like) or a symbol; use
--file/--symbol to override. The composite score weights reverse-reachability
cone + k-core coreness + churn + co-change coupling (spec §9). Routes via the
daemon (single-egress, inv-zen-088).`,
		Example: `  zen risk internal/orchestrator/merge/engine.go
  zen risk MergeEngine.Run transport.Forward --format json`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			flags.Args = args
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), riskTimeout)
			defer cancel()
			return RunRisk(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringSliceVar(&flags.Files, "file", nil, "explicit changed file (repeatable; overrides heuristic)")
	cmd.Flags().StringSliceVar(&flags.Symbols, "symbol", nil, "explicit changed symbol (repeatable; overrides heuristic)")
	cmd.Flags().StringVar(&flags.Project, "project", "", "scope to one project")
	cmd.Flags().StringVar(&flags.Format, "format", "text", "output format: text|json")
	return cmd
}

func NewRiskCmdProd() *cobra.Command {
	return NewRiskCmd(func(cmd *cobra.Command) CaronteRiskClient {
		return &productionRiskClient{c: newClientFromCmd(cmd)}
	})
}

func RunRisk(ctx context.Context, c CaronteRiskClient, flags RiskFlags, w io.Writer) error {
	if flags.Format != "text" && flags.Format != "json" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--format %q must be text or json", flags.Format))
	}
	files := append([]string{}, flags.Files...)
	symbols := append([]string{}, flags.Symbols...)

	for _, a := range flags.Args {
		if looksLikeFile(a) {
			files = append(files, a)
		} else {
			symbols = append(symbols, a)
		}
	}
	if len(files) == 0 && len(symbols) == 0 {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("risk: at least one symbol or file is required"))
	}
	resp, err := c.Risk(ctx, client.RiskRequest{
		ChangedSymbols: symbols,
		ChangedFiles:   files,
		ProjectAlias:   flags.Project,
	})
	if err != nil {
		return classifyMCPGatewayError(err, "risk")
	}
	if flags.Format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}
	fmt.Fprintf(w, "level:    %s\n", resp.Level)
	fmt.Fprintf(w, "score:    %.3f\n", resp.Score)
	fmt.Fprintf(w, "  cone=%.2f coreness=%.2f churn=%.2f coupling=%.2f\n", resp.Cone, resp.Coreness, resp.Churn, resp.Coupling)
	if len(resp.TopAffected) > 0 {
		fmt.Fprintln(w, "top affected:")
		for _, s := range resp.TopAffected {
			fmt.Fprintln(w, "  -", s)
		}
	}
	return nil
}

func looksLikeFile(arg string) bool {
	if strings.Contains(arg, "/") {
		return true
	}
	for _, ext := range []string{".go", ".ts", ".tsx", ".js", ".py", ".rs"} {
		if strings.HasSuffix(arg, ext) {
			return true
		}
	}
	return false
}
