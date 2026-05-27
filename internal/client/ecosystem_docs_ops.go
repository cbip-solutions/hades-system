// SPDX-License-Identifier: MIT
// Package client — ecosystem_docs_ops.go (release Task F-6 +
//
// Typed client methods for the ecosystem docs management endpoints:
//
// F-6 surface:
// POST /v1/knowledge/ecosystem/reindex — DocsReindex
// GET /v1/knowledge/ecosystem/status — DocsStatus
// GET /v1/knowledge/ecosystem/sources — DocsSources
// POST /v1/knowledge/ecosystem/router/retrain — DocsRouterRetrain
//
// G-5 surface (operator-confirmed retention; spec §2.9 Q9=A):
// POST /v1/ecosystem/pin — EcosystemPin
// GET /v1/ecosystem/prune-preview — EcosystemPrunePreview
// DELETE /v1/ecosystem/version — EcosystemPrune
//
// G-5 SUPERSEDES F-6 pin/prune: F-6 shipped chunk-id-based pin + dry-run /
// confirm prune (POST /v1/knowledge/ecosystem/pin|prune). G-5 evolves to
// version-level pin (sets ecosystem_versions.indefinite_retain=true) +
// retention-aware prune (refuses pinned versions; preview before commit).
// The /v1/knowledge/... pin+prune paths are retired; daemon-side handlers
// land under /v1/ecosystem/... in a later task.
//
// These power `hades docs reindex / pin / prune / status / sources /
// router-retrain` (internal/cli/docs_*.go). The daemon-side handlers land
// in — until then the endpoints surface as 503 and the CLI layer
// classifies that as unrecoverable (exit 2).
//
// Boundary stdlib only. No internal/research/ecosystem import (invariant);
// the daemon-side handler is responsible for invoking the trainer (Q15-Q20
// orchestrator-enforced — see ADR-0067) and for ingester/pruner orchestration.
//
// Wire shapes are the single source of truth for handler authors —
// any drift between this file and the daemon side breaks the contract.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type DocsReindexRequest struct {
	Ecosystem string `json:"ecosystem,omitempty"`
	Version   string `json:"version,omitempty"`
	DeltaOnly bool   `json:"delta_only"`
}

type DocsReindexResponse struct {
	PackagesIngested   int   `json:"packages_ingested"`
	ChunksIngested     int   `json:"chunks_ingested"`
	SymbolsRegistered  int   `json:"symbols_registered"`
	ChangeNodesCreated int   `json:"change_nodes_created"`
	ElapsedMs          int64 `json:"elapsed_ms"`
}

type EcosystemPinRequest struct {
	Ecosystem string `json:"ecosystem"`
	Version   string `json:"version"`
}

type EcosystemPruneRequest struct {
	Ecosystem string `json:"ecosystem"`
	Version   string `json:"version"`
}

type EcosystemPrunePreview struct {
	Ecosystem      string `json:"ecosystem"`
	Version        string `json:"version"`
	ChunkCount     int    `json:"chunk_count"`
	ChunkFP32Count int    `json:"chunk_fp32_count"`
	SymbolCount    int    `json:"symbol_count"`
	ChangeCount    int    `json:"change_count"`
	FTS5Count      int    `json:"fts5_count"`
	Pinned         bool   `json:"pinned"`
}

type DocsStatusResponse struct {
	Ecosystems []EcosystemStatus `json:"ecosystems"`
}

type EcosystemStatus struct {
	Ecosystem     string `json:"ecosystem"`
	ChunkCount    int    `json:"chunk_count"`
	SymbolCount   int    `json:"symbol_count"`
	StorageBytes  int64  `json:"storage_bytes"`
	LastPolled    int64  `json:"last_polled_unix"`
	LastIndexed   int64  `json:"last_indexed_unix"`
	RetentionDays int    `json:"retention_days"`
}

type DocsSourcesResponse struct {
	Sources []SourceStatus `json:"sources"`
}

type SourceStatus struct {
	Name        string `json:"name"`
	Ecosystem   string `json:"ecosystem"`
	SourceType  string `json:"source_type"`
	URL         string `json:"url"`
	TTLHours    int    `json:"ttl_hours"`
	LastIndexed int64  `json:"last_indexed_unix"`
	Status      string `json:"status"`
}

type RouterRetrainResponse struct {
	CheckpointPath string  `json:"checkpoint_path"`
	Accuracy       float64 `json:"accuracy"`
	ElapsedMs      int64   `json:"elapsed_ms"`
}

func (c *Client) DocsReindex(ctx context.Context, req DocsReindexRequest) (*DocsReindexResponse, error) {
	var resp DocsReindexResponse
	if err := c.postJSON(ctx, "/v1/knowledge/ecosystem/reindex", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) EcosystemPin(ctx context.Context, ecosystem, version string) error {
	if ecosystem == "" || version == "" {
		return fmt.Errorf("client: EcosystemPin requires non-empty ecosystem and version")
	}
	return c.postJSON(ctx, "/v1/ecosystem/pin", EcosystemPinRequest{
		Ecosystem: ecosystem,
		Version:   version,
	}, nil)
}

func (c *Client) EcosystemPrunePreview(ctx context.Context, ecosystem, version string) (*EcosystemPrunePreview, error) {
	if ecosystem == "" || version == "" {
		return nil, fmt.Errorf("client: EcosystemPrunePreview requires non-empty ecosystem and version")
	}
	q := url.Values{}
	q.Set("ecosystem", ecosystem)
	q.Set("version", version)
	var resp EcosystemPrunePreview
	if err := c.getJSON(ctx, "/v1/ecosystem/prune-preview?"+q.Encode(), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// EcosystemPrune calls DELETE /v1/ecosystem/version (G-5; spec §2.9 Q9=A).
//
// Hard-removes the (ecosystem, version) row and cascades to chunks,
// chunks_fp32, symbols, changes, and FTS5 entries. Pinned versions are
// refused with 409 Conflict; the operator must `hades docs unpin` first.
//
// CLI safety: the docs_prune.go RunE path enforces a promptYN gate before
// dialing; this method is the unguarded transport. Callers MUST NOT bypass
// the gate.
//
// SUPERSEDES F-6 POST /v1/knowledge/ecosystem/prune (retired DryRun-based
// flow). The release G-5 contract splits preview (GET prune-preview) from
// commit (DELETE version) so the operator-confirmation gate is naturally
// staged: preview → prompt → commit.
func (c *Client) EcosystemPrune(ctx context.Context, ecosystem, version string) error {
	if ecosystem == "" || version == "" {
		return fmt.Errorf("client: EcosystemPrune requires non-empty ecosystem and version")
	}
	return c.deleteJSON(ctx, "/v1/ecosystem/version", EcosystemPruneRequest{
		Ecosystem: ecosystem,
		Version:   version,
	})
}

func (c *Client) deleteJSON(ctx context.Context, path string, body any) error {
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = readerOf(buf)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.urlFor(path), rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpC.Do(req)
	if err != nil {
		return fmt.Errorf("DELETE %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		he := &HTTPError{Method: http.MethodDelete, Path: path, Status: resp.StatusCode, RawBody: bodyBytes}
		return fmt.Errorf("DELETE %s: %d %s: %w", path, resp.StatusCode, string(bodyBytes), he)
	}
	return nil
}

func (c *Client) DocsStatus(ctx context.Context) (*DocsStatusResponse, error) {
	var resp DocsStatusResponse
	if err := c.getJSON(ctx, "/v1/knowledge/ecosystem/status", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) DocsSources(ctx context.Context) (*DocsSourcesResponse, error) {
	var resp DocsSourcesResponse
	if err := c.getJSON(ctx, "/v1/knowledge/ecosystem/sources", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) DocsRouterRetrain(ctx context.Context) (*RouterRetrainResponse, error) {
	var resp RouterRetrainResponse
	if err := c.postJSON(ctx, "/v1/knowledge/ecosystem/router/retrain", struct{}{}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
