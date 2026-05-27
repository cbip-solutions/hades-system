// go:build cgo
//go:build cgo
// +build cgo

package researchmcpmock_test

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/research/cache"
	"github.com/cbip-solutions/hades-system/tests/testhelpers/researchmcpmock"
)

func TestMockResearchMCP_InterfaceConformance(t *testing.T) {
	t.Parallel()
	var _ cache.MCPClient = researchmcpmock.New()
}

func TestMockResearchMCP_DefaultFindings(t *testing.T) {
	t.Parallel()
	m := researchmcpmock.New()
	ctx := context.Background()
	bundle, err := m.Dispatch(ctx, "audit hash chain SOTA 2026")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if bundle.Query != "audit hash chain SOTA 2026" {
		t.Errorf("Query = %q, want echoed input", bundle.Query)
	}
	if len(bundle.Findings) != 3 {
		t.Errorf("findings count = %d, want 3", len(bundle.Findings))
	}
	for i, f := range bundle.Findings {
		if f.SourceURL == "" {
			t.Errorf("finding[%d] empty SourceURL", i)
		}
		if f.SourceURLCanonical == "" {
			t.Errorf("finding[%d] empty SourceURLCanonical", i)
		}
		if f.Ext == "" {
			t.Errorf("finding[%d] empty Ext", i)
		}
		if len(f.Body) == 0 {
			t.Errorf("finding[%d] empty Body", i)
		}
		if f.RetrievedAt.IsZero() {
			t.Errorf("finding[%d] zero RetrievedAt", i)
		}
	}
}

func TestMockResearchMCP_RegisteredFindings(t *testing.T) {
	t.Parallel()
	m := researchmcpmock.New()
	custom := cache.FreshFindings{
		Query: "test query",
		Findings: []cache.FreshFinding{
			{
				SourceURL:          "https://example.com/article",
				SourceURLCanonical: "https://example.com/article",
				Ext:                ".html",
				Body:               []byte("test body"),
				RetrievedAt:        time.Now().UTC(),
			},
		},
	}
	m.Register("test query", custom)
	got, err := m.Dispatch(context.Background(), "test query")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !reflect.DeepEqual(got, custom) {
		t.Errorf("registered FreshFindings not returned: got %+v, want %+v", got, custom)
	}
}

func TestMockResearchMCP_ErrorInjection(t *testing.T) {
	t.Parallel()
	m := researchmcpmock.New()
	wantErr := errors.New("upstream rate limited")
	m.SetError("rate limit query", wantErr)
	bundle, err := m.Dispatch(context.Background(), "rate limit query")
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}

	if bundle.Query != "" || len(bundle.Findings) != 0 {
		t.Errorf("error path returned non-zero bundle: %+v", bundle)
	}
}

func TestMockResearchMCP_ErrorInjectionPrecedesRegistered(t *testing.T) {
	t.Parallel()
	m := researchmcpmock.New()
	m.Register("q", cache.FreshFindings{Query: "q", Findings: []cache.FreshFinding{{SourceURL: "https://x"}}})
	wantErr := errors.New("override")
	m.SetError("q", wantErr)

	_, err := m.Dispatch(context.Background(), "q")
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want override err to win over registered findings", err)
	}
}

func TestMockResearchMCP_CallRecorder(t *testing.T) {
	t.Parallel()
	m := researchmcpmock.New()
	queries := []string{"first", "second", "third"}
	for _, q := range queries {
		_, _ = m.Dispatch(context.Background(), q)
	}
	calls := m.Calls()
	if len(calls) != len(queries) {
		t.Fatalf("calls count = %d, want %d", len(calls), len(queries))
	}
	for i, q := range queries {
		if calls[i] != q {
			t.Errorf("call[%d] = %q, want %q", i, calls[i], q)
		}
	}
}

func TestMockResearchMCP_CallsSnapshotIsCopy(t *testing.T) {
	t.Parallel()
	m := researchmcpmock.New()
	_, _ = m.Dispatch(context.Background(), "q1")
	snap := m.Calls()
	snap[0] = "MUTATED"
	again := m.Calls()
	if again[0] == "MUTATED" {
		t.Errorf("Calls() snapshot must be a copy; mutation leaked into mock state")
	}
}

func TestMockResearchMCP_Determinism(t *testing.T) {
	t.Parallel()
	m1 := researchmcpmock.New()
	m2 := researchmcpmock.New()

	q := "stable query"
	f1, _ := m1.Dispatch(context.Background(), q)
	f2, _ := m2.Dispatch(context.Background(), q)

	if !reflect.DeepEqual(f1, f2) {
		t.Errorf("non-deterministic default fallback:\n  f1=%+v\n  f2=%+v", f1, f2)
	}
}

func TestMockResearchMCP_LatencyHonored(t *testing.T) {
	t.Parallel()
	m := researchmcpmock.New()
	m.SetLatency(50 * time.Millisecond)
	start := time.Now()
	_, err := m.Dispatch(context.Background(), "x")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Errorf("latency not honored: elapsed = %v", elapsed)
	}
}

func TestMockResearchMCP_LatencyContextCancelled(t *testing.T) {

	m := researchmcpmock.New()
	m.SetLatency(500 * time.Millisecond)

	before := runtime.NumGoroutine()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := m.Dispatch(ctx, "y")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}

	time.Sleep(100 * time.Millisecond)
	after := runtime.NumGoroutine()

	if delta := after - before; delta > 2 {
		t.Errorf("goroutine leak: before=%d after=%d delta=%d", before, after, delta)
	}
}

func TestMockResearchMCP_Reset(t *testing.T) {
	t.Parallel()
	m := researchmcpmock.New()
	m.Register("t", cache.FreshFindings{Query: "t", Findings: []cache.FreshFinding{{SourceURL: "https://x"}}})
	m.SetError("e", errors.New("boom"))
	m.SetLatency(time.Millisecond)
	_, _ = m.Dispatch(context.Background(), "t")

	m.Reset()

	if got := m.Calls(); len(got) != 0 {
		t.Errorf("post-Reset Calls() = %d, want 0", len(got))
	}

	bundle, _ := m.Dispatch(context.Background(), "t")
	if len(bundle.Findings) != 3 {
		t.Errorf("post-Reset Dispatch should fall back to default 3, got %d", len(bundle.Findings))
	}

	_, err := m.Dispatch(context.Background(), "e")
	if err != nil {
		t.Errorf("post-Reset Dispatch(\"e\") err = %v, want nil", err)
	}
}

func TestMockResearchMCP_DispatchReturnsBundleWithEchoedQuery(t *testing.T) {
	t.Parallel()
	m := researchmcpmock.New()
	bundle, err := m.Dispatch(context.Background(), "echo me")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if bundle.Query != "echo me" {
		t.Errorf("Query echo: got %q, want %q", bundle.Query, "echo me")
	}
}

func TestMockResearchMCP_FieldsAreRealistic(t *testing.T) {
	t.Parallel()
	m := researchmcpmock.New()
	bundle, _ := m.Dispatch(context.Background(), "shape check")
	for i, f := range bundle.Findings {

		if got := f.SourceURL; len(got) < len("https://") || got[:8] != "https://" {
			t.Errorf("finding[%d].SourceURL = %q, want https:// prefix", i, got)
		}
		if f.SourceURLCanonical != f.SourceURL {
			t.Errorf("finding[%d].SourceURLCanonical = %q, want = SourceURL %q (default mock convention)",
				i, f.SourceURLCanonical, f.SourceURL)
		}
		if f.Ext != ".html" {
			t.Errorf("finding[%d].Ext = %q, want .html (default mock convention)", i, f.Ext)
		}
	}
}
