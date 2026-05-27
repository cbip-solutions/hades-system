// go:build integration && cgo

package ecosystem_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"database/sql"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

func openEcosystemDBForCrossPackage(t *testing.T) *sql.DB {
	t.Helper()
	sqlite_vec.Auto()
	dir := t.TempDir()
	db, err := sql.Open("sqlite3",
		filepath.Join(dir, "cross.db")+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := ecosystem.ApplyMigrations(db); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}
	return db
}

func buildDeterministicChunk(seed int, version string, kind ecosystem.ChunkKind) ecosystem.Chunk {
	content := fmt.Sprintf("seed-%d sample documentation text", seed)
	bin := make([]byte, 32)
	for i := range bin {
		bin[i] = byte((seed*13 + i*7) % 256)
	}
	fp32 := make([]float32, 1536)
	for i := range fp32 {
		fp32[i] = float32(((seed+i)%17)+1) * 0.0125
	}
	sum := sha256.Sum256([]byte(content))
	return ecosystem.Chunk{
		VersionIntroduced:   version,
		StableIn:            []string{version},
		ContentText:         content,
		ContextualPrefix:    fmt.Sprintf("seed %d prefix", seed),
		Fingerprint:         "sha256:" + hex.EncodeToString(sum[:]),
		Kind:                kind,
		SourceType:          ecosystem.SrcPackageDoc,
		EmbeddingBin256d:    bin,
		EmbeddingFP32_1536d: fp32,
	}
}

func TestEcosystemIntegration_IndexerEmitsAuditChainRow(t *testing.T) {
	db := openEcosystemDBForCrossPackage(t)
	chain := ecosystem.NewInMemoryRAGAuditChain()

	idx, err := ecosystem.NewIndexer(ecosystem.IndexerOptions{
		DB:       db,
		Chain:    chain,
		Doctrine: "default",
	})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}

	pkg := ecosystem.PackageRef{
		Ecosystem:           ecosystem.EcoGo,
		Name:                "io",
		CanonicalNamespace:  "io",
		UpstreamURL:         "https://pkg.go.dev/io",
		LatestStableVersion: "1.23.0",
	}
	chunks := []ecosystem.Chunk{
		buildDeterministicChunk(1, "1.23.0", ecosystem.KindFunction),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := idx.WriteChunks(ctx, pkg, "1.23.0", chunks, nil, nil); err != nil {
		t.Fatalf("WriteChunks: %v", err)
	}

	if got, want := chain.Len(), 1; got != want {
		t.Fatalf("chain.Len = %d, want %d (Indexer.WriteChunks must emit exactly one audit row)", got, want)
	}

	rec := chain.Get(1)
	if rec == nil {
		t.Fatalf("chain.Get(1) returned nil; expected the single appended record")
	}

	if rec.EventType != eventlog.EvtRAGIngestPackage {
		t.Errorf("chain row EventType = %d, want EvtRAGIngestPackage (=%d)",
			rec.EventType, eventlog.EvtRAGIngestPackage)
	}

	if rec.ParentHash != "" {
		t.Errorf("first-record ParentHash = %q, want \"\" (genesis row)", rec.ParentHash)
	}
	if rec.SelfHash == "" {
		t.Error("first-record SelfHash empty; chain hash formula must populate it")
	}
	if rec.Doctrine != "default" {
		t.Errorf("Doctrine = %q, want %q (passed via IndexerOptions.Doctrine)", rec.Doctrine, "default")
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Payload, &payload); err != nil {
		t.Fatalf("payload not valid JSON: %v (raw=%q)", err, string(rec.Payload))
	}
}

func TestEcosystemIntegration_MultiIngestPreservesChainOrder(t *testing.T) {
	db := openEcosystemDBForCrossPackage(t)
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
		Name:                "net/http",
		CanonicalNamespace:  "net/http",
		UpstreamURL:         "https://pkg.go.dev/net/http",
		LatestStableVersion: "1.23.0",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const writes = 3
	for i := 0; i < writes; i++ {

		ver := fmt.Sprintf("1.%d.0", 20+i)
		chunks := []ecosystem.Chunk{buildDeterministicChunk(100+i, ver, ecosystem.KindGuide)}
		if err := idx.WriteChunks(ctx, pkg, ver, chunks, nil, nil); err != nil {
			t.Fatalf("write #%d: %v", i, err)
		}
	}

	if got, want := chain.Len(), writes; got != want {
		t.Fatalf("chain.Len = %d, want %d (one row per WriteChunks)", got, want)
	}

	seqs := chain.AllSeqs()
	for i, seq := range seqs {
		if seq != int64(i+1) {
			t.Errorf("AllSeqs[%d] = %d, want %d (canonical monotonic order)", i, seq, i+1)
		}
	}

	for i := int64(1); i <= int64(writes); i++ {
		rec := chain.Get(i)
		if rec == nil {
			t.Errorf("chain.Get(%d) returned nil", i)
			continue
		}
		if rec.Doctrine != "max-scope" {
			t.Errorf("Get(%d).Doctrine = %q, want %q (IndexerOptions.Doctrine propagation)",
				i, rec.Doctrine, "max-scope")
		}
		if rec.EventType != eventlog.EvtRAGIngestPackage {
			t.Errorf("Get(%d).EventType = %d, want EvtRAGIngestPackage (=%d)",
				i, rec.EventType, eventlog.EvtRAGIngestPackage)
		}
	}
}

type symbolIndexLookupAdapter struct {
	idx *ecosystem.SymbolIndex
}

func (a *symbolIndexLookupAdapter) Contains(eco ecosystem.Ecosystem, symbolPath, version string) (string, bool) {
	ok := a.idx.Contains(ecosystem.SymbolRef{
		Ecosystem:  eco,
		SymbolPath: symbolPath,
		Version:    version,
	})

	return "", ok
}

// TestEcosystemIntegration_VerifierAbstainsOnFakeSymbol exercises the
// (Verifier × SymbolIndex) wiring across two package files. The
// production capa-firewall contract (verifier.go + abstention.go §
// "RefuseOnUnverified") says fake symbols MUST surface as Exists=false
// — never confabulate.
//
// Cross-package signal:
// - SymbolIndex (symbol_index.go) Register the REAL symbol
// - symbolIndexLookupAdapter (this file) bridges to SymbolIndexLookup
// - Verifier.Verify (verifier.go) cascade resolves real symbol via stage A
// (SymbolIndex hit), resolves fake symbol via stage A miss + no stage C
// runner → Exists=false with Source=skipped
//
// Drift: a regression in Verifier.verifyOne that silently treats "not in
// index" as Exists=true (the classic hallucination bug) surfaces here.
func TestEcosystemIntegration_VerifierAbstainsOnFakeSymbol(t *testing.T) {
	idx := ecosystem.NewSymbolIndex()

	idx.Register(ecosystem.EcoGo, "fmt.Println", "1.23.0")

	verifier, err := ecosystem.NewVerifier(ecosystem.VerifierConfig{
		SymbolIndex: &symbolIndexLookupAdapter{idx: idx},

		LiveCmdRunner: nil,
	})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	refs := []ecosystem.SymbolRef{
		{Ecosystem: ecosystem.EcoGo, SymbolPath: "fmt.Println", Version: "1.23.0"},
		{Ecosystem: ecosystem.EcoGo, SymbolPath: "fmt.ConfabulatedFn", Version: "1.23.0"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res, err := verifier.Verify(ctx, refs)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(res.Verifications) != len(refs) {
		t.Fatalf("Verifications len = %d, want %d", len(res.Verifications), len(refs))
	}

	if !res.Verifications[0].Exists {
		t.Errorf("real symbol fmt.Println: Exists=%v, want true (stage A SymbolIndex hit)",
			res.Verifications[0].Exists)
	}

	if res.Verifications[1].Exists {
		t.Errorf("fake symbol fmt.ConfabulatedFn: Exists=true (CONFABULATION) — capa-firewall must abstain")
	}

	if res.AllVerified {
		t.Error("AllVerified=true with a fake symbol present; capa-firewall contract violated")
	}
}

type budgetFixedSizer struct{ bytes int64 }

func (b *budgetFixedSizer) TotalBytes(_ context.Context) (int64, error) { return b.bytes, nil }

type budgetGatedIngester struct {
	monitor       *ecosystem.BudgetMonitor
	dispatched    int
	blockedReason string
}

func (b *budgetGatedIngester) IngestDelta(ctx context.Context, _eco string, _ver string) error {
	status, err := b.monitor.Check(ctx)
	if err != nil {
		return fmt.Errorf("budget check: %w", err)
	}
	if status.BlockNewIngest {
		b.blockedReason = status.State.String()
		return nil
	}
	b.dispatched++
	return nil
}

// TestEcosystemIntegration_BudgetGatesCronIngestDispatch wires
// (ecosystem.BudgetMonitor × budgetGatedIngester façade × cronWorkerFacade)
// across two package boundaries. Validates the production handoff:
// Overflow status MUST block IngestDelta dispatch — the
// cron worker may NOT silently fan out 4 IngestDelta calls when storage
// is at ≥150% of target.
//
// Cross-package signal: the cron worker (cmd/zen-docs-cron) consumes the
// research/ecosystem BudgetMonitor; this test proves the gate API
// surface (status.BlockNewIngest / status.State) is the SAME shape the
// cron worker reads at the production boundary.
func TestEcosystemIntegration_BudgetGatesCronIngestDispatch(t *testing.T) {
	const targetGB = 40.0
	const ceilingGB = 60.0

	tests := []struct {
		name               string
		sizeBytes          int64
		expectDispatched   int
		expectBlockedState string
	}{
		{
			name:               "green_allows_dispatch",
			sizeBytes:          int64(5 * (1 << 30)),
			expectDispatched:   1,
			expectBlockedState: "",
		},
		{
			name:               "red_blocks_dispatch",
			sizeBytes:          int64(50 * (1 << 30)),
			expectDispatched:   0,
			expectBlockedState: "red",
		},
		{
			name:               "overflow_blocks_dispatch",
			sizeBytes:          int64(65 * (1 << 30)),
			expectDispatched:   0,
			expectBlockedState: "overflow",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			monitor := ecosystem.NewBudgetMonitor(ecosystem.BudgetMonitorConfig{
				TargetGB:  targetGB,
				CeilingGB: ceilingGB,
				Sizer:     &budgetFixedSizer{bytes: tc.sizeBytes},
			})
			gated := &budgetGatedIngester{monitor: monitor}

			if err := gated.IngestDelta(context.Background(), "go", "1.23.0"); err != nil {
				t.Fatalf("IngestDelta: %v", err)
			}

			if got, want := gated.dispatched, tc.expectDispatched; got != want {
				t.Errorf("dispatched = %d, want %d (budget gate must honour status.BlockNewIngest)",
					got, want)
			}
			if got, want := gated.blockedReason, tc.expectBlockedState; got != want {
				t.Errorf("blockedReason = %q, want %q", got, want)
			}
		})
	}
}

func TestEcosystemIntegration_BudgetMonitorTransitionsAcrossThresholds(t *testing.T) {
	const targetGB = 40.0
	const ceilingGB = 60.0
	yellowGB := targetGB * 0.85

	cases := []struct {
		name         string
		gb           float64
		wantState    string
		wantBlockNew bool
		wantBlockAll bool
	}{
		{"green", 10.0, "green", false, false},
		{"yellow", yellowGB, "yellow", false, false},
		{"red", 50.0, "red", true, false},
		{"overflow", 65.0, "overflow", true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			monitor := ecosystem.NewBudgetMonitor(ecosystem.BudgetMonitorConfig{
				TargetGB:  targetGB,
				CeilingGB: ceilingGB,
				Sizer:     &budgetFixedSizer{bytes: int64(c.gb * float64(1<<30))},
			})
			status, err := monitor.Check(context.Background())
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if got := status.State.String(); got != c.wantState {
				t.Errorf("state = %q, want %q", got, c.wantState)
			}
			if status.BlockNewIngest != c.wantBlockNew {
				t.Errorf("BlockNewIngest = %v, want %v", status.BlockNewIngest, c.wantBlockNew)
			}
			if status.BlockAllWrites != c.wantBlockAll {
				t.Errorf("BlockAllWrites = %v, want %v", status.BlockAllWrites, c.wantBlockAll)
			}
		})
	}
}
