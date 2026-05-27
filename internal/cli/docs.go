// SPDX-License-Identifier: MIT
// Package cli — docs.go.
//
// `hades docs` is the operator-facing surface for ecosystem-docs corpus
// management. The release design registers six management subcommands:
//
// hades docs reindex rebuild/refresh
// hades docs pin --ecosystem X --version Y set indefinite_retain=true (G-5)
// hades docs prune --ecosystem X --version Y --confirm hard-remove version (G-5)
// hades docs status per-ecosystem table
// hades docs sources --list per-source health table
// hades docs router-retrain retrain D-2 classifier
//
// All six talk to the HADES daemon (hades-ctld) over the HTTP API
// (internal/client.ecosystem_docs_ops.go). The daemon-side handlers land
// in — until then the endpoints return 503 and the CLI maps that
// to exit-code 2 (unrecoverable per spec §6.2).
//
// G-5 evolution from F-6 (operator-confirmed retention per spec §2.9 Q9=A):
// - pin: chunk-id positional → flag-based --ecosystem --version. Sets
// ecosystem_versions.indefinite_retain=true; daemon path moves from
// /v1/knowledge/ecosystem/pin to /v1/ecosystem/pin.
// - prune: dry-run-or-confirm flags → preview (GET /v1/ecosystem/prune-preview)
// - promptYN gate + commit (DELETE /v1/ecosystem/version). Daemon refuses
// pruning pinned versions (409 Conflict); CLI surfaces unpin guidance.
//
// Boundary stdlib + spf13/cobra + internal/client only. No
// internal/research/ecosystem import (invariant); the orchestrator
// owns ecosystem operations and the daemon mediates.
//
// History pre-F-6, `hades docs` registered six `notImplementedSubcommand`
// stubs (show/open/diff/versions/export/recover) targeting release. F-6
// replaced them with the management surface above. G-5 refined the
// pin/prune semantics from F-6's initial chunk-id-based / dry-run-or-confirm
// shape to the canonical version-retention contract from the spec.
package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

type DocsClient interface {
	DocsReindex(ctx context.Context, req client.DocsReindexRequest) (*client.DocsReindexResponse, error)
	EcosystemPin(ctx context.Context, ecosystem, version string) error
	EcosystemPrunePreview(ctx context.Context, ecosystem, version string) (*client.EcosystemPrunePreview, error)
	EcosystemPrune(ctx context.Context, ecosystem, version string) error
	DocsStatus(ctx context.Context) (*client.DocsStatusResponse, error)
	DocsSources(ctx context.Context) (*client.DocsSourcesResponse, error)
	DocsRouterRetrain(ctx context.Context) (*client.RouterRetrainResponse, error)
}

type DocsClientFactory func(cmd *cobra.Command) DocsClient

type productionDocsClient struct {
	c *client.Client
}

func (p *productionDocsClient) DocsReindex(ctx context.Context, req client.DocsReindexRequest) (*client.DocsReindexResponse, error) {
	return p.c.DocsReindex(ctx, req)
}

func (p *productionDocsClient) EcosystemPin(ctx context.Context, ecosystem, version string) error {
	return p.c.EcosystemPin(ctx, ecosystem, version)
}

func (p *productionDocsClient) EcosystemPrunePreview(ctx context.Context, ecosystem, version string) (*client.EcosystemPrunePreview, error) {
	return p.c.EcosystemPrunePreview(ctx, ecosystem, version)
}

func (p *productionDocsClient) EcosystemPrune(ctx context.Context, ecosystem, version string) error {
	return p.c.EcosystemPrune(ctx, ecosystem, version)
}

func (p *productionDocsClient) DocsStatus(ctx context.Context) (*client.DocsStatusResponse, error) {
	return p.c.DocsStatus(ctx)
}

func (p *productionDocsClient) DocsSources(ctx context.Context) (*client.DocsSourcesResponse, error) {
	return p.c.DocsSources(ctx)
}

func (p *productionDocsClient) DocsRouterRetrain(ctx context.Context) (*client.RouterRetrainResponse, error) {
	return p.c.DocsRouterRetrain(ctx)
}

func productionDocsFactory(cmd *cobra.Command) DocsClient {
	return &productionDocsClient{c: newClientFromCmd(cmd)}
}

func NewDocsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Ecosystem documentation corpus management",
		Long: `Manage the Plan 14 ecosystem-docs corpus that powers the
RAG-grounded augmentation surface.

Subcommands talk to the HADES daemon (hades-ctld) via the HTTP API; the daemon
orchestrates ecosystem ingestion, pruning, and the local router classifier
retrain pipeline.`,
	}
	cmd.AddCommand(NewDocsReindexCmd(productionDocsFactory))
	cmd.AddCommand(NewDocsPinCmd(productionDocsFactory))
	cmd.AddCommand(NewDocsPruneCmd(productionDocsFactory))
	cmd.AddCommand(NewDocsStatusCmd(productionDocsFactory))
	cmd.AddCommand(NewDocsSourcesCmd(productionDocsFactory))
	cmd.AddCommand(NewDocsRouterRetrainCmd(productionDocsFactory))
	return cmd
}

func classifyDocsError(err error, op string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrRecoverable) {
		return err
	}
	if client.IsHTTPStatus(err, http.StatusNotFound) {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, fmt.Sprintf("docs %s: not found", op)))
	}
	if client.IsHTTPStatus(err, http.StatusConflict) {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, fmt.Sprintf("docs %s: conflict (pinned version or already pinned; unpin first or accept no-op)", op)))
	}
	if client.IsHTTPStatus(err, http.StatusUnprocessableEntity) {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, fmt.Sprintf("docs %s: daemon rejected request", op)))
	}
	return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("docs %s: %w", op, err))
}

func formatDocsUnixTime(unix int64) string {
	if unix == 0 {
		return "(never)"
	}
	return time.Unix(unix, 0).UTC().Format(time.RFC3339)
}
