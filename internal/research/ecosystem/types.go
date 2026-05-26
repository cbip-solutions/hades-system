// SPDX-License-Identifier: MIT
// internal/research/ecosystem/types.go

package ecosystem

import (
	"database/sql"
	"time"
)

type Ecosystem string

const (
	EcoGo Ecosystem = "go"

	EcoPython Ecosystem = "python"

	EcoTypeScript Ecosystem = "typescript"

	EcoRust Ecosystem = "rust"
)

var AllEcosystems = []Ecosystem{EcoGo, EcoPython, EcoTypeScript, EcoRust}

type QueryScope string

const (
	ScopeDocs QueryScope = "docs"

	ScopeSymbols QueryScope = "symbols"

	ScopeExamples QueryScope = "examples"

	ScopeAll QueryScope = "all"
)

type ChunkKind string

const (
	KindFunction ChunkKind = "function"

	KindType ChunkKind = "type"

	KindModule ChunkKind = "module"

	KindGuide ChunkKind = "guide"

	KindProse ChunkKind = "prose"
)

type SourceType string

const (
	SrcPackageDoc SourceType = "package_doc"

	SrcMDN SourceType = "mdn"

	SrcArXiv SourceType = "arxiv"

	SrcGitHub SourceType = "github"
)

type ChangeType string

const (
	ChangeAdded ChangeType = "added"

	ChangeRemoved ChangeType = "removed"

	ChangeChanged ChangeType = "changed"

	ChangeDeprecated ChangeType = "deprecated"

	ChangeMoved ChangeType = "moved"
)

// PackageRef identifies a package in any ecosystem.db. ID is the surrogate
// SQLite primary key; Name + Ecosystem form the UNIQUE business key per
// schema migration 001_ecosystem_packages.sql.
//
// Field set frozen at master §3.1; downstream Phases B/C/D/E/F MUST NOT
// change without amendment.
type PackageRef struct {
	ID                  int64
	Ecosystem           Ecosystem
	Name                string
	CanonicalNamespace  string
	UpstreamURL         string
	LatestStableVersion string
}

type Chunk struct {
	ID                  int64
	PackageID           int64
	VersionIntroduced   string
	VersionDeprecated   sql.NullString
	StableIn            []string
	ContentText         string
	ContextualPrefix    string
	Fingerprint         string
	ParentChunkID       sql.NullInt64
	SourceType          SourceType
	SymbolPath          string
	Kind                ChunkKind
	SourceURL           string
	EmbeddingBin256d    []byte
	EmbeddingFP32_1536d []float32
	Oversized           bool
}

type ChangeNode struct {
	ID              int64
	PackageID       int64
	VersionFrom     string
	VersionTo       string
	ChangeType      ChangeType
	SymbolPath      string
	Description     string
	SourceExtracted string
}

type SymbolRef struct {
	Ecosystem  Ecosystem
	SymbolPath string
	Version    string
}

type SymbolVerification struct {
	Symbol    SymbolRef
	Exists    bool
	Source    string
	Latency   time.Duration
	Signature string
}

type QueryProvenance struct {
	DetectedVersion   string
	DetectionLayer    int
	RoutingEcosystems []Ecosystem
	RoutingMethod     string
	FreshDispatch     bool
	DoctrineApplied   string
	RerankerModel     string
	EmbedderModel     string
	LatencyBreakdown  map[string]float64
}

type CitationRef struct {
	ID         string
	ChunkID    int64
	SymbolPath string
	SourceURL  string
}

type QueryChunk struct {
	ChunkID            int64
	PackageID          int64
	PackageName        string
	SymbolPath         string
	Kind               ChunkKind
	Version            string
	ContentText        string
	ContextualPrefix   string
	SourceURL          string
	SimilarityScore    float64
	RerankerScore      float64
	CitationID         string
	VerificationStatus string
}

type QueryResult struct {
	Chunks        []QueryChunk
	Citations     []CitationRef
	Verified      []SymbolVerification
	Abstained     bool
	AbstainReason string
	Provenance    QueryProvenance
	AuditChainSeq int64
}

type IngestRequest struct {
	Ecosystem Ecosystem
	Version   string
	Sources   []SourceType
	DeltaOnly bool
}

type IngestResult struct {
	PackagesIngested   int
	PackagesFailed     int
	ChunksIngested     int
	SymbolsRegistered  int
	ChangeNodesCreated int
	StartedAt          time.Time
	CompletedAt        time.Time
	AuditChainSeqStart int64
	AuditChainSeqEnd   int64
}

type QueryRequest struct {
	Query       string
	Ecosystem   Ecosystem
	Version     string
	Scope       QueryScope
	MaxResults  int
	Doctrine    string
	Strict      bool
	ProjectPath string
}

type VerifyResult struct {
	Verifications []SymbolVerification
	AllVerified   bool
}
