// SPDX-License-Identifier: MIT
// Package cli — federation.go (Plan 20 Phase I).
//
// `zen federation health [wsid]` + `zen api-impact <diff-ref>` — the
// federation health surface + the cross-repo API-impact analysis verb.
// Mirrors Plan 19 K's verb pattern (risk.go / why.go); each verb supports
// `--format text|json` per DECISION 1. Routes via the daemon
// /v1/mcpgateway/{federation/health,api-impact} sub-routes (inv-zen-088
// single-egress, inv-zen-129 no direct net/http).
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

const (
	federationHealthTimeout = 15 * time.Second
	apiImpactTimeout        = 30 * time.Second
)

type FederationClient interface {
	FederationHealth(ctx context.Context, req client.FederationHealthRequest) (*client.FederationHealthResponse, error)
	APIImpact(ctx context.Context, req client.APIImpactRequest) (*client.APIImpactResponse, error)
}

type productionFederationClient struct{ c *client.Client }

func (p *productionFederationClient) FederationHealth(ctx context.Context, req client.FederationHealthRequest) (*client.FederationHealthResponse, error) {
	return p.c.FederationHealth(ctx, req)
}
func (p *productionFederationClient) APIImpact(ctx context.Context, req client.APIImpactRequest) (*client.APIImpactResponse, error) {
	return p.c.APIImpact(ctx, req)
}

func NewFederationCmdProd() *cobra.Command {
	root := &cobra.Command{
		Use:   "federation",
		Short: "Plan 20 cross-repo federation observability",
		Long:  `Inspect federation reachability + per-workspace health.`,
	}
	root.AddCommand(NewFederationHealthCmdProd())
	return root
}

type FederationHealthFlags struct {
	WorkspaceID string
	Format      string
}

func NewFederationHealthCmd(factory func(cmd *cobra.Command) FederationClient) *cobra.Command {
	flags := FederationHealthFlags{}
	cmd := &cobra.Command{
		Use:   "health [workspace_id]",
		Short: "Report federation reachability + per-workspace health",
		Long: `Read the daemon's federation health surface. When [workspace_id] is
provided, scopes to one workspace; otherwise reports daemon-wide health.
Routes via the daemon (single-egress, inv-zen-088).`,
		Example: `  zen federation health
  zen federation health ws-1 --format json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				flags.WorkspaceID = args[0]
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), federationHealthTimeout)
			defer cancel()
			return RunFederationHealth(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.Format, "format", "text", "output format: text|json")
	return cmd
}

func RunFederationHealth(ctx context.Context, c FederationClient, flags FederationHealthFlags, w io.Writer) error {
	if flags.Format != "text" && flags.Format != "json" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--format %q must be text or json", flags.Format))
	}
	resp, err := c.FederationHealth(ctx, client.FederationHealthRequest{WorkspaceID: flags.WorkspaceID})
	if err != nil {
		return classifyCapaFirewallError(err, "federation health")
	}
	if flags.Format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}
	scope := "daemon-wide"
	if resp.WorkspaceID != "" {
		scope = "workspace " + resp.WorkspaceID
	}
	reachable := "yes"
	if !resp.Reachable {
		reachable = "no"
	}
	fmt.Fprintf(w, "scope:                     %s\n", scope)
	fmt.Fprintf(w, "reachable:                 %s\n", reachable)
	fmt.Fprintf(w, "gate_latency_p95_ms:       %.2f\n", resp.GateLatencyP95Ms)
	fmt.Fprintf(w, "indexing_currency_max_age: %ds\n", resp.IndexingCurrencyMaxAgeSec)
	fmt.Fprintf(w, "unresolved_count:          %d\n", resp.UnresolvedCount)
	fmt.Fprintf(w, "contract_links_count:      %d\n", resp.ContractLinksCount)
	fmt.Fprintf(w, "breaking_changes_open:     %d\n", resp.BreakingChangesOpenCount)
	if resp.LastAuditChainTip != "" {
		fmt.Fprintf(w, "last_audit_chain_tip:      %s\n", resp.LastAuditChainTip)
	}
	return nil
}

func NewFederationHealthCmdProd() *cobra.Command {
	return NewFederationHealthCmd(func(cmd *cobra.Command) FederationClient {
		return &productionFederationClient{c: newClientFromCmd(cmd)}
	})
}

type APIImpactFlags struct {
	DiffRef     string
	WorkspaceID string
	Format      string
}

func NewAPIImpactCmd(factory func(cmd *cobra.Command) FederationClient) *cobra.Command {
	flags := APIImpactFlags{}
	cmd := &cobra.Command{
		Use:   "api-impact <diff-ref>",
		Short: "Report consumers impacted by a diff (Plan 20 federation)",
		Long: `Analyze the impact of a contract diff (git ref or contract-diff identifier)
on cross-repo consumers. Routes via the daemon (single-egress, inv-zen-088).`,
		Example: `  zen api-impact HEAD~3..HEAD
  zen api-impact diff-abc123 --workspace ws-1 --format json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				flags.DiffRef = args[0]
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), apiImpactTimeout)
			defer cancel()
			return RunAPIImpact(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.WorkspaceID, "workspace", "", "scope to one workspace (default: daemon-wide)")
	cmd.Flags().StringVar(&flags.Format, "format", "text", "output format: text|json")
	return cmd
}

func RunAPIImpact(ctx context.Context, c FederationClient, flags APIImpactFlags, w io.Writer) error {
	if strings.TrimSpace(flags.DiffRef) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("api-impact: <diff-ref> is required"))
	}
	if flags.Format != "text" && flags.Format != "json" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--format %q must be text or json", flags.Format))
	}
	resp, err := c.APIImpact(ctx, client.APIImpactRequest{
		DiffRef: flags.DiffRef, WorkspaceID: flags.WorkspaceID,
	})
	if err != nil {
		return classifyCapaFirewallError(err, "api-impact")
	}
	if flags.Format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}
	fmt.Fprintf(w, "diff_ref:        %s\n", resp.DiffRef)
	if resp.WorkspaceID != "" {
		fmt.Fprintf(w, "workspace:       %s\n", resp.WorkspaceID)
	}
	fmt.Fprintf(w, "affected_count:  %d\n", resp.AffectedCount)
	if len(resp.Consumers) == 0 {
		fmt.Fprintln(w, "(no affected consumers)")
		return nil
	}
	fmt.Fprintln(w, "consumers:")
	for _, cons := range resp.Consumers {
		fmt.Fprintf(w, "  - %s/%s [%s]\n", cons.Repo, cons.CallID, cons.Severity)
	}
	return nil
}

func NewAPIImpactCmdProd() *cobra.Command {
	return NewAPIImpactCmd(func(cmd *cobra.Command) FederationClient {
		return &productionFederationClient{c: newClientFromCmd(cmd)}
	})
}
