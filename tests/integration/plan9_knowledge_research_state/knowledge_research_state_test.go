//go:build integration && cgo
// +build integration,cgo

package plan9_knowledge_research_state

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/knowledge/aggregator"
	"github.com/cbip-solutions/hades-system/internal/knowledge/embed"
	"github.com/cbip-solutions/hades-system/internal/knowledge/knowledgetypes"
	"github.com/cbip-solutions/hades-system/internal/research/cache"
	"github.com/cbip-solutions/hades-system/internal/state/manifest"
	"github.com/cbip-solutions/hades-system/tests/testhelpers/researchmcpmock"
)

type noopSink struct{}

func (noopSink) Emit(_ context.Context, _ string, _ []byte) error { return nil }

type recordingChain struct {
	mu    sync.Mutex
	calls int
}

func (r *recordingChain) ComputeAnchor(_ context.Context, eventID, _ string, _ []byte, createdAt time.Time) (string, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	partition := createdAt.UTC().Format("2006_01")
	return fmt.Sprintf("%s:%s:rec%08x", partition, eventID, r.calls), nil
}

type stubVaultStore struct {
	mu      sync.Mutex
	vaultDB *sql.DB
	anchors []struct{ project, note, anchor string }
}

func (s *stubVaultStore) ListAuthorizedProjects(_ context.Context) ([]knowledgetypes.ProjectHandle, error) {
	return []knowledgetypes.ProjectHandle{
		{ProjectID: "proj-A", Alias: "project-A", VaultPath: "/vault/proj-A"},
	}, nil
}

func (s *stubVaultStore) OpenProjectVault(_ context.Context, _ string) (knowledgetypes.ProjectVault, error) {
	return s.vaultDB, nil
}

func (s *stubVaultStore) UpdateAuditChainAnchor(_ context.Context, project, note, anchor string) error {
	s.mu.Lock()
	s.anchors = append(s.anchors, struct{ project, note, anchor string }{project, note, anchor})
	s.mu.Unlock()
	return nil
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)

	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file))))
}

func TestPlan9_KnowledgeResearchStateE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped under -short")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	tmp := t.TempDir()

	const projectID = "proj-A"

	pinPath := filepath.Join(tmp, "aggregator-pin.db")
	pinDB, err := aggregator.Open(ctx, pinPath)
	if err != nil {
		t.Fatalf("aggregator.Open pin: %v", err)
	}
	if err := aggregator.Init(ctx, pinDB); err != nil {
		t.Fatalf("aggregator.Init pin: %v", err)
	}
	t.Cleanup(func() { _ = pinDB.Close() })

	vaultPath := filepath.Join(tmp, "vault.db")
	vaultDB, err := aggregator.Open(ctx, vaultPath)
	if err != nil {
		t.Fatalf("aggregator.Open vault: %v", err)
	}
	if err := aggregator.Init(ctx, vaultDB); err != nil {
		t.Fatalf("aggregator.Init vault: %v", err)
	}
	t.Cleanup(func() { _ = vaultDB.Close() })

	const noteID = "proj-A:test-note"
	if _, err := vaultDB.ExecContext(ctx,
		`INSERT INTO knowledge_pin_index
		 (note_id, project_id, title, content, frontmatter_json,
		  promoted_at, promoted_by, promote_reason, audit_chain_anchor, embedding)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		noteID, projectID, "Integration Test Note",
		"test note body for K-6 integration",
		"{}",
		"2026-05-10 12:00:00", "testuser", "seed", "", nil,
	); err != nil {
		t.Fatalf("seed vault note: %v", err)
	}
	if _, err := vaultDB.ExecContext(ctx,
		`INSERT INTO knowledge_pin_fts (rowid, content, title)
		 SELECT rowid, content, title FROM knowledge_pin_index WHERE note_id = ?`,
		noteID,
	); err != nil {
		t.Fatalf("seed vault fts: %v", err)
	}

	rec := &recordingChain{}
	agg, err := aggregator.New(aggregator.Options{
		DB:       pinDB,
		Embedder: embed.NewMockEmbedder(384).WithTokenSumMode(),
		Store:    &stubVaultStore{vaultDB: vaultDB},
		Chain:    rec,
	})
	if err != nil {
		t.Fatalf("aggregator.New: %v", err)
	}

	promoteResult, err := agg.Promote(ctx, noteID, projectID, "testuser", "K-6 integration test promote")
	if err != nil {
		t.Fatalf("aggregator.Promote: %v", err)
	}
	if promoteResult.AuditChainAnchor == "" {
		t.Errorf("Promote returned empty AuditChainAnchor")
	}

	results, err := agg.Query(ctx, aggregator.QueryRequest{
		Text:             "test note body",
		Scope:            aggregator.ScopePinnedOnly,
		AuditChainFilter: true,
	})
	if err != nil {
		t.Fatalf("aggregator.Query: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("Query with AuditChainFilter returned 0 results; expected >=1")
	}
	if results[0].AuditChainAnchor == "" {
		t.Errorf("first result has empty AuditChainAnchor; expected <partition>:<event_id>:<record_hash>")
	}

	cacheDBPath := filepath.Join(tmp, "research_cache.db")
	cacheDB, err := cache.Open(ctx, cacheDBPath)
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	t.Cleanup(func() { _ = cacheDB.SQL.Close() })

	casRoot := filepath.Join(tmp, "research-cas")
	cas, err := cache.NewCAS(casRoot)
	if err != nil {
		t.Fatalf("cache.NewCAS: %v", err)
	}

	mockMCP := researchmcpmock.New()
	disp := &cache.Dispatcher{
		DB:          cacheDB,
		CAS:         cas,
		Revalidator: cache.NewRevalidator(cache.ValidateOpts{}),
		Sink:        noopSink{},
		MCP:         mockMCP,
		Embedder:    nil,
	}

	req := cache.DispatchRequest{
		Query:          "audit hash chain SOTA 2026",
		ProjectID:      projectID,
		SessionID:      "sess1",
		SkipRevalidate: true,
	}
	res1, err := disp.LookupOrDispatch(ctx, req)
	if err != nil {
		t.Fatalf("LookupOrDispatch[1]: %v", err)
	}
	if res1.HitReason != cache.CacheHitFresh {
		t.Errorf("HitReason[1] = %v, want CacheHitFresh", res1.HitReason)
	}
	if calls := mockMCP.Calls(); len(calls) != 1 {
		t.Errorf("MCP calls after first dispatch = %d, want 1", len(calls))
	}

	res2, err := disp.LookupOrDispatch(ctx, req)
	if err != nil {
		t.Fatalf("LookupOrDispatch[2]: %v", err)
	}
	if res2.HitReason != cache.CacheHitExact {
		t.Errorf("HitReason[2] = %v, want CacheHitExact", res2.HitReason)
	}
	if len(res2.Findings) != len(res1.Findings) {
		t.Errorf("cache-hit Findings count = %d, want %d (same as original dispatch)", len(res2.Findings), len(res1.Findings))
	}
	if calls := mockMCP.Calls(); len(calls) != 1 {
		t.Errorf("MCP calls after cache hit = %d, want 1 (no extra dispatch)", len(calls))
	}

	schemaPath := filepath.Join(repoRoot(t), "docs", "system-state.schema.json")
	schema, err := manifest.LoadSchema(schemaPath)
	if err != nil {
		t.Fatalf("manifest.LoadSchema: %v", err)
	}
	reg := manifest.NewRegenerator(schema)

	manifestPath := filepath.Join(tmp, "system-state.toml")
	fresh := manifest.Manifest{
		Provenance: manifest.Provenance{LastRegenerate: time.Now().UTC()},
	}
	if err := reg.RegenerateAndWrite(ctx, fresh, manifestPath); err != nil {
		t.Fatalf("RegenerateAndWrite: %v", err)
	}

	differ := manifest.NewDiffer(schema, reg)
	report, err := differ.Verify(ctx, fresh, manifestPath, time.Now().UTC(), nil)
	if err != nil {
		t.Fatalf("differ.Verify: %v", err)
	}

	if len(report.AutoDriftPaths) != 0 {
		t.Errorf("AutoDriftPaths = %v, want empty after fresh regenerate", report.AutoDriftPaths)
	}
}
