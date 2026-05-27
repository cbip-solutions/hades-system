// SPDX-License-Identifier: MIT
// Package aggregator — canonical value types for the knowledge aggregator.
//
// All types exported from this file are the single source of truth for:
// - JSON serialisation to the daemon HTTP API and Go client
// - SQL scan targets (PinNote ↔ knowledge_pin_index 1:1)
// - RRF fusion input/output (QueryResult, TopK)
// - Cross-project wikilink graph (WikilinkEdge)
//
// Phase ownership: D-3 ships the complete type surface. D-4..D-7 extend the
// behaviour (QueryFTS, QueryVec, Promote) but add NO new exported types to
// this file — they add methods to the Aggregator struct. D-9 may extend
// QueryRequest if audit-chain filters need additional fields; extend here,
// never in the method files.
//
// invariant: ScopePinnedOnly is the widest scope this aggregator honours.
// Ecosystem-wide RAG (cross-project web + external-source search) is release
// territory. Any call-site that passes Scope("ecosystem-rag") will receive an
// explicit error message pointing at release — never a silent default.
package aggregator

import (
	"errors"
	"time"

	"github.com/cbip-solutions/hades-system/internal/knowledge/knowledgetypes"
)

type PinNote struct {
	NoteID           string    `json:"note_id"`
	ProjectID        string    `json:"project_id"`
	Title            string    `json:"title"`
	Content          string    `json:"content"`
	FrontmatterJSON  string    `json:"frontmatter_json"`
	PromotedAt       time.Time `json:"promoted_at"`
	PromotedBy       string    `json:"promoted_by"`
	PromoteReason    string    `json:"promote_reason"`
	AuditChainAnchor string    `json:"audit_chain_anchor"`
}

type Scope string

const (
	ScopeGlobal Scope = "global"

	ScopeProject Scope = "project"

	ScopePinnedOnly Scope = "pinned-only"
)

const (
	defaultQueryLimit = 20

	maxQueryLimit = 100

	defaultWikilinkDepth = 2
)

type QueryRequest struct {
	Text string `json:"text"`

	Scope Scope `json:"scope"`

	ProjectID string `json:"project_id,omitempty"`

	Limit int `json:"limit,omitempty"`

	AuditChainFilter bool `json:"audit_chain_filter,omitempty"`

	WikilinkDepth int `json:"wikilink_depth,omitempty"`
}

func (r *QueryRequest) Validate() error {
	if r.Text == "" {
		return errors.New("aggregator: QueryRequest.Text is required")
	}
	switch r.Scope {
	case ScopeGlobal, ScopeProject, ScopePinnedOnly:

	default:
		return errors.New("aggregator: QueryRequest.Scope must be global|project|pinned-only " +
			"(ecosystem RAG is Plan 14 territory; see inv-hades-152)")
	}
	if r.Scope == ScopeProject && r.ProjectID == "" {
		return errors.New("aggregator: QueryRequest.ProjectID required when Scope=project")
	}
	if r.Limit <= 0 {
		r.Limit = defaultQueryLimit
	}
	if r.Limit > maxQueryLimit {
		r.Limit = maxQueryLimit
	}
	if r.WikilinkDepth <= 0 {
		r.WikilinkDepth = defaultWikilinkDepth
	}
	return nil
}

type QueryResult struct {
	NoteID string `json:"note_id"`

	Score float64 `json:"score"`

	Title string `json:"title"`

	Snippet string `json:"snippet,omitempty"`

	ProjectID string `json:"project_id"`

	AuditChainAnchor string `json:"audit_chain_anchor,omitempty"`

	Source string `json:"source"`
}

type WikilinkEdge struct {
	SourceNoteID string `json:"source_note_id"`
	TargetNoteID string `json:"target_note_id"`
	LinkType     string `json:"link_type"`
}

type ProjectHandle = knowledgetypes.ProjectHandle

type TopK struct {
	Source string `json:"source"`

	Results []QueryResult `json:"results"`
}
