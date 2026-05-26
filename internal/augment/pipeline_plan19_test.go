package augment

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeCoChangeGateway struct {
	mu          sync.Mutex
	calledTools []string
}

func (f *fakeCoChangeGateway) CallTool(_ context.Context, toolName string, _ map[string]any) (any, error) {
	f.mu.Lock()
	f.calledTools = append(f.calledTools, toolName)
	f.mu.Unlock()
	switch toolName {
	case ToolCaronteCoChange:
		return map[string]any{
			"peers": []any{
				map[string]any{"path": "internal/x/a.go", "coupling_percent": 60.0, "shared_revs": 6.0, "window_days": 90.0},
				map[string]any{"path": "internal/x/b.go", "coupling_percent": 40.0, "shared_revs": 4.0, "window_days": 90.0},
			},
		}, nil
	default:
		return map[string]any{"results": []any{}}, nil
	}
}

func TestParseCoChangeResultsMapsPeersToTemporal(t *testing.T) {
	resp := map[string]any{
		"peers": []any{
			map[string]any{"path": "internal/x/a.go", "coupling_percent": 60.0, "shared_revs": 6.0, "window_days": 90.0},
		},
	}
	got := parseCoChangeResults(resp, "proj-1")
	if len(got) != 1 {
		t.Fatalf("parseCoChangeResults len = %d; want 1", len(got))
	}
	r := got[0]
	if r.Source != "temporal" {
		t.Errorf("Source = %q; want temporal (fuses into lane 5)", r.Source)
	}
	if r.NoteID != "internal/x/a.go" || r.Title != "internal/x/a.go" {
		t.Errorf("NoteID/Title = %q/%q; want internal/x/a.go", r.NoteID, r.Title)
	}
	if r.Score != 0.60 {
		t.Errorf("Score = %v; want 0.60 (coupling_percent/100)", r.Score)
	}
	if r.ProjectID != "proj-1" {
		t.Errorf("ProjectID = %q; want proj-1", r.ProjectID)
	}
	if r.Snippet == "" {
		t.Error("Snippet should be a human co-change summary, got empty")
	}
}

func TestParseCoChangeResultsToleratesMalformed(t *testing.T) {
	if got := parseCoChangeResults("not-a-map", "p"); got != nil {
		t.Errorf("malformed payload → %v; want nil", got)
	}
	if got := parseCoChangeResults(map[string]any{"results": []any{}}, "p"); got != nil {
		t.Errorf("missing peers key → %v; want nil", got)
	}
}

func TestLaneToolNamesShareSegmentAndAreDistinct(t *testing.T) {
	const prefix = "mcp_zen-swarm_"
	const querySuffix = "query"

	if !strings.HasPrefix(ToolCaronteQuery, prefix) || !strings.HasSuffix(ToolCaronteQuery, "_"+querySuffix) {
		t.Fatalf("ToolCaronteQuery = %q; want %s<segment>_%s shape", ToolCaronteQuery, prefix, querySuffix)
	}
	segment := strings.TrimSuffix(strings.TrimPrefix(ToolCaronteQuery, prefix), "_"+querySuffix)
	if segment == "" {
		t.Fatalf("could not derive code-graph wire segment from %q", ToolCaronteQuery)
	}

	want := map[string]string{
		"ToolCaronteQuery":    prefix + segment + "_query",
		"ToolCaronteContext":  prefix + segment + "_context",
		"ToolCaronteCoChange": prefix + segment + "_get_cochange",
	}
	got := map[string]string{
		"ToolCaronteQuery":    ToolCaronteQuery,
		"ToolCaronteContext":  ToolCaronteContext,
		"ToolCaronteCoChange": ToolCaronteCoChange,
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s = %q; want %q (lane must share the %q segment)", name, got[name], w, segment)
		}
	}

	if ToolCaronteQuery == ToolCaronteContext || ToolCaronteQuery == ToolCaronteCoChange || ToolCaronteContext == ToolCaronteCoChange {
		t.Errorf("lane tool-name constants are not distinct: query=%q context=%q cochange=%q", ToolCaronteQuery, ToolCaronteContext, ToolCaronteCoChange)
	}
}

func newPipelineForLaneTest(t *testing.T, gw McpGateway) *Pipeline {
	t.Helper()
	return &Pipeline{
		doctrine: &DoctrineGate{loader: runtimeTestDoctrineLoader{}},
		budget: &BudgetGate{
			store: runtimeTestBudgetStore{},
			clock: SystemClock{},
		},
		privacy:             &PrivacyFilter{loader: runtimeTestDoctrineLoader{}, lookup: runtimeTestProjectLookup{}},
		aggregator:          &AggregatorConsumer{index: laneTestIndex{}, embedder: runtimeTestEmbedder{}},
		gateway:             gw,
		chain:               runtimeTestChainStore{},
		clock:               SystemClock{},
		auditAnchor:         &AuditAnchor{store: runtimeTestChainStore{}, clock: SystemClock{}},
		truncation:          &Truncation{},
		cacheSplit:          &CacheSplit{},
		concurrency:         10,
		queueDepth:          50,
		perLaneTO:           1 * time.Second,
		runtimeState:        &pipelineRuntime{},
		doctrineLoaderField: runtimeTestDoctrineLoader{},
	}
}

func defaultLaneTestSchema() *DoctrineSchema {
	return &DoctrineSchema{
		Augmentation: AugmentationAxis{
			Enable:      true,
			MaxKGTokens: 25000,
			TimeoutMs:   1000,
		},
	}
}

type laneTestIndex struct{}

func (laneTestIndex) QueryFTS(_ context.Context, _ string, _ int) ([]QueryResult, error) {
	return nil, nil
}
func (laneTestIndex) QueryVec(_ context.Context, _ []float32, _ int, _ float64) ([]QueryResult, error) {
	return nil, nil
}
func (laneTestIndex) QueryGraph(_ context.Context, _ []string, _, _ int) ([]QueryResult, error) {
	return nil, nil
}

func TestRunFiveLanesFusesCoChangeIntoTemporal(t *testing.T) {
	gw := &fakeCoChangeGateway{}
	p := newPipelineForLaneTest(t, gw)
	lanes, emitErr := p.runFiveLanes(context.Background(), AugmentRequest{
		Prompt: "internal/x/a.go", ProjectID: "proj-1", Doctrine: "default", RequestID: "req-1",
	}, defaultLaneTestSchema())
	if emitErr != nil {
		t.Fatalf("runFiveLanes emitErr = %v", emitErr)
	}
	// The co-change tool MUST have been dispatched.
	var sawCoChange bool
	for _, tn := range gw.calledTools {
		if tn == ToolCaronteCoChange {
			sawCoChange = true
		}
	}
	if !sawCoChange {
		t.Fatalf("lane 5 did not dispatch %s; called: %v", ToolCaronteCoChange, gw.calledTools)
	}
	// The temporal lane MUST carry the two co-change peers.
	var temporal *TopK
	for i := range lanes {
		if lanes[i].Source == "temporal" {
			temporal = &lanes[i]
		}
	}
	if temporal == nil {
		t.Fatal("no temporal lane in result; co-change peers were dropped")
	}
	var sawPeerA bool
	for _, r := range temporal.Results {
		if r.NoteID == "internal/x/a.go" {
			sawPeerA = true
		}
	}
	if !sawPeerA {
		t.Errorf("temporal lane missing co-change peer internal/x/a.go; results: %+v", temporal.Results)
	}
}

func TestLane5CoChangeErrorToleratedWithTemporalResults(t *testing.T) {

	gw := &errCoChangeGateway{}
	p := newPipelineForLaneTest(t, gw)

	lanes, emitErr := p.runFiveLanes(context.Background(), AugmentRequest{
		Prompt: "some-prompt", ProjectID: "proj-1", Doctrine: "default", RequestID: "req-2",
	}, defaultLaneTestSchema())
	if emitErr != nil {
		t.Fatalf("runFiveLanes emitErr = %v; co-change error must not propagate", emitErr)
	}

	_ = lanes
}

type errCoChangeGateway struct{}

func (e *errCoChangeGateway) CallTool(_ context.Context, _ string, _ map[string]any) (any, error) {
	return nil, errFakeCoChange("co-change unavailable")
}

type errFakeCoChange string

func (e errFakeCoChange) Error() string { return string(e) }
