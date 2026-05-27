// SPDX-License-Identifier: MIT
// Package client — contract.go.
//
// Thin pass-throughs for the daemon's release caronte REST sub-routes:
// /v1/mcpgateway/contract{,/validate,/why}. Contract reads proxy through the
// native Caronte engine; manifest validation uses the daemon-wired federation
// validator. CLI is operator-side; LLM traffic is not involved (these are
// structural queries, not generation).
//
// inv-hades-088 single-egress preserved: every round-trip proxies through the
// daemon. inv-hades-129 enforced: this file uses ONLY c.postJSON — never
// net/http directly.
package client

import "context"

type ContractRequest struct {
	Endpoint    string `json:"endpoint"`
	WorkspaceID string `json:"workspace_id,omitempty"`
}

type ContractResponse struct {
	EndpointID       string `json:"endpoint_id"`
	Repo             string `json:"repo"`
	Kind             string `json:"kind"`
	Method           string `json:"method,omitempty"`
	PathTemplate     string `json:"path_template,omitempty"`
	ProtoService     string `json:"proto_service,omitempty"`
	ProtoRPC         string `json:"proto_rpc,omitempty"`
	Topic            string `json:"topic,omitempty"`
	GraphQLType      string `json:"graphql_type,omitempty"`
	GraphQLField     string `json:"graphql_field,omitempty"`
	HandlerNodeID    string `json:"handler_node_id"`
	ContractArtifact string `json:"contract_artifact,omitempty"`
	ExtractedAt      int64  `json:"extracted_at"`
	ExtractorID      string `json:"extractor_id"`
}

func (c *Client) Contract(ctx context.Context, req ContractRequest) (*ContractResponse, error) {
	var resp ContractResponse
	if err := c.postJSON(ctx, "/v1/mcpgateway/contract", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type ContractValidateRequest struct {
	Repo        string `json:"repo"`
	WorkspaceID string `json:"workspace_id,omitempty"`
}

type ContractValidateService struct {
	BaseURLRef string `json:"base_url_ref"`
	TargetRepo string `json:"target_repo"`
}

type ContractValidateError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

type ContractValidateResponse struct {
	Valid         bool                      `json:"valid"`
	SchemaVersion int                       `json:"schema_version"`
	Services      []ContractValidateService `json:"services,omitempty"`
	Errors        []ContractValidateError   `json:"errors,omitempty"`
}

func (c *Client) ContractValidate(ctx context.Context, req ContractValidateRequest) (*ContractValidateResponse, error) {
	var resp ContractValidateResponse
	if err := c.postJSON(ctx, "/v1/mcpgateway/contract/validate", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type ContractWhyRequest struct {
	ChangeID string `json:"change_id"`
}

type ContractWhyResponse struct {
	ChangeID          string   `json:"change_id"`
	WorkspaceID       string   `json:"workspace_id"`
	EndpointID        string   `json:"endpoint_id"`
	EndpointRepo      string   `json:"endpoint_repo"`
	LoreAuthor        string   `json:"lore_author,omitempty"`
	LoreCommitSHA     string   `json:"lore_commit_sha,omitempty"`
	LoreADRRefs       []string `json:"lore_adr_refs,omitempty"`
	LoreSupersedes    []string `json:"lore_supersedes,omitempty"`
	CommitSubject     string   `json:"commit_subject,omitempty"`
	CommitBodyExcerpt string   `json:"commit_body_excerpt,omitempty"`
	DetectedAt        int64    `json:"detected_at"`
}

func (c *Client) ContractWhy(ctx context.Context, req ContractWhyRequest) (*ContractWhyResponse, error) {
	var resp ContractWhyResponse
	if err := c.postJSON(ctx, "/v1/mcpgateway/contract/why", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
