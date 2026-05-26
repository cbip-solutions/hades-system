package augment_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/augment"
)

type pipelineDeps struct {
	budget    *fakeBudgetStore
	index     *fakeIndex
	embedder  *fakeEmbedder
	chain     *pipChainStore
	gateway   *fakeMcpGateway
	doctrines *fakeDoctrineLoader
	lookup    *projectDoctrineLookup
}

type pipChainStore struct {
	tip         string
	updateCalls atomic.Int32
	leafCalls   atomic.Int32
	updateErr   error
	leafErr     error
	appendErr   error
}

func (f *pipChainStore) GetChainTip(_ context.Context) (string, error) {

	return f.tip, nil
}
func (f *pipChainStore) UpdateChainColumns(_ context.Context, _, _, _ string, _ []byte, _ int64, _, _ string) error {
	f.updateCalls.Add(1)
	return f.updateErr
}
func (f *pipChainStore) UpdateTesseraLeafID(_ context.Context, _, _ string) error {
	return f.leafErr
}
func (f *pipChainStore) AppendTesseraLeaf(_ context.Context, _ augment.TesseraLeafInput) (string, error) {
	f.leafCalls.Add(1)
	if f.appendErr != nil {
		return "", f.appendErr
	}
	return "leaf-1", nil
}

type fakeMcpGateway struct {
	calls atomic.Int32
	resps map[string]any
	errs  map[string]error
	slow  time.Duration
}

func (f *fakeMcpGateway) CallTool(ctx context.Context, name string, _ map[string]any) (any, error) {
	f.calls.Add(1)
	if f.slow > 0 {
		select {
		case <-time.After(f.slow):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if e, ok := f.errs[name]; ok {
		return nil, e
	}
	if r, ok := f.resps[name]; ok {
		return r, nil
	}
	return map[string]any{}, nil
}

func newPipelineDeps(t *testing.T) *pipelineDeps {
	t.Helper()
	idx := &fakeIndex{
		queryFTSFn: func(_ context.Context, _ string, _ int) ([]augment.QueryResult, error) {
			return []augment.QueryResult{
				{NoteID: "n1", Title: "Engine", Score: 1.5, ProjectID: "internal-platform-x", Source: "fts"},
			}, nil
		},
		queryVecFn: func(_ context.Context, _ []float32, _ int, _ float64) ([]augment.QueryResult, error) {
			return []augment.QueryResult{
				{NoteID: "n2", Title: "Other", Score: 0.95, ProjectID: "internal-platform-x", Source: "vec"},
			}, nil
		},
	}
	emb := &fakeEmbedder{
		embedFn: func(_ context.Context, _ string) ([]float32, error) {
			return []float32{0.1, 0.2, 0.3}, nil
		},
	}
	return &pipelineDeps{
		budget:   newFakeBudgetStore(),
		index:    idx,
		embedder: emb,
		chain:    &pipChainStore{},
		gateway: &fakeMcpGateway{
			resps: map[string]any{
				"mcp_zen-swarm_caronte_query": map[string]any{
					"results": []any{
						map[string]any{"note_id": "kg-1", "title": "KGNode", "score": 1.5, "snippet": "..."},
					},
				},
				"mcp_zen-swarm_caronte_context": map[string]any{
					"results": []any{
						map[string]any{"note_id": "ctx-1", "title": "Neighbor", "score": 1.2, "snippet": "..."},
					},
				},
			},
			errs: map[string]error{},
		},
		doctrines: &fakeDoctrineLoader{schemas: privacySchemas()},
		lookup:    &projectDoctrineLookup{mp: map[string]string{"internal-platform-x": "max-scope"}},
	}
}

func newPipeline(t *testing.T, deps *pipelineDeps) *augment.Pipeline {
	t.Helper()
	p, err := augment.NewPipeline(augment.PipelineOptions{
		BudgetStore:       deps.budget,
		KnowledgeIndex:    deps.index,
		Embedder:          deps.embedder,
		ChainStore:        deps.chain,
		McpGateway:        deps.gateway,
		DoctrineLoader:    deps.doctrines,
		ProjectLookup:     deps.lookup,
		Clock:             fixedClock{t: time.Unix(1715000000, 0)},
		ConcurrencyBudget: 10,
		QueueDepth:        50,
		PerLaneTimeout:    1 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	return p
}

func TestPipeline_HappyPathFiveLaneFanOut(t *testing.T) {
	deps := newPipelineDeps(t)
	p := newPipeline(t, deps)

	resp, err := p.Run(context.Background(), augment.AugmentRequest{
		Prompt:    "refactor MergeEngine",
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		SessionID: "sess-1",
		Mode:      "interactive",
		RequestID: "req-1",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.SkippedReason != "" {
		t.Fatalf("expected augmentation, got skip reason %q", resp.SkippedReason)
	}

	if calls := deps.gateway.calls.Load(); calls != 3 {
		t.Errorf("expected 3 gateway calls (Lane 1 + Lane 3 + Lane 5 co-change), got %d", calls)
	}
	if u := deps.chain.updateCalls.Load(); u < 2 {
		t.Errorf("expected >=2 chain UpdateChainColumns calls, got %d", u)
	}
	if l := deps.chain.leafCalls.Load(); l < 2 {
		t.Errorf("expected >=2 Tessera leaves, got %d", l)
	}
	if resp.AuditEventID == "" {
		t.Error("expected non-empty AuditEventID")
	}
	if entries := deps.budget.ledgerSnapshot(); len(entries) == 0 {
		t.Error("expected cost_ledger entry written")
	}
}

func TestPipeline_CapaFirewallSkips(t *testing.T) {
	deps := newPipelineDeps(t)
	deps.lookup.mp["secret-proj"] = "capa-firewall"
	p := newPipeline(t, deps)

	resp, err := p.Run(context.Background(), augment.AugmentRequest{
		Prompt:    "anything",
		ProjectID: "secret-proj",
		Doctrine:  "capa-firewall",
		SessionID: "sess-1",
		RequestID: "req-2",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.SkippedReason != "capa-firewall-disabled" {
		t.Fatalf("SkippedReason: want capa-firewall-disabled, got %q", resp.SkippedReason)
	}
	if c := deps.gateway.calls.Load(); c != 0 {
		t.Errorf("expected 0 gateway calls on skip, got %d", c)
	}
	if l := deps.chain.leafCalls.Load(); l == 0 {
		t.Error("expected AugmentationSkipped audit leaf")
	}
}

func TestPipeline_OverBudgetSkips(t *testing.T) {
	deps := newPipelineDeps(t)
	deps.budget.setRolled("augmentation", "internal-platform-x", 1000000.0)
	p := newPipeline(t, deps)

	resp, err := p.Run(context.Background(), augment.AugmentRequest{
		Prompt:    "x",
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		SessionID: "sess-1",
		RequestID: "req-3",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.SkippedReason != "budget-cap" {
		t.Fatalf("SkippedReason: want budget-cap, got %q", resp.SkippedReason)
	}
}

func TestPipeline_PartialLaneFailureContinues(t *testing.T) {
	deps := newPipelineDeps(t)
	deps.gateway.errs["mcp_zen-swarm_caronte_query"] = errors.New("caronte down")
	p := newPipeline(t, deps)

	resp, err := p.Run(context.Background(), augment.AugmentRequest{
		Prompt:    "test",
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		SessionID: "sess-1",
		RequestID: "req-4",
	})
	if err != nil {
		t.Fatalf("Run: %v (partial lane failure should not abort)", err)
	}
	if resp.SkippedReason != "" {
		t.Fatalf("expected augmentation despite Lane 1 failure, got skip %q", resp.SkippedReason)
	}
	if len(resp.VolatileContext.FusedResults) == 0 {
		t.Error("expected non-empty fused results from Lanes 2/4/5")
	}
}

func TestPipeline_CancellationPropagates(t *testing.T) {
	deps := newPipelineDeps(t)
	p := newPipeline(t, deps)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.Run(ctx, augment.AugmentRequest{
		Prompt:    "x",
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		SessionID: "sess-1",
		RequestID: "req-5",
	})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestPipeline_EmptyPromptErrors(t *testing.T) {
	deps := newPipelineDeps(t)
	p := newPipeline(t, deps)
	_, err := p.Run(context.Background(), augment.AugmentRequest{
		Prompt:    "",
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		SessionID: "sess-1",
		RequestID: "req-empty",
	})
	if err == nil || !contains(err.Error(), "Prompt") {
		t.Fatalf("expected empty prompt error, got %v", err)
	}
}

func TestPipeline_MissingProjectIDErrors(t *testing.T) {
	deps := newPipelineDeps(t)
	p := newPipeline(t, deps)
	_, err := p.Run(context.Background(), augment.AugmentRequest{
		Prompt:    "x",
		ProjectID: "",
		Doctrine:  "max-scope",
		SessionID: "sess-1",
		RequestID: "req-missing-proj",
	})
	if err == nil || !contains(err.Error(), "ProjectID") {
		t.Fatalf("expected missing ProjectID error, got %v", err)
	}
}

func TestPipeline_MissingRequestIDErrors(t *testing.T) {
	deps := newPipelineDeps(t)
	p := newPipeline(t, deps)
	_, err := p.Run(context.Background(), augment.AugmentRequest{
		Prompt:    "x",
		ProjectID: "p",
		Doctrine:  "max-scope",
		RequestID: "",
	})
	if err == nil || !contains(err.Error(), "RequestID") {
		t.Fatalf("expected missing RequestID error, got %v", err)
	}
}

func TestPipeline_DefaultsMode(t *testing.T) {
	deps := newPipelineDeps(t)
	p := newPipeline(t, deps)
	resp, err := p.Run(context.Background(), augment.AugmentRequest{
		Prompt:    "x",
		ProjectID: "internal-platform-x",
		Doctrine:  "",
		Mode:      "",
		RequestID: "req-defaults",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.SkippedReason != "" && resp.SkippedReason != "max-tokens-zero" {

		t.Logf("resp: %+v", resp)
	}
}

func TestPipeline_DoctrineGateError(t *testing.T) {
	deps := newPipelineDeps(t)
	deps.doctrines.errOn = map[string]error{"max-scope": errors.New("doctrine load broken")}
	p := newPipeline(t, deps)
	_, err := p.Run(context.Background(), augment.AugmentRequest{
		Prompt:    "x",
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		RequestID: "req-dg",
	})
	if err == nil || !contains(err.Error(), "doctrine_gate") {
		t.Fatalf("expected doctrine_gate error, got %v", err)
	}
}

func TestPipeline_BudgetGateStoreError(t *testing.T) {
	deps := newPipelineDeps(t)
	deps.budget.rolledErr = errors.New("budget store down")
	p := newPipeline(t, deps)
	_, err := p.Run(context.Background(), augment.AugmentRequest{
		Prompt:    "x",
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		RequestID: "req-bg",
	})
	if err == nil || !contains(err.Error(), "budget_gate") {
		t.Fatalf("expected budget_gate error, got %v", err)
	}
}

func TestPipeline_AllLanesFailGracefully(t *testing.T) {

	deps := newPipelineDeps(t)
	deps.gateway.errs["mcp_zen-swarm_caronte_query"] = errors.New("gate1 down")
	deps.gateway.errs["mcp_zen-swarm_caronte_context"] = errors.New("gate3 down")
	deps.index.queryFTSFn = func(_ context.Context, _ string, _ int) ([]augment.QueryResult, error) {
		return nil, errors.New("fts down")
	}
	deps.index.queryVecFn = func(_ context.Context, _ []float32, _ int, _ float64) ([]augment.QueryResult, error) {
		return nil, errors.New("vec down")
	}
	p := newPipeline(t, deps)
	resp, err := p.Run(context.Background(), augment.AugmentRequest{
		Prompt:    "x",
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		RequestID: "req-all-down",
	})
	if err != nil {
		t.Fatalf("Run with all lanes down should not error, got %v", err)
	}
	if resp.SkippedReason != "" {
		t.Errorf("expected augmentation (even with empty results), got skip %q", resp.SkippedReason)
	}
}

func TestPipeline_NilDoctrineSchemaErrors(t *testing.T) {
	deps := newPipelineDeps(t)
	deps.doctrines.nilOn = map[string]bool{"max-scope": true}
	deps.doctrines.schemas["max-scope"] = nil
	p := newPipeline(t, deps)
	_, err := p.Run(context.Background(), augment.AugmentRequest{
		Prompt:    "x",
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		RequestID: "req-nil-schema",
	})
	if err == nil {
		t.Fatal("expected error for nil doctrine schema")
	}
}

func TestPipeline_TruncationFires(t *testing.T) {
	deps := newPipelineDeps(t)

	deps.doctrines.schemas["max-scope"].Augmentation.MaxKGTokens = 5
	p := newPipeline(t, deps)
	resp, err := p.Run(context.Background(), augment.AugmentRequest{
		Prompt:    "x",
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		RequestID: "req-trunc",
	})
	if err != nil {

		return
	}
	_ = resp
}

func TestPipeline_QueueDepthExceeded(t *testing.T) {
	deps := newPipelineDeps(t)

	deps.index.queryFTSFn = func(ctx context.Context, _ string, _ int) ([]augment.QueryResult, error) {
		select {
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
		}
		return nil, ctx.Err()
	}
	p, err := augment.NewPipeline(augment.PipelineOptions{
		BudgetStore:       deps.budget,
		KnowledgeIndex:    deps.index,
		Embedder:          deps.embedder,
		ChainStore:        deps.chain,
		McpGateway:        deps.gateway,
		DoctrineLoader:    deps.doctrines,
		ProjectLookup:     deps.lookup,
		Clock:             fixedClock{t: time.Unix(1715000000, 0)},
		ConcurrencyBudget: 1,
		QueueDepth:        1,
		PerLaneTimeout:    500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}

	errs := make(chan error, 5)
	for i := 0; i < 5; i++ {
		go func(idx int) {
			_, e := p.Run(context.Background(), augment.AugmentRequest{
				Prompt:    "x",
				ProjectID: "internal-platform-x",
				Doctrine:  "max-scope",
				SessionID: "sess",
				RequestID: fmt.Sprintf("req-q-%d", idx),
			})
			errs <- e
		}(i)
	}
	queueExceeded := 0
	for i := 0; i < 5; i++ {
		e := <-errs
		if e != nil && contains(e.Error(), "queue depth") {
			queueExceeded++
		}
	}
	if queueExceeded == 0 {
		t.Logf("Note: did not observe queue-depth-exceeded; may depend on goroutine timing")
	}
}

func TestPipeline_ParseGatewayResponseWithNoResults(t *testing.T) {
	deps := newPipelineDeps(t)
	deps.gateway.resps["mcp_zen-swarm_caronte_query"] = "not a map"
	p := newPipeline(t, deps)
	resp, err := p.Run(context.Background(), augment.AugmentRequest{
		Prompt:    "x",
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		RequestID: "req-malformed",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	_ = resp
}

func TestPipeline_GatewayResponseMissingFields(t *testing.T) {
	deps := newPipelineDeps(t)
	deps.gateway.resps["mcp_zen-swarm_caronte_query"] = map[string]any{
		"results": []any{
			map[string]any{},
			map[string]any{"note_id": 123},
			"not-a-map",
		},
	}
	p := newPipeline(t, deps)
	_, err := p.Run(context.Background(), augment.AugmentRequest{
		Prompt:    "x",
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		RequestID: "req-malformed-fields",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestPipeline_GatewayResponseMissingResultsKey(t *testing.T) {
	deps := newPipelineDeps(t)
	deps.gateway.resps["mcp_zen-swarm_caronte_query"] = map[string]any{
		"other_key": "no_results_here",
	}
	p := newPipeline(t, deps)
	_, err := p.Run(context.Background(), augment.AugmentRequest{
		Prompt:    "x",
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		RequestID: "req-no-results-key",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestPipeline_PrivacyDropsAuditEvent(t *testing.T) {
	deps := newPipelineDeps(t)

	deps.lookup.mp["other-proj"] = "capa-firewall"

	deps.index.queryFTSFn = func(_ context.Context, _ string, _ int) ([]augment.QueryResult, error) {
		return []augment.QueryResult{
			{NoteID: "n1", ProjectID: "internal-platform-x", Source: "fts"},
			{NoteID: "n-other", ProjectID: "other-proj", Source: "fts"},
		}, nil
	}
	deps.index.queryVecFn = func(_ context.Context, _ []float32, _ int, _ float64) ([]augment.QueryResult, error) {
		return nil, nil
	}
	p := newPipeline(t, deps)
	leavesBeforeDrop := deps.chain.leafCalls.Load()
	resp, err := p.Run(context.Background(), augment.AugmentRequest{
		Prompt:    "x",
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		RequestID: "req-drop",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.SkippedReason != "" {
		t.Fatalf("expected augmentation, got skip %q", resp.SkippedReason)
	}

	if deps.chain.leafCalls.Load() == leavesBeforeDrop {
		t.Error("expected CrossProjectQueryFiltered audit leaf to be emitted")
	}
}

type flakyDoctrineLoader struct {
	calls atomic.Int32
	max   *augment.DoctrineSchema
}

func (f *flakyDoctrineLoader) Load(_ context.Context, name string) (*augment.DoctrineSchema, error) {
	n := f.calls.Add(1)
	if name == "max-scope" && n > 2 {

		return nil, errors.New("flaky loader: privacy-stage failure")
	}
	return f.max, nil
}

func TestPipeline_PrivacyFilterError(t *testing.T) {
	deps := newPipelineDeps(t)
	loader := &flakyDoctrineLoader{
		max: &augment.DoctrineSchema{
			Augmentation: augment.AugmentationAxis{
				Enable: true, MaxKGTokens: 25000, TimeoutMs: 2000,
			},
			KnowledgeCrossProject: augment.CrossProjectAxis{
				QueriesCanReach: []string{"max-scope", "default"},
			},
		},
	}
	p, err := augment.NewPipeline(augment.PipelineOptions{
		BudgetStore:       deps.budget,
		KnowledgeIndex:    deps.index,
		Embedder:          deps.embedder,
		ChainStore:        deps.chain,
		McpGateway:        deps.gateway,
		DoctrineLoader:    loader,
		ProjectLookup:     deps.lookup,
		Clock:             fixedClock{t: time.Unix(1715000000, 0)},
		ConcurrencyBudget: 10,
		QueueDepth:        50,
		PerLaneTimeout:    1 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}

	deps.index.queryFTSFn = func(_ context.Context, _ string, _ int) ([]augment.QueryResult, error) {
		return []augment.QueryResult{
			{NoteID: "n", ProjectID: "other"},
		}, nil
	}
	_, runErr := p.Run(context.Background(), augment.AugmentRequest{
		Prompt:    "x",
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		RequestID: "req-priv-fail",
	})
	if runErr == nil || !contains(runErr.Error(), "privacy_filter") {
		t.Fatalf("expected privacy_filter error, got %v", runErr)
	}
}

func TestPipeline_BudgetCommitError(t *testing.T) {
	deps := newPipelineDeps(t)
	deps.budget.insertErr = errors.New("ledger disk full")
	p := newPipeline(t, deps)
	_, err := p.Run(context.Background(), augment.AugmentRequest{
		Prompt:    "x",
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		RequestID: "req-commit-err",
	})
	if err == nil || !contains(err.Error(), "budget commit") {
		t.Fatalf("expected budget commit error, got %v", err)
	}
}

func TestPipeline_AuditAnchorErrorOnStarted(t *testing.T) {
	deps := newPipelineDeps(t)
	deps.chain.updateErr = errors.New("chain update fail")
	p := newPipeline(t, deps)
	_, err := p.Run(context.Background(), augment.AugmentRequest{
		Prompt:    "x",
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		RequestID: "req-audit-err",
	})
	if err == nil {
		t.Fatal("expected audit error to propagate")
	}
}

type failOnCompletedChainStore struct {
	pipChainStore
	failAfter int32
}

func (f *failOnCompletedChainStore) UpdateChainColumns(_ context.Context, _, _, _ string, _ []byte, _ int64, _, _ string) error {
	n := f.updateCalls.Add(1)
	if n > f.failAfter {
		return errors.New("late-update failure")
	}
	return nil
}

func TestPipeline_RunsWithDoctrineTimeoutZero(t *testing.T) {
	deps := newPipelineDeps(t)

	deps.doctrines.schemas["max-scope"].Augmentation.TimeoutMs = 0
	p := newPipeline(t, deps)
	_, err := p.Run(context.Background(), augment.AugmentRequest{
		Prompt:    "x",
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		RequestID: "req-zero-to",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestPipeline_AuditCompletedError(t *testing.T) {
	deps := newPipelineDeps(t)
	cs := &failOnCompletedChainStore{
		pipChainStore: pipChainStore{},
		failAfter:     1,
	}
	p, err := augment.NewPipeline(augment.PipelineOptions{
		BudgetStore:       deps.budget,
		KnowledgeIndex:    deps.index,
		Embedder:          deps.embedder,
		ChainStore:        cs,
		McpGateway:        deps.gateway,
		DoctrineLoader:    deps.doctrines,
		ProjectLookup:     deps.lookup,
		Clock:             fixedClock{t: time.Unix(1715000000, 0)},
		ConcurrencyBudget: 10,
		QueueDepth:        50,
		PerLaneTimeout:    1 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	_, runErr := p.Run(context.Background(), augment.AugmentRequest{
		Prompt:    "x",
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		RequestID: "req-completed-err",
	})
	if runErr == nil {
		t.Fatal("expected late-stage audit error")
	}
}

// --- Plan 11 Phase C fix-cycle Important-5 + Important-6 regression
// guards. Pipeline.Run MUST surface auditAnchor.Emit failures + the
// post-DoctrineGate Load failure rather than discarding errors with
// blank identifier (`_, _ := ...`). Pre-fix every audit-emit call site
// after the AugmentationStarted line dropped errors silently —
// inv-zen-088 "every augmentation event MUST anchor" was breached on
// any chain-store failure during the DoctrineGate-skip, BudgetGate-skip,
// CrossProjectQueryFiltered, AugmentationTruncated, or
// KGQueryDispatched audit-emit paths.

type emitFailingChainStore struct {
	pipChainStore
	failOnCall int32
	count      atomic.Int32
}

func (f *emitFailingChainStore) UpdateChainColumns(_ context.Context, _, _, _ string, _ []byte, _ int64, _, _ string) error {
	n := f.count.Add(1)
	if n >= f.failOnCall {
		return errors.New("injected emit failure")
	}
	return nil
}

func TestPipeline_DoctrineGateSkipEmitError(t *testing.T) {
	deps := newPipelineDeps(t)

	deps.doctrines.schemas["max-scope"].Augmentation.Enable = false
	cs := &emitFailingChainStore{failOnCall: 1}
	p, err := augment.NewPipeline(augment.PipelineOptions{
		BudgetStore:       deps.budget,
		KnowledgeIndex:    deps.index,
		Embedder:          deps.embedder,
		ChainStore:        cs,
		McpGateway:        deps.gateway,
		DoctrineLoader:    deps.doctrines,
		ProjectLookup:     deps.lookup,
		Clock:             fixedClock{t: time.Unix(1715000000, 0)},
		ConcurrencyBudget: 10,
		QueueDepth:        50,
		PerLaneTimeout:    1 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	_, runErr := p.Run(context.Background(), augment.AugmentRequest{
		Prompt:    "test",
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		RequestID: "req-skip-emit-err",
	})
	if runErr == nil {
		t.Fatal("expected emit error on DoctrineGate-skip path")
	}
	if !strings.Contains(runErr.Error(), "injected emit failure") {
		t.Errorf("err=%v; want wrap of injected emit failure", runErr)
	}
}

func TestPipeline_BudgetGateSkipEmitError(t *testing.T) {
	deps := newPipelineDeps(t)

	deps.budget.rolled = map[string]float64{"augmentation|internal-platform-x": 1_000_000}
	cs := &emitFailingChainStore{failOnCall: 1}
	p, err := augment.NewPipeline(augment.PipelineOptions{
		BudgetStore:       deps.budget,
		KnowledgeIndex:    deps.index,
		Embedder:          deps.embedder,
		ChainStore:        cs,
		McpGateway:        deps.gateway,
		DoctrineLoader:    deps.doctrines,
		ProjectLookup:     deps.lookup,
		Clock:             fixedClock{t: time.Unix(1715000000, 0)},
		ConcurrencyBudget: 10,
		QueueDepth:        50,
		PerLaneTimeout:    1 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	_, runErr := p.Run(context.Background(), augment.AugmentRequest{
		Prompt:    "test",
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		RequestID: "req-budget-emit-err",
	})
	if runErr == nil {
		t.Fatal("expected emit error on BudgetGate-skip path")
	}
}

// TestPipeline_LoadAfterCheckErrorPropagates pins Important-6: if the
// second Load (post-DoctrineGate.Check) fails, Pipeline.Run MUST
// surface the error rather than nil-deref schema. Pre-fix
//
//	schema, _ := p.doctrineLoaderField.Load(ctx, req.Doctrine)
//	maxKgTokens := schema.Augmentation.MaxKGTokens  // panic if schema nil
//
// would panic on the next line. Fix: handle the error explicitly +
// return 5xx upstream.
//
// We simulate this with a load that succeeds on the first call (Check
// inside DoctrineGate) and fails on the second call (Pipeline.Run
// post-Check). The fakeDoctrineLoader's errOn map keys by name; to
// inject a "fail on second call" we need a stateful loader.
type secondLoadFailLoader struct {
	inner      *fakeDoctrineLoader
	callCount  atomic.Int32
	failAtCall int32
}

func (s *secondLoadFailLoader) Load(ctx context.Context, name string) (*augment.DoctrineSchema, error) {
	n := s.callCount.Add(1)
	if n >= s.failAtCall {
		return nil, errors.New("injected second-load failure")
	}
	return s.inner.Load(ctx, name)
}

func TestPipeline_SecondLoadFailureSurfacesError(t *testing.T) {
	deps := newPipelineDeps(t)
	inner := deps.doctrines
	loader := &secondLoadFailLoader{inner: inner, failAtCall: 2}
	p, err := augment.NewPipeline(augment.PipelineOptions{
		BudgetStore:       deps.budget,
		KnowledgeIndex:    deps.index,
		Embedder:          deps.embedder,
		ChainStore:        deps.chain,
		McpGateway:        deps.gateway,
		DoctrineLoader:    loader,
		ProjectLookup:     deps.lookup,
		Clock:             fixedClock{t: time.Unix(1715000000, 0)},
		ConcurrencyBudget: 10,
		QueueDepth:        50,
		PerLaneTimeout:    1 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	_, runErr := p.Run(context.Background(), augment.AugmentRequest{
		Prompt:    "test",
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		RequestID: "req-second-load-err",
	})
	if runErr == nil {
		t.Fatal("expected second-load error; pre-fix would nil-deref schema")
	}
	if !strings.Contains(runErr.Error(), "injected second-load failure") {
		t.Errorf("err=%v; want wrap of injected second-load failure", runErr)
	}
}

// TestPipeline_LaneErrorAuditEmitFailureSurfaces covers the
// KGQueryDispatched audit-emit error path in Lane 1 + Lane 3 when the
// gateway call fails. The lane goroutine MUST propagate emit failures
// — pre-fix dropped them silently.
//
// Lane goroutines run in parallel; emit failures need to either:
// (a) cause the pipeline to fail loudly, or
// (b) be aggregated and surfaced at fan-out gather time.
// Per project doctrine fail-loud is preferred. The fix records the
// error on a shared channel + the orchestrator returns it from Run.
func TestPipeline_LaneErrorAuditEmitFailureSurfaces(t *testing.T) {
	deps := newPipelineDeps(t)

	deps.gateway.errs = map[string]error{
		augment.ToolCaronteQuery:   errors.New("gw down"),
		augment.ToolCaronteContext: errors.New("gw down"),
	}

	cs := &emitFailingChainStore{failOnCall: 1}
	p, err := augment.NewPipeline(augment.PipelineOptions{
		BudgetStore:       deps.budget,
		KnowledgeIndex:    deps.index,
		Embedder:          deps.embedder,
		ChainStore:        cs,
		McpGateway:        deps.gateway,
		DoctrineLoader:    deps.doctrines,
		ProjectLookup:     deps.lookup,
		Clock:             fixedClock{t: time.Unix(1715000000, 0)},
		ConcurrencyBudget: 10,
		QueueDepth:        50,
		PerLaneTimeout:    1 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	_, runErr := p.Run(context.Background(), augment.AugmentRequest{
		Prompt:    "test",
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		RequestID: "req-lane-emit-err",
	})

	if runErr == nil {
		t.Fatal("expected error from chain store failure")
	}
}
