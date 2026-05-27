// Copyright 2026 hades-system contributors. SPDX-License-Identifier: MIT

package citation

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type CitationID string

func (id CitationID) Validate() error {
	s := string(id)
	if len(s) < 4 || len(s) > 18 {
		return fmt.Errorf("citation: ID length %d out of [4..18]", len(s))
	}
	if !strings.HasPrefix(s, "c-") {
		return fmt.Errorf("citation: ID missing 'c-' prefix: %q", s)
	}
	for _, r := range s[2:] {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return fmt.Errorf("citation: ID contains invalid rune %q", r)
		}
	}
	return nil
}

type CitationType string

const (
	CitationTypeKGNode           CitationType = "kg_node"
	CitationTypeKGEdge           CitationType = "kg_edge"
	CitationTypeFileSlice        CitationType = "file_slice"
	CitationTypeCommitRef        CitationType = "commit_ref"
	CitationTypeCommunitySummary CitationType = "community_summary"
	CitationTypeAuditEvent       CitationType = "audit_event"
	CitationTypeCustom           CitationType = "custom"
)

type CitationSource string

const (
	SourceCaronteQuery CitationSource = "caronte_query"

	SourceCaronteContext CitationSource = "caronte_context"

	SourceAggregatorFTS CitationSource = "aggregator_fts"

	SourceAggregatorVec CitationSource = "aggregator_vec"

	SourceTemporal CitationSource = "temporal"

	SourceManualOverride CitationSource = "manual_override"
)

var sourceAliases = map[string]CitationSource{
	"gitnexus_query":   SourceCaronteQuery,
	"gitnexus_context": SourceCaronteContext,
}

func ParseCitationSource(wire string) (CitationSource, bool) {
	if s, ok := sourceAliases[wire]; ok {
		return s, true
	}
	switch CitationSource(wire) {
	case SourceCaronteQuery, SourceCaronteContext, SourceAggregatorFTS,
		SourceAggregatorVec, SourceTemporal, SourceManualOverride:
		return CitationSource(wire), true
	default:
		return "", false
	}
}

type RetrievalLane string

const (
	LaneSemantic RetrievalLane = "semantic"

	LaneLexical RetrievalLane = "lexical"

	LaneGraph RetrievalLane = "graph"

	LaneRerank RetrievalLane = "rerank"

	LaneTemporal RetrievalLane = "temporal"
)

type PlatformRender map[string]json.RawMessage

type PlatformRenders map[string]PlatformRender

type Envelope struct {
	ID           CitationID     `json:"id"`
	Type         CitationType   `json:"type"`
	Source       CitationSource `json:"source"`
	Lane         RetrievalLane  `json:"retrieval_lane"`
	AuditEventID string         `json:"audit_event_id"`
	Confidence   float64        `json:"confidence"`
	RRFScore     float64        `json:"rrf_score"`
	RRFRank      int            `json:"rrf_rank"`
	Expiration   time.Time      `json:"expiration,omitempty"`
	ProjectID    string         `json:"project_id"`
	Payload      string         `json:"payload"`

	PlatformRenders PlatformRenders `json:"platform_renders,omitempty"`
}

// Validate checks Envelope invariants. Returns nil iff envelope is valid
// for serialization to Tessera + rendering by any registered Renderer.
//
// Validation rejects non-finite floats (NaN/Inf) for Confidence and RRFScore
// because such values do NOT round-trip through encoding/json (Go's JSON
// encoder treats NaN/Inf as an unmarshalable value, returning an error).
// ValidateStrict (envelope.go) is the dedicated non-finite reject path with
// an explicit error message; Validate also rejects them via the dedicated
// finite-check below so callers without ValidateStrict still catch the bug.
func (e *Envelope) Validate() error {
	if err := e.ID.Validate(); err != nil {
		return fmt.Errorf("envelope.ID: %w", err)
	}
	if e.Type == "" {
		return errors.New("envelope.Type required")
	}
	if e.Source == "" {
		return errors.New("envelope.Source required")
	}
	if e.Lane == "" {
		return errors.New("envelope.Lane required")
	}
	if e.AuditEventID == "" {
		return errors.New("envelope.AuditEventID required (hades://audit/<id> deep-link)")
	}

	if isNonFinite(e.Confidence) {
		return fmt.Errorf("envelope.Confidence not finite: %v", e.Confidence)
	}
	if isNonFinite(e.RRFScore) {
		return fmt.Errorf("envelope.RRFScore not finite: %v", e.RRFScore)
	}
	if e.Confidence < 0.0 || e.Confidence > 1.0 {
		return fmt.Errorf("envelope.Confidence out of [0.0, 1.0]: %f", e.Confidence)
	}
	if e.RRFScore < 0 {
		return fmt.Errorf("envelope.RRFScore negative: %f", e.RRFScore)
	}
	if e.RRFRank < -1 {
		return fmt.Errorf("envelope.RRFRank < -1: %d (use -1 for 'not in top-K')", e.RRFRank)
	}
	if e.ProjectID == "" {
		return errors.New("envelope.ProjectID required (doctrine privacy filter)")
	}
	if e.Payload == "" {
		return errors.New("envelope.Payload required (rendering needs content)")
	}
	return nil
}

func isNonFinite(f float64) bool {

	if f != f {
		return true
	}

	if f-f != 0 {
		return true
	}
	return false
}

func (e *Envelope) AuditEventURL() string {
	return "hades://audit/" + e.AuditEventID
}

// Renderer is the substrate interface implemented by markdown_fallback.go
// and 6 platform renderers in release (Ink, Telegram, Slack,
// HTML email, voice TTS, web HTML). Render returns the platform-specific
// rendered string (markdown for fallback; whatever output format for
// platform-specific). Implementations MUST be deterministic given fixed
// (env, sess) — no time-of-day variance, no random IDs.
type Renderer interface {
	Render(env *Envelope, sess SessionContext) (string, error)

	Platform() string
}

type SessionContext struct {
	Doctrine string
	Platform string
	Now      time.Time
}

type MarkdownFallback struct {
	emitter AuditEmitter
}

// NewMarkdownFallback constructs the renderer with the audit emitter.
// emitter may be nil (tests); production callers MUST pass a real emitter.
func NewMarkdownFallback(emitter AuditEmitter) *MarkdownFallback {
	return &MarkdownFallback{emitter: emitter}
}

func (m *MarkdownFallback) Platform() string { return "markdown" }
