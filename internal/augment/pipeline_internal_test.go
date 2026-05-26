package augment

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/citation"
)

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func TestCitationSourceFromLane_AllVariants(t *testing.T) {
	cases := []struct {
		lane string
		want citation.CitationSource
	}{
		{"kg", citation.SourceCaronteQuery},
		{"graph", citation.SourceCaronteContext},
		{"fts", citation.SourceAggregatorFTS},
		{"vec", citation.SourceAggregatorVec},
		{"temporal", citation.SourceTemporal},
		{"unknown_source", citation.SourceManualOverride},
		{"", citation.SourceManualOverride},
	}
	for _, c := range cases {
		got := citationSourceFromLane(c.lane)
		if got != c.want {
			t.Errorf("lane=%q: want %v, got %v", c.lane, c.want, got)
		}
	}
}

func TestRetrievalLaneFromSource_AllVariants(t *testing.T) {
	cases := []struct {
		src  string
		want citation.RetrievalLane
	}{
		{"kg", citation.LaneSemantic},
		{"graph", citation.LaneGraph},
		{"fts", citation.LaneLexical},
		{"vec", citation.LaneRerank},
		{"temporal", citation.LaneTemporal},
		{"other", citation.LaneLexical},
	}
	for _, c := range cases {
		got := retrievalLaneFromSource(c.src)
		if got != c.want {
			t.Errorf("src=%q: want %v, got %v", c.src, c.want, got)
		}
	}
}

func TestClampConfidence(t *testing.T) {
	cases := []struct {
		in, want float64
	}{
		{-1.0, 0},
		{0, 0},
		{0.5, 0.5},
		{1.0, 1},
		{2.5, 1},
	}
	for _, c := range cases {
		got := clampConfidence(c.in)
		if got != c.want {
			t.Errorf("clampConfidence(%f): want %f, got %f", c.in, c.want, got)
		}
	}
}

func TestClampNonNegative(t *testing.T) {
	if clampNonNegative(-5) != 0 {
		t.Error("negative should clamp to 0")
	}
	if clampNonNegative(5) != 5 {
		t.Error("positive should pass through")
	}
}

func TestMakeCitationID(t *testing.T) {

	cases := []struct {
		noteID  string
		idx     int
		wantHas string
	}{
		{"AbC123", 0, "abc123"},
		{"!!!", 1, "c-idx1"},
		{"x", 2, "c-x"},

		{"abcdefghijklmnopqrstuvwxyz", 3, "c-abcdefghijkl"},
	}
	for _, c := range cases {
		got := makeCitationID(c.noteID, c.idx)

		if got[0] != 'c' || got[1] != '-' {
			t.Errorf("noteID=%q: want c- prefix, got %q", c.noteID, got)
		}
		if !contains(got, c.wantHas) {
			t.Errorf("noteID=%q: want %q in %q", c.noteID, c.wantHas, got)
		}

		if err := citation.CitationID(got).Validate(); err != nil {
			t.Errorf("noteID=%q: invalid citation id %q: %v", c.noteID, got, err)
		}
	}
}

func TestCitationHashSuffix_PaddingBranch(t *testing.T) {

	const n = 14
	got := citationHashSuffix("any-note-id", n)
	if len(got) != n {
		t.Errorf("len=%d; want %d", len(got), n)
	}

	if got[0] != '0' {
		t.Errorf("expected '0' padding at the left when truncation forces padding; got=%q", string(got))
	}
}

// TestMakeCitationID_TruncationCollisionDistinct pins Plan 11 Phase C
// fix-cycle Minor-9 closure. Two noteIDs that share the same 14-char
// alphanumeric prefix MUST produce distinct citation IDs — pre-fix the
// truncation step silently dropped the suffix and both rows landed on
// the same CitationID, breaking citation routing in the daemon.
func TestMakeCitationID_TruncationCollisionDistinct(t *testing.T) {

	a := makeCitationID("abcdefghijklmnAAAA", 0)
	b := makeCitationID("abcdefghijklmnBBBB", 1)
	if a == b {
		t.Fatalf("citation IDs collided after 14-char truncation: a=%q b=%q (Minor-9 regression)", a, b)
	}

	if err := citation.CitationID(a).Validate(); err != nil {
		t.Errorf("a=%q invalid: %v", a, err)
	}
	if err := citation.CitationID(b).Validate(); err != nil {
		t.Errorf("b=%q invalid: %v", b, err)
	}
}

func TestBuildCitations_EmptyInput(t *testing.T) {
	got := buildCitations(nil, "p")
	if got != nil {
		t.Errorf("empty input: want nil, got %v", got)
	}
}

func TestBuildCitations_FallbackPath(t *testing.T) {

	fused := []RRFFusedResult{
		{NoteID: "abc", Title: "", Snippet: "", Score: 0.5, ProjectID: "p", Source: "fts"},
	}
	out := buildCitations(fused, "p")
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	if out[0].Payload != "abc" {
		t.Errorf("expected fallback payload=NoteID, got %q", out[0].Payload)
	}

	fused2 := []RRFFusedResult{
		{NoteID: "abc", Title: "T", Score: 0.5, ProjectID: "", Source: "fts"},
	}
	out2 := buildCitations(fused2, "caller-proj")
	if out2[0].ProjectID != "caller-proj" {
		t.Errorf("expected caller-proj, got %q", out2[0].ProjectID)
	}
}

func TestBuildCitations_UnanchoredAuditID(t *testing.T) {
	fused := []RRFFusedResult{
		{NoteID: "n1", Title: "X", Score: 0.5, ProjectID: "p", AuditChainAnchor: "", Source: "kg"},
	}
	out := buildCitations(fused, "p")
	if !contains(out[0].AuditEventID, "evt-unanchored") {
		t.Errorf("expected evt-unanchored fallback, got %q", out[0].AuditEventID)
	}
}

func TestReleaseQueued(t *testing.T) {
	r := &pipelineRuntime{}
	r.mu.Lock()
	r.queued = 5
	r.mu.Unlock()
	r.releaseQueued()
	r.mu.Lock()
	if r.queued != 4 {
		t.Errorf("expected queued=4, got %d", r.queued)
	}
	r.mu.Unlock()
}

func TestReleaseQueued_BroadcastsWhenCondInit(t *testing.T) {
	r := &pipelineRuntime{}

	_, err := r.tryAcquire(1, 5)
	if err != nil {
		t.Fatalf("tryAcquire: %v", err)
	}
	_, err = r.tryAcquire(1, 5)
	if err != nil {
		t.Fatalf("tryAcquire: %v", err)
	}

	r.releaseQueued()
	if r.queued != 0 {
		t.Errorf("queued=%d; want 0 after releaseQueued", r.queued)
	}
}

func TestMustJSON_PanicOnUnmarshalable(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on unmarshalable value")
		}
	}()

	_ = mustJSON(make(chan int))
}

func TestMustJSON_HappyPath(t *testing.T) {
	got := mustJSON(map[string]string{"a": "b"})
	if string(got) != `{"a":"b"}` {
		t.Errorf("unexpected output: %s", got)
	}
}

func TestConvertToRRFFused_UnknownLane(t *testing.T) {

	fused := []QueryResult{
		{NoteID: "n1", Title: "T"},
	}
	lanes := []TopK{
		{Source: "unknown-lane-source", Results: []QueryResult{{NoteID: "n1"}}},
	}
	out := convertToRRFFused(fused, lanes)
	if len(out) != 1 {
		t.Errorf("want 1, got %d", len(out))
	}
	if len(out[0].LaneIDs) != 0 {
		t.Errorf("unknown lane source should yield no LaneIDs, got %v", out[0].LaneIDs)
	}
}

func TestParseGatewayResults_NotAMap(t *testing.T) {
	out := parseGatewayResults("not-a-map", "fts", "p")
	if out != nil {
		t.Errorf("expected nil for non-map, got %v", out)
	}
}

func TestParseGatewayResults_ResultsNotArray(t *testing.T) {
	out := parseGatewayResults(map[string]any{"results": "not-an-array"}, "fts", "p")
	if out != nil {
		t.Errorf("expected nil for non-array results, got %v", out)
	}
}

func TestParseGatewayResults_ResultNotMap(t *testing.T) {
	out := parseGatewayResults(map[string]any{
		"results": []any{"not-a-map", 123},
	}, "fts", "p")
	if len(out) != 0 {
		t.Errorf("expected empty result for non-map entries, got %v", out)
	}
}

func TestParseGatewayResults_WithAuditAnchor(t *testing.T) {
	out := parseGatewayResults(map[string]any{
		"results": []any{
			map[string]any{
				"note_id":            "n1",
				"title":              "T",
				"score":              1.5,
				"audit_chain_anchor": "2026_05:evt-x:hash",
			},
		},
	}, "fts", "p")
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	if out[0].AuditChainAnchor != "2026_05:evt-x:hash" {
		t.Errorf("unexpected anchor: %q", out[0].AuditChainAnchor)
	}
}

func TestPrivacyFilter_DropsOtherDoctrine_Debug(t *testing.T) {
	loader := struct {
		schema *DoctrineSchema
	}{
		schema: &DoctrineSchema{
			KnowledgeCrossProject: CrossProjectAxis{
				QueriesCanReach: []string{"max-scope", "default"},
			},
		},
	}
	_ = loader
	pf := NewPrivacyFilter(
		schemaLoaderDbg{schema: &DoctrineSchema{
			KnowledgeCrossProject: CrossProjectAxis{
				QueriesCanReach: []string{"max-scope", "default"},
			},
		}},
		projLookupDbg{
			"internal-platform-x": "max-scope",
			"other-proj":          "capa-firewall",
		},
	)
	filtered, dropped, err := pf.FilterCrossProject(context.Background(), PrivacyFilterInput{
		SourceDoctrine: "max-scope",
		SourceProject:  "internal-platform-x",
		Candidates: []QueryResult{
			{ProjectID: "internal-platform-x", NoteID: "n1"},
			{ProjectID: "other-proj", NoteID: "n2"},
		},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	t.Logf("filtered=%+v dropped=%+v", filtered, dropped)
	if len(dropped) != 1 || dropped[0] != "other-proj" {
		t.Errorf("expected dropped=[other-proj], got %v", dropped)
	}
}

type schemaLoaderDbg struct{ schema *DoctrineSchema }

func (s schemaLoaderDbg) Load(_ context.Context, _ string) (*DoctrineSchema, error) {
	return s.schema, nil
}

type projLookupDbg map[string]string

func (m projLookupDbg) DoctrineForProject(_ context.Context, p string) (string, error) {
	if d, ok := m[p]; ok {
		return d, nil
	}
	return "", fakeErr("not found: " + p)
}

func TestApplyPrivacyFilter_Internal_LoaderError(t *testing.T) {
	loader := errLoader{}
	lookup := mapLookup{"internal-platform-x": "max-scope"}
	pf := &PrivacyFilter{loader: loader, lookup: lookup}
	_, _, err := pf.FilterCrossProject(context.Background(), PrivacyFilterInput{
		SourceDoctrine: "max-scope",
		SourceProject:  "internal-platform-x",
		Candidates:     []QueryResult{{NoteID: "n1", ProjectID: "internal-platform-x"}},
	})
	if err == nil {
		t.Error("expected loader error")
	}
}

type errLoader struct{}

func (errLoader) Load(_ context.Context, _ string) (*DoctrineSchema, error) {
	return nil, fakeErr("intentional")
}

type fakeErr string

func (e fakeErr) Error() string { return string(e) }

type mapLookup map[string]string

func (m mapLookup) DoctrineForProject(_ context.Context, p string) (string, error) {
	if d, ok := m[p]; ok {
		return d, nil
	}
	return "", fakeErr("not found")
}

func TestAsString(t *testing.T) {
	if asString("hello") != "hello" {
		t.Error("string round-trip failed")
	}
	if asString(123) != "" {
		t.Error("non-string should return empty")
	}
	if asString(nil) != "" {
		t.Error("nil should return empty")
	}
}

func TestTryAcquireQueuesWhenInflightFull(t *testing.T) {
	r := &pipelineRuntime{}

	state, err := r.tryAcquire(1, 5)
	if err != nil || state != "inflight" {
		t.Fatalf("first acquire: state=%q err=%v; want inflight nil", state, err)
	}

	state, err = r.tryAcquire(1, 5)
	if err != nil || state != "queued" {
		t.Fatalf("second acquire: state=%q err=%v; want queued nil", state, err)
	}
}

func TestTryAcquireFailsWhenQueueFull(t *testing.T) {
	r := &pipelineRuntime{}
	if _, err := r.tryAcquire(1, 1); err != nil {
		t.Fatalf("acquire 1: %v", err)
	}
	if _, err := r.tryAcquire(1, 1); err != nil {
		t.Fatalf("acquire 2 (queued): %v", err)
	}

	if _, err := r.tryAcquire(1, 1); err == nil {
		t.Error("third acquire returned nil err; want queue-full error")
	}
}

func TestRunQueuedCancellation(t *testing.T) {
	t.Parallel()
	pipeline := newRuntimeTestPipeline(t)

	pipeline.runtimeState.mu.Lock()
	pipeline.runtimeState.inflight = 1
	pipeline.runtimeState.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	resultCh := make(chan error, 1)
	go func() {
		_, err := pipeline.Run(ctx, AugmentRequest{
			Prompt:    "x",
			ProjectID: "internal-platform-x",
			Doctrine:  "max-scope",
			SessionID: "sess-1",
			RequestID: "req-queued-cancel",
		})
		resultCh <- err
	}()

	deadline := time.After(2 * time.Second)
	for {
		pipeline.runtimeState.mu.Lock()
		queued := pipeline.runtimeState.queued
		pipeline.runtimeState.mu.Unlock()
		if queued >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("goroutine never entered queued state within 2s")
		case <-time.After(1 * time.Millisecond):
		}
	}

	cancel()

	select {
	case err := <-resultCh:
		if err == nil {
			t.Fatal("Run returned nil err; want context.Canceled from queued cancellation")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err=%v; want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run goroutine didn't return within 2s after cancel")
	}

	pipeline.runtimeState.mu.Lock()
	q := pipeline.runtimeState.queued
	pipeline.runtimeState.mu.Unlock()
	if q != 0 {
		t.Errorf("queued=%d after cancellation; want 0 (releaseQueued must have decremented)", q)
	}
}

func newRuntimeTestPipeline(t *testing.T) *Pipeline {
	t.Helper()
	return &Pipeline{
		doctrine: &DoctrineGate{loader: runtimeTestDoctrineLoader{}},
		budget: &BudgetGate{
			store: runtimeTestBudgetStore{},
			clock: SystemClock{},
		},
		privacy:             &PrivacyFilter{loader: runtimeTestDoctrineLoader{}, lookup: runtimeTestProjectLookup{}},
		aggregator:          &AggregatorConsumer{index: runtimeTestIndex{}, embedder: runtimeTestEmbedder{}},
		gateway:             runtimeTestGateway{},
		chain:               runtimeTestChainStore{},
		clock:               SystemClock{},
		auditAnchor:         &AuditAnchor{store: runtimeTestChainStore{}, clock: SystemClock{}},
		truncation:          &Truncation{},
		cacheSplit:          &CacheSplit{},
		concurrency:         1,
		queueDepth:          10,
		perLaneTO:           1 * time.Second,
		runtimeState:        &pipelineRuntime{},
		doctrineLoaderField: runtimeTestDoctrineLoader{},
	}
}

type runtimeTestDoctrineLoader struct{}

func (runtimeTestDoctrineLoader) Load(_ context.Context, _ string) (*DoctrineSchema, error) {
	return &DoctrineSchema{
		Augmentation: AugmentationAxis{Enable: true, MaxKGTokens: 25000, TimeoutMs: 1000},
	}, nil
}

type runtimeTestBudgetStore struct{}

func (runtimeTestBudgetStore) RolledUSDByAxis(_ context.Context, _, _ string, _ int64) (float64, error) {
	return 0, nil
}
func (runtimeTestBudgetStore) InsertCostLedgerEntry(_ context.Context, _ CostLedgerEntry) error {
	return nil
}

type runtimeTestProjectLookup struct{}

func (runtimeTestProjectLookup) DoctrineForProject(_ context.Context, _ string) (string, error) {
	return "max-scope", nil
}

type runtimeTestIndex struct{}

func (runtimeTestIndex) QueryFTS(_ context.Context, _ string, _ int) ([]QueryResult, error) {
	return nil, nil
}
func (runtimeTestIndex) QueryVec(_ context.Context, _ []float32, _ int, _ float64) ([]QueryResult, error) {
	return nil, nil
}
func (runtimeTestIndex) QueryGraph(_ context.Context, _ []string, _, _ int) ([]QueryResult, error) {
	return nil, nil
}

type runtimeTestEmbedder struct{}

func (runtimeTestEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{0, 0, 0}, nil
}

type runtimeTestGateway struct{}

func (runtimeTestGateway) CallTool(_ context.Context, _ string, _ map[string]any) (any, error) {
	return map[string]any{"results": []any{}}, nil
}

type runtimeTestChainStore struct{}

func (runtimeTestChainStore) GetChainTip(_ context.Context) (string, error) { return "", nil }
func (runtimeTestChainStore) UpdateChainColumns(_ context.Context, _, _, _ string, _ []byte, _ int64, _, _ string) error {
	return nil
}
func (runtimeTestChainStore) UpdateTesseraLeafID(_ context.Context, _, _ string) error { return nil }
func (runtimeTestChainStore) AppendTesseraLeaf(_ context.Context, _ TesseraLeafInput) (string, error) {
	return "leaf-x", nil
}
