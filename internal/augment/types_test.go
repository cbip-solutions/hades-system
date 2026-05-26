package augment_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/augment"
)

func TestAugmentRequestFieldSet(t *testing.T) {
	expected := []string{"prompt", "project_id", "doctrine", "session_id", "mode", "request_id"}
	got := jsonFieldNames(t, augment.AugmentRequest{})
	if !reflect.DeepEqual(expected, got) {
		t.Fatalf("AugmentRequest fields: want %v, got %v", expected, got)
	}
}

func TestAugmentResponseFieldSet(t *testing.T) {
	expected := []string{"static_context", "volatile_context", "citations", "audit_event_id", "truncated", "skipped_reason"}
	got := jsonFieldNames(t, augment.AugmentResponse{})
	if !reflect.DeepEqual(expected, got) {
		t.Fatalf("AugmentResponse fields: want %v, got %v", expected, got)
	}
}

func TestStaticContextJSONRoundTrip(t *testing.T) {
	src := augment.StaticContext{
		ProjectMeta: augment.ProjectMeta{
			ProjectID: "internal-platform-x",
			Doctrine:  "max-scope",
			Stage:     "design",
		},
		CommunitySummaries: []augment.CommunitySummary{
			{
				ClusterID:  "cluster-1",
				Topic:      "MergeEngine",
				Files:      []string{"internal/orchestrator/merge/engine.go"},
				Symbols:    []string{"Engine", "WinnerSelection"},
				NoteIDs:    []string{"n1"},
				TokenCount: 320,
			},
		},
		EstimatedTokens: 480,
	}
	b, err := json.Marshal(src)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var dst augment.StaticContext
	if err := json.Unmarshal(b, &dst); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(src, dst) {
		t.Fatalf("round-trip mismatch:\nsrc: %+v\ndst: %+v", src, dst)
	}
}

func TestVolatileContextJSONRoundTrip(t *testing.T) {
	src := augment.VolatileContext{
		FusedResults: []augment.RRFFusedResult{
			{
				NoteID:    "evt-1234",
				Title:     "Engine.SelectWinner",
				Snippet:   "func (e *Engine) SelectWinner(...) ...",
				Source:    "fts",
				Score:     0.95,
				ProjectID: "internal-platform-x",
				LaneIDs:   []int{2, 4},
			},
		},
		Callers:         []string{"orchestrator.Run"},
		Callees:         []string{"merge.Diff"},
		EstimatedTokens: 240,
	}
	b, err := json.Marshal(src)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var dst augment.VolatileContext
	if err := json.Unmarshal(b, &dst); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(src, dst) {
		t.Fatalf("round-trip mismatch:\nsrc: %+v\ndst: %+v", src, dst)
	}
}

func TestEventTypeStringRoundTrip(t *testing.T) {
	cases := map[augment.EventType]string{
		augment.EventAugmentationStarted:       "AugmentationStarted",
		augment.EventAugmentationCompleted:     "AugmentationCompleted",
		augment.EventAugmentationTruncated:     "AugmentationTruncated",
		augment.EventAugmentationSkipped:       "AugmentationSkipped",
		augment.EventKGQueryDispatched:         "KGQueryDispatched",
		augment.EventCrossProjectQueryFiltered: "CrossProjectQueryFiltered",
		augment.EventAugmentationOverridden:    "AugmentationOverridden",
	}
	for ev, want := range cases {
		if got := ev.String(); got != want {
			t.Errorf("EventType(%d).String() = %q, want %q", ev, got, want)
		}
	}

	if got := augment.EventType(999).String(); got == "" {
		t.Errorf("unknown EventType should still return non-empty descriptor, got %q", got)
	}
}

func TestLaneResultFieldSets(t *testing.T) {
	checks := []struct {
		name string
		in   any
		want []string
	}{
		{"Lane1Result", augment.Lane1Result{}, []string{"results", "elapsed_ms", "lane_id"}},
		{"Lane2Result", augment.Lane2Result{}, []string{"results", "elapsed_ms", "lane_id"}},
		{"Lane3Result", augment.Lane3Result{}, []string{"results", "elapsed_ms", "lane_id"}},
		{"Lane4Result", augment.Lane4Result{}, []string{"results", "elapsed_ms", "lane_id", "degraded"}},
		{"Lane5Result", augment.Lane5Result{}, []string{"results", "elapsed_ms", "lane_id"}},
	}
	for _, c := range checks {
		got := jsonFieldNames(t, c.in)
		if !reflect.DeepEqual(c.want, got) {
			t.Errorf("%s fields: want %v, got %v", c.name, c.want, got)
		}
	}
}

func TestPipelineNewInvokesSentinels(t *testing.T) {
	p, err := augment.NewPipeline(augment.PipelineOptions{
		BudgetStore:       &stubBudgetStore{},
		KnowledgeIndex:    &stubIndex{},
		Embedder:          &stubEmbedder{},
		ChainStore:        &stubChainStore{},
		McpGateway:        &stubGateway{},
		DoctrineLoader:    &stubDoctrineLoader{},
		ProjectLookup:     &stubProjectLookup{},
		Clock:             augment.SystemClock{},
		ConcurrencyBudget: 10,
		QueueDepth:        50,
		PerLaneTimeout:    1 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	if p == nil {
		t.Fatal("NewPipeline returned nil")
	}
}

func TestPipelineNewMissingDepsErrors(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*augment.PipelineOptions)
		wantErr string
	}{
		{"nil BudgetStore", func(o *augment.PipelineOptions) { o.BudgetStore = nil }, "BudgetStore"},
		{"nil KnowledgeIndex", func(o *augment.PipelineOptions) { o.KnowledgeIndex = nil }, "KnowledgeIndex"},
		{"nil Embedder", func(o *augment.PipelineOptions) { o.Embedder = nil }, "Embedder"},
		{"nil ChainStore", func(o *augment.PipelineOptions) { o.ChainStore = nil }, "ChainStore"},
		{"nil McpGateway", func(o *augment.PipelineOptions) { o.McpGateway = nil }, "McpGateway"},
		{"nil DoctrineLoader", func(o *augment.PipelineOptions) { o.DoctrineLoader = nil }, "DoctrineLoader"},
		{"nil ProjectLookup", func(o *augment.PipelineOptions) { o.ProjectLookup = nil }, "ProjectLookup"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts := augment.PipelineOptions{
				BudgetStore:       &stubBudgetStore{},
				KnowledgeIndex:    &stubIndex{},
				Embedder:          &stubEmbedder{},
				ChainStore:        &stubChainStore{},
				McpGateway:        &stubGateway{},
				DoctrineLoader:    &stubDoctrineLoader{},
				ProjectLookup:     &stubProjectLookup{},
				ConcurrencyBudget: 10,
				QueueDepth:        50,
				PerLaneTimeout:    1 * time.Second,
				Clock:             augment.SystemClock{},
			}
			c.mutate(&opts)
			_, err := augment.NewPipeline(opts)
			if err == nil || !contains(err.Error(), c.wantErr) {
				t.Fatalf("NewPipeline expected err containing %q, got %v", c.wantErr, err)
			}
		})
	}
}

func TestPipelineNewDefaultsApply(t *testing.T) {
	p, err := augment.NewPipeline(augment.PipelineOptions{
		BudgetStore:    &stubBudgetStore{},
		KnowledgeIndex: &stubIndex{},
		Embedder:       &stubEmbedder{},
		ChainStore:     &stubChainStore{},
		McpGateway:     &stubGateway{},
		DoctrineLoader: &stubDoctrineLoader{},
		ProjectLookup:  &stubProjectLookup{},
	})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	if p == nil {
		t.Fatal("nil pipeline")
	}
}

func TestSystemClockNow(t *testing.T) {
	before := time.Now()
	got := augment.SystemClock{}.Now()
	after := time.Now()
	if got.Before(before) || got.After(after.Add(time.Second)) {
		t.Errorf("SystemClock.Now() = %v outside [%v, %v]", got, before, after)
	}
}

func jsonFieldNames(t *testing.T, v any) []string {
	t.Helper()
	rt := reflect.TypeOf(v)
	if rt.Kind() != reflect.Struct {
		t.Fatalf("jsonFieldNames: %v is not a struct", rt)
	}
	out := make([]string, 0, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("json")
		if tag == "" {
			continue
		}
		for j, c := range tag {
			if c == ',' {
				tag = tag[:j]
				break
			}
		}
		if tag == "-" {
			continue
		}
		out = append(out, tag)
	}
	return out
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

type stubBudgetStore struct{}

func (*stubBudgetStore) RolledUSDByAxis(_ context.Context, _, _ string, _ int64) (float64, error) {
	return 0, nil
}
func (*stubBudgetStore) InsertCostLedgerEntry(_ context.Context, _ augment.CostLedgerEntry) error {
	return nil
}

type stubIndex struct{}

func (*stubIndex) QueryFTS(_ context.Context, _ string, _ int) ([]augment.QueryResult, error) {
	return nil, nil
}
func (*stubIndex) QueryVec(_ context.Context, _ []float32, _ int, _ float64) ([]augment.QueryResult, error) {
	return nil, nil
}
func (*stubIndex) QueryGraph(_ context.Context, _ []string, _, _ int) ([]augment.QueryResult, error) {
	return nil, nil
}

type stubEmbedder struct{}

func (*stubEmbedder) Embed(_ context.Context, _ string) ([]float32, error) { return nil, nil }

type stubChainStore struct{}

func (*stubChainStore) GetChainTip(_ context.Context) (string, error) { return "", nil }
func (*stubChainStore) UpdateChainColumns(_ context.Context, _, _, _ string, _ []byte, _ int64, _, _ string) error {
	return nil
}
func (*stubChainStore) UpdateTesseraLeafID(_ context.Context, _, _ string) error { return nil }
func (*stubChainStore) AppendTesseraLeaf(_ context.Context, _ augment.TesseraLeafInput) (string, error) {
	return "leaf-1", nil
}

type stubGateway struct{}

func (*stubGateway) CallTool(_ context.Context, _ string, _ map[string]any) (any, error) {
	return nil, nil
}

type stubDoctrineLoader struct{}

func (*stubDoctrineLoader) Load(_ context.Context, _ string) (*augment.DoctrineSchema, error) {
	return &augment.DoctrineSchema{
		Augmentation: augment.AugmentationAxis{
			Enable:      true,
			MaxKGTokens: 10000,
			TimeoutMs:   1000,
		},
	}, nil
}

type stubProjectLookup struct{}

func (*stubProjectLookup) DoctrineForProject(_ context.Context, _ string) (string, error) {
	return "default", nil
}
