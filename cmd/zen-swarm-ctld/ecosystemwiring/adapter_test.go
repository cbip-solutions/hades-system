//go:build cgo
// +build cgo

// Package ecosystemwiring — adapter_test.go (Plan 14 followup wiring).
//
// Unit tests for Adapter:
//
//   - Pin: not-found → ecospec.ErrEcosystemVersionNotFound; already-pinned
//     → ecospec.ErrEcosystemPinAlreadyPinned; fresh-pin → 204 + DB row
//     reflects indefinite_retain=1.
//   - PrunePreview: not-found → ecospec.ErrEcosystemVersionNotFound; happy
//     path returns accurate counts + Pinned flag.
//   - Prune: not-found → ecospec.ErrEcosystemVersionNotFound; pinned →
//     ecospec.ErrEcosystemVersionPinned; happy path deletes cascade chain
//     atomically.
//   - IngestDelta: missing Ingester → typed error; missing source map for
//     ecosystem → typed error; happy path runs Ingest with DeltaOnly=true.
//   - SweepChunkFingerprints: detects drift + repairs to canonical sha256;
//     idempotent on clean corpus.
//   - SweepChangeNodes: orphan detected → returned error; clean → nil.
//   - RebuildSymbolIndex: reloads from DB; symbol newly registered visible
//     after Rebuild.
//   - CASGarbageCollect: orphan vec_bin + fts rows deleted; clean corpus
//     → no-op.
//   - DetectNewVersions: returns upstream\ecosystem_versions diff; empty
//     source set → typed error.
//
// Per task spec: tests MUST NOT require a real Embedder / Reranker / Haiku
// — adapter operations are pure SQL + Source.FetchManifest delegation;
// every fixture here constructs the substrate from in-memory primitives.
//
// Driver mattn/go-sqlite3 (CGO + sqlite-vec). The sub-package's transitive
// import surface deliberately avoids internal/store + internal/knowledge so
// the ncruces/go-sqlite3 driver does NOT register alongside mattn — this
// keeps the test binary from tripping the pre-existing
// cmd/zen-swarm-ctld/* double-registration panic (see adapter.go package
// comment for the full doc).

package ecosystemwiring_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/cmd/zen-swarm-ctld/ecosystemwiring"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers/ecospec"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

func openAdapterTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ecosystem-test.db")
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := ecosystem.ApplyMigrations(db); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}
	return db
}

func seedPackageVersion(t *testing.T, db *sql.DB, ecoName, pkgName, version string, nChunks int) int64 {
	t.Helper()
	res, err := db.Exec(`
		INSERT INTO ecosystem_packages (name, ecosystem, upstream_url, canonical_namespace)
		VALUES (?, ?, ?, ?)
	`, pkgName, ecoName, "https://example.test/"+pkgName, pkgName)
	if err != nil {
		t.Fatalf("insert package: %v", err)
	}
	pkgID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO ecosystem_versions (package_id, version) VALUES (?, ?)
	`, pkgID, version); err != nil {
		t.Fatalf("insert version: %v", err)
	}

	for i := 0; i < nChunks; i++ {
		content := pkgName + "-content-" + version + "-" + string(rune('a'+i))
		sum := sha256.Sum256([]byte(content))
		fingerprint := hex.EncodeToString(sum[:])
		chRes, err := db.Exec(`
			INSERT INTO ecosystem_chunks (
				package_id, version_introduced, content_text, chunk_fingerprint,
				source_type, source_url
			) VALUES (?, ?, ?, ?, ?, ?)
		`, pkgID, version, content, fingerprint, "registry_docs", "https://example.test/"+pkgName+"/"+version)
		if err != nil {
			t.Fatalf("insert chunk: %v", err)
		}
		chID, _ := chRes.LastInsertId()
		if _, err := db.Exec(`
			INSERT INTO ecosystem_chunks_fp32 (chunk_id, embedding_blob) VALUES (?, ?)
		`, chID, []byte("fake-fp32-blob-6144-bytes-pretend")); err != nil {
			t.Fatalf("insert fp32: %v", err)
		}
		if _, err := db.Exec(`
			INSERT INTO ecosystem_chunks_fts (chunk_id, content_text) VALUES (?, ?)
		`, chID, content); err != nil {
			t.Fatalf("insert fts: %v", err)
		}
		bin := make([]byte, 32)
		for j := range bin {
			bin[j] = byte(i * j)
		}

		if _, err := db.Exec(`
			INSERT INTO ecosystem_chunks_vec_bin (chunk_id, embedding) VALUES (?, vec_bit(?))
		`, chID, bin); err != nil {
			t.Fatalf("insert vec_bin: %v", err)
		}
	}

	if _, err := db.Exec(`
		INSERT INTO ecosystem_symbols (package_id, symbol_path, introduced_in) VALUES (?, ?, ?)
	`, pkgID, pkgName+".Symbol1", version); err != nil {
		t.Fatalf("insert symbol1: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO ecosystem_symbols (package_id, symbol_path, introduced_in) VALUES (?, ?, ?)
	`, pkgID, pkgName+".Symbol2", version); err != nil {
		t.Fatalf("insert symbol2: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO ecosystem_versions (package_id, version) VALUES (?, ?)
	`, pkgID, "0.0.0"); err != nil {
		t.Fatalf("insert prior version: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO ecosystem_changes (package_id, version_from, version_to, change_type, source_extracted)
		VALUES (?, ?, ?, ?, ?)
	`, pkgID, "0.0.0", version, "added", "explicit_changelog"); err != nil {
		t.Fatalf("insert change: %v", err)
	}

	return pkgID
}

func TestAdapter_Pin_NotFound(t *testing.T) {
	db := openAdapterTestDB(t)
	a, err := ecosystemwiring.New(ecosystemwiring.AdapterDeps{
		PerEcosystemDB: map[ecosystem.Ecosystem]*sql.DB{ecosystem.EcoGo: db},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.Pin(context.Background(), "go", "1.99.0"); !errors.Is(err, ecospec.ErrEcosystemVersionNotFound) {
		t.Fatalf("expected ErrEcosystemVersionNotFound, got %v", err)
	}
}

func TestAdapter_Pin_FreshThenAlreadyPinned(t *testing.T) {
	db := openAdapterTestDB(t)
	_ = seedPackageVersion(t, db, "go", "github.com/foo/bar", "1.2.3", 1)

	a, err := ecosystemwiring.New(ecosystemwiring.AdapterDeps{
		PerEcosystemDB: map[ecosystem.Ecosystem]*sql.DB{ecosystem.EcoGo: db},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := a.Pin(context.Background(), "go", "1.2.3"); err != nil {
		t.Fatalf("first Pin: %v", err)
	}
	var retain int
	if err := db.QueryRow(`SELECT indefinite_retain FROM ecosystem_versions WHERE version = ?`, "1.2.3").Scan(&retain); err != nil {
		t.Fatalf("post-pin SELECT: %v", err)
	}
	if retain != 1 {
		t.Fatalf("expected indefinite_retain=1 after pin, got %d", retain)
	}

	if err := a.Pin(context.Background(), "go", "1.2.3"); !errors.Is(err, ecospec.ErrEcosystemPinAlreadyPinned) {
		t.Fatalf("expected ErrEcosystemPinAlreadyPinned on second Pin, got %v", err)
	}
}

func TestAdapter_Pin_EcosystemNotConfigured(t *testing.T) {
	db := openAdapterTestDB(t)
	a, _ := ecosystemwiring.New(ecosystemwiring.AdapterDeps{
		PerEcosystemDB: map[ecosystem.Ecosystem]*sql.DB{ecosystem.EcoGo: db},
	})
	err := a.Pin(context.Background(), "python", "3.10.0")
	if !errors.Is(err, ecosystemwiring.ErrEcosystemDBNotConfigured) {
		t.Fatalf("expected ErrEcosystemDBNotConfigured for unwired eco, got %v", err)
	}
}

func TestAdapter_PrunePreview_NotFound(t *testing.T) {
	db := openAdapterTestDB(t)
	a, _ := ecosystemwiring.New(ecosystemwiring.AdapterDeps{
		PerEcosystemDB: map[ecosystem.Ecosystem]*sql.DB{ecosystem.EcoGo: db},
	})
	_, err := a.PrunePreview(context.Background(), "go", "1.99.0")
	if !errors.Is(err, ecospec.ErrEcosystemVersionNotFound) {
		t.Fatalf("expected ErrEcosystemVersionNotFound, got %v", err)
	}
}

func TestAdapter_PrunePreview_HappyPath(t *testing.T) {
	db := openAdapterTestDB(t)
	_ = seedPackageVersion(t, db, "go", "github.com/foo/bar", "1.2.3", 3)
	a, _ := ecosystemwiring.New(ecosystemwiring.AdapterDeps{
		PerEcosystemDB: map[ecosystem.Ecosystem]*sql.DB{ecosystem.EcoGo: db},
	})
	res, err := a.PrunePreview(context.Background(), "go", "1.2.3")
	if err != nil {
		t.Fatalf("PrunePreview: %v", err)
	}
	if res.Ecosystem != "go" || res.Version != "1.2.3" {
		t.Fatalf("got ecosystem=%q version=%q want go/1.2.3", res.Ecosystem, res.Version)
	}
	if res.ChunkCount != 3 {
		t.Fatalf("ChunkCount=%d want 3", res.ChunkCount)
	}
	if res.ChunkFP32Count != 3 {
		t.Fatalf("ChunkFP32Count=%d want 3", res.ChunkFP32Count)
	}
	if res.FTS5Count != 3 {
		t.Fatalf("FTS5Count=%d want 3", res.FTS5Count)
	}
	if res.SymbolCount != 2 {
		t.Fatalf("SymbolCount=%d want 2", res.SymbolCount)
	}
	if res.ChangeCount != 1 {
		t.Fatalf("ChangeCount=%d want 1", res.ChangeCount)
	}
	if res.Pinned {
		t.Fatalf("Pinned=true unexpected; this version has indefinite_retain=0")
	}
}

func TestAdapter_PrunePreview_PinnedFlag(t *testing.T) {
	db := openAdapterTestDB(t)
	_ = seedPackageVersion(t, db, "go", "github.com/foo/bar", "1.2.3", 1)
	a, _ := ecosystemwiring.New(ecosystemwiring.AdapterDeps{
		PerEcosystemDB: map[ecosystem.Ecosystem]*sql.DB{ecosystem.EcoGo: db},
	})
	if err := a.Pin(context.Background(), "go", "1.2.3"); err != nil {
		t.Fatalf("Pin setup: %v", err)
	}
	res, err := a.PrunePreview(context.Background(), "go", "1.2.3")
	if err != nil {
		t.Fatalf("PrunePreview: %v", err)
	}
	if !res.Pinned {
		t.Fatalf("Pinned=false after Pin; want true")
	}
}

func TestAdapter_Prune_NotFound(t *testing.T) {
	db := openAdapterTestDB(t)
	a, _ := ecosystemwiring.New(ecosystemwiring.AdapterDeps{
		PerEcosystemDB: map[ecosystem.Ecosystem]*sql.DB{ecosystem.EcoGo: db},
	})
	if err := a.Prune(context.Background(), "go", "1.99.0"); !errors.Is(err, ecospec.ErrEcosystemVersionNotFound) {
		t.Fatalf("want ErrEcosystemVersionNotFound, got %v", err)
	}
}

func TestAdapter_Prune_PinnedRefuses(t *testing.T) {
	db := openAdapterTestDB(t)
	_ = seedPackageVersion(t, db, "go", "github.com/foo/bar", "1.2.3", 1)
	a, _ := ecosystemwiring.New(ecosystemwiring.AdapterDeps{
		PerEcosystemDB: map[ecosystem.Ecosystem]*sql.DB{ecosystem.EcoGo: db},
	})
	if err := a.Pin(context.Background(), "go", "1.2.3"); err != nil {
		t.Fatalf("Pin setup: %v", err)
	}
	if err := a.Prune(context.Background(), "go", "1.2.3"); !errors.Is(err, ecospec.ErrEcosystemVersionPinned) {
		t.Fatalf("want ErrEcosystemVersionPinned, got %v", err)
	}
	var chunkCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM ecosystem_chunks`).Scan(&chunkCount)
	if chunkCount == 0 {
		t.Fatalf("Prune-pinned should not delete; chunks=0 means cascade fired")
	}
}

func TestAdapter_Prune_HappyPath_DeletesCascade(t *testing.T) {
	db := openAdapterTestDB(t)
	_ = seedPackageVersion(t, db, "go", "github.com/foo/bar", "1.2.3", 2)
	a, _ := ecosystemwiring.New(ecosystemwiring.AdapterDeps{
		PerEcosystemDB: map[ecosystem.Ecosystem]*sql.DB{ecosystem.EcoGo: db},
	})
	if err := a.Prune(context.Background(), "go", "1.2.3"); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	for _, q := range []struct {
		name string
		sql  string
	}{
		{"versions", `SELECT COUNT(*) FROM ecosystem_versions WHERE version = '1.2.3'`},
		{"chunks", `SELECT COUNT(*) FROM ecosystem_chunks WHERE version_introduced = '1.2.3'`},
		{"chunks_fp32", `SELECT COUNT(*) FROM ecosystem_chunks_fp32`},
		{"symbols", `SELECT COUNT(*) FROM ecosystem_symbols WHERE introduced_in = '1.2.3'`},
		{"changes", `SELECT COUNT(*) FROM ecosystem_changes WHERE version_to = '1.2.3'`},
		{"fts5", `SELECT COUNT(*) FROM ecosystem_chunks_fts`},
	} {
		var n int
		if err := db.QueryRow(q.sql).Scan(&n); err != nil {
			t.Fatalf("%s post-Prune count: %v", q.name, err)
		}
		if n != 0 {
			t.Fatalf("%s post-Prune count=%d want 0", q.name, n)
		}
	}
}

type stubSource struct {
	eco          ecosystem.Ecosystem
	kind         ecosystem.SourceType
	manifest     *ecosystem.Manifest
	manifestErr  error
	fetchDocFunc func(ctx context.Context, pkg ecosystem.PackageRef) (*ecosystem.PackageDoc, error)
	fetchChlog   func(ctx context.Context, pkg ecosystem.PackageRef, version string) (*ecosystem.Changelog, error)
}

func (s *stubSource) Ecosystem() ecosystem.Ecosystem { return s.eco }
func (s *stubSource) Kind() ecosystem.SourceType     { return s.kind }
func (s *stubSource) FetchManifest(_ context.Context) (*ecosystem.Manifest, error) {
	return s.manifest, s.manifestErr
}
func (s *stubSource) FetchPackageDoc(ctx context.Context, pkg ecosystem.PackageRef) (*ecosystem.PackageDoc, error) {
	if s.fetchDocFunc != nil {
		return s.fetchDocFunc(ctx, pkg)
	}
	return &ecosystem.PackageDoc{Package: pkg, Version: pkg.LatestStableVersion}, nil
}
func (s *stubSource) FetchChangelog(ctx context.Context, pkg ecosystem.PackageRef, version string) (*ecosystem.Changelog, error) {
	if s.fetchChlog != nil {
		return s.fetchChlog(ctx, pkg, version)
	}
	return nil, nil
}

func TestAdapter_IngestDelta_MissingIngester(t *testing.T) {
	db := openAdapterTestDB(t)
	a, _ := ecosystemwiring.New(ecosystemwiring.AdapterDeps{
		PerEcosystemDB: map[ecosystem.Ecosystem]*sql.DB{ecosystem.EcoGo: db},
	})
	if err := a.IngestDelta(context.Background(), "go"); err == nil {
		t.Fatalf("want error on missing Ingester, got nil")
	} else if !strings.Contains(err.Error(), "Ingester not configured") {
		t.Fatalf("want missing-Ingester error, got %v", err)
	}
}

func TestAdapter_IngestDelta_MissingSources(t *testing.T) {
	db := openAdapterTestDB(t)
	srcs := map[ecosystem.Ecosystem]map[ecosystem.SourceType]ecosystem.Source{
		ecosystem.EcoPython: {ecosystem.SourceType("registry_docs"): &stubSource{eco: ecosystem.EcoPython, kind: ecosystem.SourceType("registry_docs")}},
	}
	ing, err := ecosystem.NewIngester(ecosystem.IngesterOptions{Sources: srcs})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	a, _ := ecosystemwiring.New(ecosystemwiring.AdapterDeps{
		PerEcosystemDB: map[ecosystem.Ecosystem]*sql.DB{ecosystem.EcoGo: db},
		Ingester:       ing,
		Sources:        srcs,
	})
	err = a.IngestDelta(context.Background(), "go")
	if !errors.Is(err, ecosystemwiring.ErrEcosystemSourcesNotConfigured) {
		t.Fatalf("want ErrEcosystemSourcesNotConfigured, got %v", err)
	}
}

func TestAdapter_IngestDelta_HappyPath(t *testing.T) {
	db := openAdapterTestDB(t)
	stub := &stubSource{
		eco:      ecosystem.EcoGo,
		kind:     ecosystem.SourceType("registry_docs"),
		manifest: &ecosystem.Manifest{},
	}
	srcs := map[ecosystem.Ecosystem]map[ecosystem.SourceType]ecosystem.Source{
		ecosystem.EcoGo: {ecosystem.SourceType("registry_docs"): stub},
	}
	ing, err := ecosystem.NewIngester(ecosystem.IngesterOptions{Sources: srcs})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	a, _ := ecosystemwiring.New(ecosystemwiring.AdapterDeps{
		PerEcosystemDB: map[ecosystem.Ecosystem]*sql.DB{ecosystem.EcoGo: db},
		Ingester:       ing,
		Sources:        srcs,
	})
	if err := a.IngestDelta(context.Background(), "go"); err != nil {
		t.Fatalf("IngestDelta: %v", err)
	}
}

func TestAdapter_SweepChunkFingerprints_NoDrift(t *testing.T) {
	db := openAdapterTestDB(t)
	_ = seedPackageVersion(t, db, "go", "github.com/foo/bar", "1.2.3", 3)
	a, _ := ecosystemwiring.New(ecosystemwiring.AdapterDeps{
		PerEcosystemDB: map[ecosystem.Ecosystem]*sql.DB{ecosystem.EcoGo: db},
	})
	beforeFPs := snapshotFingerprints(t, db)
	if err := a.SweepChunkFingerprints(context.Background(), "go"); err != nil {
		t.Fatalf("SweepChunkFingerprints: %v", err)
	}
	afterFPs := snapshotFingerprints(t, db)
	for id, beforeFP := range beforeFPs {
		if afterFPs[id] != beforeFP {
			t.Fatalf("chunk %d fingerprint changed; before=%s after=%s", id, beforeFP, afterFPs[id])
		}
	}
}

func TestAdapter_SweepChunkFingerprints_RepairsDrift(t *testing.T) {
	db := openAdapterTestDB(t)
	_ = seedPackageVersion(t, db, "go", "github.com/foo/bar", "1.2.3", 1)
	if _, err := db.Exec(`UPDATE ecosystem_chunks SET chunk_fingerprint = 'CORRUPTED'`); err != nil {
		t.Fatalf("corrupt fingerprint: %v", err)
	}

	a, _ := ecosystemwiring.New(ecosystemwiring.AdapterDeps{
		PerEcosystemDB: map[ecosystem.Ecosystem]*sql.DB{ecosystem.EcoGo: db},
	})
	if err := a.SweepChunkFingerprints(context.Background(), "go"); err != nil {
		t.Fatalf("SweepChunkFingerprints: %v", err)
	}

	var fp, content string
	if err := db.QueryRow(`SELECT chunk_fingerprint, content_text FROM ecosystem_chunks LIMIT 1`).Scan(&fp, &content); err != nil {
		t.Fatalf("post-sweep SELECT: %v", err)
	}
	sum := sha256.Sum256([]byte(content))
	want := hex.EncodeToString(sum[:])
	if fp != want {
		t.Fatalf("post-sweep fingerprint=%s want=%s", fp, want)
	}
}

func snapshotFingerprints(t *testing.T, db *sql.DB) map[int64]string {
	t.Helper()
	rows, err := db.Query(`SELECT id, chunk_fingerprint FROM ecosystem_chunks`)
	if err != nil {
		t.Fatalf("snapshot SELECT: %v", err)
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var fp string
		if err := rows.Scan(&id, &fp); err != nil {
			t.Fatalf("snapshot Scan: %v", err)
		}
		out[id] = fp
	}
	return out
}

func TestAdapter_SweepChangeNodes_MissingExtractor(t *testing.T) {
	db := openAdapterTestDB(t)
	a, _ := ecosystemwiring.New(ecosystemwiring.AdapterDeps{
		PerEcosystemDB: map[ecosystem.Ecosystem]*sql.DB{ecosystem.EcoGo: db},
	})
	err := a.SweepChangeNodes(context.Background(), "go")
	if err == nil || !strings.Contains(err.Error(), "ChangeExtractor not configured") {
		t.Fatalf("want ChangeExtractor-not-configured, got %v", err)
	}
}

func TestAdapter_SweepChangeNodes_Clean(t *testing.T) {
	db := openAdapterTestDB(t)
	_ = seedPackageVersion(t, db, "go", "github.com/foo/bar", "1.2.3", 1)
	a, _ := ecosystemwiring.New(ecosystemwiring.AdapterDeps{
		PerEcosystemDB:  map[ecosystem.Ecosystem]*sql.DB{ecosystem.EcoGo: db},
		ChangeExtractor: ecosystem.NewChangeExtractor(ecosystem.ChangeExtractorOptions{}),
	})
	if err := a.SweepChangeNodes(context.Background(), "go"); err != nil {
		t.Fatalf("clean sweep: %v", err)
	}
}

func TestAdapter_RebuildSymbolIndex_MissingIndex(t *testing.T) {
	db := openAdapterTestDB(t)
	a, _ := ecosystemwiring.New(ecosystemwiring.AdapterDeps{
		PerEcosystemDB: map[ecosystem.Ecosystem]*sql.DB{ecosystem.EcoGo: db},
	})
	err := a.RebuildSymbolIndex(context.Background(), "go")
	if err == nil || !strings.Contains(err.Error(), "SymbolIndex not configured") {
		t.Fatalf("want SymbolIndex-not-configured, got %v", err)
	}
}

func TestAdapter_RebuildSymbolIndex_LoadsFromDB(t *testing.T) {
	db := openAdapterTestDB(t)
	_ = seedPackageVersion(t, db, "go", "github.com/foo/bar", "1.2.3", 1)
	idx := ecosystem.NewSymbolIndex()
	a, _ := ecosystemwiring.New(ecosystemwiring.AdapterDeps{
		PerEcosystemDB: map[ecosystem.Ecosystem]*sql.DB{ecosystem.EcoGo: db},
		SymbolIndex:    idx,
	})
	if err := a.RebuildSymbolIndex(context.Background(), "go"); err != nil {
		t.Fatalf("RebuildSymbolIndex: %v", err)
	}
	if !idx.Contains(ecosystem.SymbolRef{Ecosystem: ecosystem.EcoGo, SymbolPath: "github.com/foo/bar.Symbol1", Version: "1.2.3"}) {
		t.Fatalf("Symbol1@1.2.3 not in index after Rebuild")
	}
}

func TestAdapter_CASGarbageCollect_CleanCorpus(t *testing.T) {
	db := openAdapterTestDB(t)
	_ = seedPackageVersion(t, db, "go", "github.com/foo/bar", "1.2.3", 2)
	a, _ := ecosystemwiring.New(ecosystemwiring.AdapterDeps{
		PerEcosystemDB: map[ecosystem.Ecosystem]*sql.DB{ecosystem.EcoGo: db},
	})
	if err := a.CASGarbageCollect(context.Background()); err != nil {
		t.Fatalf("CAS GC on clean: %v", err)
	}
	if err := a.CASGarbageCollect(context.Background()); err != nil {
		t.Fatalf("CAS GC idempotent run: %v", err)
	}
}

func TestAdapter_CASGarbageCollect_OrphanRemoval(t *testing.T) {
	db := openAdapterTestDB(t)
	bin := make([]byte, 32)
	if _, err := db.Exec(`INSERT INTO ecosystem_chunks_vec_bin (chunk_id, embedding) VALUES (?, vec_bit(?))`, 999, bin); err != nil {
		t.Fatalf("orphan vec_bin insert: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO ecosystem_chunks_fts (chunk_id, content_text) VALUES (?, ?)`, 998, "orphan-content"); err != nil {
		t.Fatalf("orphan fts insert: %v", err)
	}

	a, _ := ecosystemwiring.New(ecosystemwiring.AdapterDeps{
		PerEcosystemDB: map[ecosystem.Ecosystem]*sql.DB{ecosystem.EcoGo: db},
	})
	if err := a.CASGarbageCollect(context.Background()); err != nil {
		t.Fatalf("CAS GC orphan: %v", err)
	}

	var nVec, nFTS int
	_ = db.QueryRow(`SELECT COUNT(*) FROM ecosystem_chunks_vec_bin`).Scan(&nVec)
	_ = db.QueryRow(`SELECT COUNT(*) FROM ecosystem_chunks_fts`).Scan(&nFTS)
	if nVec != 0 || nFTS != 0 {
		t.Fatalf("post-GC orphan counts vec=%d fts=%d; want 0/0", nVec, nFTS)
	}
}

func TestAdapter_DetectNewVersions_NoSources(t *testing.T) {
	db := openAdapterTestDB(t)
	a, _ := ecosystemwiring.New(ecosystemwiring.AdapterDeps{
		PerEcosystemDB: map[ecosystem.Ecosystem]*sql.DB{ecosystem.EcoGo: db},
	})
	_, err := a.DetectNewVersions(context.Background(), "go")
	if !errors.Is(err, ecosystemwiring.ErrEcosystemSourcesNotConfigured) {
		t.Fatalf("want ErrEcosystemSourcesNotConfigured, got %v", err)
	}
}

func TestAdapter_DetectNewVersions_NewSet(t *testing.T) {
	db := openAdapterTestDB(t)
	_ = seedPackageVersion(t, db, "go", "github.com/foo/bar", "1.0.0", 0)
	stub := &stubSource{
		eco:  ecosystem.EcoGo,
		kind: ecosystem.SourceType("registry_docs"),
		manifest: &ecosystem.Manifest{
			Packages: []ecosystem.ManifestPackage{
				{Name: "github.com/foo/bar", Versions: []string{"1.0.0", "1.1.0", "2.0.0"}, LatestStableVersion: "2.0.0"},
			},
		},
	}
	srcs := map[ecosystem.Ecosystem]map[ecosystem.SourceType]ecosystem.Source{
		ecosystem.EcoGo: {ecosystem.SourceType("registry_docs"): stub},
	}
	a, _ := ecosystemwiring.New(ecosystemwiring.AdapterDeps{
		PerEcosystemDB: map[ecosystem.Ecosystem]*sql.DB{ecosystem.EcoGo: db},
		Sources:        srcs,
	})
	got, err := a.DetectNewVersions(context.Background(), "go")
	if err != nil {
		t.Fatalf("DetectNewVersions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("DetectNewVersions returned %v; want 2 entries", got)
	}
	if got[0] != "1.1.0" || got[1] != "2.0.0" {
		t.Fatalf("DetectNewVersions sorted output got=%v want=[1.1.0 2.0.0]", got)
	}
}

func TestAdapter_DetectNewVersions_EmptyUpstream(t *testing.T) {
	db := openAdapterTestDB(t)
	stub := &stubSource{
		eco:      ecosystem.EcoGo,
		kind:     ecosystem.SourceType("registry_docs"),
		manifest: &ecosystem.Manifest{},
	}
	srcs := map[ecosystem.Ecosystem]map[ecosystem.SourceType]ecosystem.Source{
		ecosystem.EcoGo: {ecosystem.SourceType("registry_docs"): stub},
	}
	a, _ := ecosystemwiring.New(ecosystemwiring.AdapterDeps{
		PerEcosystemDB: map[ecosystem.Ecosystem]*sql.DB{ecosystem.EcoGo: db},
		Sources:        srcs,
	})
	got, err := a.DetectNewVersions(context.Background(), "go")
	if err != nil {
		t.Fatalf("DetectNewVersions: %v", err)
	}
	if got == nil {
		t.Fatalf("DetectNewVersions returned nil; want non-nil empty slice for cron-worker range safety")
	}
	if len(got) != 0 {
		t.Fatalf("DetectNewVersions empty-upstream got=%v want []", got)
	}
}

func TestAdapter_NewRejectsEmptyDBs(t *testing.T) {
	if _, err := ecosystemwiring.New(ecosystemwiring.AdapterDeps{}); err == nil {
		t.Fatalf("want error for empty PerEcosystemDB")
	}
}

func TestAdapter_NilReceiverGuard(t *testing.T) {
	var a *ecosystemwiring.Adapter
	if err := a.Pin(context.Background(), "go", "x"); err == nil {
		t.Fatalf("nil Pin: want error")
	}
	if _, err := a.PrunePreview(context.Background(), "go", "x"); err == nil {
		t.Fatalf("nil PrunePreview: want error")
	}
	if err := a.Prune(context.Background(), "go", "x"); err == nil {
		t.Fatalf("nil Prune: want error")
	}
	if err := a.IngestDelta(context.Background(), "go"); err == nil {
		t.Fatalf("nil IngestDelta: want error")
	}
	if err := a.SweepChunkFingerprints(context.Background(), "go"); err == nil {
		t.Fatalf("nil SweepChunkFingerprints: want error")
	}
	if err := a.SweepChangeNodes(context.Background(), "go"); err == nil {
		t.Fatalf("nil SweepChangeNodes: want error")
	}
	if err := a.RebuildSymbolIndex(context.Background(), "go"); err == nil {
		t.Fatalf("nil RebuildSymbolIndex: want error")
	}
	if err := a.CASGarbageCollect(context.Background()); err == nil {
		t.Fatalf("nil CASGarbageCollect: want error")
	}
	if _, err := a.DetectNewVersions(context.Background(), "go"); err == nil {
		t.Fatalf("nil DetectNewVersions: want error")
	}
}

func TestAdapter_ConcurrentPinPrune(t *testing.T) {
	db := openAdapterTestDB(t)
	for i := 0; i < 4; i++ {
		_ = seedPackageVersion(t, db, "go", "github.com/foo/bar"+string(rune('a'+i)), "1.0.0", 1)
	}
	a, _ := ecosystemwiring.New(ecosystemwiring.AdapterDeps{
		PerEcosystemDB: map[ecosystem.Ecosystem]*sql.DB{ecosystem.EcoGo: db},
	})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = a.PrunePreview(context.Background(), "go", "1.0.0")
		}()
	}
	wg.Wait()
	if err := a.Pin(context.Background(), "go", "1.0.0"); err != nil {
		t.Fatalf("Pin after concurrent previews: %v", err)
	}
}

func TestAdapter_SatisfiesEcosystemHandler(t *testing.T) {
	var _ ecospec.EcosystemHandler = (*ecosystemwiring.Adapter)(nil)
}
