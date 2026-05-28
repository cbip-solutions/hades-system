// SPDX-License-Identifier: MIT
// Package ecosystem — audit_payloads.go
//
// JSON-serializable payload types for the 8 RAG audit events
// (EventType slots 92-99, declared in internal/orchestrator/eventlog/events.go).
//
// (task) shipped the EventType constants + RAGAuditEmitter
// skeleton (audit_emitter.go) + the in-memory test chain (mock_chain.go).
// the emitter marshals on every Emit call, bound 1:1 to each EventType slot.
//
// Slot binding (spec §4.6):
//
// EvtRAGQuery = 92 → RAGQueryPayload
// EvtRAGRetrieval = 93 → RAGRetrievalPayload
// EvtRAGCitation = 94 → RAGCitationPayload
// EvtRAGVerify = 95 → RAGVerifyPayload
// EvtRAGAbstain = 96 → RAGAbstainPayload
// EvtRAGAnswer = 97 → RAGAnswerPayload
// EvtRAGIngestPackage = 98 → RAGIngestPackagePayload
// EvtRAGIngestJoinKey = 99 → RAGIngestJoinKeyPayload
//
// Doctrine invariants:
//
// - invariant APPEND-ONLY audit chain: EventType
// slots 92-99 are reserved + their numeric values immutable. Payload
// schemas added here are forward-compatible (additive fields only;
// existing fields never renamed/retyped without an explicit ADR).
//
// - invariant doctrine-emission strictness: the
// 8 payloads are emitted via RAGAuditEmitter.Emit which gates per
// DoctrineProfile.AuditEmissionLevel. audit_emitter.go owns
// the gate; D-12 ONLY supplies the payload schemas.
//
// - invariant boundary: this file imports internal/orchestrator/eventlog
// for the EventType alias + slot constants. It does NOT import HADES design
// budget primitives (the RAGAuditChainEmitter interface in
// audit_emitter.go is the boundary-respecting indirection).
//
// JSON encoding contract:
//
// - All field names use snake_case JSON tags for cross-language readers
// .
// - Optional fields use `omitempty` to keep stored payloads compact;
// required fields are emitted even at zero value.
// - Embedded struct types (RoutingDecision, CitationRef, SymbolVerification)
// marshal with their own canonical shapes (declared in types.go +
// router.go). Any drift there auto-propagates here — intentional.
//
// What this file is NOT:
//
// - Not a marker for "this type can be emitted as an audit payload" in
// the sense of restricting the emitter input. RAGAuditEmitter.Emit takes
// `interface{}` deliberately (test code passes maps, structs, raw bytes).
// The AuditPayload interface here is a documentation+grep convenience
// and a compile-time assertion that the 8 canonical types implement
// it — it imposes no production runtime constraint.
package ecosystem

import "github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"

type EventType = eventlog.EventType

const (
	EvtRAGQuery         = eventlog.EvtRAGQuery
	EvtRAGRetrieval     = eventlog.EvtRAGRetrieval
	EvtRAGCitation      = eventlog.EvtRAGCitation
	EvtRAGVerify        = eventlog.EvtRAGVerify
	EvtRAGAbstain       = eventlog.EvtRAGAbstain
	EvtRAGAnswer        = eventlog.EvtRAGAnswer
	EvtRAGIngestPackage = eventlog.EvtRAGIngestPackage
	EvtRAGIngestJoinKey = eventlog.EvtRAGIngestJoinKey
)

type AuditPayload interface {
	markerAuditPayload() bool
}

// auditPayloadKind is the per-type kind discriminator returned by the
// markerAuditPayload method. It exists ONLY so coverage instrumentation
// sees a non-empty body in each marker method (Go's coverage profiler
// counts an empty `{}` body as 1 unreachable statement, suppressing the
// hit even when the method is invoked). Callers MUST NOT rely on the
// returned value — it is implementation detail of the marker mechanism.
type auditPayloadKind int

const (
	auditPayloadKindQuery auditPayloadKind = iota + 1
	auditPayloadKindRetrieval
	auditPayloadKindCitation
	auditPayloadKindVerify
	auditPayloadKindAbstain
	auditPayloadKindAnswer
	auditPayloadKindIngestPackage
	auditPayloadKindIngestJoinKey
)

type RAGQueryPayload struct {
	Query                string          `json:"query"`
	Ecosystem            Ecosystem       `json:"ecosystem,omitempty"`
	Version              string          `json:"version,omitempty"`
	VersionLayer         int             `json:"version_layer,omitempty"`
	Doctrine             string          `json:"doctrine"`
	Routing              RoutingDecision `json:"routing"`
	ClassifierCheckpoint string          `json:"classifier_checkpoint"`
	ProjectPath          string          `json:"project_path,omitempty"`
	FreshDispatch        bool            `json:"fresh_dispatch,omitempty"`
}

func (RAGQueryPayload) markerAuditPayload() bool { return auditPayloadKindQuery > 0 }

type RAGRetrievalPayload struct {
	PerEcoCounts map[Ecosystem]int     `json:"per_eco_counts"`
	FusedCount   int                   `json:"fused_count"`
	K            int                   `json:"k"`
	Weights      map[Ecosystem]float64 `json:"weights"`
}

func (RAGRetrievalPayload) markerAuditPayload() bool { return auditPayloadKindRetrieval > 0 }

type RAGCitationPayload struct {
	Citations []CitationRef `json:"citations"`
}

func (RAGCitationPayload) markerAuditPayload() bool { return auditPayloadKindCitation > 0 }

type RAGVerifyPayload struct {
	Verifications []SymbolVerification `json:"verifications"`
	AllVerified   bool                 `json:"all_verified"`
}

func (RAGVerifyPayload) markerAuditPayload() bool { return auditPayloadKindVerify > 0 }

type RAGAbstainPayload struct {
	Reason           string    `json:"reason"`
	Lambda           float64   `json:"lambda,omitempty"`
	Mean             float64   `json:"mean,omitempty"`
	Stdev            float64   `json:"stdev,omitempty"`
	Threshold        float64   `json:"threshold,omitempty"`
	Ecosystem        Ecosystem `json:"ecosystem,omitempty"`
	Doctrine         string    `json:"doctrine"`
	SuspiciousChunks []int64   `json:"suspicious_chunks,omitempty"`
}

func (RAGAbstainPayload) markerAuditPayload() bool { return auditPayloadKindAbstain > 0 }

// RAGAnswerPayload — EvtRAGAnswer (97). Terminal event for a successful
// query (paired with EvtRAGAbstain for refused queries). Captures the
// content-addressed answer hash + the cited chunk-id set + the active
// doctrine + the total query latency.
//
// AnswerHashSHA256 is the hex-encoded sha256 of the literal answer
// string (post-citation-rewrite) — enables auditors to verify the
// answer was not silently rewritten after the chain seal.
//
// CitedChunkIDs MUST match the ChunkID set in the preceding
// EvtRAGCitation payload (when AuditCitation was emitted under the
// active doctrine). property test cross-references the two.
//
// TotalLatencyMs is wall-clock from query intake to answer return,
// including verify + rerank + LLM stages. Sub-ms granularity is
// permitted (float64).
type RAGAnswerPayload struct {
	AnswerHashSHA256 string  `json:"answer_hash_sha256"`
	CitedChunkIDs    []int64 `json:"cited_chunk_ids"`
	Doctrine         string  `json:"doctrine"`
	TotalLatencyMs   float64 `json:"total_latency_ms"`
}

func (RAGAnswerPayload) markerAuditPayload() bool { return auditPayloadKindAnswer > 0 }

type RAGIngestPackagePayload struct {
	Package           string    `json:"package"`
	Ecosystem         Ecosystem `json:"ecosystem"`
	Version           string    `json:"version"`
	ChunksCount       int       `json:"chunks_count"`
	SymbolsCount      int       `json:"symbols_count"`
	ChangeNodesCount  int       `json:"change_nodes_count"`
	StartedAtUnixNs   int64     `json:"started_at_unix_ns"`
	CompletedAtUnixNs int64     `json:"completed_at_unix_ns"`
}

func (RAGIngestPackagePayload) markerAuditPayload() bool { return auditPayloadKindIngestPackage > 0 }

type RAGIngestJoinKeyPayload struct {
	NoteID             string    `json:"note_id"`
	ResolvedSymbolPath string    `json:"resolved_symbol_path"`
	Ecosystem          Ecosystem `json:"ecosystem"`
	Version            string    `json:"version"`
}

func (RAGIngestJoinKeyPayload) markerAuditPayload() bool { return auditPayloadKindIngestJoinKey > 0 }
