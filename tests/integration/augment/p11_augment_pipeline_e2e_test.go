// go:build integration

package augment_integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/chain"
	"github.com/cbip-solutions/hades-system/internal/augment"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
)

type e2eDoctrineReader struct {
	doctrine string
	enable   bool
	maxTok   int
}

func (d *e2eDoctrineReader) AugmentationConfig(_ context.Context, _ string) (handlers.AugmentationConfig, error) {
	return handlers.AugmentationConfig{
		Enable:       d.enable,
		MaxKGTokens:  d.maxTok,
		DoctrineName: d.doctrine,
	}, nil
}

type e2eEnv struct {
	t        *testing.T
	pipeline *augment.Pipeline
	chain    *e2eChainStore
	budget   *e2eBudgetStore
	gateway  *e2eGateway
	server   *httptest.Server
}

func setupE2E(t *testing.T) *e2eEnv {
	t.Helper()

	chain := &e2eChainStore{}
	budget := &e2eBudgetStore{}
	gateway := &e2eGateway{
		resps: map[string]any{
			"mcp_zen-swarm_caronte_query": map[string]any{
				"results": []any{
					map[string]any{"note_id": "kg-1", "title": "Engine.SelectWinner | internal/orchestrator/merge/engine.go", "score": 1.5, "snippet": "..."},
				},
			},
			"mcp_zen-swarm_caronte_context": map[string]any{
				"results": []any{
					map[string]any{"note_id": "ctx-1", "title": "Neighbors | internal/orchestrator/merge/decision.go", "score": 1.2, "snippet": "..."},
				},
			},
		},
	}
	idx := &e2eIndex{}
	emb := &e2eEmbedder{}
	doctrines := &e2eDoctrineLoader{
		schemas: map[string]*augment.DoctrineSchema{
			"max-scope": {
				Augmentation: augment.AugmentationAxis{
					Enable: true, MaxKGTokens: 25000, TimeoutMs: 2000,
					CrossProjectScope: "opt-in",
				},
				KnowledgeCrossProject: augment.CrossProjectAxis{
					VisibleTo:       []string{"max-scope", "default"},
					QueriesCanReach: []string{"max-scope", "default"},
				},
			},
		},
	}
	lookup := &e2eProjectLookup{
		projectToDoctrine: map[string]string{"internal-platform-x": "max-scope"},
	}

	pipeline, err := augment.NewPipeline(augment.PipelineOptions{
		BudgetStore:       budget,
		KnowledgeIndex:    idx,
		Embedder:          emb,
		ChainStore:        chain,
		McpGateway:        gateway,
		DoctrineLoader:    doctrines,
		ProjectLookup:     lookup,
		Clock:             augment.SystemClock{},
		ConcurrencyBudget: 10,
		QueueDepth:        50,
		PerLaneTimeout:    1 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}

	dr := &e2eDoctrineReader{doctrine: "max-scope", enable: true, maxTok: 25000}
	runner := func(ctx context.Context, req handlers.PipelineRequest) (handlers.PipelineResponse, error) {
		resp, err := pipeline.Run(ctx, augment.AugmentRequest{
			Prompt:    req.Prompt,
			ProjectID: req.ProjectID,
			Doctrine:  req.Doctrine,
			SessionID: req.SessionID,
			Mode:      req.Mode,
			RequestID: req.RequestID,
		})
		if err != nil {
			return handlers.PipelineResponse{}, err
		}
		staticJSON, _ := json.Marshal(resp.StaticContext)
		volatileJSON, _ := json.Marshal(resp.VolatileContext)
		citationsJSON, _ := json.Marshal(resp.Citations)
		return handlers.PipelineResponse{
			StaticContext:   string(staticJSON),
			VolatileContext: string(volatileJSON),
			Citations:       citationsJSON,
			AuditEventID:    resp.AuditEventID,
			Truncated:       resp.Truncated,
			SkippedReason:   resp.SkippedReason,
		}, nil
	}
	handler := handlers.AugmentWithPipeline(dr, runner)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &e2eEnv{
		t:        t,
		pipeline: pipeline,
		chain:    chain,
		budget:   budget,
		gateway:  gateway,
		server:   server,
	}
}

func TestAugmentE2E_HappyPath(t *testing.T) {
	env := setupE2E(t)

	reqBody, _ := json.Marshal(handlers.AugmentRequest{
		SessionID:  "sess-e2e-1",
		Project:    "internal-platform-x",
		Prompt:     "refactor MergeEngine to support N candidates",
		PromptHash: "abc123",
		Mode:       "interactive",
	})
	resp, err := http.Post(env.server.URL, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}
	var ar handlers.AugmentResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ar.StaticContext == "" {
		t.Error("expected populated StaticContext")
	}
	if ar.AuditEventID == "" {
		t.Error("expected non-empty AuditEventID")
	}

	if len(ar.Citations) == 0 {
		t.Error("expected citations from fused results")
	}
}

func TestAugmentE2E_AuditChainSequential(t *testing.T) {
	env := setupE2E(t)

	reqBody, _ := json.Marshal(handlers.AugmentRequest{
		SessionID:  "sess-e2e-2",
		Project:    "internal-platform-x",
		Prompt:     "test",
		PromptHash: "req-e2e-2",
	})
	resp, err := http.Post(env.server.URL, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	leaves := env.chain.Leaves()
	if len(leaves) < 2 {
		t.Fatalf("expected >=2 chain leaves, got %d", len(leaves))
	}
	for i := 1; i < len(leaves); i++ {
		if leaves[i].PrevHash != leaves[i-1].RecordHash {
			t.Errorf("chain break at i=%d: leaf[%d].prevHash=%q != leaf[%d].recordHash=%q",
				i, i, leaves[i].PrevHash, i-1, leaves[i-1].RecordHash)
		}
	}
}

func TestAugmentE2E_CostLedgerWritten(t *testing.T) {
	env := setupE2E(t)

	reqBody, _ := json.Marshal(handlers.AugmentRequest{
		SessionID:  "sess-e2e-3",
		Project:    "internal-platform-x",
		Prompt:     "test",
		PromptHash: "req-e2e-3",
	})
	resp, err := http.Post(env.server.URL, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	entries := env.budget.LedgerEntries()
	if len(entries) == 0 {
		t.Fatal("expected at least 1 cost_ledger entry")
	}
}

func TestAugmentE2E_LatencyP95Under250ms(t *testing.T) {
	env := setupE2E(t)
	const N = 30
	durations := make([]time.Duration, N)
	for i := 0; i < N; i++ {
		start := time.Now()
		reqBody, _ := json.Marshal(handlers.AugmentRequest{
			SessionID:  "sess-e2e-lat",
			Project:    "internal-platform-x",
			Prompt:     "test",
			PromptHash: fmt.Sprintf("lat-%d", i),
		})
		resp, err := http.Post(env.server.URL, "application/json", bytes.NewReader(reqBody))
		if err != nil {
			t.Fatalf("POST %d: %v", i, err)
		}
		resp.Body.Close()
		durations[i] = time.Since(start)
	}

	sortDurations(durations)
	p95 := durations[(N*95)/100]
	t.Logf("Latency p50=%v p95=%v max=%v", durations[N/2], p95, durations[N-1])
	if p95 > 250*time.Millisecond {
		t.Errorf("p95 latency exceeds budget: %v > 250ms", p95)
	}
}

func sortDurations(d []time.Duration) {
	for i := 1; i < len(d); i++ {
		for j := i; j > 0 && d[j-1] > d[j]; j-- {
			d[j], d[j-1] = d[j-1], d[j]
		}
	}
}

func TestAugmentE2E_CapaFirewallSkipsWith204(t *testing.T) {
	chain := &e2eChainStore{}
	budget := &e2eBudgetStore{}
	gateway := &e2eGateway{}
	idx := &e2eIndex{}
	emb := &e2eEmbedder{}
	doctrines := &e2eDoctrineLoader{
		schemas: map[string]*augment.DoctrineSchema{
			"capa-firewall": {
				Augmentation: augment.AugmentationAxis{
					Enable: false, MaxKGTokens: 0, TimeoutMs: 500,
				},
			},
		},
	}
	lookup := &e2eProjectLookup{
		projectToDoctrine: map[string]string{"secret-proj": "capa-firewall"},
	}
	pipeline, err := augment.NewPipeline(augment.PipelineOptions{
		BudgetStore:       budget,
		KnowledgeIndex:    idx,
		Embedder:          emb,
		ChainStore:        chain,
		McpGateway:        gateway,
		DoctrineLoader:    doctrines,
		ProjectLookup:     lookup,
		Clock:             augment.SystemClock{},
		ConcurrencyBudget: 10,
		QueueDepth:        50,
		PerLaneTimeout:    500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}

	dr := &e2eDoctrineReader{doctrine: "capa-firewall", enable: false, maxTok: 0}
	runner := func(ctx context.Context, req handlers.PipelineRequest) (handlers.PipelineResponse, error) {
		resp, err := pipeline.Run(ctx, augment.AugmentRequest{
			Prompt:    req.Prompt,
			ProjectID: req.ProjectID,
			Doctrine:  req.Doctrine,
			SessionID: req.SessionID,
			RequestID: req.RequestID,
		})
		if err != nil {
			return handlers.PipelineResponse{}, err
		}
		return handlers.PipelineResponse{SkippedReason: resp.SkippedReason}, nil
	}
	handler := handlers.AugmentWithPipeline(dr, runner)
	server := httptest.NewServer(handler)
	defer server.Close()

	reqBody, _ := json.Marshal(handlers.AugmentRequest{
		Project: "secret-proj", Prompt: "anything", SessionID: "sess-e2e-cf",
	})
	resp, err := http.Post(server.URL, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status: want 204, got %d", resp.StatusCode)
	}
}

type chainLeafRecord struct {
	EventID     string
	PrevHash    string
	RecordHash  string
	Partition   string
	LeafID      string
	ProjectID   string
	EventType   string
	Payload     []byte
	EmittedAt   int64
	PartitionID string
}

type e2eChainStore struct {
	mu     sync.Mutex
	leaves []chainLeafRecord
	tip    string
}

func (s *e2eChainStore) GetChainTip(_ context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.tip, nil
}

func (s *e2eChainStore) UpdateChainColumns(_ context.Context, eventID, prevHash, eventType string, payload []byte, emittedAt int64, recordHash, partition string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	payloadCopy := append([]byte(nil), payload...)
	s.leaves = append(s.leaves, chainLeafRecord{
		EventID:     eventID,
		PrevHash:    prevHash,
		RecordHash:  recordHash,
		Partition:   partition,
		PartitionID: partition,
		EventType:   eventType,
		Payload:     payloadCopy,
		EmittedAt:   emittedAt,
	})
	s.tip = recordHash
	return nil
}

func (s *e2eChainStore) UpdateTesseraLeafID(_ context.Context, eventID, leafID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.leaves {
		if s.leaves[i].EventID == eventID {
			s.leaves[i].LeafID = leafID
			return nil
		}
	}
	return nil
}

func (s *e2eChainStore) AppendTesseraLeaf(_ context.Context, in augment.TesseraLeafInput) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	leafID := fmt.Sprintf("leaf-%d", len(s.leaves))
	for i := range s.leaves {
		if s.leaves[i].EventID == in.EventID {
			s.leaves[i].LeafID = leafID
			s.leaves[i].ProjectID = in.ProjectID
			break
		}
	}
	return leafID, nil
}

func (s *e2eChainStore) Leaves() []chainLeafRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]chainLeafRecord, len(s.leaves))
	copy(out, s.leaves)
	return out
}

type e2eBudgetStore struct {
	mu      sync.Mutex
	ledger  []augment.CostLedgerEntry
	rolled  map[string]float64
	seenIDs map[string]bool
}

func (b *e2eBudgetStore) RolledUSDByAxis(_ context.Context, axis, value string, _ int64) (float64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.rolled[axis+"|"+value], nil
}

func (b *e2eBudgetStore) InsertCostLedgerEntry(_ context.Context, entry augment.CostLedgerEntry) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.seenIDs == nil {
		b.seenIDs = map[string]bool{}
	}
	if b.seenIDs[entry.RequestID] {
		return nil
	}
	b.seenIDs[entry.RequestID] = true
	b.ledger = append(b.ledger, entry)
	return nil
}

func (b *e2eBudgetStore) LedgerEntries() []augment.CostLedgerEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]augment.CostLedgerEntry, len(b.ledger))
	copy(out, b.ledger)
	return out
}

type e2eGateway struct {
	calls atomic.Int32
	resps map[string]any
}

func (g *e2eGateway) CallTool(_ context.Context, name string, _ map[string]any) (any, error) {
	g.calls.Add(1)
	if r, ok := g.resps[name]; ok {
		return r, nil
	}
	return map[string]any{"results": []any{}}, nil
}

type e2eDoctrineLoader struct {
	schemas map[string]*augment.DoctrineSchema
}

func (d *e2eDoctrineLoader) Load(_ context.Context, name string) (*augment.DoctrineSchema, error) {
	if s, ok := d.schemas[name]; ok {
		return s, nil
	}
	return nil, fmt.Errorf("e2eDoctrineLoader: unknown %q", name)
}

type e2eProjectLookup struct {
	projectToDoctrine map[string]string
}

func (l *e2eProjectLookup) DoctrineForProject(_ context.Context, p string) (string, error) {
	if d, ok := l.projectToDoctrine[p]; ok {
		return d, nil
	}
	return "", fmt.Errorf("project not found: %q", p)
}

type e2eIndex struct{}

func (e2eIndex) QueryFTS(_ context.Context, _ string, _ int) ([]augment.QueryResult, error) {
	return []augment.QueryResult{
		{NoteID: "n-fts", Title: "BudgetGate.Check | internal/budget/enforce.go", Score: 1.5, ProjectID: "internal-platform-x", Source: "fts"},
	}, nil
}
func (e2eIndex) QueryVec(_ context.Context, _ []float32, _ int, _ float64) ([]augment.QueryResult, error) {
	return []augment.QueryResult{
		{NoteID: "n-vec", Title: "Compute | internal/audit/chain/compute.go", Score: 0.95, ProjectID: "internal-platform-x", Source: "vec"},
	}, nil
}
func (e2eIndex) QueryGraph(_ context.Context, _ []string, _, _ int) ([]augment.QueryResult, error) {
	return nil, nil
}

type e2eEmbedder struct{}

func (e2eEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{0.1, 0.2, 0.3}, nil
}

type e2eChainEventStore struct {
	store *e2eChainStore
}

func (e *e2eChainEventStore) ListPartitions(_ context.Context) ([]chain.PartitionStat, error) {
	e.store.mu.Lock()
	defer e.store.mu.Unlock()
	partitionToFinal := map[string]string{}
	partitionToCount := map[string]int64{}
	partitionToFirst := map[string]string{}
	partitionToLast := map[string]string{}
	for _, leaf := range e.store.leaves {
		partitionToCount[leaf.Partition]++
		if _, ok := partitionToFirst[leaf.Partition]; !ok {
			partitionToFirst[leaf.Partition] = leaf.EventID
		}
		partitionToLast[leaf.Partition] = leaf.EventID
		partitionToFinal[leaf.Partition] = leaf.RecordHash
	}
	out := make([]chain.PartitionStat, 0, len(partitionToCount))
	for p, n := range partitionToCount {
		out = append(out, chain.PartitionStat{
			PartitionID:     p,
			FirstID:         partitionToFirst[p],
			LastID:          partitionToLast[p],
			EventCount:      n,
			FinalRecordHash: partitionToFinal[p],
		})
	}
	return out, nil
}

func (e *e2eChainEventStore) ListEventsForPartition(_ context.Context, partition string) ([]chain.EventRow, error) {
	e.store.mu.Lock()
	defer e.store.mu.Unlock()
	out := make([]chain.EventRow, 0)
	for _, leaf := range e.store.leaves {
		if leaf.Partition != partition {
			continue
		}
		out = append(out, chain.EventRow{
			ID:          leaf.EventID,
			ProjectID:   leaf.ProjectID,
			Type:        leaf.EventType,
			PayloadJSON: string(leaf.Payload),
			EmittedAt:   leaf.EmittedAt,
			PrevHash:    leaf.PrevHash,
			RecordHash:  leaf.RecordHash,
			PartitionID: leaf.Partition,
		})
	}
	return out, nil
}

func (*e2eChainEventStore) GetChainTip(_ context.Context) (string, error) {
	return "", chain.ErrNoChainTip
}
func (*e2eChainEventStore) GetEventByID(_ context.Context, _ string) (*chain.EventRow, error) {
	return nil, chain.ErrEventNotFound
}
func (*e2eChainEventStore) GetByEventID(_ context.Context, _ string) (*chain.EventRow, error) {
	return nil, chain.ErrEventNotFound
}
func (*e2eChainEventStore) UpdateChainColumns(_ context.Context, _, _, _, _ string) error {
	return errors.New("e2eChainEventStore: not implemented")
}
func (*e2eChainEventStore) UpdateTesseraLeafID(_ context.Context, _, _ string) error {
	return errors.New("e2eChainEventStore: not implemented")
}
func (*e2eChainEventStore) InsertPartitionSeal(_ context.Context, _ chain.SealRecord) error {
	return errors.New("e2eChainEventStore: not implemented")
}
func (*e2eChainEventStore) GetPartitionSeal(_ context.Context, _ string) (*chain.SealRecord, error) {
	return nil, chain.ErrPartitionSealNotFound
}
func (*e2eChainEventStore) BackfillScan(_ context.Context, _ int64, _ int) ([]chain.BackfillCursorRow, error) {
	return nil, errors.New("e2eChainEventStore: not implemented")
}

// TestAugmentE2E_ChainWalkerVerifiesAllEvents materialises real audit
// events via augment.Pipeline.Run AND THEN runs chain.Walker over them
// — the canonical audit-chain-integrity doctor. Every event MUST report
// as Verified (zero Tampered, zero Gaps). Pre-fix this test would fail
// for 100% of events because augment used NUL-byte separators + nano
// timestamps while chain.Compute uses pipe + unix-seconds.
//
// fix-cycle Critical-1 + Critical-2 + Important-4 + Important-7
// closure proof: this test passes ONLY when ALL four bugs are closed.
func TestAugmentE2E_ChainWalkerVerifiesAllEvents(t *testing.T) {
	env := setupE2E(t)

	const runs = 5
	for i := 0; i < runs; i++ {
		reqBody, _ := json.Marshal(handlers.AugmentRequest{
			SessionID:  fmt.Sprintf("sess-walker-%d", i),
			Project:    "internal-platform-x",
			Prompt:     fmt.Sprintf("verify chain integrity %d", i),
			PromptHash: fmt.Sprintf("walker-req-%d", i),
			Mode:       "interactive",
		})
		resp, err := http.Post(env.server.URL, "application/json", bytes.NewReader(reqBody))
		if err != nil {
			t.Fatalf("POST %d: %v", i, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d: want 200, got %d", i, resp.StatusCode)
		}
	}

	leaves := env.chain.Leaves()
	if len(leaves) < runs*2 {
		t.Fatalf("expected >= %d chain leaves (started+completed per run), got %d", runs*2, len(leaves))
	}

	store := &e2eChainEventStore{store: env.chain}
	report, err := chain.Walk(context.Background(), store, "internal-platform-x")
	if err != nil {
		t.Fatalf("chain.Walk: %v", err)
	}

	if len(report.Tampered) != 0 {

		t.Errorf("chain.Walk reported %d Tampered events (want 0):", len(report.Tampered))
		for _, r := range report.Tampered {
			t.Errorf("  - %s in %s: expected %s, stored %s", r.EventID, r.PartitionID, r.ExpectedHash, r.StoredHash)
		}
	}
	if len(report.GapsDetected) != 0 {
		t.Errorf("chain.Walk reported %d Gaps (want 0):", len(report.GapsDetected))
		for _, g := range report.GapsDetected {
			t.Errorf("  - %s in %s: expected prev %s, stored %s", g.EventID, g.PartitionID, g.ExpectedPrevHash, g.StoredPrevHash)
		}
	}
	if report.EventsWalked < int64(runs*2) {
		t.Errorf("EventsWalked=%d; want >= %d", report.EventsWalked, runs*2)
	}
	if report.PartitionsWalked < 1 {
		t.Errorf("PartitionsWalked=%d; want >= 1", report.PartitionsWalked)
	}

	for _, leaf := range leaves {
		if leaf.ProjectID != "internal-platform-x" {
			t.Errorf("leaf %s ProjectID=%q; want internal-platform-x (Important-4)", leaf.EventID, leaf.ProjectID)
		}
		if leaf.EventType == "" || leaf.EventType == "augmentation.chain.placeholder" {
			t.Errorf("leaf %s EventType=%q; want canonical augment label (Important-4)", leaf.EventID, leaf.EventType)
		}
	}

	now := time.Now().Unix()
	for _, leaf := range leaves {
		if leaf.EmittedAt > now+10 || leaf.EmittedAt < now-3600 {
			t.Errorf("leaf %s EmittedAt=%d outside reasonable unix-seconds window (now=%d); Critical-2 regression suspected",
				leaf.EventID, leaf.EmittedAt, now)
		}
	}
}
