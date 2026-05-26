// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/augment"
	"github.com/cbip-solutions/hades-system/internal/daemon/auditadapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/dispatcheradapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/mcpgateway"
	"github.com/cbip-solutions/hades-system/internal/doctrine/active"
	"github.com/cbip-solutions/hades-system/internal/store"
)

func withTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "augw.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return st
}

func withRegistry(t *testing.T) {
	t.Helper()
	active.ResetForTest()
	t.Cleanup(active.ResetForTest)
	if err := bootDoctrineRegistry(); err != nil {
		t.Fatalf("bootDoctrineRegistry: %v", err)
	}
}

func TestAugmentDoctrineReader_DefaultProjectResolvesMaxScope(t *testing.T) {
	withRegistry(t)
	dr := augmentDoctrineReader{}
	cfg, err := dr.AugmentationConfig(context.Background(), "")
	if err != nil {
		t.Fatalf("AugmentationConfig: %v", err)
	}

	if cfg.DoctrineName != "max-scope" {
		t.Errorf("DoctrineName=%q; want %q (canonical name, not version)", cfg.DoctrineName, "max-scope")
	}
	if !cfg.Enable {
		t.Errorf("Enable=false; want true for max-scope")
	}
	if cfg.MaxKGTokens == 0 {
		t.Errorf("MaxKGTokens=0; want > 0 for max-scope")
	}
}

func TestAugmentDoctrineReader_ProjectMappingHonored(t *testing.T) {
	withRegistry(t)

	originalActive := active.Active()
	active.SetForProject("internal-platform-x", originalActive)

	dr := augmentDoctrineReader{}
	cfg, err := dr.AugmentationConfig(context.Background(), "internal-platform-x")
	if err != nil {
		t.Fatalf("AugmentationConfig: %v", err)
	}
	if cfg.DoctrineName == "" {
		t.Errorf("DoctrineName empty; want non-empty")
	}
}

func TestAugmentDoctrineLoader_LoadsActiveSchema(t *testing.T) {
	withRegistry(t)
	loader := augmentDoctrineLoader{}
	schema, err := loader.Load(context.Background(), "max-scope")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if schema == nil {
		t.Fatal("schema is nil")
	}
	if !schema.Augmentation.Enable {
		t.Errorf("Enable=false; want true (max-scope)")
	}
	if schema.Augmentation.MaxKGTokens == 0 {
		t.Errorf("MaxKGTokens=0; want > 0")
	}
	if schema.Augmentation.BudgetAxis != augment.BudgetAxisName {
		t.Errorf("BudgetAxis=%q; want %q", schema.Augmentation.BudgetAxis, augment.BudgetAxisName)
	}
}

func TestAugmentDoctrineLoader_ResolvesEachBuiltinByName(t *testing.T) {
	withRegistry(t)
	loader := augmentDoctrineLoader{}
	cases := []struct {
		name       string
		wantEnable bool
	}{
		{"max-scope", true},
		{"default", true},
		{"capa-firewall", false},
	}
	prevTokens := -1
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			schema, err := loader.Load(context.Background(), c.name)
			if err != nil {
				t.Fatalf("Load(%q): %v", c.name, err)
			}
			if schema == nil {
				t.Fatalf("Load(%q): nil schema", c.name)
			}
			if schema.Augmentation.Enable != c.wantEnable {
				t.Errorf("Load(%q).Augmentation.Enable = %v; want %v",
					c.name, schema.Augmentation.Enable, c.wantEnable)
			}

			if prevTokens >= 0 && schema.Augmentation.MaxKGTokens == prevTokens && c.name != "max-scope" {
				t.Errorf("Load(%q).Augmentation.MaxKGTokens = %d matches the previous doctrine; pre-fix Critical-3 leaked userDefault across all names",
					c.name, schema.Augmentation.MaxKGTokens)
			}
			prevTokens = schema.Augmentation.MaxKGTokens
		})
	}
}

func TestAugmentDoctrineLoader_UnknownNameReturnsError(t *testing.T) {
	withRegistry(t)
	loader := augmentDoctrineLoader{}
	_, err := loader.Load(context.Background(), "never-registered-doctrine")
	if err == nil {
		t.Fatal("Load(unknown) returned nil err; want non-nil")
	}
	if !strings.Contains(err.Error(), "never-registered-doctrine") {
		t.Errorf("err = %v; should mention the unknown name for operator diagnosis", err)
	}
}

func TestAugmentDoctrineLoader_EmptyNameFallsBackToActive(t *testing.T) {
	withRegistry(t)
	loader := augmentDoctrineLoader{}
	schema, err := loader.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("Load(\"\"): %v", err)
	}
	if schema == nil {
		t.Fatal("schema is nil")
	}
	// The schema returned MUST equal active.For("") (userDefault) — pinned
	// here so behaviour change is detectable.
	wantSchema := active.For("")
	if wantSchema == nil {
		t.Fatal("active.For(\"\") nil; bootDoctrineRegistry should populate")
	}
	if schema.Augmentation.Enable != wantSchema.Augmentation.Enable {
		t.Errorf("Load(\"\").Augmentation.Enable = %v; want %v (userDefault)",
			schema.Augmentation.Enable, wantSchema.Augmentation.Enable)
	}
}

func TestAugmentProjectLookup_ResolvesDoctrineName(t *testing.T) {
	withRegistry(t)
	lookup := augmentProjectLookup{}
	got, err := lookup.DoctrineForProject(context.Background(), "")
	if err != nil {
		t.Fatalf("DoctrineForProject: %v", err)
	}

	if got != "max-scope" {
		t.Errorf("doctrine=%q; want %q (canonical name)", got, "max-scope")
	}
}

type fakeSubsystem struct {
	name     string
	tool     string
	response map[string]any
}

func (f fakeSubsystem) Name() string { return f.name }
func (f fakeSubsystem) Tools() []mcpgateway.ToolEntry {
	return []mcpgateway.ToolEntry{{
		Name: mcpgateway.MustToolName(f.name, f.tool),
		Handler: func(_ context.Context, _ mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
			body, _ := json.Marshal(f.response)
			return mcpgateway.CallResponse{
				Content:   []mcpgateway.CallContentItem{{Type: "text", Text: string(body)}},
				Subsystem: f.name,
			}, nil
		},
		Meta: mcpgateway.ToolMeta{Description: "fake"},
	}}
}

func TestAugmentMcpGateway_RenamesHitsToResults(t *testing.T) {
	disp := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{})
	t.Cleanup(func() { _ = disp.Close() })
	if err := disp.RegisterSubsystem(fakeSubsystem{
		name: "caronte",
		tool: "query",
		response: map[string]any{
			"hits":       []any{map[string]any{"note_id": "n1", "title": "T"}},
			"project_id": "internal-platform-x",
		},
	}); err != nil {
		t.Fatalf("RegisterSubsystem: %v", err)
	}

	gw := &augmentMcpGateway{disp: disp}
	got, err := gw.CallTool(context.Background(), augment.ToolCaronteQuery, map[string]any{
		"project_id": "internal-platform-x",
		"query":      "test",
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("got %T; want map[string]any", got)
	}
	if _, hadHits := m["hits"]; hadHits {
		t.Errorf("response still contains \"hits\" key; want renamed to \"results\"")
	}
	results, ok := m["results"].([]any)
	if !ok {
		t.Fatalf("results is not []any: %T", m["results"])
	}
	if len(results) != 1 {
		t.Errorf("results len=%d; want 1", len(results))
	}
}

func TestAugmentMcpGateway_ParseToolNameError(t *testing.T) {
	disp := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{})
	t.Cleanup(func() { _ = disp.Close() })
	gw := &augmentMcpGateway{disp: disp}
	_, err := gw.CallTool(context.Background(), "not-a-canonical-name", nil)
	if err == nil {
		t.Fatal("CallTool with bogus tool name returned nil err; want non-nil")
	}
	if !strings.Contains(err.Error(), "parse tool name") {
		t.Errorf("err=%v; want it to mention parse-tool-name failure", err)
	}
}

type emptyContentSubsystem struct{}

func (emptyContentSubsystem) Name() string { return "caronte" }
func (emptyContentSubsystem) Tools() []mcpgateway.ToolEntry {
	return []mcpgateway.ToolEntry{{
		Name: mcpgateway.MustToolName("caronte", "context"),
		Handler: func(_ context.Context, _ mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
			return mcpgateway.CallResponse{Content: nil}, nil
		},
		Meta: mcpgateway.ToolMeta{Description: "empty"},
	}}
}

func TestAugmentMcpGateway_EmptyContentReturnsEmptyResults(t *testing.T) {
	disp := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{})
	t.Cleanup(func() { _ = disp.Close() })
	if err := disp.RegisterSubsystem(emptyContentSubsystem{}); err != nil {
		t.Fatalf("RegisterSubsystem: %v", err)
	}

	gw := &augmentMcpGateway{disp: disp}
	got, err := gw.CallTool(context.Background(), augment.ToolCaronteContext, map[string]any{
		"project_id": "internal-platform-x",
		"query":      "test",
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("got %T; want map[string]any", got)
	}
	results, ok := m["results"].([]any)
	if !ok || len(results) != 0 {
		t.Errorf("results=%v; want empty []any", m["results"])
	}
}

func TestAugmentChainStore_GetChainTipEmpty(t *testing.T) {
	st := withTestStore(t)
	cs := &augmentChainStore{
		st:    st,
		audit: auditadapter.New(st),
	}
	tip, err := cs.GetChainTip(context.Background())
	if err != nil {
		t.Fatalf("GetChainTip: %v", err)
	}
	if tip != "" {
		t.Errorf("tip=%q; want \"\" on empty chain (augment converts to genesis)", tip)
	}
}

func TestAugmentChainStore_UpdateChainColumnsInsertsRowIfMissing(t *testing.T) {
	st := withTestStore(t)
	cs := &augmentChainStore{
		st:    st,
		audit: auditadapter.New(st),
	}

	const hex64a = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	const hex64b = "ffeeddccbbaa99887766554433221100ffeeddccbbaa99887766554433221100"
	err := cs.UpdateChainColumns(
		context.Background(),
		"evt-new-1",
		hex64a,
		"AugmentationStarted",
		[]byte(`{"project":"internal-platform-x","session_id":"s"}`),
		time.Now().Unix(),
		hex64b,
		"2026_05",
	)
	if err != nil {
		t.Fatalf("UpdateChainColumns: %v", err)
	}

	tip, err := cs.GetChainTip(context.Background())
	if err != nil {
		t.Fatalf("GetChainTip: %v", err)
	}
	if tip != hex64b {
		t.Errorf("tip=%q; want %q", tip, hex64b)
	}
}

func TestAugmentChainStore_UpdateChainColumnsPersistsEventIdentity(t *testing.T) {
	st := withTestStore(t)
	cs := &augmentChainStore{
		st:    st,
		audit: auditadapter.New(st),
	}
	const hex64a = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	const hex64b = "ffeeddccbbaa99887766554433221100ffeeddccbbaa99887766554433221100"
	emittedAt := time.Now().Unix()
	payload := []byte(`{"project":"internal-platform-x","session_id":"s","doctrine":"max-scope"}`)
	if err := cs.UpdateChainColumns(
		context.Background(),
		"evt-identity-1",
		hex64a,
		"AugmentationStarted",
		payload,
		emittedAt,
		hex64b,
		"2026_05",
	); err != nil {
		t.Fatalf("UpdateChainColumns: %v", err)
	}

	row := st.DB().QueryRowContext(context.Background(),
		`SELECT project_id, type, payload_json, emitted_at FROM audit_events_raw WHERE id = ?`,
		"evt-identity-1",
	)
	var gotProject, gotType, gotPayload string
	var gotEmittedAt int64
	if err := row.Scan(&gotProject, &gotType, &gotPayload, &gotEmittedAt); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if gotProject != "internal-platform-x" {
		t.Errorf("project_id=%q; want %q (pre-fix wrote \"\")", gotProject, "internal-platform-x")
	}
	if gotType != "AugmentationStarted" {
		t.Errorf("type=%q; want %q (pre-fix wrote \"augmentation.chain.placeholder\")", gotType, "AugmentationStarted")
	}
	if gotPayload != string(payload) {
		t.Errorf("payload_json=%q; want %q (pre-fix wrote \"{}\")", gotPayload, string(payload))
	}
	if gotEmittedAt != emittedAt {
		t.Errorf("emitted_at=%d; want %d (pre-fix wrote UnixMilli)", gotEmittedAt, emittedAt)
	}
}

// TestAugmentChainStore_UpdateChainColumnsRejectsMillisecondTimestamp is the
// adapter MUST reject timestamps that look like unix-milliseconds (which
// is what the pre-fix code wrote via UnixMilli). Migration 055 stores
// emitted_at as unix-seconds; migration 059's
// strftime('%Y_%m', emitted_at, 'unixepoch') would compute year ~50000
// for millisecond timestamps. The adapter's emittedAt > 0 guard PLUS
// the chain.Compute canonical contract (unix-seconds) ensure this can
// never happen in the new code path.
func TestAugmentChainStore_UpdateChainColumnsRejectsZeroEmittedAt(t *testing.T) {
	st := withTestStore(t)
	cs := &augmentChainStore{
		st:    st,
		audit: auditadapter.New(st),
	}
	err := cs.UpdateChainColumns(
		context.Background(),
		"evt-zero-ts",
		"",
		"AugmentationStarted",
		[]byte(`{}`),
		0,
		"",
		"2026_05",
	)
	if err == nil {
		t.Fatal("UpdateChainColumns with emittedAt=0 returned nil err; want non-nil")
	}
	if !strings.Contains(err.Error(), "emittedAt must be > 0") {
		t.Errorf("err=%v; want \"emittedAt must be > 0\" guard", err)
	}
}

func TestAugmentChainStore_UpdateTesseraLeafID(t *testing.T) {
	st := withTestStore(t)
	cs := &augmentChainStore{
		st:    st,
		audit: auditadapter.New(st),
	}
	const hex64a = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	const hex64b = "ffeeddccbbaa99887766554433221100ffeeddccbbaa99887766554433221100"
	if err := cs.UpdateChainColumns(
		context.Background(),
		"evt-x",
		hex64a,
		"AugmentationStarted",
		[]byte(`{"project":"p"}`),
		time.Now().Unix(),
		hex64b,
		"2026_05",
	); err != nil {
		t.Fatalf("UpdateChainColumns: %v", err)
	}
	if err := cs.UpdateTesseraLeafID(context.Background(), "evt-x", "leaf-abc"); err != nil {
		t.Fatalf("UpdateTesseraLeafID: %v", err)
	}
}

func TestAugmentChainStore_AppendTesseraLeaf_DegradedPath(t *testing.T) {
	st := withTestStore(t)
	cs := &augmentChainStore{
		st:    st,
		audit: auditadapter.New(st),
		tess:  nil,
	}
	const hex64 = "ffeeddccbbaa99887766554433221100ffeeddccbbaa99887766554433221100"
	leafID, err := cs.AppendTesseraLeaf(context.Background(), augment.TesseraLeafInput{
		EventID:    "evt-degraded",
		EventType:  "AugmentationStarted",
		ProjectID:  "internal-platform-x",
		Partition:  "2026_05",
		Payload:    []byte("payload"),
		RecordHash: hex64,
	})
	if err != nil {
		t.Fatalf("AppendTesseraLeaf: %v", err)
	}
	if !strings.HasPrefix(leafID, "tess-degraded-") {
		t.Errorf("leafID=%q; want \"tess-degraded-*\" placeholder", leafID)
	}
}

func TestAugmentChainStore_CancelledContext(t *testing.T) {
	st := withTestStore(t)
	cs := &augmentChainStore{
		st:    st,
		audit: auditadapter.New(st),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := cs.UpdateChainColumns(
		ctx, "evt-cancelled", "", "AugmentationStarted",
		[]byte(`{}`), time.Now().Unix(), "", "2026_05",
	)
	if err == nil {
		t.Fatal("UpdateChainColumns on cancelled ctx returned nil err; want non-nil")
	}
}

func TestSha256Bytes(t *testing.T) {
	out := sha256Bytes([]byte("hello"))
	if len(out) != 32 {
		t.Errorf("len=%d; want 32 (sha256 size)", len(out))
	}

	if string(out) != string(sha256Bytes([]byte("hello"))) {
		t.Error("sha256Bytes is non-deterministic")
	}
}

func TestAugmentBudgetStore_RolledZeroPreCostLedger(t *testing.T) {
	st := withTestStore(t)
	bs := &augmentBudgetStore{
		st: st,
		ba: dispatcheradapter.NewBudgetAdapter(st),
	}
	got, err := bs.RolledUSDByAxis(context.Background(), "augmentation", "internal-platform-x", 0)
	if err != nil {
		t.Fatalf("RolledUSDByAxis: %v", err)
	}
	if got != 0 {
		t.Errorf("rolled=%v; want 0 (pre cost_ledger merge per dispatcheradapter contract)", got)
	}
}

func TestAugmentBudgetStore_InsertCostLedgerEntry(t *testing.T) {
	st := withTestStore(t)
	bs := &augmentBudgetStore{
		st: st,
		ba: dispatcheradapter.NewBudgetAdapter(st),
	}
	err := bs.InsertCostLedgerEntry(context.Background(), augment.CostLedgerEntry{
		RequestID: "req-1",
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		USD:       0.01,
		Tokens:    100,
		EmittedAt: time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("InsertCostLedgerEntry: %v", err)
	}
}

func TestAugmentBudgetStore_InsertIdempotentOnRequestID(t *testing.T) {
	st := withTestStore(t)
	bs := &augmentBudgetStore{
		st: st,
		ba: dispatcheradapter.NewBudgetAdapter(st),
	}
	entry := augment.CostLedgerEntry{
		RequestID: "req-idempotent",
		ProjectID: "p",
		Doctrine:  "max-scope",
		USD:       0.01,
		Tokens:    100,
		EmittedAt: time.Now().UnixMilli(),
	}
	if err := bs.InsertCostLedgerEntry(context.Background(), entry); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	if err := bs.InsertCostLedgerEntry(context.Background(), entry); err != nil {
		t.Errorf("second insert (same RequestID): %v — should be idempotent", err)
	}
}

func TestBuildAugmentation_NilKnowledgeIndexReturnsNil(t *testing.T) {
	withRegistry(t)
	st := withTestStore(t)
	h, err := buildAugmentation(augmentationDeps{
		store:          st,
		auditAdapter:   auditadapter.New(st),
		budgetAdapter:  dispatcheradapter.NewBudgetAdapter(st),
		mcpDispatcher:  mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{}),
		knowledgeIndex: nil,
		embedder:       fakeEmbedder{},
		logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("buildAugmentation: %v", err)
	}
	if h != nil {
		t.Error("handler is non-nil; want nil (knowledgeIndex absent → graceful-degrade)")
	}
}

func TestBuildAugmentation_NilEmbedderReturnsNil(t *testing.T) {
	withRegistry(t)
	st := withTestStore(t)
	h, err := buildAugmentation(augmentationDeps{
		store:          st,
		auditAdapter:   auditadapter.New(st),
		budgetAdapter:  dispatcheradapter.NewBudgetAdapter(st),
		mcpDispatcher:  mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{}),
		knowledgeIndex: fakeKnowledgeIndex{},
		embedder:       nil,
		logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("buildAugmentation: %v", err)
	}
	if h != nil {
		t.Error("handler is non-nil; want nil")
	}
}

func TestBuildAugmentation_NilMcpDispatcherReturnsNil(t *testing.T) {
	withRegistry(t)
	st := withTestStore(t)
	h, err := buildAugmentation(augmentationDeps{
		store:          st,
		auditAdapter:   auditadapter.New(st),
		budgetAdapter:  dispatcheradapter.NewBudgetAdapter(st),
		mcpDispatcher:  nil,
		knowledgeIndex: fakeKnowledgeIndex{},
		embedder:       fakeEmbedder{},
		logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("buildAugmentation: %v", err)
	}
	if h != nil {
		t.Error("handler is non-nil; want nil")
	}
}

func TestBuildAugmentation_NilAuditReturnsNil(t *testing.T) {
	withRegistry(t)
	st := withTestStore(t)
	h, err := buildAugmentation(augmentationDeps{
		store:          st,
		auditAdapter:   nil,
		budgetAdapter:  dispatcheradapter.NewBudgetAdapter(st),
		mcpDispatcher:  mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{}),
		knowledgeIndex: fakeKnowledgeIndex{},
		embedder:       fakeEmbedder{},
		logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("buildAugmentation: %v", err)
	}
	if h != nil {
		t.Error("handler is non-nil; want nil")
	}
}

func TestBuildAugmentation_NilBudgetReturnsNil(t *testing.T) {
	withRegistry(t)
	st := withTestStore(t)
	h, err := buildAugmentation(augmentationDeps{
		store:          st,
		auditAdapter:   auditadapter.New(st),
		budgetAdapter:  nil,
		mcpDispatcher:  mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{}),
		knowledgeIndex: fakeKnowledgeIndex{},
		embedder:       fakeEmbedder{},
		logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("buildAugmentation: %v", err)
	}
	if h != nil {
		t.Error("handler is non-nil; want nil")
	}
}

func TestBuildAugmentation_NilLoggerUsesDefault(t *testing.T) {
	withRegistry(t)
	st := withTestStore(t)

	_, err := buildAugmentation(augmentationDeps{
		store:        st,
		auditAdapter: auditadapter.New(st),
		logger:       nil,
	})
	if err != nil {
		t.Fatalf("buildAugmentation with nil logger: %v", err)
	}
}

func TestBuildAugmentation_FullyWiredReturnsHandler(t *testing.T) {
	withRegistry(t)
	st := withTestStore(t)
	disp := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{})
	t.Cleanup(func() { _ = disp.Close() })

	h, err := buildAugmentation(augmentationDeps{
		store:          st,
		auditAdapter:   auditadapter.New(st),
		budgetAdapter:  dispatcheradapter.NewBudgetAdapter(st),
		mcpDispatcher:  disp,
		knowledgeIndex: fakeKnowledgeIndex{},
		embedder:       fakeEmbedder{},
		logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("buildAugmentation: %v", err)
	}
	if h == nil {
		t.Fatal("handler is nil; want non-nil (fully wired substrate)")
	}

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	body := strings.NewReader(`{"project":"internal-platform-x","prompt":"x","mode":"interactive","prompt_hash":"req-hash-1"}`)
	resp, err := srv.Client().Post(srv.URL, "application/json", body)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode >= 500 {
		respBody, _ := io.ReadAll(resp.Body)
		t.Errorf("status=%d; want < 500\nbody: %s", resp.StatusCode, string(respBody))
	}
}

type fakeKnowledgeIndex struct{}

func (fakeKnowledgeIndex) QueryFTS(_ context.Context, _ string, _ int) ([]augment.QueryResult, error) {
	return nil, nil
}
func (fakeKnowledgeIndex) QueryVec(_ context.Context, _ []float32, _ int, _ float64) ([]augment.QueryResult, error) {
	return nil, nil
}
func (fakeKnowledgeIndex) QueryGraph(_ context.Context, _ []string, _, _ int) ([]augment.QueryResult, error) {
	return nil, nil
}

type fakeEmbedder struct{}

func (fakeEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{0, 0, 0}, nil
}
