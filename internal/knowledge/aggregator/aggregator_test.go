package aggregator

import (
	"context"
	"testing"
	"time"
)

func TestAggregatorBoundaryRespectSentinelReachable(t *testing.T) {
	if err := aggregatorBoundaryRespectSentinel(); err != nil {
		t.Errorf("sentinel failure: %v", err)
	}
}

func TestAggregatorNoWebSentinelReachable(t *testing.T) {
	if err := aggregatorNoWebSentinel(); err != nil {
		t.Errorf("sentinel failure: %v", err)
	}
}

func TestPromoteRequiresReasonSentinelReachable(t *testing.T) {
	if err := promoteRequiresReasonSentinel(); err != nil {
		t.Errorf("sentinel failure: %v", err)
	}
}

func TestNewAggregatorRejectsNilEmbedder(t *testing.T) {
	_, err := New(Options{
		DB:       nil,
		Embedder: nil,
		Store:    newMockStore(),
	})
	if err == nil {
		t.Error("New accepted nil Embedder; expected error")
	}
}

func TestNewAggregatorRejectsNilStore(t *testing.T) {
	_, err := New(Options{
		DB:       nil,
		Embedder: newMockEmbedder(384),
		Store:    nil,
	})
	if err == nil {
		t.Error("New accepted nil Store; expected error")
	}
}

func TestNewAggregatorRejectsEmbedderDimensionMismatch(t *testing.T) {
	_, err := New(Options{
		DB:       nil,
		Embedder: newMockEmbedder(512),
		Store:    newMockStore(),
	})
	if err == nil {
		t.Error("New accepted dimension mismatch; expected error")
	}
}

func TestNewAggregatorAcceptsCorrectInputs(t *testing.T) {
	a, err := New(Options{
		DB:       nil,
		Embedder: newMockEmbedder(384),
		Store:    newMockStore(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a == nil {
		t.Fatal("New returned nil Aggregator with no error")
	}
	if a.clock == nil {
		t.Error("Aggregator.clock unset; expected systemClock auto-default")
	}
	if a.chain == nil {
		t.Error("Aggregator.chain unset; expected noopChainAnchorComputer auto-default")
	}
	if a.Degraded() {
		t.Error("freshly constructed Aggregator should not be Degraded()")
	}
}

func TestNewAggregatorAcceptsExplicitClockAndChain(t *testing.T) {
	myClock := fixedClock{}
	myChain := noopChainAnchorComputer{}
	a, err := New(Options{
		DB:       nil,
		Embedder: newMockEmbedder(384),
		Store:    newMockStore(),
		Clock:    myClock,
		Chain:    myChain,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := a.clock.(fixedClock); !ok {
		t.Errorf("a.clock type = %T; want fixedClock (DI honoured)", a.clock)
	}
}

// TestAggregatorMarkDegraded asserts the degraded-mode toggle works.
// (zen doctor) consults Degraded() to surface the warning
// banner. The internal markDegraded path is exercised by D-7
// embedder-load-fails branch + Init's ErrCGODisabled detection.
func TestAggregatorMarkDegraded(t *testing.T) {
	a, err := New(Options{
		DB:       nil,
		Embedder: newMockEmbedder(384),
		Store:    newMockStore(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.Degraded() {
		t.Fatal("freshly constructed should not be Degraded()")
	}
	a.markDegraded()
	if !a.Degraded() {
		t.Error("after markDegraded, Degraded() = false; want true")
	}
}

// TestAggregatorCloseHandlesNilDB asserts Close on an Aggregator with
// no DB attached (constructed for unit tests that do not touch the
// schema) returns nil rather than panicking.
func TestAggregatorCloseHandlesNilDB(t *testing.T) {
	a, err := New(Options{
		DB:       nil,
		Embedder: newMockEmbedder(384),
		Store:    newMockStore(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Errorf("Close on nil-DB Aggregator: %v", err)
	}
}

func TestAggregatorDBAccessorNilBranch(t *testing.T) {
	a, err := New(Options{
		DB:       nil,
		Embedder: newMockEmbedder(384),
		Store:    newMockStore(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.DB() != nil {
		t.Errorf("DB() with Options.DB=nil: want nil; got non-nil")
	}
}

func TestSystemClockNow(t *testing.T) {
	clk := systemClock{}
	got := clk.Now()
	since := time.Since(got)
	if since < 0 || since > time.Second {
		t.Errorf("systemClock.Now diverged from time.Now: delta=%v", since)
	}
}

func TestNoopChainAnchorComputer(t *testing.T) {
	c := noopChainAnchorComputer{}
	ts := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	anchor, err := c.ComputeAnchor(context.Background(), "evt-1", "PinPromoted", []byte("payload"), ts)
	if err != nil {
		t.Fatalf("ComputeAnchor: %v", err)
	}
	if anchor != "2026_05:evt-1:noop-pre-phase-b" {
		t.Errorf("anchor = %q; want \"2026_05:evt-1:noop-pre-phase-b\"", anchor)
	}
}

func newMockEmbedder(dim int) Embedder { return &mockEmbedder{dim: dim} }

type mockEmbedder struct{ dim int }

func (m *mockEmbedder) Dimensions() int { return m.dim }
func (m *mockEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	v := make([]float32, m.dim)
	return v, nil
}

func newMockStore() PerProjectKnowledgeStore { return &mockStore{} }

type mockStore struct{}

func (*mockStore) ListAuthorizedProjects(_ context.Context) ([]ProjectHandle, error) {
	return nil, nil
}
func (*mockStore) OpenProjectVault(_ context.Context, _ string) (ProjectVault, error) {
	return nil, nil
}
func (*mockStore) UpdateAuditChainAnchor(_ context.Context, _, _, _ string) error {
	return nil
}

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Unix(0, 0).UTC() }
