// SPDX-License-Identifier: MIT
// Package cli — codegraph.go
//
// daemon /v1/mcpgateway/{codegraph,impact,context,wiki}. These bypass
// Hermes (so operators can query the KG without a Hermes session
// running) but route via the daemon — single-egress + audit chain
// preserved.
//
// Each command lazily resolves *client.Client at RunE time via
// newClientFromCmd. Tests inject CodegraphClient interface fakes.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

const (
	codegraphTimeout = 15 * time.Second
	impactTimeout    = 30 * time.Second
	contextTimeout   = 15 * time.Second
	wikiTimeout      = 60 * time.Second
)

type CodegraphClient interface {
	CodegraphQuery(ctx context.Context, req client.CodegraphQueryRequest) (*client.CodegraphQueryResponse, error)
	Impact(ctx context.Context, req client.ImpactRequest) (*client.ImpactResponse, error)
	Context360(ctx context.Context, req client.Context360Request) (*client.Context360Response, error)
	Wiki(ctx context.Context, req client.WikiRequest) (*client.WikiResponse, error)
}

type CodegraphClientFactory func(cmd *cobra.Command) CodegraphClient

type productionCodegraphClient struct {
	c *client.Client
}

func (p *productionCodegraphClient) CodegraphQuery(ctx context.Context, req client.CodegraphQueryRequest) (*client.CodegraphQueryResponse, error) {
	return p.c.CodegraphQuery(ctx, req)
}
func (p *productionCodegraphClient) Impact(ctx context.Context, req client.ImpactRequest) (*client.ImpactResponse, error) {
	return p.c.Impact(ctx, req)
}
func (p *productionCodegraphClient) Context360(ctx context.Context, req client.Context360Request) (*client.Context360Response, error) {
	return p.c.Context360(ctx, req)
}
func (p *productionCodegraphClient) Wiki(ctx context.Context, req client.WikiRequest) (*client.WikiResponse, error) {
	return p.c.Wiki(ctx, req)
}

type CodegraphFlags struct {
	Query   string
	Project string
	Limit   int
	Format  string
}

func NewCodegraphCmd(factory CodegraphClientFactory) *cobra.Command {
	flags := CodegraphFlags{}
	cmd := &cobra.Command{
		Use:   "codegraph <query>",
		Short: "Direct caronte code-graph query (bypasses Hermes; routes via daemon → mcpgateway → caronte)",
		Long: `Query the caronte code-graph directly. Single-egress preserved
via daemon /v1/mcpgateway/codegraph route (inv-zen-088); the daemon
proxies to the in-process caronte engine.`,
		Example: `  zen codegraph MergeEngine
  zen codegraph "RRF.*" --project internal-platform-x --format json | jq '.hits[].file'`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				flags.Query = args[0]
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), codegraphTimeout)
			defer cancel()
			return RunCodegraph(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.Project, "project", "", "limit query to one project (alias)")
	cmd.Flags().IntVar(&flags.Limit, "limit", 0, "max hits (default daemon-side: 50)")
	cmd.Flags().StringVar(&flags.Format, "format", "text", "output format: text|json")
	return cmd
}

func NewCodegraphCmdProd() *cobra.Command {
	return NewCodegraphCmd(func(cmd *cobra.Command) CodegraphClient {
		return &productionCodegraphClient{c: newClientFromCmd(cmd)}
	})
}

func RunCodegraph(ctx context.Context, c CodegraphClient, flags CodegraphFlags, w io.Writer) error {
	if strings.TrimSpace(flags.Query) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("codegraph: <query> is required"))
	}
	if flags.Format != "text" && flags.Format != "json" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--format %q must be text or json", flags.Format))
	}
	resp, err := c.CodegraphQuery(ctx, client.CodegraphQueryRequest{
		Query:        flags.Query,
		ProjectAlias: flags.Project,
		Limit:        flags.Limit,
	})
	if err != nil {
		return classifyMCPGatewayError(err, "codegraph")
	}
	if flags.Format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}
	if len(resp.Hits) == 0 {
		_, e := fmt.Fprintln(w, "(no hits)")
		return e
	}
	for _, h := range resp.Hits {
		fmt.Fprintf(w, "%s\t%s:%d\t%s\n", h.Symbol, h.File, h.Line, h.Kind)
	}
	return nil
}

type ImpactFlags struct {
	Symbol  string
	Project string
	Format  string
}

func NewImpactCmd(factory CodegraphClientFactory) *cobra.Command {
	flags := ImpactFlags{}
	cmd := &cobra.Command{
		Use:   "impact <symbol>",
		Short: "Blast-radius impact analysis for a symbol (low|medium|high + affected files)",
		Long: `Compute the blast radius of changing <symbol>. The daemon delegates
to the caronte engine for caller graph + GraphRAG community-aware spread; the
score is graded against doctrine.preflight.impact_thresholds.`,
		Example: `  zen impact MergeEngine.Run --project zen-swarm
  zen impact transport.ZenSwarmTransport.Forward --format json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				flags.Symbol = args[0]
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), impactTimeout)
			defer cancel()
			return RunImpact(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.Project, "project", "", "scope to one project")
	cmd.Flags().StringVar(&flags.Format, "format", "text", "output format: text|json")
	return cmd
}

func NewImpactCmdProd() *cobra.Command {
	return NewImpactCmd(func(cmd *cobra.Command) CodegraphClient {
		return &productionCodegraphClient{c: newClientFromCmd(cmd)}
	})
}

func RunImpact(ctx context.Context, c CodegraphClient, flags ImpactFlags, w io.Writer) error {
	if strings.TrimSpace(flags.Symbol) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("impact: <symbol> is required"))
	}
	resp, err := c.Impact(ctx, client.ImpactRequest{
		Symbol:       flags.Symbol,
		ProjectAlias: flags.Project,
	})
	if err != nil {
		return classifyMCPGatewayError(err, "impact")
	}
	if flags.Format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}
	fmt.Fprintf(w, "symbol:        %s\n", resp.Symbol)
	fmt.Fprintf(w, "blast_radius:  %s\n", resp.BlastRadius)
	fmt.Fprintf(w, "score:         %d\n", resp.Score)
	if len(resp.AffectedFiles) > 0 {
		fmt.Fprintln(w, "affected_files:")
		for _, f := range resp.AffectedFiles {
			fmt.Fprintln(w, "  -", f)
		}
	}
	return nil
}

type ContextFlags struct {
	Symbol  string
	Project string
	Format  string
}

func NewContextCmd(factory CodegraphClientFactory) *cobra.Command {
	flags := ContextFlags{}
	cmd := &cobra.Command{
		Use:   "context <symbol>",
		Short: "360° context for a symbol: callers + callees + community + neighbors",
		Long: `Show the symbol's full graph context: who calls it, what it calls,
GraphRAG community label, and immediate KG neighbors.`,
		Example: `  zen context MergeEngine
  zen context internal/orchestrator.WaitForReviews --format json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				flags.Symbol = args[0]
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), contextTimeout)
			defer cancel()
			return RunContext(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.Project, "project", "", "scope to one project")
	cmd.Flags().StringVar(&flags.Format, "format", "text", "output format: text|json")
	return cmd
}

func NewContextCmdProd() *cobra.Command {
	return NewContextCmd(func(cmd *cobra.Command) CodegraphClient {
		return &productionCodegraphClient{c: newClientFromCmd(cmd)}
	})
}

func RunContext(ctx context.Context, c CodegraphClient, flags ContextFlags, w io.Writer) error {
	if strings.TrimSpace(flags.Symbol) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("context: <symbol> is required"))
	}
	resp, err := c.Context360(ctx, client.Context360Request{
		Symbol:       flags.Symbol,
		ProjectAlias: flags.Project,
	})
	if err != nil {
		return classifyMCPGatewayError(err, "context")
	}
	if flags.Format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}
	fmt.Fprintf(w, "symbol:    %s\n", resp.Symbol)
	if resp.Community != "" {
		fmt.Fprintf(w, "community: %s\n", resp.Community)
	}
	if len(resp.Callers) > 0 {
		fmt.Fprintln(w, "callers:")
		for _, x := range resp.Callers {
			fmt.Fprintln(w, "  -", x)
		}
	}
	if len(resp.Callees) > 0 {
		fmt.Fprintln(w, "callees:")
		for _, x := range resp.Callees {
			fmt.Fprintln(w, "  -", x)
		}
	}
	if len(resp.Neighbors) > 0 {
		fmt.Fprintln(w, "neighbors:")
		for _, x := range resp.Neighbors {
			fmt.Fprintln(w, "  -", x)
		}
	}
	return nil
}

type WikiFlags struct {
	Module     string
	Project    string
	Regenerate bool
}

func NewWikiCmd(factory CodegraphClientFactory) *cobra.Command {
	flags := WikiFlags{}
	cmd := &cobra.Command{
		Use:   "wiki [module]",
		Short: "View or regenerate auto-generated KG wiki for a module (or full project)",
		Long: `Render the caronte auto-generated wiki. Without [module] returns the
full project wiki; with [module] scopes to one Go package.

--regenerate forces a fresh build (bypasses cached output).`,
		Example: `  zen wiki | glow
  zen wiki internal/orchestrator/merge --regenerate`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				flags.Module = args[0]
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), wikiTimeout)
			defer cancel()
			return RunWiki(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.Project, "project", "", "scope to one project")
	cmd.Flags().BoolVar(&flags.Regenerate, "regenerate", false, "force rebuild (bypass cache)")
	return cmd
}

func NewWikiCmdProd() *cobra.Command {
	return NewWikiCmd(func(cmd *cobra.Command) CodegraphClient {
		return &productionCodegraphClient{c: newClientFromCmd(cmd)}
	})
}

func RunWiki(ctx context.Context, c CodegraphClient, flags WikiFlags, w io.Writer) error {
	resp, err := c.Wiki(ctx, client.WikiRequest{
		Module:       flags.Module,
		ProjectAlias: flags.Project,
		Regenerate:   flags.Regenerate,
	})
	if err != nil {
		return classifyMCPGatewayError(err, "wiki")
	}
	_, e := io.WriteString(w, resp.Markdown)
	return e
}

func ClassifyMCPGatewayErrorForTest(err error, op string) error {
	return classifyMCPGatewayError(err, op)
}

func classifyMCPGatewayError(err error, op string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrRecoverable) {
		return err
	}
	if client.IsHTTPStatus(err, http.StatusServiceUnavailable) {
		return ierrors.Wrap(ierrors.Code("plugin.mcp-handshake-fail"), recoverableWrap(err,
			fmt.Sprintf("%s: caronte unreachable (daemon will retry)", op)))
	}
	if client.IsHTTPStatus(err, http.StatusUnprocessableEntity) {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, fmt.Sprintf("%s: daemon rejected input", op)))
	}
	if client.IsHTTPStatus(err, http.StatusNotFound) {
		return ierrors.Wrap(ierrors.CodeEndpointNotFound, fmt.Errorf("%s: daemon returned 404 (endpoint moved or deprecated): %w", op, err))
	}
	if client.IsHTTPStatus(err, http.StatusBadRequest) {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("%s: daemon returned 400 (bad request): %w", op, err))
	}

	var httpErr *client.HTTPError
	if errors.As(err, &httpErr) {
		return ierrors.Wrap(ierrors.Code("daemon.responded-with-error"),
			fmt.Errorf("%s: daemon responded with %d: %w", op, httpErr.Status, err))
	}
	return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("%s: %w", op, err))
}
