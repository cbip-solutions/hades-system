// SPDX-License-Identifier: MIT
// codegraph_daemon.go — CaronteCodeGraph adapter for the standalone research MCP.
//
// standalone research MCP through the daemon's sovereign caronte engine via
// POST /v1/mcpgateway/codegraph. Replaces the NoOpGitnexus placeholder
// (DECISION L-3: the GitnexusClient interface name is retained as the stable
// drop-in contract — do NOT rename it).
//
// Boundary: this file imports ONLY internal/mcp/client.
// The daemon is the sole binary that imports internal/caronte. The research
// MCP reaches caronte exclusively over the daemon HTTP API.
//
// Score normalisation: the REST handler's scoreToConfidence converts a [0..1]
// caronte score to an int [0..100] (scoreToConfidence in
// internal/daemon/handlers/mcpgateway_rest.go). The inverse here is
// float64(Confidence)/100 — monotonic, zero-anchored, exact for the five
// confidence tiers: exact_static→1.00, exact_vta→1.00, exact_cha→~0.75,
// scip_impl→~0.50, heuristic_name→~0.25, llm_hint→~0.10 (exact mapping
// depends on the engine's internal score; the key property is monotonicity
// preserved by both legs of the round-trip).
//
// URL scheme: caronte://<projectID>/<symbol>. The cite verifier's
// localSchemes default ({"file","caronte"}) recognises this scheme and
// skips the HEAD probe, so code-graph hit citations pass verification
// without network round-trips.
package research

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/cbip-solutions/hades-system/internal/mcp/client"
)

type daemonCodegraphDoer interface {
	Do(req *http.Request) (*http.Response, error)
	BaseURL() string
}

type codegraphRESTRequest struct {
	Query        string `json:"query"`
	ProjectAlias string `json:"project_alias,omitempty"`
	Limit        int    `json:"limit,omitempty"`
}

type codegraphRESTHit struct {
	Symbol     string `json:"symbol"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Kind       string `json:"kind,omitempty"`
	Confidence int    `json:"confidence,omitempty"`
}

type codegraphRESTResponse struct {
	Hits []codegraphRESTHit `json:"hits"`
}

type CaronteCodeGraph struct {
	doer daemonCodegraphDoer
}

var _ GitnexusClient = (*CaronteCodeGraph)(nil)

func NewCaronteCodeGraph(c *client.Client) *CaronteCodeGraph {
	return &CaronteCodeGraph{doer: c}
}

func (a *CaronteCodeGraph) CodeGraph(ctx context.Context, query, projectID string) (CodeGraphResult, error) {
	reqBody := codegraphRESTRequest{
		Query:        query,
		ProjectAlias: projectID,
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return CodeGraphResult{}, fmt.Errorf("caronte: marshal codegraph request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.doer.BaseURL()+"/v1/mcpgateway/codegraph",
		bytes.NewReader(payload))
	if err != nil {
		return CodeGraphResult{}, fmt.Errorf("caronte: build codegraph request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	}

	resp, err := a.doer.Do(httpReq)
	if err != nil {
		return CodeGraphResult{}, fmt.Errorf("caronte: codegraph POST: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return CodeGraphResult{}, fmt.Errorf("caronte: codegraph daemon returned %d: %s", resp.StatusCode, body)
	}

	var restResp codegraphRESTResponse
	if err := json.NewDecoder(resp.Body).Decode(&restResp); err != nil {
		return CodeGraphResult{}, fmt.Errorf("caronte: decode codegraph response: %w", err)
	}

	hits := make([]CodeGraphHit, 0, len(restResp.Hits))
	for _, h := range restResp.Hits {
		hits = append(hits, CodeGraphHit{
			Node:  h.Symbol,
			Score: confidenceToScore(h.Confidence),
			URL:   caronteURL(projectID, h.Symbol),
		})
	}
	return CodeGraphResult{
		Hits:      hits,
		ProjectID: projectID,
	}, nil
}

func (a *CaronteCodeGraph) Close() error { return nil }

func confidenceToScore(confidence int) float64 {
	switch {
	case confidence <= 0:
		return 0
	case confidence >= 100:
		return 1
	default:
		return float64(confidence) / 100.0
	}
}

func caronteURL(projectID, symbol string) string {
	return "caronte://" + projectID + "/" + symbol
}
