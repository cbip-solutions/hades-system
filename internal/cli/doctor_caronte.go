// SPDX-License-Identifier: MIT
// Package cli — doctor_caronte.go
//
// Caronte is the in-daemon, Apache-2.0 sovereign code-graph engine
// . This section probes its five health surfaces:
// - caronte.engine.healthy — engine constructed + Degraded()==false
// - caronte.index.freshness — last-index age vs index-currency threshold
// - caronte.language.coverage — which of Go/TS/Py/Rust parsers loaded
// - caronte.project-db.status — per-project.hades/caronte.db reachable
// - caronte.rerank.available — BGE reranker model installed (;
// inv-hades-278; missing → KNN-distance order)
//
// invokes the `mcp_hades-system_caronte_get_health` MCP tool via
// /v1/mcpgateway JSON-RPC tools/call and synthesizes the probe rows
// from the returned HealthReport. The legacy GET /v1/caronte/probe
// route is retired from the CLI caller surface (the daemon route
// remains registered as a fallback during the migration window).
//
// The doctor probe is project-scoped via --project (or HADES_PROJECT_ID
// env). When neither is set the daemon-default-project path is taken
// (empty alias → no X-HADES-Project-ID header sent).
//
// No license-disclosure probe: Caronte is in-house Apache-2.0; there is
// no third-party commercial-use disclosure obligation (sovereignty).
package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

func doctorCaronteCmd() *cobra.Command {
	var projectFlag string
	cmd := &cobra.Command{
		Use:   "caronte",
		Short: "caronte code-graph engine health (Plan 19; in-daemon, Apache-2.0)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			explicit := resolveCaronteProjectAlias(projectFlag)
			return runOneSection(cmd, "Caronte (Plan 19)", func(ctx context.Context, c *client.Client) []CheckResult {

				if explicit != "" {
					return runCaronteChecksWithAlias(ctx, c, explicit)
				}
				return runCaronteChecks(ctx, c)
			})
		},
	}
	cmd.Flags().StringVar(&projectFlag, "project", "", "scope health checks to one project (alias); falls back to $HADES_PROJECT_ID then cwd-auto-resolve; default = daemon-default-project")
	return cmd
}

const caronteProbeTimeout = 3 * time.Second

func resolveCaronteProjectAlias(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return os.Getenv("HADES_PROJECT_ID")
}

func resolveCaronteAliasViaCwd(ctx context.Context, c *client.Client) string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	cctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	resp, err := c.ProjectDoctor(cctx, "", cwd, false)
	if err != nil || resp == nil {
		return ""
	}
	return resp.Alias
}

func runCaronteChecks(ctx context.Context, c *client.Client) []CheckResult {
	alias := os.Getenv("HADES_PROJECT_ID")
	if alias == "" {
		alias = resolveCaronteAliasViaCwd(ctx, c)
	}
	return runCaronteChecksWithAlias(ctx, c, alias)
}

func runCaronteChecksWithAlias(ctx context.Context, c *client.Client, projectAlias string) []CheckResult {
	out := make([]CheckResult, 0, 5)
	probes := []struct {
		probeName  string
		resultName string
		hint       string
	}{
		{
			probeName:  "engine.healthy",
			resultName: "caronte.engine.healthy",
			hint:       "caronte engine degraded; check daemon logs or restart: hades daemon restart",
		},
		{
			probeName:  "index.freshness",
			resultName: "caronte.index.freshness",
			hint:       "index stale; trigger reindex: hades caronte reindex <project>",
		},
		{
			probeName:  "language.coverage",
			resultName: "caronte.language.coverage",
			hint:       "one or more language parsers (Go/TS/Py/Rust) failed to load; check daemon logs",
		},
		{
			probeName:  "project-db.status",
			resultName: "caronte.project-db.status",
			hint:       "per-project .hades/caronte.db unreachable; check project registration: hades project register <path>",
		},

		{
			probeName:  "rerank.available",
			resultName: "caronte.rerank.available",
			hint:       "BGE reranker model missing; get_why uses KNN-distance order. Install: scripts/download-bge-model.sh",
		},
	}
	for _, p := range probes {
		cctx, cancel := context.WithTimeout(ctx, caronteProbeTimeout)
		r, err := c.CaronteProbe(cctx, p.probeName, projectAlias)
		cancel()
		out = append(out, caronteResultFrom(p.resultName, r, err, p.hint))
	}
	return out
}

func caronteResultFrom(name string, r *client.CaronteProbeResp, err error, hint string) CheckResult {
	if err != nil {

		var he *client.HTTPError
		if errors.As(err, &he) && he.Status == http.StatusNotFound {
			endpointHint := hint
			if entry := ierrors.Lookup(ierrors.CodeEndpointNotFound); entry != nil {
				endpointHint = entry.RecoveryHint
			}
			return CheckResult{
				Name:   name,
				Status: "fail",
				Detail: fmt.Sprintf("daemon returned 404 (endpoint moved or deprecated): %s", err.Error()),
				Hint:   endpointHint,
			}
		}
		return CheckResult{Name: name, Status: "fail", Detail: err.Error(), Hint: hint}
	}
	res := CheckResult{Name: name, Status: r.Status, Detail: r.Detail}
	if r.Status != "ok" {
		res.Hint = hint
	}
	return res
}
