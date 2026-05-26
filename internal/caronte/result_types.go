// SPDX-License-Identifier: MIT
package caronte

import (
	"encoding/json"
	"time"
)

// IndexReport summarises a full per-project reindex pass (Plan v0.20.0 Phase C
// inv-zen-273). Surfaced by Engine.IndexProject + the daemon
// POST /v1/caronte/reindex endpoint + the `zen caronte reindex` CLI.
//
// Field-completeness contract (sister-tested at compliance level — the
// reindex MUST report every reachable file even when the parser short-
// circuits a single file; a partially failed walk surfaces via Completed
// = false + the wrapped error). Empty walks return Completed = true with
// zero counts (not an error).
//
// Idempotency a second call against an unchanged repo returns the SAME
// final NodesCreated / EdgesCreated / FilesIndexed totals (the parser
// indexer skips unchanged content_hashes via store.ContentHashFor — see
// internal/caronte/parser/indexer.go::writeNodes). The reported "Created"
// counts therefore include re-written rows (a second call against an
// unchanged tree reports the same totals as the first pass).
type IndexReport struct {
	ProjectID      string         `json:"project_id"`
	NodesCreated   int            `json:"nodes_created"`
	EdgesCreated   int            `json:"edges_created"`
	FilesIndexed   int            `json:"files_indexed"`
	LanguageCounts map[string]int `json:"language_counts"`
	DurationMillis int64          `json:"duration_ms"`
	StartedAt      time.Time      `json:"started_at"`
	Completed      bool           `json:"completed"`
}

type ContextResult struct {
	Symbol    string
	Callers   []string
	Callees   []string
	Neighbors []string
	Community string
	Coreness  int
	SCCID     int
	Cyclic    bool
}

type HealthReport struct {
	ProjectID    string
	NodeCount    int
	EdgeCount    int
	PackageCount int
	CyclicSCCs   int
	Languages    []string
	Degraded     bool
	ResolveMode  string
	LastIndexed  int64
}

type ArchitectureReport struct {
	Packages []PackageNode
	Cycles   []SCCGroup
}

type PackageNode struct {
	PackageID string
	NodeCount int
	Coreness  int
}

type SCCGroup struct {
	SCCID   int
	Members []string
}

type CoChangePeer struct {
	Path            string
	CouplingPercent float64
	SharedRevs      int
	WindowDays      int
}

type WikiDoc struct {
	Module   string
	Markdown string
}

type ContractPayload struct {
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

type ConsumerLink struct {
	CallID     string `json:"call_id"`
	Repo       string `json:"repo"`
	CallerFile string `json:"caller_file,omitempty"`
	CallerLine int    `json:"caller_line,omitempty"`
	Confidence string `json:"confidence"`
	LinkMethod string `json:"link_method"`
}

type ConsumerList struct {
	EndpointID   string         `json:"endpoint_id"`
	EndpointRepo string         `json:"endpoint_repo"`
	WorkspaceID  string         `json:"workspace_id"`
	Consumers    []ConsumerLink `json:"consumers"`
}

type LoreAttributionPayload struct {
	Author     string   `json:"author,omitempty"`
	CommitSHA  string   `json:"commit_sha,omitempty"`
	ADRRefs    []string `json:"adr_refs,omitempty"`
	Supersedes []string `json:"supersedes,omitempty"`
}

type BreakingChangePayload struct {
	ChangeID     string                 `json:"change_id"`
	WorkspaceID  string                 `json:"workspace_id"`
	EndpointID   string                 `json:"endpoint_id"`
	EndpointRepo string                 `json:"endpoint_repo"`
	Kind         string                 `json:"kind"`
	Detail       json.RawMessage        `json:"detail,omitempty"`
	DetectedAt   int64                  `json:"detected_at"`
	DetectorID   string                 `json:"detector_id"`
	Consumers    []ConsumerLink         `json:"consumers"`
	Lore         LoreAttributionPayload `json:"lore"`
}

type APICallTrace struct {
	CallID       string                 `json:"call_id"`
	CallRepo     string                 `json:"call_repo"`
	WorkspaceID  string                 `json:"workspace_id"`
	EndpointID   string                 `json:"endpoint_id,omitempty"`
	EndpointRepo string                 `json:"endpoint_repo,omitempty"`
	Confidence   string                 `json:"confidence,omitempty"`
	LinkMethod   string                 `json:"link_method,omitempty"`
	Unresolved   bool                   `json:"unresolved"`
	Lore         LoreAttributionPayload `json:"lore"`
}

type WorkspaceSnapshot struct {
	WorkspaceID   string   `json:"workspace_id"`
	OwningProject string   `json:"owning_project"`
	Members       []string `json:"members"`
	PolicyLocked  bool     `json:"policy_locked"`
	CreatedAt     int64    `json:"created_at"`
	SchemaVersion int      `json:"schema_version"`
}

type FederationHealthReport struct {
	WorkspaceID               string  `json:"workspace_id"`
	Reachable                 bool    `json:"reachable"`
	GateLatencyP95Ms          float64 `json:"gate_latency_p95_ms"`
	IndexingCurrencyMaxAgeSec int64   `json:"indexing_currency_max_age_sec"`
	UnresolvedCount           int     `json:"unresolved_count"`
	ContractLinksCount        int     `json:"contract_links_count"`
	BreakingChangesOpenCount  int     `json:"breaking_changes_open_count"`
	LastAuditChainTip         string  `json:"last_audit_chain_tip,omitempty"`
}

type ContractDiff struct {
	EndpointID   string          `json:"endpoint_id"`
	EndpointRepo string          `json:"endpoint_repo"`
	SinceUnix    int64           `json:"since_unix"`
	HeadUnix     int64           `json:"head_unix"`
	DetectorID   string          `json:"detector_id"`
	Severity     string          `json:"severity"`
	Kind         string          `json:"kind"`
	Detail       json.RawMessage `json:"detail,omitempty"`
}

type WhyBreakingChange struct {
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
