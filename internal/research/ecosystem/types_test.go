package ecosystem

import (
	"database/sql"
	"testing"
	"time"
)

func TestEcosystemEnumValues(t *testing.T) {
	cases := []struct {
		got  Ecosystem
		want string
	}{
		{EcoGo, "go"},
		{EcoPython, "python"},
		{EcoTypeScript, "typescript"},
		{EcoRust, "rust"},
	}
	for _, c := range cases {
		if string(c.got) != c.want {
			t.Errorf("Ecosystem %v = %q; want %q", c.got, string(c.got), c.want)
		}
	}

	want := []Ecosystem{EcoGo, EcoPython, EcoTypeScript, EcoRust}
	if len(AllEcosystems) != len(want) {
		t.Fatalf("len(AllEcosystems) = %d; want %d", len(AllEcosystems), len(want))
	}
	for i, eco := range AllEcosystems {
		if eco != want[i] {
			t.Errorf("AllEcosystems[%d] = %q; want %q (load-bearing per spec §2.7 Q7=A doctrine λ-default table + Phase D router fan-out iteration)",
				i, eco, want[i])
		}
	}
}

func TestQueryScopeEnumValues(t *testing.T) {
	cases := []struct {
		got  QueryScope
		want string
	}{
		{ScopeDocs, "docs"},
		{ScopeSymbols, "symbols"},
		{ScopeExamples, "examples"},
		{ScopeAll, "all"},
	}
	for _, c := range cases {
		if string(c.got) != c.want {
			t.Errorf("QueryScope %v = %q; want %q", c.got, string(c.got), c.want)
		}
	}
}

func TestChunkKindEnumValues(t *testing.T) {
	cases := []struct {
		got  ChunkKind
		want string
	}{
		{KindFunction, "function"},
		{KindType, "type"},
		{KindModule, "module"},
		{KindGuide, "guide"},
		{KindProse, "prose"},
	}
	for _, c := range cases {
		if string(c.got) != c.want {
			t.Errorf("ChunkKind %v = %q; want %q", c.got, string(c.got), c.want)
		}
	}
}

func TestSourceTypeEnumValues(t *testing.T) {
	cases := []struct {
		got  SourceType
		want string
	}{
		{SrcPackageDoc, "package_doc"},
		{SrcMDN, "mdn"},
		{SrcArXiv, "arxiv"},
		{SrcGitHub, "github"},
	}
	for _, c := range cases {
		if string(c.got) != c.want {
			t.Errorf("SourceType %v = %q; want %q", c.got, string(c.got), c.want)
		}
	}
}

func TestChangeTypeEnumValues(t *testing.T) {
	cases := []struct {
		got  ChangeType
		want string
	}{
		{ChangeAdded, "added"},
		{ChangeRemoved, "removed"},
		{ChangeChanged, "changed"},
		{ChangeDeprecated, "deprecated"},
		{ChangeMoved, "moved"},
	}
	for _, c := range cases {
		if string(c.got) != c.want {
			t.Errorf("ChangeType %v = %q; want %q", c.got, string(c.got), c.want)
		}
	}
}

func TestPackageRefFields(t *testing.T) {
	p := PackageRef{
		ID:                  42,
		Ecosystem:           EcoGo,
		Name:                "crypto/sha256",
		CanonicalNamespace:  "crypto/sha256",
		UpstreamURL:         "https://pkg.go.dev/crypto/sha256",
		LatestStableVersion: "1.22.3",
	}
	if p.ID != 42 || p.Ecosystem != EcoGo {
		t.Errorf("PackageRef field-set mismatch (master §3.1 freeze): %+v", p)
	}
	if p.Name != "crypto/sha256" || p.CanonicalNamespace != "crypto/sha256" {
		t.Errorf("PackageRef Name/CanonicalNamespace not preserved: %+v", p)
	}
	if p.UpstreamURL == "" || p.LatestStableVersion != "1.22.3" {
		t.Errorf("PackageRef UpstreamURL/LatestStableVersion not preserved: %+v", p)
	}
}

func TestChunkFields(t *testing.T) {
	emb := make([]float32, 1536)
	c := Chunk{
		ID:                  1,
		PackageID:           2,
		VersionIntroduced:   "1.22.0",
		VersionDeprecated:   sql.NullString{String: "1.24.0", Valid: true},
		StableIn:            []string{"1.22.0", "1.22.1", "1.22.2"},
		ContentText:         "func Sum256(data []byte) [Size]byte",
		ContextualPrefix:    "Package crypto/sha256 in Go stdlib; SHA-256 hashing primitives",
		Fingerprint:         "abc123...",
		ParentChunkID:       sql.NullInt64{Int64: 99, Valid: true},
		SourceType:          SrcPackageDoc,
		SymbolPath:          "crypto/sha256.Sum256",
		Kind:                KindFunction,
		SourceURL:           "https://pkg.go.dev/crypto/sha256#Sum256",
		EmbeddingBin256d:    make([]byte, 32),
		EmbeddingFP32_1536d: emb,
		Oversized:           false,
	}
	if c.PackageID != 2 || len(c.EmbeddingBin256d) != 32 || len(c.EmbeddingFP32_1536d) != 1536 {
		t.Errorf("Chunk field-set mismatch (master §3.1 freeze): %+v", c)
	}
	if c.Oversized {
		t.Errorf("Chunk.Oversized default must be false")
	}
}

func TestChangeNodeFields(t *testing.T) {
	cn := ChangeNode{
		ID:              1,
		PackageID:       2,
		VersionFrom:     "1.22.0",
		VersionTo:       "1.23.0",
		ChangeType:      ChangeAdded,
		SymbolPath:      "crypto/sha256.SumOfFile",
		Description:     "Added file-streaming convenience helper.",
		SourceExtracted: "explicit_changelog",
	}
	if cn.PackageID != 2 || cn.ChangeType != ChangeAdded {
		t.Errorf("ChangeNode field-set mismatch: %+v", cn)
	}
	if cn.VersionFrom != "1.22.0" || cn.VersionTo != "1.23.0" {
		t.Errorf("ChangeNode versions not preserved: %+v", cn)
	}
	if cn.SymbolPath == "" || cn.Description == "" || cn.SourceExtracted != "explicit_changelog" {
		t.Errorf("ChangeNode SymbolPath/Description/SourceExtracted not preserved: %+v", cn)
	}
}

func TestSymbolRefFields(t *testing.T) {
	s := SymbolRef{
		Ecosystem:  EcoGo,
		SymbolPath: "crypto/sha256.Sum256",
		Version:    "1.22.3",
	}
	if s.Ecosystem != EcoGo || s.SymbolPath == "" {
		t.Errorf("SymbolRef field-set mismatch: %+v", s)
	}
}

func TestSymbolVerificationFields(t *testing.T) {
	v := SymbolVerification{
		Symbol:    SymbolRef{Ecosystem: EcoGo, SymbolPath: "crypto/sha256.Sum256", Version: "1.22.3"},
		Exists:    true,
		Source:    "symbol_index",
		Latency:   125 * time.Microsecond,
		Signature: "func Sum256(data []byte) [Size]byte",
	}
	if !v.Exists || v.Source != "symbol_index" {
		t.Errorf("SymbolVerification field-set mismatch: %+v", v)
	}
	if v.Symbol.SymbolPath != "crypto/sha256.Sum256" || v.Symbol.Version != "1.22.3" {
		t.Errorf("SymbolVerification.Symbol fields not preserved: %+v", v)
	}
	if v.Latency <= 0 || v.Signature == "" {
		t.Errorf("SymbolVerification Latency/Signature not preserved: %+v", v)
	}
}

func TestQueryProvenanceFields(t *testing.T) {
	p := QueryProvenance{
		DetectedVersion:   "1.22.3",
		DetectionLayer:    2,
		RoutingEcosystems: []Ecosystem{EcoGo},
		RoutingMethod:     "single",
		FreshDispatch:     false,
		DoctrineApplied:   "max-scope",
		RerankerModel:     "bge-reranker-v2-m3",
		EmbedderModel:     "jina-code-embeddings-1.5b",
		LatencyBreakdown:  map[string]float64{"fanout": 87.3, "rerank": 150.1},
	}
	if p.DetectionLayer != 2 || len(p.RoutingEcosystems) != 1 {
		t.Errorf("QueryProvenance field-set mismatch: %+v", p)
	}
}

func TestCitationRefFields(t *testing.T) {
	c := CitationRef{
		ID:         "doc_42",
		ChunkID:    42,
		SymbolPath: "crypto/sha256.Sum256",
		SourceURL:  "https://pkg.go.dev/crypto/sha256#Sum256",
	}
	if c.ID != "doc_42" || c.ChunkID != 42 {
		t.Errorf("CitationRef field-set mismatch: %+v", c)
	}
}

func TestQueryChunkFields(t *testing.T) {
	qc := QueryChunk{
		ChunkID:            42,
		PackageID:          7,
		PackageName:        "crypto/sha256",
		SymbolPath:         "crypto/sha256.Sum256",
		Kind:               KindFunction,
		Version:            "1.22.3",
		ContentText:        "...",
		ContextualPrefix:   "...",
		SourceURL:          "https://pkg.go.dev/crypto/sha256#Sum256",
		SimilarityScore:    0.82,
		RerankerScore:      0.91,
		CitationID:         "doc_42",
		VerificationStatus: "exists",
	}
	if qc.SymbolPath == "" || qc.SimilarityScore < 0 || qc.RerankerScore < 0 {
		t.Errorf("QueryChunk field-set mismatch: %+v", qc)
	}
}

func TestQueryResultFields(t *testing.T) {
	r := QueryResult{
		Chunks:        []QueryChunk{{ChunkID: 1}},
		Citations:     []CitationRef{{ID: "doc_1"}},
		Verified:      []SymbolVerification{{Exists: true}},
		Abstained:     false,
		AbstainReason: "",
		Provenance:    QueryProvenance{DoctrineApplied: "default"},
		AuditChainSeq: 1234,
	}
	if r.AuditChainSeq != 1234 || len(r.Chunks) != 1 {
		t.Errorf("QueryResult field-set mismatch: %+v", r)
	}
	if r.Abstained {
		t.Errorf("QueryResult.Abstained default must be false")
	}
	if r.Provenance.DoctrineApplied != "default" {
		t.Errorf("QueryResult.Provenance.DoctrineApplied not preserved: %+v", r.Provenance)
	}
	if len(r.Citations) != 1 || len(r.Verified) != 1 {
		t.Errorf("QueryResult.Citations/Verified slices not preserved: citations=%d verified=%d", len(r.Citations), len(r.Verified))
	}
}

func TestIngestRequestFields(t *testing.T) {
	r := IngestRequest{
		Ecosystem: EcoGo,
		Version:   "1.23.0",
		Sources:   []SourceType{SrcPackageDoc, SrcGitHub},
		DeltaOnly: true,
	}
	if r.Ecosystem != EcoGo || !r.DeltaOnly {
		t.Errorf("IngestRequest field-set mismatch: %+v", r)
	}
}

func TestIngestResultFields(t *testing.T) {
	now := time.Now()
	r := IngestResult{
		PackagesIngested:   5000,
		PackagesFailed:     7,
		ChunksIngested:     200000,
		SymbolsRegistered:  50000,
		ChangeNodesCreated: 1000,
		StartedAt:          now,
		CompletedAt:        now.Add(1 * time.Hour),
		AuditChainSeqStart: 1000,
		AuditChainSeqEnd:   1500,
	}
	if r.PackagesIngested != 5000 || r.AuditChainSeqEnd-r.AuditChainSeqStart != 500 {
		t.Errorf("IngestResult field-set mismatch: %+v", r)
	}
	if r.PackagesFailed != 7 {
		t.Errorf("PackagesFailed = %d; want 7 (additive amendment)", r.PackagesFailed)
	}
}

func TestQueryRequestFields(t *testing.T) {
	r := QueryRequest{
		Query:       "how to hash a file in go",
		Ecosystem:   EcoGo,
		Version:     "1.22.3",
		Scope:       ScopeDocs,
		MaxResults:  10,
		Doctrine:    "max-scope",
		Strict:      false,
		ProjectPath: "/path/to/projects/example",
	}
	if r.MaxResults != 10 || r.Doctrine != "max-scope" {
		t.Errorf("QueryRequest field-set mismatch: %+v", r)
	}
}

func TestVerifyResultFields(t *testing.T) {
	r := VerifyResult{
		Verifications: []SymbolVerification{
			{Symbol: SymbolRef{SymbolPath: "crypto/sha256.Sum256"}, Exists: true},
			{Symbol: SymbolRef{SymbolPath: "crypto/sha256.Imaginary"}, Exists: false},
		},
		AllVerified: false,
	}
	if r.AllVerified {
		t.Errorf("VerifyResult.AllVerified should be false when any sub-verification is false")
	}
	if len(r.Verifications) != 2 {
		t.Errorf("VerifyResult.Verifications len = %d; want 2", len(r.Verifications))
	}
}

func TestEnumStringers(t *testing.T) {
	checks := []struct {
		name string
		got  string
		want string
	}{
		{"Ecosystem", string(EcoGo), "go"},
		{"QueryScope", string(ScopeAll), "all"},
		{"ChunkKind", string(KindFunction), "function"},
		{"SourceType", string(SrcMDN), "mdn"},
		{"ChangeType", string(ChangeRemoved), "removed"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s implicit string = %q; want %q", c.name, c.got, c.want)
		}
	}
}
