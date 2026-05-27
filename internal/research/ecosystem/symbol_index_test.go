// go:build cgo
//go:build cgo
// +build cgo

// Package ecosystem — symbol_index_test.go
//
// Tests for SymbolIndex.
//
// Coverage discipline: per project doctrine `feedback_no_tech_debt.md`,
// security/correctness-critical files require ≥90% per-function coverage.
// SymbolIndex is the first-line hallucination defence (invariant enforces
// p99 ≤1ms via benchmark) so EVERY method is exercised here:
//
// - NewSymbolIndex TestSymbolIndex_Empty_NotContains
// - Register TestSymbolIndex_Register_Contains, _RegisterIdempotent
// - Contains (any-version) TestSymbolIndex_VersionFilter
// - Contains (version-filtered) TestSymbolIndex_VersionFilter
// - ContainsVersioned TestSymbolIndex_ContainsVersioned
// - Load TestSymbolIndex_Load_FromDB, _Load_NilDB
// - Load (atomic publish) TestSymbolIndex_Load_AtomicReplace
// - Load (ctx cancel) TestSymbolIndex_Load_CtxCancel
// - Load (versionless rows) TestSymbolIndex_Load_NullIntroducedIn
// - Rebuild TestSymbolIndex_Rebuild_Atomicity
// - Stats (loaded) TestSymbolIndex_Stats
// - Stats (unknown eco) TestSymbolIndex_Stats_Unknown
// - getOrCreateSet (double-check) TestSymbolIndex_Concurrent_RaceClean
//
// Drift reconciliation:
// - plan-file Step 2 used column `language` for the UNIQUE on
// ecosystem_packages (lines 3193, 3217, 3304). Real
// migration 001 + indexer_test.go use `ecosystem` (A-9 rename).
// All SQL in this file uses the canonical `ecosystem` column.
// - plan-file setupTestDBForSymbolIndex hand-wrote a minimal schema;
// this file uses ApplyMigrations(db) for parity with indexer_test
// (same schema the daemon actually runs against — no inline DDL
// drift surface).
//
// Build tag `cgo`: this file requires sqlite3 driver (mattn/go-sqlite3)
// transitively via ApplyMigrations + sqlite-vec; mirrors indexer_test.go.

package ecosystem

import (
	"context"
	"database/sql"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func setupSymbolIndexTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/symbol-index-test.db"
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

func insertSymbolPkg(t *testing.T, db *sql.DB, name string, eco Ecosystem) int64 {
	t.Helper()
	res, err := db.Exec(
		`INSERT INTO ecosystem_packages (name, ecosystem, upstream_url, canonical_namespace) VALUES (?, ?, ?, ?)`,
		name, string(eco), "https://example.test/"+name, name,
	)
	if err != nil {
		t.Fatalf("insert pkg %q: %v", name, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	return id
}

func insertSymbolRow(t *testing.T, db *sql.DB, pkgID int64, path, kind, introducedIn string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO ecosystem_symbols (package_id, symbol_path, kind, introduced_in) VALUES (?, ?, ?, ?)`,
		pkgID, path, kind, introducedIn,
	); err != nil {
		t.Fatalf("insert symbol %q@%q: %v", path, introducedIn, err)
	}
}

func TestSymbolIndex_Empty_NotContains(t *testing.T) {
	t.Parallel()
	idx := NewSymbolIndex()
	if idx.Contains(SymbolRef{Ecosystem: EcoGo, SymbolPath: "x", Version: "1.0"}) {
		t.Error("fresh SymbolIndex Contains returned true; want false")
	}

	if idx.Contains(SymbolRef{Ecosystem: EcoPython, SymbolPath: "x", Version: ""}) {
		t.Error("fresh SymbolIndex cross-eco Contains(no-version) returned true")
	}
}

func TestSymbolIndex_Register_Contains(t *testing.T) {
	t.Parallel()
	idx := NewSymbolIndex()
	idx.Register(EcoGo, "crypto/sha256.Sum256", "1.23")
	if !idx.Contains(SymbolRef{Ecosystem: EcoGo, SymbolPath: "crypto/sha256.Sum256", Version: "1.23"}) {
		t.Error("Contains after Register returned false; want true")
	}
}

func TestSymbolIndex_VersionFilter(t *testing.T) {
	t.Parallel()
	idx := NewSymbolIndex()
	idx.Register(EcoGo, "fmt.Println", "1.23")

	if !idx.Contains(SymbolRef{Ecosystem: EcoGo, SymbolPath: "fmt.Println", Version: "1.23"}) {
		t.Error("Contains(go,fmt.Println,1.23) false; want true (exact match)")
	}
	if idx.Contains(SymbolRef{Ecosystem: EcoGo, SymbolPath: "fmt.Println", Version: "1.99"}) {
		t.Error("Contains(go,fmt.Println,1.99) true; want false (wrong version)")
	}
	if !idx.Contains(SymbolRef{Ecosystem: EcoGo, SymbolPath: "fmt.Println", Version: ""}) {
		t.Error("Contains(go,fmt.Println,\"\") false; want true (any-version match)")
	}
	if idx.Contains(SymbolRef{Ecosystem: EcoPython, SymbolPath: "fmt.Println", Version: ""}) {
		t.Error("Contains(python,fmt.Println,\"\") true; want false (cross-ecosystem isolation)")
	}
}

func TestSymbolIndex_ContainsVersioned(t *testing.T) {
	t.Parallel()
	idx := NewSymbolIndex()
	idx.Register(EcoGo, "x", "1")

	if !idx.ContainsVersioned(EcoGo, "x", "1") {
		t.Error("ContainsVersioned(go,x,1) false; want true")
	}
	if idx.ContainsVersioned(EcoGo, "x", "2") {
		t.Error("ContainsVersioned(go,x,2) true; want false (wrong version)")
	}

	if idx.ContainsVersioned(EcoGo, "x", "") {
		t.Error("ContainsVersioned(go,x,\"\") true; want false (empty version rejected)")
	}

	if idx.ContainsVersioned(EcoPython, "x", "1") {
		t.Error("ContainsVersioned(python,x,1) true; want false (cross-ecosystem)")
	}
}

func TestSymbolIndex_Load_FromDB(t *testing.T) {
	t.Parallel()
	db := setupSymbolIndexTestDB(t)
	pkgID := insertSymbolPkg(t, db, "crypto/sha256", EcoGo)
	insertSymbolRow(t, db, pkgID, "crypto/sha256.Sum256", "function", "1.23")
	insertSymbolRow(t, db, pkgID, "crypto/sha256.SHA256", "type", "1.23")

	idx := NewSymbolIndex()
	if err := idx.Load(context.Background(), db, EcoGo); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !idx.Contains(SymbolRef{Ecosystem: EcoGo, SymbolPath: "crypto/sha256.Sum256", Version: "1.23"}) {
		t.Error("Sum256 not loaded; Load did not populate any-set + versions overlay")
	}
	if !idx.Contains(SymbolRef{Ecosystem: EcoGo, SymbolPath: "crypto/sha256.SHA256", Version: "1.23"}) {
		t.Error("SHA256 not loaded")
	}

	if idx.Contains(SymbolRef{Ecosystem: EcoPython, SymbolPath: "crypto/sha256.Sum256", Version: ""}) {
		t.Error("Load(EcoGo) leaked into EcoPython namespace")
	}
}

func TestSymbolIndex_Load_NilDB(t *testing.T) {
	t.Parallel()
	idx := NewSymbolIndex()
	err := idx.Load(context.Background(), nil, EcoGo)
	if err == nil {
		t.Fatal("Load(nil db) returned nil error; want defensive error")
	}
}

func TestSymbolIndex_Load_AtomicReplace(t *testing.T) {
	t.Parallel()
	db := setupSymbolIndexTestDB(t)
	pkgID := insertSymbolPkg(t, db, "pkg1", EcoGo)
	insertSymbolRow(t, db, pkgID, "pkg1.OldSym", "function", "1")

	idx := NewSymbolIndex()
	if err := idx.Load(context.Background(), db, EcoGo); err != nil {
		t.Fatalf("Load #1: %v", err)
	}
	if !idx.Contains(SymbolRef{Ecosystem: EcoGo, SymbolPath: "pkg1.OldSym", Version: "1"}) {
		t.Fatal("OldSym missing after Load #1")
	}

	if _, err := db.Exec(`DELETE FROM ecosystem_symbols WHERE symbol_path = ?`, "pkg1.OldSym"); err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	insertSymbolRow(t, db, pkgID, "pkg1.NewSym", "function", "2")

	if err := idx.Load(context.Background(), db, EcoGo); err != nil {
		t.Fatalf("Load #2: %v", err)
	}
	if idx.Contains(SymbolRef{Ecosystem: EcoGo, SymbolPath: "pkg1.OldSym", Version: "1"}) {
		t.Error("OldSym leaked after atomic replace; Load did not drop prior state")
	}
	if !idx.Contains(SymbolRef{Ecosystem: EcoGo, SymbolPath: "pkg1.NewSym", Version: "2"}) {
		t.Error("NewSym missing after Load #2")
	}
}

func TestSymbolIndex_Load_CtxCancel(t *testing.T) {
	t.Parallel()
	db := setupSymbolIndexTestDB(t)
	pkgID := insertSymbolPkg(t, db, "pkg-ctx", EcoGo)
	for i := 0; i < 50; i++ {
		insertSymbolRow(t, db, pkgID, "pkg-ctx.Sym"+strconv.Itoa(i), "function", "1")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	idx := NewSymbolIndex()
	err := idx.Load(ctx, db, EcoGo)
	if err == nil {
		t.Fatal("Load with pre-cancelled ctx returned nil; want ctx.Err propagation")
	}
}

func TestSymbolIndex_Load_NullIntroducedIn(t *testing.T) {
	t.Parallel()
	db := setupSymbolIndexTestDB(t)
	pkgID := insertSymbolPkg(t, db, "pkg-null", EcoGo)

	if _, err := db.Exec(
		`INSERT INTO ecosystem_symbols (package_id, symbol_path, kind, introduced_in) VALUES (?, ?, ?, NULL)`,
		pkgID, "pkg-null.Sym", "function",
	); err != nil {
		t.Fatalf("insert NULL row: %v", err)
	}

	idx := NewSymbolIndex()
	if err := idx.Load(context.Background(), db, EcoGo); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !idx.Contains(SymbolRef{Ecosystem: EcoGo, SymbolPath: "pkg-null.Sym", Version: ""}) {
		t.Error("NULL-version row not findable via any-version Contains")
	}

	if idx.ContainsVersioned(EcoGo, "pkg-null.Sym", "1") {
		t.Error("ContainsVersioned matched against NULL introduced_in; want false")
	}
}

func TestSymbolIndex_Rebuild_Atomicity(t *testing.T) {
	t.Parallel()
	db := setupSymbolIndexTestDB(t)
	pkgID := insertSymbolPkg(t, db, "pkg-rebuild", EcoGo)
	insertSymbolRow(t, db, pkgID, "pkg-rebuild.Old", "function", "1")

	idx := NewSymbolIndex()
	if err := idx.Load(context.Background(), db, EcoGo); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !idx.Contains(SymbolRef{Ecosystem: EcoGo, SymbolPath: "pkg-rebuild.Old", Version: "1"}) {
		t.Fatal("Old missing after initial Load")
	}

	if _, err := db.Exec(`DELETE FROM ecosystem_symbols WHERE symbol_path = ?`, "pkg-rebuild.Old"); err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	insertSymbolRow(t, db, pkgID, "pkg-rebuild.New", "function", "2")

	if err := idx.Rebuild(context.Background(), db, EcoGo); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if idx.Contains(SymbolRef{Ecosystem: EcoGo, SymbolPath: "pkg-rebuild.Old", Version: "1"}) {
		t.Error("Old leaked after Rebuild; atomic replace contract violated")
	}
	if !idx.Contains(SymbolRef{Ecosystem: EcoGo, SymbolPath: "pkg-rebuild.New", Version: "2"}) {
		t.Error("New missing after Rebuild")
	}
}

func TestSymbolIndex_Concurrent_RaceClean(t *testing.T) {
	t.Parallel()
	idx := NewSymbolIndex()
	const N = 100
	const W = 8

	var wg sync.WaitGroup
	wg.Add(W)
	for w := 0; w < W; w++ {
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < N; i++ {
				path := "pkg.Sym" + strconv.Itoa(i)
				idx.Register(EcoGo, path, "1.0")
				_ = idx.Contains(SymbolRef{Ecosystem: EcoGo, SymbolPath: path, Version: "1.0"})
			}
		}(w)
	}
	wg.Wait()

	for i := 0; i < N; i++ {
		path := "pkg.Sym" + strconv.Itoa(i)
		if !idx.Contains(SymbolRef{Ecosystem: EcoGo, SymbolPath: path, Version: "1.0"}) {
			t.Errorf("post-concurrent Contains(%s) false", path)
		}
	}

	stats := idx.Stats(EcoGo)
	if stats.TotalSymbols != N {
		t.Errorf("TotalSymbols=%d after %d writers × %d symbols; want %d (idempotent)", stats.TotalSymbols, W, N, N)
	}
}

func TestSymbolIndex_Stats(t *testing.T) {
	t.Parallel()
	idx := NewSymbolIndex()
	idx.Register(EcoGo, "a", "1")
	idx.Register(EcoGo, "b", "1")
	idx.Register(EcoGo, "a", "2")

	stats := idx.Stats(EcoGo)
	if stats.Ecosystem != EcoGo {
		t.Errorf("Ecosystem=%q; want %q", stats.Ecosystem, EcoGo)
	}
	if stats.TotalSymbols != 2 {
		t.Errorf("TotalSymbols=%d; want 2", stats.TotalSymbols)
	}

	if stats.TotalVersions != 3 {
		t.Errorf("TotalVersions=%d; want 3", stats.TotalVersions)
	}
}

// TestSymbolIndex_Stats_Unknown asserts the unknown-ecosystem path
// returns a zero-value Stats struct (with Ecosystem populated) rather
// than panicking. Doctor command MUST stay panic-free even if a
// future ecosystem hasn't been loaded.
func TestSymbolIndex_Stats_Unknown(t *testing.T) {
	t.Parallel()
	idx := NewSymbolIndex()
	stats := idx.Stats(EcoRust)
	if stats.Ecosystem != EcoRust {
		t.Errorf("Ecosystem=%q; want %q", stats.Ecosystem, EcoRust)
	}
	if stats.TotalSymbols != 0 || stats.TotalVersions != 0 {
		t.Errorf("unknown-eco Stats=%+v; want zeroed counts", stats)
	}
}

func TestSymbolIndex_RegisterIdempotent(t *testing.T) {
	t.Parallel()
	idx := NewSymbolIndex()
	idx.Register(EcoGo, "x", "1")
	idx.Register(EcoGo, "x", "1")
	stats := idx.Stats(EcoGo)
	if stats.TotalSymbols != 1 {
		t.Errorf("TotalSymbols=%d after duplicate Register; want 1 (idempotent)", stats.TotalSymbols)
	}
	if stats.TotalVersions != 1 {
		t.Errorf("TotalVersions=%d after duplicate Register; want 1 (idempotent)", stats.TotalVersions)
	}
}

const (
	invZen195SampleN    = 10_000
	invZen195CeilingP99 = 1 * time.Millisecond
	invZen195P99Index   = 9900
)

func sortLatencies(d []time.Duration) {
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
}

func TestSymbolIndex_InvZen195_P99Cold10k(t *testing.T) {
	t.Parallel()
	idx := NewSymbolIndex()
	for i := 0; i < invZen195SampleN; i++ {
		idx.Register(EcoGo, "pkg.Sym"+strconv.Itoa(i), "1.0")
	}

	latencies := make([]time.Duration, invZen195SampleN)
	for i := 0; i < invZen195SampleN; i++ {
		path := "pkg.Sym" + strconv.Itoa(i)
		start := time.Now()
		_ = idx.Contains(SymbolRef{Ecosystem: EcoGo, SymbolPath: path, Version: "1.0"})
		latencies[i] = time.Since(start)
	}
	sortLatencies(latencies)
	p99 := latencies[invZen195P99Index]
	t.Logf("inv-zen-195 cold p99 (N=%d) = %v (ceiling %v)", invZen195SampleN, p99, invZen195CeilingP99)
	if p99 > invZen195CeilingP99 {
		t.Errorf("inv-zen-195 violation (cold): p99 = %v > %v", p99, invZen195CeilingP99)
	}
}

func TestSymbolIndex_InvZen195_P99Parallel10k(t *testing.T) {
	t.Parallel()
	idx := NewSymbolIndex()
	for i := 0; i < invZen195SampleN; i++ {
		idx.Register(EcoGo, "pkg.Sym"+strconv.Itoa(i), "1.0")
	}

	const W = 8
	perWorker := invZen195SampleN / W

	allLatencies := make([]time.Duration, invZen195SampleN)
	var idxCounter int64

	var wg sync.WaitGroup
	wg.Add(W)
	for w := 0; w < W; w++ {
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				path := "pkg.Sym" + strconv.Itoa((worker*perWorker+i)%invZen195SampleN)
				start := time.Now()
				_ = idx.Contains(SymbolRef{Ecosystem: EcoGo, SymbolPath: path, Version: "1.0"})
				d := time.Since(start)
				pos := atomic.AddInt64(&idxCounter, 1) - 1
				allLatencies[pos] = d
			}
		}(w)
	}
	wg.Wait()

	written := int(atomic.LoadInt64(&idxCounter))
	if written != invZen195SampleN {
		t.Fatalf("recorded %d latencies; want %d", written, invZen195SampleN)
	}
	sortLatencies(allLatencies)
	p99 := allLatencies[invZen195P99Index]
	t.Logf("inv-zen-195 parallel p99 (N=%d, W=%d) = %v (ceiling %v)", invZen195SampleN, W, p99, invZen195CeilingP99)
	if p99 > invZen195CeilingP99 {
		t.Errorf("inv-zen-195 violation (parallel): p99 = %v > %v", p99, invZen195CeilingP99)
	}
}

func TestSymbolIndex_Load_QueryError(t *testing.T) {
	t.Parallel()
	db := setupSymbolIndexTestDB(t)

	if _, err := db.Exec(`DROP TABLE ecosystem_packages`); err != nil {
		t.Fatalf("DROP: %v", err)
	}
	idx := NewSymbolIndex()
	err := idx.Load(context.Background(), db, EcoGo)
	if err == nil {
		t.Fatal("Load against missing table returned nil; want query error")
	}
	if !strings.Contains(err.Error(), "go") {
		t.Errorf("error %q missing eco name; operator log loses context", err)
	}
}

func TestSymbolIndex_Load_RowsErr(t *testing.T) {
	t.Parallel()
	db := setupSymbolIndexTestDB(t)
	pkgID := insertSymbolPkg(t, db, "pkg-rows-err", EcoGo)
	for i := 0; i < 200; i++ {
		insertSymbolRow(t, db, pkgID, "pkg-rows-err.Sym"+strconv.Itoa(i), "function", "1")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()

	time.Sleep(10 * time.Microsecond)
	idx := NewSymbolIndex()
	err := idx.Load(ctx, db, EcoGo)

	if err == nil {
		t.Fatal("Load with cancelled ctx returned nil; want ctx propagation")
	}
}

func TestSymbolIndex_Load_InnerLoopCtxCancel(t *testing.T) {
	t.Parallel()
	db := setupSymbolIndexTestDB(t)
	pkgID := insertSymbolPkg(t, db, "pkg-mid-cancel", EcoGo)
	const Rows = 5000
	for i := 0; i < Rows; i++ {
		insertSymbolRow(t, db, pkgID, "pkg-mid-cancel.Sym"+strconv.Itoa(i), "function", "1")
	}

	ctx, cancel := context.WithCancel(context.Background())
	idx := NewSymbolIndex()

	go func() {
		time.Sleep(100 * time.Microsecond)
		cancel()
	}()
	err := idx.Load(ctx, db, EcoGo)

	if err == nil {
		t.Fatal("Load with mid-iteration cancel returned nil; want ctx propagation")
	}
}

func TestSymbolIndex_GetOrCreateSet_DoubleCheckRace(t *testing.T) {

	idx := NewSymbolIndex()
	const W = 64

	for _, eco := range []Ecosystem{EcoRust, EcoTypeScript} {
		eco := eco
		var ready, start sync.WaitGroup
		ready.Add(W)
		start.Add(1)
		var done sync.WaitGroup
		done.Add(W)
		for w := 0; w < W; w++ {
			go func(worker int) {
				defer done.Done()
				ready.Done()
				start.Wait()
				idx.Register(eco, "pkg.RaceSym"+strconv.Itoa(worker), "1.0")
			}(w)
		}
		ready.Wait()
		start.Done()
		done.Wait()

		stats := idx.Stats(eco)
		if stats.TotalSymbols != W {
			t.Errorf("eco=%s TotalSymbols=%d after %d racing workers; want %d", eco, stats.TotalSymbols, W, W)
		}
	}
}

func TestSymbolIndex_Register_EmptyVersion(t *testing.T) {
	t.Parallel()
	idx := NewSymbolIndex()
	idx.Register(EcoGo, "no-ver.Sym", "")

	if !idx.Contains(SymbolRef{Ecosystem: EcoGo, SymbolPath: "no-ver.Sym", Version: ""}) {
		t.Error("Contains(no-ver,any) false; Register with empty version did not populate anySet")
	}
	if idx.ContainsVersioned(EcoGo, "no-ver.Sym", "1") {
		t.Error("ContainsVersioned(no-ver,1) true; empty-version Register leaked into version overlay")
	}
	stats := idx.Stats(EcoGo)
	if stats.TotalVersions != 0 {
		t.Errorf("TotalVersions=%d after empty-version Register; want 0", stats.TotalVersions)
	}
}
