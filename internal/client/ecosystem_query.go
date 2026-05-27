// SPDX-License-Identifier: MIT
// Package client — ecosystem_query.go.
//
// Typed client method for POST /v1/knowledge/ecosystem/query (daemon-side
// handler wired by or daemon integration). Wire types mirror the
// daemon handler; no internal/research/ecosystem import.
//
// Endpoint POST /v1/knowledge/ecosystem/query
// Auth same UDS transport as other client methods.
//
// Co-authored seam: F-3 (knowledge_remote.go) and F-4 (memory_query.go) both
// need these types + method. Whichever task lands first creates the file;
// the other rebases without changes. The plan-file (lines 547-622) is the
// single source of truth for the wire shape.
package client

import "context"

type EcosystemQueryRequest struct {
	Query      string `json:"query"`
	Ecosystem  string `json:"ecosystem,omitempty"`
	Version    string `json:"version,omitempty"`
	Scope      string `json:"scope,omitempty"`
	MaxResults int    `json:"max_results,omitempty"`
	Doctrine   string `json:"doctrine,omitempty"`
	Strict     bool   `json:"strict,omitempty"`
}

type EcosystemQueryResponse struct {
	Chunks        []EcosystemChunk    `json:"chunks"`
	Citations     []EcosystemCitation `json:"citations,omitempty"`
	Abstained     bool                `json:"abstained"`
	AbstainReason string              `json:"abstain_reason,omitempty"`
	Provenance    EcosystemProvenance `json:"provenance"`
}

type EcosystemChunk struct {
	PackageName        string  `json:"package_name"`
	SymbolPath         string  `json:"symbol_path"`
	Kind               string  `json:"kind"`
	Version            string  `json:"version"`
	ContentText        string  `json:"content_text"`
	ContextualPrefix   string  `json:"contextual_prefix,omitempty"`
	SourceURL          string  `json:"source_url"`
	SimilarityScore    float64 `json:"similarity_score"`
	RerankerScore      float64 `json:"reranker_score"`
	CitationID         string  `json:"citation_id,omitempty"`
	VerificationStatus string  `json:"verification_status,omitempty"`
}

type EcosystemCitation struct {
	ID         string `json:"id"`
	SymbolPath string `json:"symbol_path,omitempty"`
	SourceURL  string `json:"source_url"`
}

type EcosystemProvenance struct {
	DetectedVersion   string   `json:"detected_version,omitempty"`
	DetectionLayer    int      `json:"detection_layer"`
	RoutingEcosystems []string `json:"routing_ecosystems,omitempty"`
	RoutingMethod     string   `json:"routing_method"`
	FreshDispatch     bool     `json:"fresh_dispatch"`
	DoctrineApplied   string   `json:"doctrine_applied"`
	RerankerModel     string   `json:"reranker_model,omitempty"`
	EmbedderModel     string   `json:"embedder_model,omitempty"`
}

func (c *Client) EcosystemQuery(ctx context.Context, req EcosystemQueryRequest) (*EcosystemQueryResponse, error) {
	var resp EcosystemQueryResponse
	if err := c.postJSON(ctx, "/v1/knowledge/ecosystem/query", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
