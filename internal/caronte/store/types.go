// SPDX-License-Identifier: MIT
package store

type Confidence string

// C-3 frozen confidence tiers (master §C-3). String values are a stored
// contract — do NOT change without a schema migration.
const (
	ConfExactStatic   Confidence = "exact_static"
	ConfExactVTA      Confidence = "exact_vta"
	ConfExactCHA      Confidence = "exact_cha"
	ConfSCIPImpl      Confidence = "scip_impl"
	ConfHeuristicName Confidence = "heuristic_name"
	ConfLLMHint       Confidence = "llm_hint"
)

func AllConfidences() []Confidence {
	return []Confidence{
		ConfExactStatic, ConfExactVTA, ConfExactCHA,
		ConfSCIPImpl, ConfHeuristicName, ConfLLMHint,
	}
}

func (c Confidence) Valid() bool {
	switch c {
	case ConfExactStatic, ConfExactVTA, ConfExactCHA,
		ConfSCIPImpl, ConfHeuristicName, ConfLLMHint:
		return true
	default:
		return false
	}
}

type NodeKind string

const (
	KindFunction NodeKind = "function"

	KindMethod NodeKind = "method"

	KindStruct NodeKind = "struct"

	KindInterface NodeKind = "interface"

	KindType NodeKind = "type"

	KindField NodeKind = "field"

	KindPackage NodeKind = "package"
)

func AllNodeKinds() []NodeKind {
	return []NodeKind{
		KindFunction, KindMethod, KindStruct, KindInterface,
		KindType, KindField, KindPackage,
	}
}

type EdgeKind string

const (
	EdgeCalls EdgeKind = "calls"

	EdgeInvoke EdgeKind = "invoke"

	EdgeImplements EdgeKind = "implements"

	EdgeEmbeds EdgeKind = "embeds"

	EdgeImports EdgeKind = "imports"

	EdgeReferences EdgeKind = "references"
)

type LinkKind string

const (
	LinkExplicitRef LinkKind = "explicit_ref"

	LinkCoverageManifest LinkKind = "coverage_manifest"

	LinkSemantic LinkKind = "semantic"
)

type TrailerKind string

const (
	TrailerConstraint TrailerKind = "constraint"

	TrailerRejected TrailerKind = "rejected"

	TrailerAgentDirective TrailerKind = "agent_directive"

	TrailerVerification TrailerKind = "verification"
)

type Node struct {
	NodeID, Name, Kind, Language, FilePath string
	StartLine, EndLine                     int
	Signature, Doc                         string
	Coreness                               int
	SCCID                                  int
	PackageID                              string
	ContentHash                            string
}

// Edge is a relation between two nodes. C-4 frozen field set; mirrors the
// graph_edges columns (spec §4.2). Reachable is a *bool: nil ⇒ NULL
// (CHA/SCIP, not pruned); &true/&false ⇒ VTA/RTA reachable-set result.
// Confidence MUST be Valid() (invariant).
type Edge struct {
	SourceID, TargetID, Kind string
	Confidence               Confidence
	Reachable                *bool
	SiteFile                 string
	SiteLine                 int
}

type CoChange struct {
	FileA, FileB             string
	SharedRevs, RevsA, RevsB int
	WindowDays               int
	UpdatedAt                int64
}

type Churn struct {
	Path                                string
	WindowDays, TouchCount, AuthorCount int
	LastTouched, UpdatedAt              int64
}

type ADRLink struct {
	ADRID, NodeID, PackageID, LinkKind string
	Confidence                         float64
	Stale                              bool
}

type LoreTrailer struct {
	CommitSHA, FilePath, NodeID, TrailerKind, Body string
	AuthoredAt                                     int64
}

// APIEndpointKind enumerates api_endpoints.kind (spec §4 + master C-3
// CHECK constraint: http|grpc|graphql|mq|ws). Typed for downstream callers
// (Phases C extractor-registry, D/E concrete extractors); persisted as the
// bare string.
//
// invariant: every api_endpoints.kind value in storage MUST be one of
// these; the schema.go CHECK clause references these literals + the
// compliance test tests/compliance/inv_hades_263_*.go asserts both halves
// (schema-side + Go-side) stay in sync.
type APIEndpointKind string

const (
	KindHTTP    APIEndpointKind = "http"
	KindGRPC    APIEndpointKind = "grpc"
	KindGraphQL APIEndpointKind = "graphql"
	KindMQ      APIEndpointKind = "mq"
	KindWS      APIEndpointKind = "ws"
)

func AllAPIEndpointKinds() []APIEndpointKind {
	return []APIEndpointKind{KindHTTP, KindGRPC, KindGraphQL, KindMQ, KindWS}
}

type APIEndpoint struct {
	EndpointID       string
	Repo             string
	Kind             string
	Method           string
	PathTemplate     string
	ProtoService     string
	ProtoRPC         string
	Topic            string
	GraphQLType      string
	GraphQLField     string
	HandlerNodeID    string
	ContractArtifact string
	ExtractedAt      int64
	ExtractorID      string
}

type APICall struct {
	CallID             string
	Repo               string
	CallerNodeID       string
	TargetMethod       string
	TargetPathTemplate string
	TargetProto        string
	TargetTopic        string
	TargetGraphQLType  string
	TargetGraphQLField string
	BaseURLRef         string

	Confidence  string
	ExtractedAt int64
	ExtractorID string
}

// BreakingChange is the CGO-agnostic value type for the release
// breaking-change event. Mirrors
// federation.BreakingChange columns exactly so federation can adapt freely
// (federation.ToStoreBreakingChange is the cgo-tagged adapter that produces
// this value from the on-disk row). coordinated/ package consumes
// this (CGO-agnostic boundary — coordinated/ MUST stay CGO-clean per the
//
// Field set: identical to federation.BreakingChange but with JSON-blob
// columns expressed as []byte (canonical-bytes preservation) instead of
// the federation row's TEXT-typed string mirrors. reads the
// JSON payloads through this byte-typed surface.
//
// Lives in this file rather than a fresh types.go (per
// drift report: this file pre-exists with the release C confidence /
// node-kind set + APIEndpoint/APICall above; FIX-3 appends
// additively so existing tests stay green).
type BreakingChange struct {
	ChangeID       string
	WorkspaceID    string
	EndpointID     string
	EndpointRepo   string
	Kind           string
	Detail         []byte
	DetectedAt     int64
	DetectorID     string
	LoreAuthor     string
	LoreCommitSHA  string
	LoreADRRefs    []byte
	LoreSupersedes []byte
}
