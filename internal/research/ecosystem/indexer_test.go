//go:build cgo
// +build cgo

// Package ecosystem — indexer_test.go
//
// Tests for Indexer.WriteChunks (Plan 14 Phase C Task C-3).
//
// Coverage discipline: per project doctrine `feedback_no_tech_debt.md`,
// security/correctness-critical files require ≥90% per-function coverage.
// This package: indexer.WriteChunks wraps a SQLite transaction over 7+1
// tables; partial-state-on-failure is forbidden (no tech debt). Tests
// cover happy-path, every rollback trigger (NOT-NULL, FK, CHECK, audit
// emitter error, ctx-cancel), UPSERT semantics (package + symbol + change),
// audit chain linking (parent_hash → next-row.parent_hash), FTS5 MATCH,
// vec0 Hamming MATCH (build-tag-gated), and constructor validation.
//
// Drift reconciliation (Stage 0 reality-check, 2026-05-17):
//   - plan-file used column `language` for UNIQUE on ecosystem_packages;
//     real Phase A migration uses `ecosystem` (A-9 reconciliation). All
//     SQL + Go field accesses here use `ecosystem`.
//   - plan-file fakeAuditEmitter.Append used `uint16` for event type;
//     real RAGAuditChainEmitter.Append takes `eventlog.EventType` (int).
//     fakeAuditEmitter below uses the canonical signature.
//   - plan-file did not pin chain seq alignment between RAGAuditChainEmitter
//     and ecosystem_audit_chain.seq AUTOINCREMENT; tests verify that the
//     persisted row's seq matches the value chain.Append returned (the
//     indexer writes that seq explicitly, overriding AUTOINCREMENT).
//
// Build tag `cgo`: this file requires sqlite3 driver (mattn/go-sqlite3) +
// sqlite-vec virtual table support (registered via ApplyMigrations).

package ecosystem

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

func setupIndexerTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/indexer-test.db"
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := ApplyMigrations(db); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}
	return db
}

type fakeAuditEmitter struct {
	mu             sync.Mutex
	callCount      int
	seqs           []int64
	lastPayload    []byte
	lastEventType  eventlog.EventType
	lastDoctrine   string
	returnErr      error
	lastHashReturn string
}

func (f *fakeAuditEmitter) Append(ctx context.Context, evt eventlog.EventType, payload []byte, doctrine string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callCount++
	if f.returnErr != nil {
		return 0, f.returnErr
	}
	seq := int64(f.callCount)
	f.seqs = append(f.seqs, seq)
	f.lastPayload = append([]byte(nil), payload...)
	f.lastEventType = evt
	f.lastDoctrine = doctrine
	return seq, nil
}

func (f *fakeAuditEmitter) LastHash(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastHashReturn, nil
}

func (f *fakeAuditEmitter) SealPartition(ctx context.Context, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func indexerFingerprint(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func indexerRandomBin256(seed byte) []byte {
	out := make([]byte, 32)
	for i := range out {
		out[i] = seed ^ byte(i*7)
	}
	return out
}

func indexerRandomFP32_1536(seed float32) []float32 {
	out := make([]float32, 1536)
	for i := range out {
		out[i] = seed + float32(i)/1536.0
	}
	return out
}

func indexerCountRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func indexerAssertCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()
	got := indexerCountRows(t, db, table)
	if got != want {
		t.Errorf("count(%s) = %d; want %d", table, got, want)
	}
}

func makeMinimalChunk(symbolPath, version string) Chunk {
	return Chunk{
		VersionIntroduced:   version,
		StableIn:            []string{version},
		ContentText:         "minimal " + symbolPath,
		Fingerprint:         indexerFingerprint(symbolPath + version),
		SourceType:          SrcPackageDoc,
		SymbolPath:          symbolPath,
		Kind:                KindFunction,
		SourceURL:           "https://example.test",
		EmbeddingBin256d:    indexerRandomBin256(0x42),
		EmbeddingFP32_1536d: indexerRandomFP32_1536(0.5),
	}
}

func TestNewIndexer_RejectsNilDB(t *testing.T) {
	_, err := NewIndexer(IndexerOptions{DB: nil, Chain: &fakeAuditEmitter{}})
	if err == nil {
		t.Fatal("NewIndexer(DB=nil): want error; got nil")
	}
	if !strings.Contains(err.Error(), "DB") {
		t.Errorf("err = %v; want error mentioning 'DB'", err)
	}
}

func TestNewIndexer_RejectsNilChain(t *testing.T) {
	db := setupIndexerTestDB(t)
	_, err := NewIndexer(IndexerOptions{DB: db, Chain: nil})
	if err == nil {
		t.Fatal("NewIndexer(Chain=nil): want error; got nil")
	}
	if !strings.Contains(err.Error(), "Chain") {
		t.Errorf("err = %v; want error mentioning 'Chain'", err)
	}
}

func TestNewIndexer_AcceptsValidOptions(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	if idx == nil {
		t.Fatal("NewIndexer returned nil indexer with nil error")
	}
}

func TestNewIndexer_DefaultsDoctrine(t *testing.T) {
	db := setupIndexerTestDB(t)
	emitter := &fakeAuditEmitter{}
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: emitter})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}

	pkg := PackageRef{Ecosystem: EcoGo, Name: "fmt", CanonicalNamespace: "fmt", UpstreamURL: "x"}
	chunk := makeMinimalChunk("fmt.X", "1.0")
	if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{chunk}, nil, nil); err != nil {
		t.Fatalf("WriteChunks: %v", err)
	}
	emitter.mu.Lock()
	got := emitter.lastDoctrine
	emitter.mu.Unlock()
	if got != "default" {
		t.Errorf("doctrine = %q; want 'default'", got)
	}
}

func TestNewIndexer_HonoursCustomDoctrine(t *testing.T) {
	db := setupIndexerTestDB(t)
	emitter := &fakeAuditEmitter{}
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: emitter, Doctrine: "max-scope"})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "fmt", CanonicalNamespace: "fmt", UpstreamURL: "x"}
	chunk := makeMinimalChunk("fmt.X", "1.0")
	if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{chunk}, nil, nil); err != nil {
		t.Fatalf("WriteChunks: %v", err)
	}
	emitter.mu.Lock()
	got := emitter.lastDoctrine
	emitter.mu.Unlock()
	if got != "max-scope" {
		t.Errorf("doctrine = %q; want 'max-scope'", got)
	}
}

func TestIndexer_WriteChunks_HappyPath(t *testing.T) {
	db := setupIndexerTestDB(t)
	emitter := &fakeAuditEmitter{}
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: emitter})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{
		Ecosystem:           EcoGo,
		Name:                "crypto/sha256",
		CanonicalNamespace:  "crypto/sha256",
		UpstreamURL:         "https://pkg.go.dev/crypto/sha256",
		LatestStableVersion: "1.23",
	}
	chunks := []Chunk{
		{
			VersionIntroduced:   "1.23",
			StableIn:            []string{"1.23", "1.22", "1.21"},
			ContentText:         "func Sum256(data []byte) [Size]byte",
			ContextualPrefix:    "crypto/sha256 package: Sum256 returns SHA256 hash",
			Fingerprint:         indexerFingerprint("func Sum256(data []byte) [Size]byte"),
			SourceType:          SrcPackageDoc,
			SymbolPath:          "crypto/sha256.Sum256",
			Kind:                KindFunction,
			SourceURL:           "https://pkg.go.dev/crypto/sha256#Sum256",
			EmbeddingBin256d:    indexerRandomBin256(0x11),
			EmbeddingFP32_1536d: indexerRandomFP32_1536(0.1),
		},
		{
			VersionIntroduced:   "1.23",
			StableIn:            []string{"1.23"},
			ContentText:         "type SHA256 struct {}",
			Fingerprint:         indexerFingerprint("type SHA256 struct {}"),
			SourceType:          SrcPackageDoc,
			SymbolPath:          "crypto/sha256.SHA256",
			Kind:                KindType,
			SourceURL:           "https://pkg.go.dev/crypto/sha256#SHA256",
			EmbeddingBin256d:    indexerRandomBin256(0x22),
			EmbeddingFP32_1536d: indexerRandomFP32_1536(0.2),
		},
	}
	symbols := []SymbolRef{
		{Ecosystem: EcoGo, SymbolPath: "crypto/sha256.Sum256", Version: "1.23"},
		{Ecosystem: EcoGo, SymbolPath: "crypto/sha256.SHA256", Version: "1.23"},
	}
	changes := []ChangeNode{
		{
			VersionFrom:     "1.22",
			VersionTo:       "1.23",
			ChangeType:      ChangeAdded,
			SymbolPath:      "crypto/sha256.Sum224",
			Description:     "added Sum224",
			SourceExtracted: "explicit_changelog",
		},
	}
	ctx := context.Background()
	if err := idx.WriteChunks(ctx, pkg, "1.23", chunks, symbols, changes); err != nil {
		t.Fatalf("WriteChunks: %v", err)
	}

	indexerAssertCount(t, db, "ecosystem_packages", 1)
	indexerAssertCount(t, db, "ecosystem_versions", 1)
	indexerAssertCount(t, db, "ecosystem_chunks", 2)
	indexerAssertCount(t, db, "ecosystem_chunks_fp32", 2)
	indexerAssertCount(t, db, "ecosystem_chunks_vec_bin", 2)
	indexerAssertCount(t, db, "ecosystem_symbols", 2)
	indexerAssertCount(t, db, "ecosystem_changes", 1)
	indexerAssertCount(t, db, "ecosystem_audit_chain", 1)

	var ftsCount int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM ecosystem_chunks_fts WHERE ecosystem_chunks_fts MATCH ?`,
		"Sum256",
	).Scan(&ftsCount); err != nil {
		t.Fatalf("FTS5 MATCH query: %v", err)
	}
	if ftsCount < 1 {
		t.Errorf("FTS5 MATCH 'Sum256' returned %d rows; want >=1", ftsCount)
	}

	var evtType int
	if err := db.QueryRow(
		`SELECT event_type FROM ecosystem_audit_chain ORDER BY seq LIMIT 1`,
	).Scan(&evtType); err != nil {
		t.Fatalf("audit row: %v", err)
	}
	if evtType != 98 {
		t.Errorf("audit event_type = %d; want 98", evtType)
	}
	if evtType != int(eventlog.EvtRAGIngestPackage) {
		t.Errorf("audit event_type = %d; want %d (EvtRAGIngestPackage)",
			evtType, int(eventlog.EvtRAGIngestPackage))
	}

	emitter.mu.Lock()
	callCount := emitter.callCount
	lastEvt := emitter.lastEventType
	emitter.mu.Unlock()
	if callCount != 1 {
		t.Errorf("emitter.callCount = %d; want 1", callCount)
	}
	if lastEvt != eventlog.EvtRAGIngestPackage {
		t.Errorf("emitter.lastEventType = %d; want EvtRAGIngestPackage (98)", int(lastEvt))
	}

	var gotEco, gotName string
	if err := db.QueryRow(
		`SELECT ecosystem, name FROM ecosystem_packages WHERE name = ?`,
		"crypto/sha256",
	).Scan(&gotEco, &gotName); err != nil {
		t.Fatalf("packages row: %v", err)
	}
	if gotEco != string(EcoGo) {
		t.Errorf("ecosystem = %q; want %q", gotEco, string(EcoGo))
	}
	if gotName != "crypto/sha256" {
		t.Errorf("name = %q; want 'crypto/sha256'", gotName)
	}
}

func TestIndexer_WriteChunks_RollbackOnBinValidation(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "bad", CanonicalNamespace: "bad", UpstreamURL: "x"}
	chunks := []Chunk{
		{
			VersionIntroduced:   "1.0",
			StableIn:            []string{"1.0"},
			ContentText:         "x",
			Fingerprint:         "deadbeef",
			SourceType:          SrcPackageDoc,
			SymbolPath:          "x.y",
			Kind:                KindFunction,
			SourceURL:           "x",
			EmbeddingBin256d:    []byte{0x01, 0x02},
			EmbeddingFP32_1536d: indexerRandomFP32_1536(0.0),
		},
	}
	err = idx.WriteChunks(context.Background(), pkg, "1.0", chunks, nil, nil)
	if err == nil {
		t.Fatal("expected bin-length-validation error; got nil")
	}
	if !strings.Contains(err.Error(), "bin") && !strings.Contains(err.Error(), "32") {
		t.Errorf("err = %v; want error mentioning bin length", err)
	}

	indexerAssertCount(t, db, "ecosystem_packages", 0)
	indexerAssertCount(t, db, "ecosystem_versions", 0)
	indexerAssertCount(t, db, "ecosystem_chunks", 0)
	indexerAssertCount(t, db, "ecosystem_chunks_fp32", 0)
	indexerAssertCount(t, db, "ecosystem_chunks_vec_bin", 0)
	indexerAssertCount(t, db, "ecosystem_symbols", 0)
	indexerAssertCount(t, db, "ecosystem_audit_chain", 0)
}

func TestIndexer_WriteChunks_RollbackOnFP32Validation(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "bad-fp32", CanonicalNamespace: "bad", UpstreamURL: "x"}
	chunks := []Chunk{
		{
			VersionIntroduced:   "1.0",
			StableIn:            []string{"1.0"},
			ContentText:         "x",
			Fingerprint:         "abc",
			SourceType:          SrcPackageDoc,
			SymbolPath:          "x.y",
			Kind:                KindFunction,
			SourceURL:           "x",
			EmbeddingBin256d:    indexerRandomBin256(0x33),
			EmbeddingFP32_1536d: []float32{1.0, 2.0, 3.0},
		},
	}
	err = idx.WriteChunks(context.Background(), pkg, "1.0", chunks, nil, nil)
	if err == nil {
		t.Fatal("expected fp32-length-validation error; got nil")
	}
	if !strings.Contains(err.Error(), "1536") && !strings.Contains(err.Error(), "fp32") {
		t.Errorf("err = %v; want error mentioning fp32 length", err)
	}

	indexerAssertCount(t, db, "ecosystem_packages", 0)
	indexerAssertCount(t, db, "ecosystem_chunks", 0)
	indexerAssertCount(t, db, "ecosystem_audit_chain", 0)
}

func TestIndexer_WriteChunks_RollbackOnInvalidChangeType(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "ok-but-bad-change", CanonicalNamespace: "x", UpstreamURL: "x"}
	chunks := []Chunk{makeMinimalChunk("x.y", "1.0")}
	changes := []ChangeNode{
		{
			VersionFrom:     "1.0",
			VersionTo:       "1.1",
			ChangeType:      ChangeType("frobnicated"),
			SymbolPath:      "x.y",
			Description:     "invalid",
			SourceExtracted: "explicit_changelog",
		},
	}
	err = idx.WriteChunks(context.Background(), pkg, "1.0", chunks, nil, changes)
	if err == nil {
		t.Fatal("expected CHECK violation on change_type; got nil")
	}

	indexerAssertCount(t, db, "ecosystem_packages", 0)
	indexerAssertCount(t, db, "ecosystem_versions", 0)
	indexerAssertCount(t, db, "ecosystem_chunks", 0)
	indexerAssertCount(t, db, "ecosystem_changes", 0)
	indexerAssertCount(t, db, "ecosystem_audit_chain", 0)
}

// TestIndexer_WriteChunks_RollbackOnAuditEmitterError forces the audit
// chain emitter to fail. Verifies the FULL transaction rolls back —
// the chunks/symbols/changes written earlier MUST not persist if the
// audit row can't be emitted (inv-zen-197 backstop: a write WITHOUT
// an audit event is forbidden).
func TestIndexer_WriteChunks_RollbackOnAuditEmitterError(t *testing.T) {
	db := setupIndexerTestDB(t)
	emitter := &fakeAuditEmitter{returnErr: errors.New("audit chain corrupt")}
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: emitter})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "fmt", CanonicalNamespace: "fmt", UpstreamURL: "x"}
	chunk := makeMinimalChunk("fmt.X", "1.0")
	err = idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{chunk}, nil, nil)
	if err == nil {
		t.Fatal("expected error from audit emitter; got nil")
	}
	if !strings.Contains(err.Error(), "audit") && !strings.Contains(err.Error(), "chain") {
		t.Errorf("err = %v; want error mentioning audit chain", err)
	}

	indexerAssertCount(t, db, "ecosystem_packages", 0)
	indexerAssertCount(t, db, "ecosystem_chunks", 0)
	indexerAssertCount(t, db, "ecosystem_audit_chain", 0)
}

// TestIndexer_WriteChunks_RollbackOnContextCancellation a context that's
// already cancelled before WriteChunks runs MUST return ctx.Err immediately
// without writing ANY row. Defense-in-depth: callers that wrap WriteChunks
// in a timeout context expect early-exit semantics on cancellation, not
// partial writes.
func TestIndexer_WriteChunks_RollbackOnContextCancellation(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pkg := PackageRef{Ecosystem: EcoGo, Name: "fmt", CanonicalNamespace: "fmt", UpstreamURL: "x"}
	chunk := makeMinimalChunk("fmt.X", "1.0")
	err = idx.WriteChunks(ctx, pkg, "1.0", []Chunk{chunk}, nil, nil)
	if err == nil {
		t.Fatal("expected ctx-cancel error; got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v; want context.Canceled", err)
	}

	indexerAssertCount(t, db, "ecosystem_packages", 0)
	indexerAssertCount(t, db, "ecosystem_chunks", 0)
}

func TestIndexer_WriteChunks_PackageUpsertOnRepeatCall(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoPython, Name: "numpy", CanonicalNamespace: "numpy",
		UpstreamURL: "https://pypi.org/project/numpy"}
	chunks1 := []Chunk{makeMinimalChunk("numpy.array", "1.26")}
	if err := idx.WriteChunks(context.Background(), pkg, "1.26", chunks1, nil, nil); err != nil {
		t.Fatalf("WriteChunks #1: %v", err)
	}
	chunks2 := []Chunk{makeMinimalChunk("numpy.array", "2.0")}
	if err := idx.WriteChunks(context.Background(), pkg, "2.0", chunks2, nil, nil); err != nil {
		t.Fatalf("WriteChunks #2: %v", err)
	}
	indexerAssertCount(t, db, "ecosystem_packages", 1)
	indexerAssertCount(t, db, "ecosystem_versions", 2)
	indexerAssertCount(t, db, "ecosystem_chunks", 2)
	indexerAssertCount(t, db, "ecosystem_audit_chain", 2)
}

func TestIndexer_WriteChunks_PackageUpsertUpdatesMetadata(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg1 := PackageRef{Ecosystem: EcoPython, Name: "requests", CanonicalNamespace: "requests",
		UpstreamURL: "https://old.url", LatestStableVersion: "1.0"}
	chunk := makeMinimalChunk("requests.get", "1.0")
	if err := idx.WriteChunks(context.Background(), pkg1, "1.0", []Chunk{chunk}, nil, nil); err != nil {
		t.Fatalf("write #1: %v", err)
	}

	pkg2 := PackageRef{Ecosystem: EcoPython, Name: "requests", CanonicalNamespace: "requests",
		UpstreamURL: "https://new.url", LatestStableVersion: "2.0"}
	chunk2 := makeMinimalChunk("requests.get", "2.0")
	if err := idx.WriteChunks(context.Background(), pkg2, "2.0", []Chunk{chunk2}, nil, nil); err != nil {
		t.Fatalf("write #2: %v", err)
	}
	var url, lsv string
	if err := db.QueryRow(
		`SELECT upstream_url, latest_stable_version FROM ecosystem_packages WHERE name = ?`,
		"requests",
	).Scan(&url, &lsv); err != nil {
		t.Fatalf("packages row: %v", err)
	}
	if url != "https://new.url" {
		t.Errorf("upstream_url = %q; want 'https://new.url' (upsert update)", url)
	}
	if lsv != "2.0" {
		t.Errorf("latest_stable_version = %q; want '2.0'", lsv)
	}
}

func TestIndexer_WriteChunks_SymbolUpsertNoDuplicates(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "fmt", CanonicalNamespace: "fmt", UpstreamURL: "x"}
	syms := []SymbolRef{
		{Ecosystem: EcoGo, SymbolPath: "fmt.Println", Version: "1.23"},
	}
	chunk := makeMinimalChunk("fmt.Println", "1.23")

	if err := idx.WriteChunks(context.Background(), pkg, "1.23", []Chunk{chunk}, syms, nil); err != nil {
		t.Fatalf("write #1: %v", err)
	}
	chunk2 := makeMinimalChunk("fmt.Println-v2", "1.23")
	if err := idx.WriteChunks(context.Background(), pkg, "1.23", []Chunk{chunk2}, syms, nil); err != nil {
		t.Fatalf("write #2: %v", err)
	}
	indexerAssertCount(t, db, "ecosystem_symbols", 1)
}

func TestIndexer_WriteChunks_SymbolDifferentVersionInsertsNew(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "fmt", CanonicalNamespace: "fmt", UpstreamURL: "x"}
	chunk := makeMinimalChunk("fmt.Println", "1.22")
	syms1 := []SymbolRef{{Ecosystem: EcoGo, SymbolPath: "fmt.Println", Version: "1.22"}}
	if err := idx.WriteChunks(context.Background(), pkg, "1.22", []Chunk{chunk}, syms1, nil); err != nil {
		t.Fatalf("write #1: %v", err)
	}
	chunk2 := makeMinimalChunk("fmt.Println", "1.23")
	syms2 := []SymbolRef{{Ecosystem: EcoGo, SymbolPath: "fmt.Println", Version: "1.23"}}
	if err := idx.WriteChunks(context.Background(), pkg, "1.23", []Chunk{chunk2}, syms2, nil); err != nil {
		t.Fatalf("write #2: %v", err)
	}
	indexerAssertCount(t, db, "ecosystem_symbols", 2)
}

func TestIndexer_WriteChunks_ChangeUpsertOnDuplicate(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x", UpstreamURL: "x"}
	chunk := makeMinimalChunk("x.y", "1.0")
	change1 := ChangeNode{
		VersionFrom: "1.0", VersionTo: "1.1", ChangeType: ChangeAdded,
		SymbolPath: "x.y", Description: "first description",
		SourceExtracted: "explicit_changelog",
	}
	if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{chunk}, nil, []ChangeNode{change1}); err != nil {
		t.Fatalf("write #1: %v", err)
	}

	change2 := ChangeNode{
		VersionFrom: "1.0", VersionTo: "1.1", ChangeType: ChangeAdded,
		SymbolPath: "x.y", Description: "second description",
		SourceExtracted: "implicit_deepdiff",
	}
	chunk2 := makeMinimalChunk("x.y.v2", "1.0")
	if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{chunk2}, nil, []ChangeNode{change2}); err != nil {
		t.Fatalf("write #2: %v", err)
	}
	indexerAssertCount(t, db, "ecosystem_changes", 1)
	var desc, src string
	if err := db.QueryRow(
		`SELECT description, source_extracted FROM ecosystem_changes WHERE symbol_path = ?`,
		"x.y",
	).Scan(&desc, &src); err != nil {
		t.Fatalf("change row: %v", err)
	}
	if desc != "second description" {
		t.Errorf("description = %q; want 'second description' (upsert update)", desc)
	}
	if src != "implicit_deepdiff" {
		t.Errorf("source_extracted = %q; want 'implicit_deepdiff' (upsert update)", src)
	}
}

func TestIndexer_WriteChunks_AuditChainLinking(t *testing.T) {
	db := setupIndexerTestDB(t)

	chain := NewInMemoryRAGAuditChain()
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: chain, Doctrine: "default"})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "fmt", CanonicalNamespace: "fmt", UpstreamURL: "x"}
	for i := 0; i < 3; i++ {
		chunk := makeMinimalChunk(fmt.Sprintf("fmt.X%d", i), fmt.Sprintf("v%d", i))
		if err := idx.WriteChunks(context.Background(), pkg, fmt.Sprintf("v%d", i), []Chunk{chunk}, nil, nil); err != nil {
			t.Fatalf("write #%d: %v", i, err)
		}
	}

	if chain.Len() != 3 {
		t.Errorf("chain.Len = %d; want 3", chain.Len())
	}
	r1 := chain.Get(1)
	r2 := chain.Get(2)
	r3 := chain.Get(3)
	if r1 == nil || r2 == nil || r3 == nil {
		t.Fatalf("missing chain records (r1=%v r2=%v r3=%v)", r1, r2, r3)
	}
	if r1.ParentHash != "" {
		t.Errorf("r1.ParentHash = %q; want '' (genesis)", r1.ParentHash)
	}
	if r2.ParentHash != r1.SelfHash {
		t.Errorf("r2.ParentHash = %q; want r1.SelfHash %q", r2.ParentHash, r1.SelfHash)
	}
	if r3.ParentHash != r2.SelfHash {
		t.Errorf("r3.ParentHash = %q; want r2.SelfHash %q", r3.ParentHash, r2.SelfHash)
	}

	indexerAssertCount(t, db, "ecosystem_audit_chain", 3)
	rows, err := db.Query(`SELECT seq, event_type, parent_hash, self_hash FROM ecosystem_audit_chain ORDER BY seq`)
	if err != nil {
		t.Fatalf("audit rows: %v", err)
	}
	defer rows.Close()
	type row struct {
		seq        int64
		evtType    int
		parentHash string
		selfHash   string
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.seq, &r.evtType, &r.parentHash, &r.selfHash); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, r)
	}
	if len(got) != 3 {
		t.Fatalf("got %d rows; want 3", len(got))
	}
	for i, r := range got {
		if r.seq != int64(i+1) {
			t.Errorf("row %d: seq = %d; want %d", i, r.seq, i+1)
		}
		if r.evtType != 98 {
			t.Errorf("row %d: event_type = %d; want 98", i, r.evtType)
		}
	}

	if got[0].parentHash != "" {
		t.Errorf("per-DB row1 parent_hash = %q; want ''", got[0].parentHash)
	}
	if got[1].parentHash != got[0].selfHash {
		t.Errorf("per-DB row2 parent_hash = %q; want row1 self_hash %q",
			got[1].parentHash, got[0].selfHash)
	}
	if got[2].parentHash != got[1].selfHash {
		t.Errorf("per-DB row3 parent_hash = %q; want row2 self_hash %q",
			got[2].parentHash, got[1].selfHash)
	}
}

func TestIndexer_WriteChunks_FP32BlobShape(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "fmt", CanonicalNamespace: "fmt", UpstreamURL: "x"}
	chunk := makeMinimalChunk("fmt.X", "1.0")
	if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{chunk}, nil, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	var blob []byte
	if err := db.QueryRow(
		`SELECT embedding_blob FROM ecosystem_chunks_fp32 LIMIT 1`,
	).Scan(&blob); err != nil {
		t.Fatalf("fp32 row: %v", err)
	}
	if len(blob) != 6144 {
		t.Errorf("fp32 blob length = %d; want 6144", len(blob))
	}

	first := math.Float32frombits(binary.LittleEndian.Uint32(blob[0:4]))
	want := chunk.EmbeddingFP32_1536d[0]
	if first != want {
		t.Errorf("decoded first float = %v; want %v", first, want)
	}
}

func TestIndexer_WriteChunks_VecBinBlobShape(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "fmt", CanonicalNamespace: "fmt", UpstreamURL: "x"}
	chunk := makeMinimalChunk("fmt.X", "1.0")
	chunk.EmbeddingBin256d = indexerRandomBin256(0xAA)
	if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{chunk}, nil, nil); err != nil {
		t.Fatalf("write: %v", err)
	}

	var blob []byte
	if err := db.QueryRow(
		`SELECT embedding_binary_256d FROM ecosystem_chunks LIMIT 1`,
	).Scan(&blob); err != nil {
		t.Fatalf("chunks.embedding_binary_256d: %v", err)
	}
	if len(blob) != 32 {
		t.Errorf("binary embedding length = %d; want 32", len(blob))
	}
	for i, b := range blob {
		if b != chunk.EmbeddingBin256d[i] {
			t.Errorf("binary embedding byte[%d] = 0x%x; want 0x%x", i, b, chunk.EmbeddingBin256d[i])
		}
	}
}

func TestIndexer_WriteChunks_StableInJSONEncoding(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "fmt", CanonicalNamespace: "fmt", UpstreamURL: "x"}
	chunk := makeMinimalChunk("fmt.X", "1.0")
	chunk.StableIn = []string{"1.0", "1.1", "1.2"}
	if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{chunk}, nil, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	var got string
	if err := db.QueryRow(`SELECT stable_in_json FROM ecosystem_chunks LIMIT 1`).Scan(&got); err != nil {
		t.Fatalf("stable_in_json: %v", err)
	}
	want := `["1.0","1.1","1.2"]`
	if got != want {
		t.Errorf("stable_in_json = %q; want %q", got, want)
	}
}

func TestIndexer_WriteChunks_EmptyStableInEncodesAsArray(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "fmt", CanonicalNamespace: "fmt", UpstreamURL: "x"}
	chunk := makeMinimalChunk("fmt.X", "1.0")
	chunk.StableIn = nil
	if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{chunk}, nil, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	var got string
	if err := db.QueryRow(`SELECT stable_in_json FROM ecosystem_chunks LIMIT 1`).Scan(&got); err != nil {
		t.Fatalf("stable_in_json: %v", err)
	}

	if got != "[]" {
		t.Errorf("empty StableIn encoded as %q; want '[]' (must satisfy CHECK)", got)
	}
}

func TestIndexer_WriteChunks_VersionDeprecatedNullable(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "fmt", CanonicalNamespace: "fmt", UpstreamURL: "x"}
	chunk := makeMinimalChunk("fmt.X", "1.0")

	if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{chunk}, nil, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	var dep sql.NullString
	if err := db.QueryRow(`SELECT version_deprecated FROM ecosystem_chunks LIMIT 1`).Scan(&dep); err != nil {
		t.Fatalf("version_deprecated: %v", err)
	}
	if dep.Valid {
		t.Errorf("version_deprecated = %q (Valid=true); want NULL (Valid=false)", dep.String)
	}
}

func TestIndexer_WriteChunks_VersionDeprecatedSetPersists(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "fmt", CanonicalNamespace: "fmt", UpstreamURL: "x"}
	chunk := makeMinimalChunk("fmt.X", "1.0")
	chunk.VersionDeprecated = sql.NullString{String: "1.5", Valid: true}
	if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{chunk}, nil, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	var dep sql.NullString
	if err := db.QueryRow(`SELECT version_deprecated FROM ecosystem_chunks LIMIT 1`).Scan(&dep); err != nil {
		t.Fatalf("version_deprecated: %v", err)
	}
	if !dep.Valid || dep.String != "1.5" {
		t.Errorf("version_deprecated = %v; want '1.5'", dep)
	}
}

func TestIndexer_WriteChunks_ParentChunkIDNullable(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "fmt", CanonicalNamespace: "fmt", UpstreamURL: "x"}
	chunk := makeMinimalChunk("fmt.X", "1.0")

	if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{chunk}, nil, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	var pid sql.NullInt64
	if err := db.QueryRow(`SELECT parent_chunk_id FROM ecosystem_chunks LIMIT 1`).Scan(&pid); err != nil {
		t.Fatalf("parent_chunk_id: %v", err)
	}
	if pid.Valid {
		t.Errorf("parent_chunk_id = %d (Valid=true); want NULL", pid.Int64)
	}
}

func TestIndexer_WriteChunks_Vec0HammingMatchSelfDistanceZero(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "fmt", CanonicalNamespace: "fmt", UpstreamURL: "x"}
	chunk := makeMinimalChunk("fmt.X", "1.0")
	bin := indexerRandomBin256(0xCC)
	chunk.EmbeddingBin256d = bin
	if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{chunk}, nil, nil); err != nil {
		t.Fatalf("write: %v", err)
	}

	rows, err := db.Query(`
		SELECT chunk_id, distance FROM ecosystem_chunks_vec_bin
		WHERE embedding MATCH vec_bit(?)
		ORDER BY distance LIMIT 1
	`, bin)
	if err != nil {
		t.Fatalf("vec0 MATCH: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("vec0 MATCH returned no rows")
	}
	var chunkID int64
	var distance float64
	if err := rows.Scan(&chunkID, &distance); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if chunkID <= 0 {
		t.Errorf("chunk_id = %d; want > 0", chunkID)
	}
	if distance != 0 {
		t.Errorf("distance = %v; want 0 (identical binary)", distance)
	}
}

func TestIndexer_WriteChunks_PayloadContainsCounts(t *testing.T) {
	db := setupIndexerTestDB(t)
	emitter := &fakeAuditEmitter{}
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: emitter})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "fmt", CanonicalNamespace: "fmt", UpstreamURL: "x"}
	chunks := []Chunk{
		makeMinimalChunk("fmt.A", "1.0"),
		makeMinimalChunk("fmt.B", "1.0"),
		makeMinimalChunk("fmt.C", "1.0"),
	}
	symbols := []SymbolRef{
		{Ecosystem: EcoGo, SymbolPath: "fmt.A", Version: "1.0"},
		{Ecosystem: EcoGo, SymbolPath: "fmt.B", Version: "1.0"},
	}
	changes := []ChangeNode{
		{VersionFrom: "0.9", VersionTo: "1.0", ChangeType: ChangeAdded,
			SymbolPath: "fmt.A", Description: "x", SourceExtracted: "explicit_changelog"},
	}
	if err := idx.WriteChunks(context.Background(), pkg, "1.0", chunks, symbols, changes); err != nil {
		t.Fatalf("write: %v", err)
	}
	emitter.mu.Lock()
	payload := append([]byte(nil), emitter.lastPayload...)
	emitter.mu.Unlock()

	s := string(payload)
	wantSubs := []string{`"package_name":"fmt"`, `"version":"1.0"`,
		`"chunks_count":3`, `"symbols_count":2`, `"change_nodes_count":1`,
		`"ecosystem":"go"`}
	for _, sub := range wantSubs {
		if !strings.Contains(s, sub) {
			t.Errorf("payload missing %q; got %s", sub, s)
		}
	}
}

func TestIndexer_WriteChunks_EmptySlicesAreValid(t *testing.T) {
	db := setupIndexerTestDB(t)
	emitter := &fakeAuditEmitter{}
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: emitter})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "empty", CanonicalNamespace: "empty", UpstreamURL: "x"}
	if err := idx.WriteChunks(context.Background(), pkg, "1.0", nil, nil, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	indexerAssertCount(t, db, "ecosystem_packages", 1)
	indexerAssertCount(t, db, "ecosystem_versions", 1)
	indexerAssertCount(t, db, "ecosystem_chunks", 0)
	indexerAssertCount(t, db, "ecosystem_audit_chain", 1)
	emitter.mu.Lock()
	defer emitter.mu.Unlock()
	if emitter.callCount != 1 {
		t.Errorf("emitter.callCount = %d; want 1", emitter.callCount)
	}
}

func TestIndexer_WriteChunks_PartitionIDIsYYYYMM(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "fmt", CanonicalNamespace: "fmt", UpstreamURL: "x"}
	chunk := makeMinimalChunk("fmt.X", "1.0")
	if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{chunk}, nil, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	var pid string
	if err := db.QueryRow(`SELECT partition_id FROM ecosystem_audit_chain LIMIT 1`).Scan(&pid); err != nil {
		t.Fatalf("partition_id: %v", err)
	}
	if len(pid) != 7 || pid[4] != '-' {
		t.Errorf("partition_id = %q; want YYYY-MM format", pid)
	}

	if _, err := time.Parse("2006-01", pid); err != nil {
		t.Errorf("partition_id %q not parseable as YYYY-MM: %v", pid, err)
	}
}

func TestFP32ToBytes_RoundTrip(t *testing.T) {
	cases := [][]float32{
		{},
		{0.0},
		{1.0, -1.0, 0.0},
		{math.MaxFloat32, math.SmallestNonzeroFloat32, float32(math.NaN())},
		indexerRandomFP32_1536(0.7),
	}
	for i, in := range cases {
		t.Run(fmt.Sprintf("case%d_len%d", i, len(in)), func(t *testing.T) {
			out := fp32ToBytes(in)
			if len(out) != 4*len(in) {
				t.Fatalf("len(out) = %d; want %d", len(out), 4*len(in))
			}

			for j := range in {
				got := math.Float32frombits(binary.LittleEndian.Uint32(out[j*4 : j*4+4]))
				if math.IsNaN(float64(in[j])) {
					if !math.IsNaN(float64(got)) {
						t.Errorf("float %d: got %v; want NaN", j, got)
					}
				} else if got != in[j] {
					t.Errorf("float %d: got %v; want %v", j, got, in[j])
				}
			}
		})
	}
}

func TestIndexer_WriteChunks_BeginTxFailsOnClosedDB(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "fmt", CanonicalNamespace: "fmt", UpstreamURL: "x"}
	chunk := makeMinimalChunk("fmt.X", "1.0")
	err = idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{chunk}, nil, nil)
	if err == nil {
		t.Fatal("WriteChunks on closed DB: want error; got nil")
	}
	if !strings.Contains(err.Error(), "BeginTx") {
		t.Errorf("err = %v; want wrap mentioning 'BeginTx'", err)
	}
}

func TestIndexer_NullInt64ToArg_ValidReturnsInt64(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "fmt", CanonicalNamespace: "fmt", UpstreamURL: "x"}

	parentChunk := makeMinimalChunk("fmt.Parent", "1.0")
	leafChunk := makeMinimalChunk("fmt.Leaf", "1.0")
	leafChunk.ParentChunkID = sql.NullInt64{Int64: 1, Valid: true}
	if err := idx.WriteChunks(context.Background(), pkg, "1.0",
		[]Chunk{parentChunk, leafChunk}, nil, nil); err != nil {
		t.Fatalf("write: %v", err)
	}

	var pid sql.NullInt64
	if err := db.QueryRow(
		`SELECT parent_chunk_id FROM ecosystem_chunks WHERE symbol_path = ?`,
		"fmt.Leaf",
	).Scan(&pid); err != nil {
		t.Fatalf("parent_chunk_id: %v", err)
	}
	if !pid.Valid {
		t.Errorf("parent_chunk_id is NULL; want Valid=true")
	}
	if pid.Int64 != 1 {
		t.Errorf("parent_chunk_id = %d; want 1", pid.Int64)
	}
}

func TestIndexer_WriteChunks_SymbolEmptyVersionFallbackToCallArg(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "fmt", CanonicalNamespace: "fmt", UpstreamURL: "x"}
	chunk := makeMinimalChunk("fmt.X", "1.42")
	syms := []SymbolRef{{Ecosystem: EcoGo, SymbolPath: "fmt.X", Version: ""}}
	if err := idx.WriteChunks(context.Background(), pkg, "1.42", []Chunk{chunk}, syms, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	var introduced string
	if err := db.QueryRow(
		`SELECT introduced_in FROM ecosystem_symbols WHERE symbol_path = ?`,
		"fmt.X",
	).Scan(&introduced); err != nil {
		t.Fatalf("introduced_in: %v", err)
	}
	if introduced != "1.42" {
		t.Errorf("introduced_in = %q; want '1.42' (call-arg fallback)", introduced)
	}
}

func TestIndexer_WriteChunks_FP32ValidationRejectsOversizedSlice(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "fmt", CanonicalNamespace: "fmt", UpstreamURL: "x"}
	chunk := makeMinimalChunk("fmt.X", "1.0")
	chunk.EmbeddingFP32_1536d = make([]float32, 2048)
	err = idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{chunk}, nil, nil)
	if err == nil {
		t.Fatal("expected length-validation error; got nil")
	}
	if !strings.Contains(err.Error(), "1536") {
		t.Errorf("err = %v; want error mentioning 1536", err)
	}
}

func TestIndexer_WriteChunks_BinValidationRejectsOversizedSlice(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "fmt", CanonicalNamespace: "fmt", UpstreamURL: "x"}
	chunk := makeMinimalChunk("fmt.X", "1.0")
	chunk.EmbeddingBin256d = make([]byte, 64)
	err = idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{chunk}, nil, nil)
	if err == nil {
		t.Fatal("expected bin length-validation error; got nil")
	}
	if !strings.Contains(err.Error(), "32") {
		t.Errorf("err = %v; want error mentioning '32'", err)
	}
}

func TestIndexer_WriteChunks_ChainAppendCtxCancelRollsBack(t *testing.T) {
	db := setupIndexerTestDB(t)
	emitter := &fakeAuditEmitter{returnErr: context.Canceled}
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: emitter})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "fmt", CanonicalNamespace: "fmt", UpstreamURL: "x"}
	chunk := makeMinimalChunk("fmt.X", "1.0")
	err = idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{chunk}, nil, nil)
	if err == nil {
		t.Fatal("expected error; got nil")
	}
	indexerAssertCount(t, db, "ecosystem_chunks", 0)
}

func TestIndexer_WriteChunks_ConcurrentDistinctPackages(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	const N = 5
	errs := make(chan error, N)
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pkg := PackageRef{
				Ecosystem: EcoGo, Name: fmt.Sprintf("pkg%d", i),
				CanonicalNamespace: fmt.Sprintf("pkg%d", i), UpstreamURL: "x",
			}
			chunk := makeMinimalChunk(fmt.Sprintf("pkg%d.x", i), "1.0")
			errs <- idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{chunk}, nil, nil)
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Errorf("WriteChunks concurrent: %v", e)
		}
	}
	indexerAssertCount(t, db, "ecosystem_packages", N)
	indexerAssertCount(t, db, "ecosystem_chunks", N)
	indexerAssertCount(t, db, "ecosystem_audit_chain", N)

	rows, err := db.Query(`SELECT seq FROM ecosystem_audit_chain ORDER BY seq`)
	if err != nil {
		t.Fatalf("audit seqs: %v", err)
	}
	defer rows.Close()
	var seqs []int64
	for rows.Next() {
		var s int64
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scan: %v", err)
		}
		seqs = append(seqs, s)
	}
	for i, s := range seqs {
		if s != int64(i+1) {
			t.Errorf("seq[%d] = %d; want %d (monotonic)", i, s, i+1)
		}
	}
}

func helperItoa(i int) string { return fmt.Sprintf("%d", i) }

func TestIndexer_Stage1Binary_TopK(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x", UpstreamURL: "x"}
	for i := 0; i < 10; i++ {
		c := makeMinimalChunk("x.S"+helperItoa(i), "1.0")
		bin := make([]byte, 32)
		bin[0] = byte(i)
		c.EmbeddingBin256d = bin
		if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{c}, nil, nil); err != nil {
			t.Fatalf("WriteChunks[%d]: %v", i, err)
		}
	}
	queryBin := make([]byte, 32)
	queryBin[0] = 5
	got, err := idx.Stage1Binary(context.Background(), EcoGo, queryBin, 3, "")
	if err != nil {
		t.Fatalf("Stage1Binary: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("len=%d, want 3", len(got))
	}

	for i := 1; i < len(got); i++ {
		if got[i-1].HammingDistance > got[i].HammingDistance {
			t.Errorf("not sorted: got[%d].Hamming=%d > got[%d].Hamming=%d",
				i-1, got[i-1].HammingDistance, i, got[i].HammingDistance)
		}
	}
}

func TestIndexer_Stage1Binary_HammingDistance(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x", UpstreamURL: "x"}

	c0 := makeMinimalChunk("x.A", "1.0")
	c0.EmbeddingBin256d = make([]byte, 32)
	c1 := makeMinimalChunk("x.B", "1.0")
	c1.EmbeddingBin256d = make([]byte, 32)
	c1.EmbeddingBin256d[0] = 0x01
	c2 := makeMinimalChunk("x.C", "1.0")
	c2.EmbeddingBin256d = make([]byte, 32)
	c2.EmbeddingBin256d[0] = 0xFF
	for _, c := range []Chunk{c0, c1, c2} {
		if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{c}, nil, nil); err != nil {
			t.Fatalf("WriteChunks: %v", err)
		}
	}

	queryBin := make([]byte, 32)
	got, err := idx.Stage1Binary(context.Background(), EcoGo, queryBin, 5, "")
	if err != nil {
		t.Fatalf("Stage1Binary: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len=%d, want 3", len(got))
	}
	if got[0].HammingDistance != 0 {
		t.Errorf("got[0].Hamming=%d, want 0", got[0].HammingDistance)
	}
	if got[1].HammingDistance != 1 {
		t.Errorf("got[1].Hamming=%d, want 1", got[1].HammingDistance)
	}
	if got[2].HammingDistance != 8 {
		t.Errorf("got[2].Hamming=%d, want 8", got[2].HammingDistance)
	}
}

func TestIndexer_Stage1Binary_VersionFilter(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x", UpstreamURL: "x"}
	for i := 0; i < 3; i++ {
		c := makeMinimalChunk("x.A"+helperItoa(i), "1.0")
		if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{c}, nil, nil); err != nil {
			t.Fatalf("WriteChunks v1.0[%d]: %v", i, err)
		}
	}
	for i := 0; i < 3; i++ {
		c := makeMinimalChunk("x.B"+helperItoa(i), "2.0")
		if err := idx.WriteChunks(context.Background(), pkg, "2.0", []Chunk{c}, nil, nil); err != nil {
			t.Fatalf("WriteChunks v2.0[%d]: %v", i, err)
		}
	}
	got, err := idx.Stage1Binary(context.Background(), EcoGo, make([]byte, 32), 10, "1.0")
	if err != nil {
		t.Fatalf("Stage1Binary v1.0: %v", err)
	}

	if len(got) != 3 {
		t.Errorf("len=%d, want 3 (post-filter)", len(got))
	}
	for _, cand := range got {
		var version string
		if err := db.QueryRow(`SELECT version_introduced FROM ecosystem_chunks WHERE id = ?`, cand.ChunkID).Scan(&version); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if version != "1.0" {
			t.Errorf("got chunk @v=%s under filter v1.0", version)
		}
	}
}

func TestIndexer_Stage1Binary_VersionFilterSemverOrdering(t *testing.T) {

	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x", UpstreamURL: "x"}

	c1 := makeMinimalChunk("x.A", "1.2")
	if err := idx.WriteChunks(context.Background(), pkg, "1.2", []Chunk{c1}, nil, nil); err != nil {
		t.Fatalf("WriteChunks 1.2: %v", err)
	}
	c2 := makeMinimalChunk("x.B", "1.10")
	if err := idx.WriteChunks(context.Background(), pkg, "1.10", []Chunk{c2}, nil, nil); err != nil {
		t.Fatalf("WriteChunks 1.10: %v", err)
	}
	got, err := idx.Stage1Binary(context.Background(), EcoGo, make([]byte, 32), 10, "1.10")
	if err != nil {
		t.Fatalf("Stage1Binary: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len=%d, want 2 (both 1.2 and 1.10 <= 1.10 under SemVer)", len(got))
	}

	got, err = idx.Stage1Binary(context.Background(), EcoGo, make([]byte, 32), 10, "1.5")
	if err != nil {
		t.Fatalf("Stage1Binary v1.5: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("len=%d, want 1 (only 1.2 <= 1.5 under SemVer)", len(got))
	}
}

func TestIndexer_Stage1Binary_VersionFilterDeprecated(t *testing.T) {

	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x", UpstreamURL: "x"}
	c := makeMinimalChunk("x.A", "1.0")
	if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{c}, nil, nil); err != nil {
		t.Fatalf("WriteChunks: %v", err)
	}

	if _, err := db.Exec(`UPDATE ecosystem_chunks SET version_deprecated = ? WHERE symbol_path = ?`, "2.0", "x.A"); err != nil {
		t.Fatalf("set deprecated: %v", err)
	}

	got, err := idx.Stage1Binary(context.Background(), EcoGo, make([]byte, 32), 5, "1.5")
	if err != nil {
		t.Fatalf("Stage1Binary v1.5: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("len=%d, want 1 (v1.5 in [1.0, 2.0))", len(got))
	}

	got, err = idx.Stage1Binary(context.Background(), EcoGo, make([]byte, 32), 5, "2.0")
	if err != nil {
		t.Fatalf("Stage1Binary v2.0: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len=%d, want 0 (v2.0 == deprecated upper)", len(got))
	}
}

func TestIndexer_Stage1Binary_EcosystemScoped(t *testing.T) {

	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkgGo := PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x", UpstreamURL: "x"}
	pkgPy := PackageRef{Ecosystem: EcoPython, Name: "y", CanonicalNamespace: "y", UpstreamURL: "y"}
	cGo := makeMinimalChunk("x.A", "1.0")
	cPy := makeMinimalChunk("y.B", "1.0")
	if err := idx.WriteChunks(context.Background(), pkgGo, "1.0", []Chunk{cGo}, nil, nil); err != nil {
		t.Fatalf("WriteChunks go: %v", err)
	}
	if err := idx.WriteChunks(context.Background(), pkgPy, "1.0", []Chunk{cPy}, nil, nil); err != nil {
		t.Fatalf("WriteChunks python: %v", err)
	}
	gotGo, err := idx.Stage1Binary(context.Background(), EcoGo, make([]byte, 32), 5, "")
	if err != nil {
		t.Fatalf("Stage1Binary go: %v", err)
	}
	if len(gotGo) != 1 {
		t.Errorf("len(go)=%d, want 1", len(gotGo))
	}
	gotPy, err := idx.Stage1Binary(context.Background(), EcoPython, make([]byte, 32), 5, "")
	if err != nil {
		t.Fatalf("Stage1Binary python: %v", err)
	}
	if len(gotPy) != 1 {
		t.Errorf("len(python)=%d, want 1", len(gotPy))
	}
}

func TestIndexer_Stage1Binary_QueryLengthValidation(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	for _, length := range []int{0, 1, 31, 33, 64} {
		_, err := idx.Stage1Binary(context.Background(), EcoGo, make([]byte, length), 5, "")
		if err == nil {
			t.Errorf("len=%d: expected error, got nil", length)
		} else if !strings.Contains(err.Error(), "want 32") {
			t.Errorf("len=%d: error %q does not mention `want 32`", length, err.Error())
		}
	}
}

func TestIndexer_Stage1Binary_TopKValidation(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	for _, k := range []int{0, -1, -100} {
		_, err := idx.Stage1Binary(context.Background(), EcoGo, make([]byte, 32), k, "")
		if err == nil {
			t.Errorf("topK=%d: expected error, got nil", k)
		} else if !strings.Contains(err.Error(), "must be > 0") {
			t.Errorf("topK=%d: error %q does not mention `must be > 0`", k, err.Error())
		}
	}
}

func TestIndexer_Stage1Binary_QueryErrorOnClosedDB(t *testing.T) {

	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}
	_, err = idx.Stage1Binary(context.Background(), EcoGo, make([]byte, 32), 5, "")
	if err == nil {
		t.Error("expected query error on closed DB, got nil")
	} else if !strings.Contains(err.Error(), "Stage1Binary query") {
		t.Errorf("err=%q does not mention `Stage1Binary query`", err.Error())
	}
}

func TestIndexer_Stage1Binary_ContextCanceled(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x", UpstreamURL: "x"}
	c := makeMinimalChunk("x.A", "1.0")
	if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{c}, nil, nil); err != nil {
		t.Fatalf("WriteChunks: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = idx.Stage1Binary(ctx, EcoGo, make([]byte, 32), 5, "")
	if err == nil {
		t.Error("expected ctx.Canceled error, got nil")
	}
}

func TestIndexer_Stage2FP32Rerank_CosineOrder(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x", UpstreamURL: "x"}
	queryFP := make([]float32, 1536)
	for i := range queryFP {
		queryFP[i] = 1.0
	}
	makeFP := func(start float32, step float32) []float32 {
		out := make([]float32, 1536)
		for i := range out {
			out[i] = start + float32(i)*step
		}
		return out
	}

	c0 := makeMinimalChunk("x.A", "1.0")
	c0.EmbeddingFP32_1536d = makeFP(1.0, 0)

	c1 := makeMinimalChunk("x.B", "1.0")
	c1.EmbeddingFP32_1536d = makeFP(1.0, 0.001)

	c2 := makeMinimalChunk("x.C", "1.0")
	c2.EmbeddingFP32_1536d = makeFP(-1.0, 0)
	for _, c := range []Chunk{c0, c1, c2} {
		if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{c}, nil, nil); err != nil {
			t.Fatalf("WriteChunks: %v", err)
		}
	}
	var ids []int64
	rows, err := db.Query(`SELECT id FROM ecosystem_chunks ORDER BY id ASC`)
	if err != nil {
		t.Fatalf("query ids: %v", err)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan id: %v", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	candidates := []ChunkCandidate{
		{ChunkID: ids[0], HammingDistance: 0},
		{ChunkID: ids[1], HammingDistance: 0},
		{ChunkID: ids[2], HammingDistance: 0},
	}
	got, err := idx.Stage2FP32Rerank(context.Background(), queryFP, candidates, 3)
	if err != nil {
		t.Fatalf("Stage2FP32Rerank: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len=%d, want 3", len(got))
	}
	if got[0].ChunkID != ids[0] {
		t.Errorf("top1 chunk_id=%d, want %d", got[0].ChunkID, ids[0])
	}
	if got[2].ChunkID != ids[2] {
		t.Errorf("bottom chunk_id=%d, want %d (opposite sign)", got[2].ChunkID, ids[2])
	}

	for i := 1; i < len(got); i++ {
		if got[i-1].CosineScore < got[i].CosineScore {
			t.Errorf("scores not descending: got[%d]=%.4f < got[%d]=%.4f", i-1, got[i-1].CosineScore, i, got[i].CosineScore)
		}
	}
}

func TestIndexer_Stage2FP32Rerank_TopK(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x", UpstreamURL: "x"}
	for i := 0; i < 10; i++ {
		c := makeMinimalChunk("x.S"+helperItoa(i), "1.0")
		if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{c}, nil, nil); err != nil {
			t.Fatalf("WriteChunks[%d]: %v", i, err)
		}
	}
	var ids []int64
	rows, err := db.Query(`SELECT id FROM ecosystem_chunks ORDER BY id ASC`)
	if err != nil {
		t.Fatalf("query ids: %v", err)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan id: %v", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	candidates := make([]ChunkCandidate, len(ids))
	for i, id := range ids {
		candidates[i] = ChunkCandidate{ChunkID: id}
	}
	queryFP := make([]float32, 1536)
	queryFP[0] = 1.0
	got, err := idx.Stage2FP32Rerank(context.Background(), queryFP, candidates, 5)
	if err != nil {
		t.Fatalf("Stage2FP32Rerank: %v", err)
	}
	if len(got) != 5 {
		t.Errorf("len=%d, want 5", len(got))
	}
}

func TestIndexer_Stage2FP32Rerank_EmptyCandidates(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	got, err := idx.Stage2FP32Rerank(context.Background(), make([]float32, 1536), nil, 5)
	if err != nil {
		t.Errorf("err=%v, want nil", err)
	}
	if len(got) != 0 {
		t.Errorf("len=%d, want 0", len(got))
	}
}

func TestIndexer_Stage2FP32Rerank_QueryDimensionMismatch(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	for _, length := range []int{0, 100, 1535, 1537, 3072} {
		_, err := idx.Stage2FP32Rerank(context.Background(), make([]float32, length), []ChunkCandidate{{ChunkID: 1}}, 5)
		if err == nil {
			t.Errorf("len=%d: expected error, got nil", length)
		} else if !strings.Contains(err.Error(), "want 1536") {
			t.Errorf("len=%d: error %q does not mention `want 1536`", length, err.Error())
		}
	}
}

func TestIndexer_Stage2FP32Rerank_TopKValidation(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	for _, k := range []int{0, -1, -100} {
		_, err := idx.Stage2FP32Rerank(context.Background(), make([]float32, 1536), []ChunkCandidate{{ChunkID: 1}}, k)
		if err == nil {
			t.Errorf("topK=%d: expected error, got nil", k)
		} else if !strings.Contains(err.Error(), "must be > 0") {
			t.Errorf("topK=%d: error %q does not mention `must be > 0`", k, err.Error())
		}
	}
}

func TestIndexer_Stage2FP32Rerank_MetadataPopulated(t *testing.T) {

	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "fmt", CanonicalNamespace: "fmt", UpstreamURL: "x"}
	c := makeMinimalChunk("fmt.Println", "1.23")
	c.ContentText = "func Println(a ...any) (n int, err error)"
	c.SourceURL = "https://pkg.go.dev/fmt#Println"
	c.Kind = KindFunction
	if err := idx.WriteChunks(context.Background(), pkg, "1.23", []Chunk{c}, nil, nil); err != nil {
		t.Fatalf("WriteChunks: %v", err)
	}
	var id int64
	if err := db.QueryRow(`SELECT id FROM ecosystem_chunks LIMIT 1`).Scan(&id); err != nil {
		t.Fatalf("query id: %v", err)
	}
	queryFP := make([]float32, 1536)
	queryFP[0] = 1.0
	got, err := idx.Stage2FP32Rerank(context.Background(), queryFP, []ChunkCandidate{{ChunkID: id}}, 5)
	if err != nil {
		t.Fatalf("Stage2FP32Rerank: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len=%d, want 1", len(got))
	}
	if got[0].ContentText != "func Println(a ...any) (n int, err error)" {
		t.Errorf("ContentText=%q", got[0].ContentText)
	}
	if got[0].SymbolPath != "fmt.Println" {
		t.Errorf("SymbolPath=%q", got[0].SymbolPath)
	}
	if got[0].SourceURL != "https://pkg.go.dev/fmt#Println" {
		t.Errorf("SourceURL=%q", got[0].SourceURL)
	}
	if got[0].VersionIntroduced != "1.23" {
		t.Errorf("VersionIntroduced=%q", got[0].VersionIntroduced)
	}
	if got[0].Kind != string(KindFunction) {
		t.Errorf("Kind=%q", got[0].Kind)
	}
	if got[0].PackageID == 0 {
		t.Error("PackageID is zero")
	}
}

func TestIndexer_Stage2FP32Rerank_ZeroQueryNorm(t *testing.T) {

	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x", UpstreamURL: "x"}
	for i := 0; i < 3; i++ {
		c := makeMinimalChunk("x.S"+helperItoa(i), "1.0")
		if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{c}, nil, nil); err != nil {
			t.Fatalf("WriteChunks[%d]: %v", i, err)
		}
	}
	var ids []int64
	rows, err := db.Query(`SELECT id FROM ecosystem_chunks ORDER BY id ASC`)
	if err != nil {
		t.Fatalf("query ids: %v", err)
	}
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	candidates := make([]ChunkCandidate, len(ids))
	for i, id := range ids {
		candidates[i] = ChunkCandidate{ChunkID: id}
	}
	got, err := idx.Stage2FP32Rerank(context.Background(), make([]float32, 1536), candidates, 2)
	if err != nil {
		t.Fatalf("Stage2FP32Rerank: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len=%d, want 2", len(got))
	}
	for i, c := range got {
		if c.CosineScore != 0 {
			t.Errorf("got[%d].CosineScore=%v, want 0 (zero query norm)", i, c.CosineScore)
		}
	}
}

func TestIndexer_Stage2FP32Rerank_ContextCanceled(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x", UpstreamURL: "x"}
	for i := 0; i < 5; i++ {
		c := makeMinimalChunk("x.S"+helperItoa(i), "1.0")
		if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{c}, nil, nil); err != nil {
			t.Fatalf("WriteChunks[%d]: %v", i, err)
		}
	}
	var ids []int64
	rows, _ := db.Query(`SELECT id FROM ecosystem_chunks ORDER BY id ASC`)
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	candidates := make([]ChunkCandidate, len(ids))
	for i, id := range ids {
		candidates[i] = ChunkCandidate{ChunkID: id}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	queryFP := make([]float32, 1536)
	queryFP[0] = 1.0
	_, err = idx.Stage2FP32Rerank(ctx, queryFP, candidates, 5)
	if err == nil {
		t.Error("expected ctx.Canceled error, got nil")
	}
}

func TestIndexer_Stage2FP32Rerank_QueryErrorOnClosedDB(t *testing.T) {

	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}
	queryFP := make([]float32, 1536)
	queryFP[0] = 1.0
	_, err = idx.Stage2FP32Rerank(context.Background(), queryFP, []ChunkCandidate{{ChunkID: 1}}, 5)
	if err == nil {
		t.Error("expected query error on closed DB, got nil")
	} else if !strings.Contains(err.Error(), "Stage2FP32Rerank query") {
		t.Errorf("err=%q does not mention `Stage2FP32Rerank query`", err.Error())
	}
}

func TestIndexer_Stage2FP32Rerank_SwapPath(t *testing.T) {

	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x", UpstreamURL: "x"}
	// chunk0 (id=1): fp32[0] = -1.0 → cosine ≈ -1 with query
	// chunk1 (id=2): fp32[0] = +1.0 → cosine ≈ +1 with query
	// SQL returns id-order [1, 2]; insertion-sort MUST swap so chunk1 wins.
	c0 := makeMinimalChunk("x.A", "1.0")
	c0.EmbeddingFP32_1536d = make([]float32, 1536)
	c0.EmbeddingFP32_1536d[0] = -1.0
	c1 := makeMinimalChunk("x.B", "1.0")
	c1.EmbeddingFP32_1536d = make([]float32, 1536)
	c1.EmbeddingFP32_1536d[0] = 1.0
	for _, c := range []Chunk{c0, c1} {
		if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{c}, nil, nil); err != nil {
			t.Fatalf("WriteChunks: %v", err)
		}
	}
	var ids []int64
	rows, _ := db.Query(`SELECT id FROM ecosystem_chunks ORDER BY id ASC`)
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	candidates := []ChunkCandidate{
		{ChunkID: ids[0]},
		{ChunkID: ids[1]},
	}
	queryFP := make([]float32, 1536)
	queryFP[0] = 1.0
	got, err := idx.Stage2FP32Rerank(context.Background(), queryFP, candidates, 2)
	if err != nil {
		t.Fatalf("Stage2FP32Rerank: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2", len(got))
	}

	if got[0].ChunkID != ids[1] {
		t.Errorf("got[0].ChunkID=%d, want %d (swap path didn't reorder)", got[0].ChunkID, ids[1])
	}
	if got[1].ChunkID != ids[0] {
		t.Errorf("got[1].ChunkID=%d, want %d", got[1].ChunkID, ids[0])
	}
}

func TestIndexer_Stage2FP32Rerank_BlobDecodeError(t *testing.T) {

	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x", UpstreamURL: "x"}
	c := makeMinimalChunk("x.A", "1.0")
	if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{c}, nil, nil); err != nil {
		t.Fatalf("WriteChunks: %v", err)
	}
	if _, err := db.Exec(`UPDATE ecosystem_chunks_fp32 SET embedding_blob = ?`, []byte{0x01, 0x02, 0x03}); err != nil {
		t.Fatalf("corrupt fp32: %v", err)
	}
	var id int64
	_ = db.QueryRow(`SELECT id FROM ecosystem_chunks LIMIT 1`).Scan(&id)
	queryFP := make([]float32, 1536)
	queryFP[0] = 1.0
	_, err = idx.Stage2FP32Rerank(context.Background(), queryFP, []ChunkCandidate{{ChunkID: id}}, 5)
	if err == nil {
		t.Error("expected decode error, got nil")
	}
}

func TestIndexer_Stage2FP32Rerank_BlobDimMismatch(t *testing.T) {

	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x", UpstreamURL: "x"}
	c := makeMinimalChunk("x.A", "1.0")
	if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{c}, nil, nil); err != nil {
		t.Fatalf("WriteChunks: %v", err)
	}
	if _, err := db.Exec(`UPDATE ecosystem_chunks_fp32 SET embedding_blob = ?`, make([]byte, 4*100)); err != nil {
		t.Fatalf("corrupt fp32: %v", err)
	}
	var id int64
	_ = db.QueryRow(`SELECT id FROM ecosystem_chunks LIMIT 1`).Scan(&id)
	queryFP := make([]float32, 1536)
	queryFP[0] = 1.0
	_, err = idx.Stage2FP32Rerank(context.Background(), queryFP, []ChunkCandidate{{ChunkID: id}}, 5)
	if err == nil {
		t.Error("expected dim mismatch error, got nil")
	} else if !strings.Contains(err.Error(), "fp32 len=") {
		t.Errorf("err=%q does not mention `fp32 len=`", err.Error())
	}
}

func TestCosineSimilarity_IdenticalVectors(t *testing.T) {
	a := []float32{1, 2, 3, 4}
	aNorm := vectorNorm(a)
	if got := cosineSimilarity(a, a, aNorm); math.Abs(got-1.0) > 1e-9 {
		t.Errorf("got=%v, want 1.0", got)
	}
}

func TestCosineSimilarity_OppositeVectors(t *testing.T) {
	a := []float32{1, 2, 3, 4}
	b := []float32{-1, -2, -3, -4}
	aNorm := vectorNorm(a)
	if got := cosineSimilarity(a, b, aNorm); math.Abs(got-(-1.0)) > 1e-9 {
		t.Errorf("got=%v, want -1.0", got)
	}
}

func TestCosineSimilarity_OrthogonalVectors(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{0, 1}
	aNorm := vectorNorm(a)
	if got := cosineSimilarity(a, b, aNorm); math.Abs(got) > 1e-9 {
		t.Errorf("got=%v, want 0", got)
	}
}

func TestCosineSimilarity_DimMismatch(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{1, 2}
	aNorm := vectorNorm(a)
	if got := cosineSimilarity(a, b, aNorm); got != 0 {
		t.Errorf("got=%v, want 0 (dim mismatch defensive)", got)
	}
}

func TestCosineSimilarity_ZeroCandidateVector(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{0, 0, 0}
	aNorm := vectorNorm(a)
	if got := cosineSimilarity(a, b, aNorm); got != 0 {
		t.Errorf("got=%v, want 0 (zero candidate norm)", got)
	}
}

func TestVectorNorm(t *testing.T) {

	if got := vectorNorm([]float32{3, 4}); math.Abs(got-5.0) > 1e-9 {
		t.Errorf("got=%v, want 5", got)
	}

	if got := vectorNorm([]float32{0, 0}); got != 0 {
		t.Errorf("got=%v, want 0", got)
	}

	if got := vectorNorm(nil); got != 0 {
		t.Errorf("got=%v, want 0 (nil)", got)
	}
}

func TestBytesToFP32_Roundtrip(t *testing.T) {

	orig := []float32{1.5, -2.25, 3.125, 0, 1e10, -1e-5}
	buf := fp32ToBytes(orig)
	got, err := bytesToFP32(buf)
	if err != nil {
		t.Fatalf("bytesToFP32: %v", err)
	}
	if len(got) != len(orig) {
		t.Fatalf("len=%d, want %d", len(got), len(orig))
	}
	for i := range orig {
		if got[i] != orig[i] {
			t.Errorf("got[%d]=%v, want %v", i, got[i], orig[i])
		}
	}
}

func TestBytesToFP32_NonMod4Length(t *testing.T) {
	for _, n := range []int{1, 2, 3, 5, 7} {
		_, err := bytesToFP32(make([]byte, n))
		if err == nil {
			t.Errorf("len=%d: expected error, got nil", n)
		}
	}
}

func TestBytesToFP32_Empty(t *testing.T) {
	got, err := bytesToFP32(nil)
	if err != nil {
		t.Errorf("err=%v, want nil", err)
	}
	if len(got) != 0 {
		t.Errorf("len=%d, want 0", len(got))
	}
}

func TestVersionInRange(t *testing.T) {
	type tc struct {
		query, introduced, deprecated string
		want                          bool
	}
	cases := []tc{

		{"1.0", "", "", true},
		{"99.99", "", "", true},

		{"1.5", "1.0", "", true},
		{"1.0", "1.0", "", true},

		{"0.9", "1.0", "", false},

		{"1.10", "1.2", "", true},
		{"1.2", "1.10", "", false},

		{"1.5", "1.0", "2.0", true},
		{"2.0", "1.0", "2.0", false},
		{"2.5", "1.0", "2.0", false},
		{"1.0", "1.0", "2.0", true},
	}
	for _, c := range cases {
		got := versionInRange(c.query, c.introduced, c.deprecated)
		if got != c.want {
			t.Errorf("versionInRange(%q, %q, %q) = %v, want %v",
				c.query, c.introduced, c.deprecated, got, c.want)
		}
	}
}

func TestCompareSemverLike(t *testing.T) {
	type tc struct {
		a, b string
		want int
	}
	cases := []tc{
		{"1.0", "1.0", 0},
		{"1.0", "1.1", -1},
		{"1.1", "1.0", 1},

		{"1.10", "1.2", 1},
		{"1.2", "1.10", -1},

		{"1.2.3", "1.2.4", -1},
		{"1.2.3", "1.2.3", 0},

		{"1.2", "1.2.0", -1},
		{"1.2.0", "1.2", 1},

		{"1.0-alpha", "1.0-beta", -1},
		{"1.0-beta", "1.0-alpha", 1},
		{"1.0-rc", "1.0-rc", 0},
	}
	for _, c := range cases {
		got := compareSemverLike(c.a, c.b)
		if got != c.want {
			t.Errorf("compareSemverLike(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestSplitDot(t *testing.T) {
	type tc struct {
		in   string
		want []string
	}
	cases := []tc{
		{"", []string{""}},
		{"1", []string{"1"}},
		{"1.2", []string{"1", "2"}},
		{"1.2.3", []string{"1", "2", "3"}},
		{"1..2", []string{"1", "", "2"}},
		{".", []string{"", ""}},
	}
	for _, c := range cases {
		got := splitDot(c.in)
		if len(got) != len(c.want) {
			t.Errorf("splitDot(%q) len=%d, want %d", c.in, len(got), len(c.want))
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("splitDot(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestParseUint(t *testing.T) {

	for _, c := range []struct {
		s    string
		want uint64
	}{
		{"0", 0},
		{"1", 1},
		{"42", 42},
		{"123456789", 123456789},
	} {
		got, err := parseUint(c.s)
		if err != nil {
			t.Errorf("parseUint(%q) err=%v", c.s, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseUint(%q) = %d, want %d", c.s, got, c.want)
		}
	}

	for _, s := range []string{"", "abc", "1a", "a1", "-1", "1.0"} {
		if _, err := parseUint(s); err == nil {
			t.Errorf("parseUint(%q) expected error, got nil", s)
		}
	}
}

func TestIndexer_LookupExistingEmbedding_Miss(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	got, err := idx.LookupExistingEmbedding(context.Background(), "deadbeef")
	if err != nil {
		t.Fatalf("LookupExistingEmbedding: %v", err)
	}
	if got != nil {
		t.Errorf("miss returned non-nil")
	}
}

func TestIndexer_LookupExistingEmbedding_EmptyFingerprintError(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	got, err := idx.LookupExistingEmbedding(context.Background(), "")
	if err == nil {
		t.Fatalf("expected error for empty fingerprint, got nil; result=%v", got)
	}
}

func TestIndexer_LookupExistingEmbedding_Hit(t *testing.T) {
	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x", UpstreamURL: "x"}
	c := makeMinimalChunk("x.A", "1.0")
	c.Fingerprint = "cafebabe"
	bin := make([]byte, 32)
	bin[0] = 42
	fp := make([]float32, 1536)
	for i := range fp {
		fp[i] = float32(i) / 1000.0
	}
	c.EmbeddingBin256d = bin
	c.EmbeddingFP32_1536d = fp
	if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{c}, nil, nil); err != nil {
		t.Fatalf("WriteChunks: %v", err)
	}

	got, err := idx.LookupExistingEmbedding(context.Background(), "cafebabe")
	if err != nil {
		t.Fatalf("LookupExistingEmbedding: %v", err)
	}
	if got == nil {
		t.Fatal("hit returned nil")
	}
	if string(got.Bin) != string(bin) {
		t.Errorf("bin mismatch: got %x, want %x", got.Bin, bin)
	}
	if len(got.FP32) != 1536 {
		t.Errorf("fp32 len=%d, want 1536", len(got.FP32))
	}
	if got.FP32[0] != fp[0] || got.FP32[1535] != fp[1535] {
		t.Errorf("fp32[0]=%f (want %f); fp32[1535]=%f (want %f)", got.FP32[0], fp[0], got.FP32[1535], fp[1535])
	}
}

func TestIndexer_LookupExistingEmbedding_CrossVersion(t *testing.T) {

	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "x", CanonicalNamespace: "x", UpstreamURL: "x"}
	c := makeMinimalChunk("x.A", "1.0")
	c.Fingerprint = "sharedfp"
	if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{c}, nil, nil); err != nil {
		t.Fatalf("WriteChunks: %v", err)
	}

	got, err := idx.LookupExistingEmbedding(context.Background(), "sharedfp")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got == nil {
		t.Fatal("cross-version lookup miss")
	}
}

func TestIndexer_LookupExistingEmbedding_BinRoundTrip(t *testing.T) {

	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "rt", CanonicalNamespace: "rt", UpstreamURL: "rt"}
	c := makeMinimalChunk("rt.F", "2.0")
	c.Fingerprint = "roundtrip"
	bin := make([]byte, 32)
	for i := range bin {
		bin[i] = byte(i * 7)
	}
	c.EmbeddingBin256d = bin
	if err := idx.WriteChunks(context.Background(), pkg, "2.0", []Chunk{c}, nil, nil); err != nil {
		t.Fatalf("WriteChunks: %v", err)
	}
	got, err := idx.LookupExistingEmbedding(context.Background(), "roundtrip")
	if err != nil {
		t.Fatalf("LookupExistingEmbedding: %v", err)
	}
	if got == nil {
		t.Fatal("expected hit")
	}
	for i, b := range got.Bin {
		if b != bin[i] {
			t.Errorf("bin[%d]=%x, want %x", i, b, bin[i])
		}
	}
}

func TestIndexer_LookupExistingEmbedding_ScanErrorOnClosedDB(t *testing.T) {

	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}
	_, err = idx.LookupExistingEmbedding(context.Background(), "anything")
	if err == nil {
		t.Fatal("expected error on closed DB, got nil")
	}
}

func TestIndexer_LookupExistingEmbedding_CorruptBinLength(t *testing.T) {

	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}

	pkg := PackageRef{Ecosystem: EcoGo, Name: "corrupt", CanonicalNamespace: "corrupt", UpstreamURL: "corrupt"}
	c := makeMinimalChunk("corrupt.A", "1.0")
	c.Fingerprint = "corruptfp"
	if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{c}, nil, nil); err != nil {
		t.Fatalf("WriteChunks: %v", err)
	}

	if _, err := db.ExecContext(context.Background(),
		`UPDATE ecosystem_chunks SET embedding_binary_256d = ? WHERE chunk_fingerprint = ?`,
		make([]byte, 16), "corruptfp",
	); err != nil {
		t.Fatalf("corrupt UPDATE: %v", err)
	}
	_, err = idx.LookupExistingEmbedding(context.Background(), "corruptfp")
	if err == nil {
		t.Fatal("expected error for corrupt bin length, got nil")
	}
}

func TestIndexer_LookupExistingEmbedding_CorruptFP32Blob(t *testing.T) {

	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "corruptfp32", CanonicalNamespace: "corruptfp32", UpstreamURL: "corruptfp32"}
	c := makeMinimalChunk("corruptfp32.F", "1.0")
	c.Fingerprint = "corruptblob"
	if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{c}, nil, nil); err != nil {
		t.Fatalf("WriteChunks: %v", err)
	}

	if _, err := db.ExecContext(context.Background(),
		`UPDATE ecosystem_chunks_fp32
		 SET embedding_blob = ?
		 WHERE chunk_id = (SELECT id FROM ecosystem_chunks WHERE chunk_fingerprint = ?)`,
		make([]byte, 5), "corruptblob",
	); err != nil {
		t.Fatalf("corrupt UPDATE: %v", err)
	}
	_, err = idx.LookupExistingEmbedding(context.Background(), "corruptblob")
	if err == nil {
		t.Fatal("expected error for corrupt fp32 blob, got nil")
	}
}

func TestIndexer_LookupExistingEmbedding_CorruptFP32WrongDim(t *testing.T) {

	db := setupIndexerTestDB(t)
	idx, err := NewIndexer(IndexerOptions{DB: db, Chain: &fakeAuditEmitter{}})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	pkg := PackageRef{Ecosystem: EcoGo, Name: "corruptdim", CanonicalNamespace: "corruptdim", UpstreamURL: "corruptdim"}
	c := makeMinimalChunk("corruptdim.F", "1.0")
	c.Fingerprint = "corruptdim"
	if err := idx.WriteChunks(context.Background(), pkg, "1.0", []Chunk{c}, nil, nil); err != nil {
		t.Fatalf("WriteChunks: %v", err)
	}

	if _, err := db.ExecContext(context.Background(),
		`UPDATE ecosystem_chunks_fp32
		 SET embedding_blob = ?
		 WHERE chunk_id = (SELECT id FROM ecosystem_chunks WHERE chunk_fingerprint = ?)`,
		make([]byte, 4*4), "corruptdim",
	); err != nil {
		t.Fatalf("corrupt UPDATE: %v", err)
	}
	_, err = idx.LookupExistingEmbedding(context.Background(), "corruptdim")
	if err == nil {
		t.Fatal("expected error for wrong fp32 dimension, got nil")
	}
}
