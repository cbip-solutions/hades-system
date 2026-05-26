//go:build integration && cgo

package ecosystem_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

func TestIndexer_Integration_FullPath(t *testing.T) {
	sqlite_vec.Auto()
	dir := t.TempDir()
	db, err := sql.Open("sqlite3",
		filepath.Join(dir, "go.db")+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	if err := ecosystem.ApplyMigrations(db); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	var vecVer string
	if err := db.QueryRow("SELECT vec_version()").Scan(&vecVer); err != nil {
		t.Fatalf("vec_version: %v", err)
	}
	if vecVer == "" {
		t.Fatal("vec_version empty; sqlite-vec not loaded")
	}

	chain := ecosystem.NewInMemoryRAGAuditChain()
	idx, err := ecosystem.NewIndexer(ecosystem.IndexerOptions{
		DB:       db,
		Chain:    chain,
		Doctrine: "max-scope",
	})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}

	pkg := ecosystem.PackageRef{
		Ecosystem:           ecosystem.EcoGo,
		Name:                "fmt",
		CanonicalNamespace:  "fmt",
		UpstreamURL:         "https://pkg.go.dev/fmt",
		LatestStableVersion: "1.23",
	}
	bin := make([]byte, 32)
	for i := range bin {
		bin[i] = byte(i * 11)
	}
	fp32 := make([]float32, 1536)
	for i := range fp32 {
		fp32[i] = float32(i%10) / 10.0
	}
	chunk := ecosystem.Chunk{
		VersionIntroduced:   "1.23",
		StableIn:            []string{"1.23"},
		ContentText:         "func Println(a ...any) (n int, err error)",
		ContextualPrefix:    "fmt package Println formatted output",
		Fingerprint:         "abc",
		SourceType:          ecosystem.SrcPackageDoc,
		SymbolPath:          "fmt.Println",
		Kind:                ecosystem.KindFunction,
		SourceURL:           "https://pkg.go.dev/fmt#Println",
		EmbeddingBin256d:    bin,
		EmbeddingFP32_1536d: fp32,
	}
	symbols := []ecosystem.SymbolRef{
		{Ecosystem: ecosystem.EcoGo, SymbolPath: "fmt.Println", Version: "1.23"},
	}
	changes := []ecosystem.ChangeNode{
		{VersionFrom: "1.22", VersionTo: "1.23", ChangeType: ecosystem.ChangeChanged,
			SymbolPath: "fmt.Println", Description: "improved formatting",
			SourceExtracted: "explicit_changelog"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := idx.WriteChunks(ctx, pkg, "1.23",
		[]ecosystem.Chunk{chunk}, symbols, changes,
	); err != nil {
		t.Fatalf("WriteChunks: %v", err)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT chunk_id, distance FROM ecosystem_chunks_vec_bin
		WHERE embedding MATCH vec_bit(?)
		ORDER BY distance LIMIT 1
	`, bin)
	if err != nil {
		t.Fatalf("vec_bin MATCH: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("vec_bin MATCH returned no rows")
	}
	var chunkID int64
	var dist float64
	if err := rows.Scan(&chunkID, &dist); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if chunkID == 0 {
		t.Error("chunk_id is zero")
	}
	if dist != 0 {
		t.Errorf("distance = %v; want 0 (identical binary)", dist)
	}

	var ftsCount int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM ecosystem_chunks_fts WHERE ecosystem_chunks_fts MATCH ?`,
		"Println",
	).Scan(&ftsCount); err != nil {
		t.Fatalf("FTS5: %v", err)
	}
	if ftsCount < 1 {
		t.Errorf("FTS5 returned %d; want >=1", ftsCount)
	}

	var evtType int
	if err := db.QueryRow(
		`SELECT event_type FROM ecosystem_audit_chain LIMIT 1`,
	).Scan(&evtType); err != nil {
		t.Fatalf("audit_chain: %v", err)
	}
	if evtType != 98 {
		t.Errorf("event_type = %d; want 98", evtType)
	}

	if chain.Len() != 1 {
		t.Errorf("chain.Len = %d; want 1", chain.Len())
	}
	r := chain.Get(1)
	if r == nil {
		t.Fatal("chain has no record at seq 1")
	}
	var dbSeq int64
	if err := db.QueryRow(`SELECT seq FROM ecosystem_audit_chain LIMIT 1`).Scan(&dbSeq); err != nil {
		t.Fatalf("db seq: %v", err)
	}
	if dbSeq != r.Seq {
		t.Errorf("per-DB seq = %d; want chain.Seq %d", dbSeq, r.Seq)
	}

	var dbSelfHash string
	if err := db.QueryRow(`SELECT self_hash FROM ecosystem_audit_chain LIMIT 1`).Scan(&dbSelfHash); err != nil {
		t.Fatalf("db self_hash: %v", err)
	}
	if dbSelfHash != r.SelfHash {
		t.Errorf("per-DB self_hash = %q; want chain self_hash %q", dbSelfHash, r.SelfHash)
	}
}

func TestIndexer_Integration_MultiplePackages(t *testing.T) {
	sqlite_vec.Auto()
	dir := t.TempDir()
	db, err := sql.Open("sqlite3",
		filepath.Join(dir, "go.db")+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	if err := ecosystem.ApplyMigrations(db); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	chain := ecosystem.NewInMemoryRAGAuditChain()
	idx, err := ecosystem.NewIndexer(ecosystem.IndexerOptions{
		DB: db, Chain: chain,
	})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}

	const N = 4
	for i := 0; i < N; i++ {
		bin := make([]byte, 32)
		for j := range bin {
			bin[j] = byte(i*32 + j)
		}
		fp32 := make([]float32, 1536)
		for j := range fp32 {
			fp32[j] = float32(i+j) / 100.0
		}
		pkg := ecosystem.PackageRef{
			Ecosystem:          ecosystem.EcoGo,
			Name:               fmt.Sprintf("pkg%d", i),
			CanonicalNamespace: fmt.Sprintf("pkg%d", i),
			UpstreamURL:        "x",
		}
		chunk := ecosystem.Chunk{
			VersionIntroduced:   "1.0",
			StableIn:            []string{"1.0"},
			ContentText:         fmt.Sprintf("chunk text for pkg%d", i),
			Fingerprint:         fmt.Sprintf("fp-%d", i),
			SourceType:          ecosystem.SrcPackageDoc,
			SymbolPath:          fmt.Sprintf("pkg%d.X", i),
			Kind:                ecosystem.KindFunction,
			SourceURL:           "x",
			EmbeddingBin256d:    bin,
			EmbeddingFP32_1536d: fp32,
		}
		if err := idx.WriteChunks(context.Background(), pkg, "1.0",
			[]ecosystem.Chunk{chunk}, nil, nil); err != nil {
			t.Fatalf("WriteChunks #%d: %v", i, err)
		}
	}

	var nPkgs, nChunks, nAudit int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ecosystem_packages`).Scan(&nPkgs); err != nil {
		t.Fatalf("count packages: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM ecosystem_chunks`).Scan(&nChunks); err != nil {
		t.Fatalf("count chunks: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM ecosystem_audit_chain`).Scan(&nAudit); err != nil {
		t.Fatalf("count audit_chain: %v", err)
	}
	if nPkgs != N {
		t.Errorf("packages count = %d; want %d", nPkgs, N)
	}
	if nChunks != N {
		t.Errorf("chunks count = %d; want %d", nChunks, N)
	}
	if nAudit != N {
		t.Errorf("audit_chain count = %d; want %d", nAudit, N)
	}
	if chain.Len() != N {
		t.Errorf("canonical chain Len = %d; want %d", chain.Len(), N)
	}
}

func TestIndexer_Integration_TwoStageRetrieval(t *testing.T) {
	sqlite_vec.Auto()
	dir := t.TempDir()
	db, err := sql.Open("sqlite3",
		filepath.Join(dir, "go.db")+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	if err := ecosystem.ApplyMigrations(db); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	chain := ecosystem.NewInMemoryRAGAuditChain()
	idx, err := ecosystem.NewIndexer(ecosystem.IndexerOptions{
		DB: db, Chain: chain,
	})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}

	pkg := ecosystem.PackageRef{
		Ecosystem:          ecosystem.EcoGo,
		Name:               "fmt",
		CanonicalNamespace: "fmt",
		UpstreamURL:        "https://pkg.go.dev/fmt",
	}

	target := "fmt.Println"
	chunks := []ecosystem.Chunk{}
	for i := 0; i < 5; i++ {
		bin := make([]byte, 32)

		bin[0] = byte(1<<i) - 1
		fp32 := make([]float32, 1536)
		fp32[0] = 1.0 - float32(i)*0.2
		chunks = append(chunks, ecosystem.Chunk{
			VersionIntroduced:   "1.23",
			StableIn:            []string{"1.23"},
			ContentText:         fmt.Sprintf("Println variant %d", i),
			ContextualPrefix:    "fmt package",
			Fingerprint:         fmt.Sprintf("fp-%d", i),
			SourceType:          ecosystem.SrcPackageDoc,
			SymbolPath:          fmt.Sprintf("fmt.Println%d", i),
			Kind:                ecosystem.KindFunction,
			SourceURL:           "https://pkg.go.dev/fmt#Println",
			EmbeddingBin256d:    bin,
			EmbeddingFP32_1536d: fp32,
		})
	}

	chunks[0].SymbolPath = target

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i, c := range chunks {
		if err := idx.WriteChunks(ctx, pkg, "1.23",
			[]ecosystem.Chunk{c}, nil, nil); err != nil {
			t.Fatalf("WriteChunks[%d]: %v", i, err)
		}
	}

	queryBin := make([]byte, 32)
	stage1, err := idx.Stage1Binary(ctx, ecosystem.EcoGo, queryBin, 3, "")
	if err != nil {
		t.Fatalf("Stage1Binary: %v", err)
	}
	if len(stage1) != 3 {
		t.Fatalf("stage1 len=%d, want 3", len(stage1))
	}

	for i := 1; i < len(stage1); i++ {
		if stage1[i-1].HammingDistance > stage1[i].HammingDistance {
			t.Errorf("stage1 not sorted: got[%d].Hamming=%d > got[%d].Hamming=%d",
				i-1, stage1[i-1].HammingDistance, i, stage1[i].HammingDistance)
		}
	}

	if stage1[0].HammingDistance != 0 {
		t.Errorf("stage1[0].Hamming=%d, want 0 (chunk0 exact match)",
			stage1[0].HammingDistance)
	}

	queryFP := make([]float32, 1536)
	queryFP[0] = 1.0
	stage2, err := idx.Stage2FP32Rerank(ctx, queryFP, stage1, 2)
	if err != nil {
		t.Fatalf("Stage2FP32Rerank: %v", err)
	}
	if len(stage2) != 2 {
		t.Fatalf("stage2 len=%d, want 2", len(stage2))
	}

	if stage2[0].CosineScore < stage2[1].CosineScore {
		t.Errorf("stage2 scores not descending: [0]=%v < [1]=%v",
			stage2[0].CosineScore, stage2[1].CosineScore)
	}

	if stage2[0].SymbolPath != target {
		t.Errorf("stage2[0].SymbolPath=%q, want %q (chunk0 highest cosine + lowest Hamming)",
			stage2[0].SymbolPath, target)
	}

	if stage2[0].PackageID == 0 {
		t.Error("stage2[0].PackageID is zero — metadata not populated")
	}
	if stage2[0].SourceURL == "" {
		t.Error("stage2[0].SourceURL empty — metadata not populated")
	}
	if stage2[0].VersionIntroduced != "1.23" {
		t.Errorf("stage2[0].VersionIntroduced=%q, want 1.23",
			stage2[0].VersionIntroduced)
	}
	if stage2[0].ContentText == "" {
		t.Error("stage2[0].ContentText empty — metadata not populated")
	}
	if stage2[0].Kind != string(ecosystem.KindFunction) {
		t.Errorf("stage2[0].Kind=%q, want %q",
			stage2[0].Kind, string(ecosystem.KindFunction))
	}

	if stage2[0].HammingDistance != 0 {
		t.Errorf("stage2[0].Hamming=%d, want 0 (preserved from Stage 1)",
			stage2[0].HammingDistance)
	}
}

func TestIndexer_Integration_Stage1VersionFilter(t *testing.T) {
	sqlite_vec.Auto()
	dir := t.TempDir()
	db, err := sql.Open("sqlite3",
		filepath.Join(dir, "go.db")+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	if err := ecosystem.ApplyMigrations(db); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	chain := ecosystem.NewInMemoryRAGAuditChain()
	idx, err := ecosystem.NewIndexer(ecosystem.IndexerOptions{
		DB: db, Chain: chain,
	})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}

	pkg := ecosystem.PackageRef{
		Ecosystem:          ecosystem.EcoGo,
		Name:               "x",
		CanonicalNamespace: "x",
		UpstreamURL:        "x",
	}

	for _, ver := range []string{"1.2", "1.10"} {
		bin := make([]byte, 32)
		fp32 := make([]float32, 1536)
		chunk := ecosystem.Chunk{
			VersionIntroduced:   ver,
			StableIn:            []string{ver},
			ContentText:         "x.Y@" + ver,
			Fingerprint:         "fp-" + ver,
			SourceType:          ecosystem.SrcPackageDoc,
			SymbolPath:          "x.Y",
			Kind:                ecosystem.KindFunction,
			SourceURL:           "x",
			EmbeddingBin256d:    bin,
			EmbeddingFP32_1536d: fp32,
		}
		if err := idx.WriteChunks(context.Background(), pkg, ver,
			[]ecosystem.Chunk{chunk}, nil, nil); err != nil {
			t.Fatalf("WriteChunks @%s: %v", ver, err)
		}
	}

	got, err := idx.Stage1Binary(context.Background(), ecosystem.EcoGo,
		make([]byte, 32), 10, "1.10")
	if err != nil {
		t.Fatalf("Stage1Binary v1.10: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("v1.10 filter: len=%d, want 2", len(got))
	}

	got, err = idx.Stage1Binary(context.Background(), ecosystem.EcoGo,
		make([]byte, 32), 10, "1.5")
	if err != nil {
		t.Fatalf("Stage1Binary v1.5: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("v1.5 filter: len=%d, want 1 (only v1.2 <= 1.5 under SemVer)", len(got))
	}
	if len(got) == 1 && got[0].VersionIntroduced != "1.2" {
		t.Errorf("v1.5 filter survivor: version=%q, want 1.2", got[0].VersionIntroduced)
	}
}
