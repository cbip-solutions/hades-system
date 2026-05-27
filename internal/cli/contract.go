// SPDX-License-Identifier: MIT
// Package cli — contract.go.
//
// zen contract <endpoint> — text/JSON dump for an endpoint (mirrors get_contract MCP).
// zen contract validate <repo> — validate caronte.yaml against schema v1 (§6 corpus).
// zen contract why <change_id> — D7 Lore-attribution for a breaking_changes row.
//
// Routes via the daemon /v1/mcpgateway/contract* sub-routes → caronte engine.
// Single-egress preserved. Tests inject ContractClient fakes.
// Capa-firewall errors (store.ErrCrossProjectDenied / ErrUnauthorizedProject)
// surface as actionable operator hints via classifyCapaFirewallError
// .
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

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

const (
	contractTimeout         = 15 * time.Second
	contractValidateTimeout = 30 * time.Second
	contractWhyTimeout      = 15 * time.Second
)

type ContractClient interface {
	Contract(ctx context.Context, req client.ContractRequest) (*client.ContractResponse, error)
	ContractValidate(ctx context.Context, req client.ContractValidateRequest) (*client.ContractValidateResponse, error)
	ContractWhy(ctx context.Context, req client.ContractWhyRequest) (*client.ContractWhyResponse, error)
}

type productionContractClient struct{ c *client.Client }

func (p *productionContractClient) Contract(ctx context.Context, req client.ContractRequest) (*client.ContractResponse, error) {
	return p.c.Contract(ctx, req)
}
func (p *productionContractClient) ContractValidate(ctx context.Context, req client.ContractValidateRequest) (*client.ContractValidateResponse, error) {
	return p.c.ContractValidate(ctx, req)
}
func (p *productionContractClient) ContractWhy(ctx context.Context, req client.ContractWhyRequest) (*client.ContractWhyResponse, error) {
	return p.c.ContractWhy(ctx, req)
}

type ContractFlags struct {
	Endpoint    string
	WorkspaceID string
	Format      string
}

func NewContractCmd(factory func(cmd *cobra.Command) ContractClient) *cobra.Command {
	flags := ContractFlags{}
	cmd := &cobra.Command{
		Use:   "contract <endpoint>",
		Short: "Fetch an endpoint's contract artifact + extractor metadata (Plan 20 federation)",
		Long: `Look up a Plan-20 federated endpoint by its endpoint_id. The daemon
delegates to the Caronte engine; output is the extractor-resolved metadata
(method/path/handler_node_id + the contract_artifact pointer + the
extractor_id provenance). Routes via the daemon (single-egress, inv-zen-088).`,
		Example: `  zen contract repo-a:http:GET:/users/{id}
  zen contract endpoint-1 --workspace ws-1 --format json | jq '.extractor_id'`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				flags.Endpoint = args[0]
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), contractTimeout)
			defer cancel()
			return RunContract(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.WorkspaceID, "workspace", "", "scope to one workspace (default: project-scoped)")
	cmd.Flags().StringVar(&flags.Format, "format", "text", "output format: text|json")
	return cmd
}

func RunContract(ctx context.Context, c ContractClient, flags ContractFlags, w io.Writer) error {
	if strings.TrimSpace(flags.Endpoint) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("contract: <endpoint> is required"))
	}
	if flags.Format != "text" && flags.Format != "json" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--format %q must be text or json", flags.Format))
	}
	resp, err := c.Contract(ctx, client.ContractRequest{Endpoint: flags.Endpoint, WorkspaceID: flags.WorkspaceID})
	if err != nil {
		return classifyCapaFirewallError(err, "contract")
	}
	if flags.Format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}
	fmt.Fprintf(w, "endpoint_id:       %s\n", resp.EndpointID)
	if resp.Method != "" {
		fmt.Fprintf(w, "method:            %s\n", resp.Method)
	}
	if resp.PathTemplate != "" {
		fmt.Fprintf(w, "path_template:     %s\n", resp.PathTemplate)
	}
	if resp.ProtoService != "" {
		fmt.Fprintf(w, "proto:             %s.%s\n", resp.ProtoService, resp.ProtoRPC)
	}
	if resp.ContractArtifact != "" {
		fmt.Fprintf(w, "contract_artifact: %s\n", resp.ContractArtifact)
	}
	fmt.Fprintf(w, "handler_node_id:   %s\n", resp.HandlerNodeID)
	fmt.Fprintf(w, "extractor_id:      %s\n", resp.ExtractorID)
	fmt.Fprintf(w, "extracted_at:      %d\n", resp.ExtractedAt)
	return nil
}

func NewContractCmdProd() *cobra.Command {
	root := NewContractCmd(func(cmd *cobra.Command) ContractClient {
		return &productionContractClient{c: newClientFromCmd(cmd)}
	})
	root.AddCommand(NewContractValidateCmdProd())
	root.AddCommand(NewContractWhyCmdProd())
	return root
}

type ContractValidateFlags struct {
	Repo        string
	WorkspaceID string
	Format      string
}

func NewContractValidateCmd(factory func(cmd *cobra.Command) ContractClient) *cobra.Command {
	flags := ContractValidateFlags{}
	cmd := &cobra.Command{
		Use:   "validate <repo>",
		Short: "Validate the repo's caronte.yaml against schema v1 (Plan 20 §6)",
		Long: `Validate the repo's caronte.yaml federation manifest. Refuses on:
missing schema_version; multiple base_url variants per service entry;
unknown target_repo; inline secrets (blacklisted field names); regex past
MaxPatternRunes; regex-DoS pattern; invalid unresolved_policy. Routes via
the daemon (single-egress, inv-zen-088).`,
		Example: `  zen contract validate /path/to/projects/backend
  zen contract validate . --workspace ws-1 --format json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				flags.Repo = args[0]
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), contractValidateTimeout)
			defer cancel()
			return RunContractValidate(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.WorkspaceID, "workspace", "", "workspace id whose member roster validates target_repo")
	cmd.Flags().StringVar(&flags.Format, "format", "text", "output format: text|json")
	return cmd
}

func RunContractValidate(ctx context.Context, c ContractClient, flags ContractValidateFlags, w io.Writer) error {
	if strings.TrimSpace(flags.Repo) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("contract validate: <repo> is required"))
	}
	if flags.Format != "text" && flags.Format != "json" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--format %q must be text or json", flags.Format))
	}
	resp, err := c.ContractValidate(ctx, client.ContractValidateRequest{
		Repo: flags.Repo, WorkspaceID: strings.TrimSpace(flags.WorkspaceID),
	})
	if err != nil {
		return classifyCapaFirewallError(err, "contract validate")
	}
	if flags.Format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}
	if resp.Valid {
		fmt.Fprintf(w, "caronte.yaml is valid (schema v%d)\n", resp.SchemaVersion)
		fmt.Fprintf(w, "  services: %d\n", len(resp.Services))
		for _, s := range resp.Services {
			fmt.Fprintf(w, "    - %s -> %s\n", s.BaseURLRef, s.TargetRepo)
		}
		return nil
	}
	fmt.Fprintln(w, "caronte.yaml validation FAILED:")
	for _, e := range resp.Errors {
		fmt.Fprintf(w, "  - %s: %s\n", e.Code, e.Message)
	}
	return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("contract validate: %d errors", len(resp.Errors)))
}

func NewContractValidateCmdProd() *cobra.Command {
	return NewContractValidateCmd(func(cmd *cobra.Command) ContractClient {
		return &productionContractClient{c: newClientFromCmd(cmd)}
	})
}

type ContractWhyFlags struct {
	ChangeID string
	Format   string
}

func NewContractWhyCmd(factory func(cmd *cobra.Command) ContractClient) *cobra.Command {
	flags := ContractWhyFlags{}
	cmd := &cobra.Command{
		Use:   "why <change_id>",
		Short: "D7 Lore-attribution for a breaking_changes row (author, ADR refs, supersession)",
		Long: `Surface the D7 Lore-attribution for a breaking_changes row: who made the
change, the commit SHA, the ADR refs cited in trailers, the supersession
history. Mirrors get_why_breaking_change MCP.`,
		Example: `  zen contract why chg-001
  zen contract why chg-001 --format json | jq '.lore_adr_refs'`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				flags.ChangeID = args[0]
			}
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), contractWhyTimeout)
			defer cancel()
			return RunContractWhy(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.Format, "format", "text", "output format: text|json")
	return cmd
}

func RunContractWhy(ctx context.Context, c ContractClient, flags ContractWhyFlags, w io.Writer) error {
	if strings.TrimSpace(flags.ChangeID) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("contract why: <change_id> is required"))
	}
	if flags.Format != "text" && flags.Format != "json" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("--format %q must be text or json", flags.Format))
	}
	resp, err := c.ContractWhy(ctx, client.ContractWhyRequest{ChangeID: flags.ChangeID})
	if err != nil {
		return classifyCapaFirewallError(err, "contract why")
	}
	if flags.Format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}
	fmt.Fprintf(w, "change_id:    %s\n", resp.ChangeID)
	fmt.Fprintf(w, "endpoint:     %s (%s)\n", resp.EndpointID, resp.EndpointRepo)
	if resp.LoreAuthor != "" {
		fmt.Fprintf(w, "author:       %s\n", resp.LoreAuthor)
	}
	if resp.LoreCommitSHA != "" {
		fmt.Fprintf(w, "commit:       %s\n", shortSHA(resp.LoreCommitSHA))
	}
	if len(resp.LoreADRRefs) > 0 {
		fmt.Fprintf(w, "adr_refs:     %s\n", strings.Join(resp.LoreADRRefs, ", "))
	}
	if len(resp.LoreSupersedes) > 0 {
		fmt.Fprintf(w, "supersedes:   %s\n", strings.Join(resp.LoreSupersedes, ", "))
	}
	if resp.CommitSubject != "" {
		fmt.Fprintf(w, "subject:      %s\n", resp.CommitSubject)
	}
	if resp.CommitBodyExcerpt != "" {
		fmt.Fprintf(w, "body:         %s\n", truncateLine(resp.CommitBodyExcerpt, 200))
	}
	return nil
}

func NewContractWhyCmdProd() *cobra.Command {
	return NewContractWhyCmd(func(cmd *cobra.Command) ContractClient {
		return &productionContractClient{c: newClientFromCmd(cmd)}
	})
}

func classifyCapaFirewallError(err error, op string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrRecoverable) {
		return err
	}
	if errors.Is(err, store.ErrCrossProjectDenied) {
		return ierrors.Wrap(ierrors.Code("cli.capa-firewall-denied"), recoverableWrap(err,
			fmt.Sprintf("%s: cross-project access denied — workspace privacy policy is locked. "+
				"To proceed, either (1) work within the owning project's scope, or "+
				"(2) ask the operator to run `zen workspace policy set <wsid> permissive`.", op)))
	}
	if errors.Is(err, store.ErrUnauthorizedProject) {
		return ierrors.Wrap(ierrors.Code("cli.capa-firewall-denied"), recoverableWrap(err,
			fmt.Sprintf("%s: project not on workspace roster — "+
				"run `zen workspace members <wsid>` to inspect, "+
				"or `zen workspace link <wsid> <project>` to add.", op)))
	}

	if client.IsHTTPStatus(err, http.StatusForbidden) {
		return ierrors.Wrap(ierrors.Code("cli.capa-firewall-denied"), recoverableWrap(err,
			fmt.Sprintf("%s: capa-firewall denied — workspace policy or roster blocked the request.", op)))
	}
	return classifyMCPGatewayError(err, op)
}
