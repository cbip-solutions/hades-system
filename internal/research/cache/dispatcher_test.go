// go:build cgo
//go:build cgo
// +build cgo

package cache

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type mockMCPClient struct {
	fixedFindings FreshFindings
	fixedErr      error
	callCount     int
}

func (m *mockMCPClient) Dispatch(_ context.Context, query string) (FreshFindings, error) {
	m.callCount++
	if m.fixedErr != nil {
		return FreshFindings{}, m.fixedErr
	}
	result := m.fixedFindings
	result.Query = query
	return result, nil
}

type mockEmbedder struct {
	fixedEmbedding []float32
	fixedErr       error
	callCount      int
}

func (m *mockEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	m.callCount++
	if m.fixedErr != nil {
		return nil, m.fixedErr
	}
	return m.fixedEmbedding, nil
}

type mockSink struct {
	events []mockSinkEvent
}

type mockSinkEvent struct {
	EventType string
	Payload   []byte
}

func (s *mockSink) Emit(_ context.Context, eventType string, payload []byte) error {
	s.events = append(s.events, mockSinkEvent{EventType: eventType, Payload: payload})
	return nil
}

func newTestDispatcher(t *testing.T, mcp MCPClient, emb Embedder, sink Sink) *Dispatcher {
	t.Helper()
	db := openTestCacheDB(t)

	casDir := t.TempDir()
	cas, err := NewCAS(casDir)
	if err != nil {
		t.Fatalf("NewCAS: %v", err)
	}

	rev := NewRevalidator(ValidateOpts{})

	return &Dispatcher{
		DB:          db,
		CAS:         cas,
		Revalidator: rev,
		Sink:        sink,
		MCP:         mcp,
		Embedder:    emb,
	}
}

func sampleFreshFindings(n int) FreshFindings {
	ff := FreshFindings{Query: "test-query"}
	for i := 0; i < n; i++ {
		ff.Findings = append(ff.Findings, FreshFinding{
			SourceURL:          "https://example.com/fresh/" + itoa(i),
			SourceURLCanonical: "https://example.com/fresh/" + itoa(i),
			Ext:                "html",
			Body:               []byte("body content " + itoa(i)),
			RetrievedAt:        time.Now().UTC(),
		})
	}
	return ff
}

func TestDispatcherFreshDispatchOnEmptyCache(t *testing.T) {
	t.Parallel()
	mcp := &mockMCPClient{fixedFindings: sampleFreshFindings(2)}
	emb := &mockEmbedder{fixedEmbedding: makeTestEmbedding()}
	sink := &mockSink{}

	d := newTestDispatcher(t, mcp, emb, sink)

	res, err := d.LookupOrDispatch(context.Background(), DispatchRequest{
		Query:          "fresh dispatch test",
		ProjectID:      "proj-dispatcher-A",
		SessionID:      "session-test",
		SkipRevalidate: true,
	})
	if err != nil {
		t.Fatalf("LookupOrDispatch: %v", err)
	}
	if res.HitReason != CacheHitFresh {
		t.Errorf("HitReason = %v, want %v", res.HitReason, CacheHitFresh)
	}
	if len(res.Findings) != 2 {
		t.Errorf("Findings count = %d, want 2", len(res.Findings))
	}
	if mcp.callCount != 1 {
		t.Errorf("MCP.Dispatch call count = %d, want 1", mcp.callCount)
	}
	if res.Dispatch == nil {
		t.Fatal("Dispatch must not be nil on fresh hit")
	}
}

func TestDispatcherExactHit(t *testing.T) {
	t.Parallel()
	mcp := &mockMCPClient{fixedFindings: sampleFreshFindings(1)}
	emb := &mockEmbedder{fixedEmbedding: makeTestEmbedding()}
	sink := &mockSink{}

	d := newTestDispatcher(t, mcp, emb, sink)

	const query = "exact hit dispatcher test"

	seedDispatch(t, d.DB, query, "proj-dispatcher-B", 3)

	res, err := d.LookupOrDispatch(context.Background(), DispatchRequest{
		Query:          query,
		ProjectID:      "proj-dispatcher-B",
		SessionID:      "s",
		SkipRevalidate: true,
	})
	if err != nil {
		t.Fatalf("LookupOrDispatch: %v", err)
	}
	if res.HitReason != CacheHitExact {
		t.Errorf("HitReason = %v, want %v", res.HitReason, CacheHitExact)
	}
	if mcp.callCount != 0 {
		t.Errorf("MCP.Dispatch must not be called on exact hit; callCount = %d", mcp.callCount)
	}
	if len(res.Findings) != 3 {
		t.Errorf("Findings count = %d, want 3", len(res.Findings))
	}
}

func TestDispatcherSkipCache(t *testing.T) {
	t.Parallel()
	mcp := &mockMCPClient{fixedFindings: sampleFreshFindings(1)}
	emb := &mockEmbedder{fixedEmbedding: makeTestEmbedding()}
	sink := &mockSink{}

	d := newTestDispatcher(t, mcp, emb, sink)

	const query = "skip cache test"

	seedDispatch(t, d.DB, query, "proj-dispatcher-C", 2)

	res, err := d.LookupOrDispatch(context.Background(), DispatchRequest{
		Query:          query,
		ProjectID:      "proj-dispatcher-C",
		SessionID:      "s",
		SkipCache:      true,
		SkipRevalidate: true,
	})
	if err != nil {
		t.Fatalf("LookupOrDispatch: %v", err)
	}
	if res.HitReason != CacheHitFresh {
		t.Errorf("HitReason = %v, want %v (SkipCache forces fresh)", res.HitReason, CacheHitFresh)
	}
	if mcp.callCount != 1 {
		t.Errorf("MCP.Dispatch must be called exactly once with SkipCache; callCount = %d", mcp.callCount)
	}
}

func TestDispatcherRequiresProjectID(t *testing.T) {
	t.Parallel()
	mcp := &mockMCPClient{fixedFindings: sampleFreshFindings(1)}
	emb := &mockEmbedder{fixedEmbedding: makeTestEmbedding()}
	sink := &mockSink{}

	d := newTestDispatcher(t, mcp, emb, sink)

	_, err := d.LookupOrDispatch(context.Background(), DispatchRequest{
		Query:     "any query",
		ProjectID: "",
		SessionID: "s",
	})
	if !errors.Is(err, ErrProjectIDRequired) {
		t.Errorf("empty ProjectID: err = %v, want ErrProjectIDRequired", err)
	}
	if mcp.callCount != 0 {
		t.Errorf("MCP.Dispatch must not be called when ProjectID empty; callCount = %d", mcp.callCount)
	}
}

func TestDispatcherMCPErrorPropagates(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("mcp sentinel error")
	mcp := &mockMCPClient{fixedErr: sentinel}
	emb := &mockEmbedder{fixedEmbedding: makeTestEmbedding()}
	sink := &mockSink{}

	d := newTestDispatcher(t, mcp, emb, sink)

	_, err := d.LookupOrDispatch(context.Background(), DispatchRequest{
		Query:          "mcp error test",
		ProjectID:      "proj-dispatcher-D",
		SessionID:      "s",
		SkipRevalidate: true,
	})
	if err == nil {
		t.Fatal("expected error from MCP, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, expected to wrap sentinel %v", err, sentinel)
	}
}

func TestDispatcherWritebackThenSecondHit(t *testing.T) {
	t.Parallel()
	mcp := &mockMCPClient{fixedFindings: sampleFreshFindings(2)}
	emb := &mockEmbedder{fixedEmbedding: makeTestEmbedding()}
	sink := &mockSink{}

	d := newTestDispatcher(t, mcp, emb, sink)

	req := DispatchRequest{
		Query:          "writeback test query",
		ProjectID:      "proj-dispatcher-E",
		SessionID:      "s",
		SkipRevalidate: true,
	}

	res1, err := d.LookupOrDispatch(context.Background(), req)
	if err != nil {
		t.Fatalf("first LookupOrDispatch: %v", err)
	}
	if res1.HitReason != CacheHitFresh {
		t.Errorf("first call HitReason = %v, want %v", res1.HitReason, CacheHitFresh)
	}
	if mcp.callCount != 1 {
		t.Errorf("first call: MCP callCount = %d, want 1", mcp.callCount)
	}

	res2, err := d.LookupOrDispatch(context.Background(), req)
	if err != nil {
		t.Fatalf("second LookupOrDispatch: %v", err)
	}
	if res2.HitReason != CacheHitExact {
		t.Errorf("second call HitReason = %v, want %v (should be cached)", res2.HitReason, CacheHitExact)
	}
	if mcp.callCount != 1 {
		t.Errorf("second call: MCP callCount = %d, want 1 (must not re-dispatch)", mcp.callCount)
	}
	if len(res2.Findings) != 2 {
		t.Errorf("second call Findings count = %d, want 2", len(res2.Findings))
	}
}

func TestDispatcherOrchestrationOrdering(t *testing.T) {
	t.Parallel()
	mcp := &mockMCPClient{fixedFindings: sampleFreshFindings(1)}
	emb := &mockEmbedder{fixedEmbedding: makeTestEmbedding()}
	sink := &mockSink{}

	d := newTestDispatcher(t, mcp, emb, sink)

	_, err := d.LookupOrDispatch(context.Background(), DispatchRequest{
		Query:          "orchestration order test",
		ProjectID:      "proj-dispatcher-F",
		SessionID:      "s",
		SkipRevalidate: true,
	})
	if err != nil {
		t.Fatalf("LookupOrDispatch: %v", err)
	}

	if len(sink.events) < 2 {
		t.Fatalf("expected ≥2 events, got %d: %v", len(sink.events), sink.events)
	}

	if sink.events[0].EventType != EventResearchDispatchInitiated {
		t.Errorf("events[0].EventType = %q, want %q", sink.events[0].EventType, EventResearchDispatchInitiated)
	}

	last := sink.events[len(sink.events)-1]
	if last.EventType != EventResearchFindingsReturned {
		t.Errorf("events[last].EventType = %q, want %q", last.EventType, EventResearchFindingsReturned)
	}
}

func TestDispatcherSemanticHit(t *testing.T) {
	t.Parallel()
	mcp := &mockMCPClient{fixedFindings: sampleFreshFindings(1)}
	emb := &mockEmbedder{fixedEmbedding: makeTestEmbedding()}
	sink := &mockSink{}

	d := newTestDispatcher(t, mcp, emb, sink)

	const storedQuery = "semantic dispatcher stored"
	_, rowid := seedDispatchWithRowID(t, d.DB, storedQuery, "proj-dispatcher-H", 1)
	seedQueryVec(t, d.DB, rowid, makeTestEmbedding())

	const lookupQuery = "semantic dispatcher lookup — different text"
	res, err := d.LookupOrDispatch(context.Background(), DispatchRequest{
		Query:          lookupQuery,
		ProjectID:      "proj-dispatcher-H",
		SessionID:      "s",
		SkipRevalidate: true,
	})
	if err != nil {
		t.Fatalf("LookupOrDispatch: %v", err)
	}
	if res.HitReason != CacheHitSemantic {
		t.Errorf("HitReason = %v, want %v", res.HitReason, CacheHitSemantic)
	}
	if mcp.callCount != 0 {
		t.Errorf("MCP must not be called on semantic hit; callCount = %d", mcp.callCount)
	}
}

func TestDispatcherRevalidateFindingsEmptyURL(t *testing.T) {
	t.Parallel()
	mcp := &mockMCPClient{fixedFindings: sampleFreshFindings(1)}
	emb := &mockEmbedder{fixedEmbedding: makeTestEmbedding()}
	sink := &mockSink{}

	d := newTestDispatcher(t, mcp, emb, sink)

	const query = "revalidate empty url test"
	hash := ComputeQueryHash(query)
	dispatchID := "dispatch-revtest-" + hash[:8]

	_, err := d.DB.SQL.ExecContext(context.Background(),
		`INSERT INTO research_dispatches
		 (id, query, query_text_hash, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		dispatchID, query, hash,
		string(DispatchStatusDone),
		time.Now().UTC().Unix()-5,
		time.Now().UTC().Unix(),
	)
	if err != nil {
		t.Fatalf("seed dispatch: %v", err)
	}

	_, err = d.DB.SQL.ExecContext(context.Background(),
		`INSERT INTO research_findings
		 (id, dispatch_id, url, title, snippet, freshness_status, retrieved_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"finding-revtest-emptyurl", dispatchID,
		"",
		"Title", "Snippet",
		string(FreshnessFresh),
		time.Now().UTC().Unix(),
	)
	if err != nil {
		t.Fatalf("seed finding: %v", err)
	}

	res, err := d.LookupOrDispatch(context.Background(), DispatchRequest{
		Query:          query,
		ProjectID:      "proj-dispatcher-I",
		SessionID:      "s",
		SkipRevalidate: false,
	})
	if err != nil {
		t.Fatalf("LookupOrDispatch: %v", err)
	}

	if res.HitReason != CacheHitExact {
		t.Errorf("HitReason = %v, want %v", res.HitReason, CacheHitExact)
	}

	if res.FreshnessStatus != FreshnessStale {
		t.Errorf("FreshnessStatus = %v, want %v (empty URL treated as stale)", res.FreshnessStatus, FreshnessStale)
	}
}

func TestDispatcherWritebackZeroFindings(t *testing.T) {
	t.Parallel()
	mcp := &mockMCPClient{fixedFindings: FreshFindings{Query: "zero", Findings: nil}}
	emb := &mockEmbedder{fixedEmbedding: makeTestEmbedding()}
	sink := &mockSink{}

	d := newTestDispatcher(t, mcp, emb, sink)

	res, err := d.LookupOrDispatch(context.Background(), DispatchRequest{
		Query:          "zero findings test",
		ProjectID:      "proj-dispatcher-J",
		SessionID:      "s",
		SkipRevalidate: true,
	})
	if err != nil {
		t.Fatalf("LookupOrDispatch zero findings: %v", err)
	}
	if res.HitReason != CacheHitFresh {
		t.Errorf("HitReason = %v, want %v", res.HitReason, CacheHitFresh)
	}
	if len(res.Findings) != 0 {
		t.Errorf("Findings count = %d, want 0", len(res.Findings))
	}
}

func TestDispatcherEmbedderErrorSkipsSemantic(t *testing.T) {
	t.Parallel()
	mcp := &mockMCPClient{fixedFindings: sampleFreshFindings(1)}
	embedErr := errors.New("embed test error")
	emb := &mockEmbedder{fixedErr: embedErr}
	sink := &mockSink{}

	d := newTestDispatcher(t, mcp, emb, sink)

	res, err := d.LookupOrDispatch(context.Background(), DispatchRequest{
		Query:          "embedder error test",
		ProjectID:      "proj-dispatcher-K",
		SessionID:      "s",
		SkipRevalidate: true,
	})
	if err != nil {
		t.Fatalf("LookupOrDispatch with embedder error: %v", err)
	}

	if res.HitReason != CacheHitFresh {
		t.Errorf("HitReason = %v, want %v (embedder error skips semantic)", res.HitReason, CacheHitFresh)
	}
	if mcp.callCount != 1 {
		t.Errorf("MCP callCount = %d, want 1", mcp.callCount)
	}
}

func TestDispatcherRevalidateFindingsWithHTTPServer(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fresh":
			w.WriteHeader(http.StatusNotModified)
		case "/error":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	mcp := &mockMCPClient{fixedFindings: sampleFreshFindings(1)}
	emb := &mockEmbedder{fixedEmbedding: makeTestEmbedding()}
	sink := &mockSink{}

	rev := NewRevalidator(ValidateOpts{Client: srv.Client(), Timeout: 2e9})

	db := openTestCacheDB(t)
	casDir := t.TempDir()
	cas, err := NewCAS(casDir)
	if err != nil {
		t.Fatalf("NewCAS: %v", err)
	}
	d := &Dispatcher{
		DB:          db,
		CAS:         cas,
		Revalidator: rev,
		Sink:        sink,
		MCP:         mcp,
		Embedder:    emb,
	}

	const query = "revalidate http fresh test"
	hash := ComputeQueryHash(query)
	dispatchID := "dispatch-revhttp-" + hash[:8]

	_, err = d.DB.SQL.ExecContext(context.Background(),
		`INSERT INTO research_dispatches
		 (id, query, query_text_hash, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		dispatchID, query, hash,
		string(DispatchStatusDone),
		time.Now().UTC().Unix()-5,
		time.Now().UTC().Unix(),
	)
	if err != nil {
		t.Fatalf("seed dispatch: %v", err)
	}
	_, err = d.DB.SQL.ExecContext(context.Background(),
		`INSERT INTO research_findings
		 (id, dispatch_id, url, title, snippet, freshness_status, retrieved_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"finding-revhttp-fresh", dispatchID,
		srv.URL+"/fresh",
		"Fresh Title", "Fresh Snippet",
		string(FreshnessFresh),
		time.Now().UTC().Unix(),
	)
	if err != nil {
		t.Fatalf("seed finding: %v", err)
	}

	res, err := d.LookupOrDispatch(context.Background(), DispatchRequest{
		Query:          query,
		ProjectID:      "proj-dispatcher-L",
		SessionID:      "s",
		SkipRevalidate: false,
	})
	if err != nil {
		t.Fatalf("LookupOrDispatch: %v", err)
	}
	if res.HitReason != CacheHitExact {
		t.Errorf("HitReason = %v, want %v", res.HitReason, CacheHitExact)
	}

	if res.FreshnessStatus != FreshnessFresh {
		t.Errorf("FreshnessStatus = %v, want %v (304 → fresh)", res.FreshnessStatus, FreshnessFresh)
	}
}

func TestDispatcherRevalidateFindingsHTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	mcp := &mockMCPClient{fixedFindings: sampleFreshFindings(1)}
	emb := &mockEmbedder{fixedEmbedding: makeTestEmbedding()}
	sink := &mockSink{}

	rev := NewRevalidator(ValidateOpts{Client: srv.Client(), Timeout: 2e9})
	db := openTestCacheDB(t)
	casDir := t.TempDir()
	cas, err := NewCAS(casDir)
	if err != nil {
		t.Fatalf("NewCAS: %v", err)
	}
	d := &Dispatcher{
		DB:          db,
		CAS:         cas,
		Revalidator: rev,
		Sink:        sink,
		MCP:         mcp,
		Embedder:    emb,
	}

	const query = "revalidate 5xx error test"
	hash := ComputeQueryHash(query)
	dispatchID := "dispatch-rev5xx-" + hash[:8]

	_, err = d.DB.SQL.ExecContext(context.Background(),
		`INSERT INTO research_dispatches
		 (id, query, query_text_hash, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		dispatchID, query, hash,
		string(DispatchStatusDone),
		time.Now().UTC().Unix()-5,
		time.Now().UTC().Unix(),
	)
	if err != nil {
		t.Fatalf("seed dispatch: %v", err)
	}
	_, err = d.DB.SQL.ExecContext(context.Background(),
		`INSERT INTO research_findings
		 (id, dispatch_id, url, title, snippet, freshness_status, retrieved_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"finding-rev5xx", dispatchID,
		srv.URL+"/error",
		"Error Title", "Error Snippet",
		string(FreshnessFresh),
		time.Now().UTC().Unix(),
	)
	if err != nil {
		t.Fatalf("seed finding: %v", err)
	}

	res, err := d.LookupOrDispatch(context.Background(), DispatchRequest{
		Query:          query,
		ProjectID:      "proj-dispatcher-M",
		SessionID:      "s",
		SkipRevalidate: false,
	})
	if err != nil {
		t.Fatalf("LookupOrDispatch: %v", err)
	}

	if res.FreshnessStatus != FreshnessStale {
		t.Errorf("FreshnessStatus = %v, want %v (5xx error → stale)", res.FreshnessStatus, FreshnessStale)
	}
}

func TestWorseFreshness(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b, want FreshnessStatus
	}{
		{FreshnessFresh, FreshnessFresh, FreshnessFresh},
		{FreshnessFresh, FreshnessStale, FreshnessStale},
		{FreshnessStale, FreshnessFresh, FreshnessStale},
		{FreshnessStale, FreshnessExpired, FreshnessExpired},
		{FreshnessExpired, FreshnessStale, FreshnessExpired},
		{FreshnessExpired, FreshnessFresh, FreshnessExpired},
		{FreshnessFresh, FreshnessExpired, FreshnessExpired},
	}
	for _, tc := range cases {
		got := worseFreshness(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("worseFreshness(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestNullableStringAndBlob(t *testing.T) {
	t.Parallel()
	if nullableString("") != nil {
		t.Error("nullableString(\"\") should be nil")
	}
	ns := nullableString("hello")
	if ns == nil {
		t.Error("nullableString(\"hello\") should not be nil")
	}
	if nullableBlob(nil) != nil {
		t.Error("nullableBlob(nil) should be nil")
	}
	if nullableBlob([]byte{}) != nil {
		t.Error("nullableBlob([]) should be nil")
	}
	nb := nullableBlob([]byte{1, 2, 3})
	if nb == nil {
		t.Error("nullableBlob([1,2,3]) should not be nil")
	}
}

func TestDispatcherNilEmbedderSkipsSemantic(t *testing.T) {
	t.Parallel()
	mcp := &mockMCPClient{fixedFindings: sampleFreshFindings(1)}
	sink := &mockSink{}

	db := openTestCacheDB(t)
	casDir := t.TempDir()
	cas, err := NewCAS(casDir)
	if err != nil {
		t.Fatalf("NewCAS: %v", err)
	}
	rev := NewRevalidator(ValidateOpts{})
	d := &Dispatcher{
		DB:          db,
		CAS:         cas,
		Revalidator: rev,
		Sink:        sink,
		MCP:         mcp,
		Embedder:    nil,
	}

	res, err := d.LookupOrDispatch(context.Background(), DispatchRequest{
		Query:          "nil embedder test",
		ProjectID:      "proj-dispatcher-G",
		SessionID:      "s",
		SkipRevalidate: true,
	})
	if err != nil {
		t.Fatalf("LookupOrDispatch with nil Embedder: %v", err)
	}
	if res.HitReason != CacheHitFresh {
		t.Errorf("HitReason = %v, want %v (nil embedder skips semantic, falls to MCP)", res.HitReason, CacheHitFresh)
	}
	if mcp.callCount != 1 {
		t.Errorf("MCP.Dispatch callCount = %d, want 1", mcp.callCount)
	}
}
