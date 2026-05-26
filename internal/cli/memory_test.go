package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
)

type fakeMemoryClient struct {
	aggResp    *client.AggQueryResponse
	aggErr     error
	listResp   []client.AggPinNote
	listErr    error
	pinErr     error
	unpinErr   error
	promoteErr error
	ecoResp    *client.EcosystemQueryResponse
	ecoErr     error

	lastAggReq     client.AggQueryRequest
	lastListLimit  int
	lastListOffset int
	lastPinID      string
	lastUnpinID    string
	lastPromoteReq client.AggPromoteRequest
	lastEcoReq     client.EcosystemQueryRequest
	ecoQueryCalled bool
	aggQueryCalled bool
	listCalled     bool
	pinCalled      bool
	unpinCalled    bool
	promoteCalled  bool
}

func (f *fakeMemoryClient) MemoryQuery(_ context.Context, req client.AggQueryRequest) (*client.AggQueryResponse, error) {
	f.aggQueryCalled = true
	f.lastAggReq = req
	if f.aggErr != nil {
		return nil, f.aggErr
	}
	if f.aggResp == nil {
		return &client.AggQueryResponse{Results: []client.AggQueryResultRow{}}, nil
	}
	return f.aggResp, nil
}

func (f *fakeMemoryClient) MemoryList(_ context.Context, limit, offset int) (*client.AggListResponse, error) {
	f.listCalled = true
	f.lastListLimit = limit
	f.lastListOffset = offset
	if f.listErr != nil {
		return nil, f.listErr
	}
	return &client.AggListResponse{Notes: f.listResp}, nil
}

func (f *fakeMemoryClient) MemoryPin(_ context.Context, noteID string) error {
	f.pinCalled = true
	f.lastPinID = noteID
	return f.pinErr
}

func (f *fakeMemoryClient) MemoryUnpin(_ context.Context, noteID string) error {
	f.unpinCalled = true
	f.lastUnpinID = noteID
	return f.unpinErr
}

func (f *fakeMemoryClient) MemoryPromote(_ context.Context, req client.AggPromoteRequest) error {
	f.promoteCalled = true
	f.lastPromoteReq = req
	return f.promoteErr
}

func (f *fakeMemoryClient) EcosystemQuery(_ context.Context, req client.EcosystemQueryRequest) (*client.EcosystemQueryResponse, error) {
	f.ecoQueryCalled = true
	f.lastEcoReq = req
	if f.ecoErr != nil {
		return nil, f.ecoErr
	}
	return f.ecoResp, nil
}

func TestMemoryCmdRegistersSubcommands(t *testing.T) {
	cmd := NewMemoryCmd()
	if cmd.Use != "memory" {
		t.Errorf("Use = %q, want memory", cmd.Use)
	}
	wantSubs := []string{"query", "list", "pin", "unpin", "promote"}
	for _, name := range wantSubs {
		found := false
		for _, sc := range cmd.Commands() {
			if strings.HasPrefix(sc.Use, name) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("subcommand %q not registered", name)
		}
	}
}

func TestMemoryQueryFusesAggregatorAndEcosystem(t *testing.T) {
	c := &fakeMemoryClient{
		aggResp: &client.AggQueryResponse{Results: []client.AggQueryResultRow{
			{NoteID: "n1", Title: "memory note about context", Score: 0.9, Snippet: "context lives in goroutines"},
		}},
		ecoResp: &client.EcosystemQueryResponse{Chunks: []client.EcosystemChunk{
			{SymbolPath: "context.Context", SourceURL: "https://pkg.go.dev/context", RerankerScore: 0.85,
				Version: "1.22.0", ContentText: "Context carries deadlines, cancellation signals, and other request-scoped values."},
		}},
	}
	flags := MemoryQueryFlags{FreeText: "context", Remote: true, Limit: 10, Format: "json"}
	var w bytes.Buffer
	if err := RunMemoryQuery(context.Background(), c, flags, &w); err != nil {
		t.Fatalf("RunMemoryQuery: %v", err)
	}
	out := w.String()
	if !strings.Contains(out, "aggregator") {
		t.Errorf("output missing aggregator source: %s", out)
	}
	if !strings.Contains(out, "ecosystem_docs") {
		t.Errorf("output missing ecosystem_docs source: %s", out)
	}

	if !c.aggQueryCalled {
		t.Error("aggregator query was not called")
	}
	if !c.ecoQueryCalled {
		t.Error("ecosystem query was not called with --remote=true")
	}

	var hits []MemoryHit
	if err := json.Unmarshal(w.Bytes(), &hits); err != nil {
		t.Fatalf("JSON decode failed: %v\noutput: %s", err, out)
	}
	if len(hits) != 2 {
		t.Errorf("hits = %d, want 2 (one per source)", len(hits))
	}

	for _, h := range hits {
		if h.RRFScore <= 0 {
			t.Errorf("non-positive RRF score for hit %+v", h)
		}
	}
}

func TestMemoryQueryAggregatorOnlyWhenNoRemote(t *testing.T) {
	c := &fakeMemoryClient{
		aggResp: &client.AggQueryResponse{Results: []client.AggQueryResultRow{
			{NoteID: "n1", Title: "local note", Score: 0.8, Snippet: "local content"},
		}},

		ecoResp: &client.EcosystemQueryResponse{Chunks: []client.EcosystemChunk{
			{SymbolPath: "should.not.appear", SourceURL: "x"},
		}},
	}
	flags := MemoryQueryFlags{FreeText: "local", Remote: false, Format: "json"}
	var w bytes.Buffer
	if err := RunMemoryQuery(context.Background(), c, flags, &w); err != nil {
		t.Fatalf("RunMemoryQuery: %v", err)
	}
	if c.ecoQueryCalled {
		t.Error("EcosystemQuery should not be called without --remote")
	}

	out := w.String()
	if strings.Contains(out, "ecosystem_docs") {
		t.Errorf("output should not include ecosystem when --remote=false: %s", out)
	}
	if !strings.Contains(out, "aggregator") {
		t.Errorf("output missing aggregator source: %s", out)
	}
	if strings.Contains(out, "should.not.appear") {
		t.Errorf("ecosystem chunk leaked: %s", out)
	}
}

func TestMemoryQuerySoftFailWhenAggregatorErrors(t *testing.T) {
	c := &fakeMemoryClient{
		aggErr: errors.New("aggregator down"),
		ecoResp: &client.EcosystemQueryResponse{Chunks: []client.EcosystemChunk{
			{SymbolPath: "fmt.Println", SourceURL: "https://pkg.go.dev/fmt", RerankerScore: 0.7,
				Version: "1.22.0", ContentText: "Println prints a line to stdout"},
		}},
	}
	flags := MemoryQueryFlags{FreeText: "println", Remote: true, Format: "json"}
	var w bytes.Buffer
	if err := RunMemoryQuery(context.Background(), c, flags, &w); err != nil {
		t.Fatalf("expected soft-fail (no error), got: %v", err)
	}
	out := w.String()
	if !strings.Contains(out, "ecosystem_docs") {
		t.Errorf("expected ecosystem rendering despite agg error: %s", out)
	}
}

func TestMemoryQuerySoftFailWhenEcosystemErrors(t *testing.T) {
	c := &fakeMemoryClient{
		aggResp: &client.AggQueryResponse{Results: []client.AggQueryResultRow{
			{NoteID: "n1", Title: "still here", Score: 0.5, Snippet: "still here"},
		}},
		ecoErr: errors.New("ecosystem down"),
	}
	flags := MemoryQueryFlags{FreeText: "anything", Remote: true, Format: "json"}
	var w bytes.Buffer
	if err := RunMemoryQuery(context.Background(), c, flags, &w); err != nil {
		t.Fatalf("expected soft-fail (no error), got: %v", err)
	}
	out := w.String()
	if !strings.Contains(out, "aggregator") {
		t.Errorf("expected aggregator rendering despite eco error: %s", out)
	}
}

func TestMemoryQueryHardFailWhenBothFail(t *testing.T) {
	c := &fakeMemoryClient{
		aggErr: errors.New("agg down"),
		ecoErr: errors.New("eco down"),
	}
	flags := MemoryQueryFlags{FreeText: "anything", Remote: true, Format: "json"}
	var w bytes.Buffer
	err := RunMemoryQuery(context.Background(), c, flags, &w)
	if err == nil {
		t.Fatal("expected error when both sources fail")
	}
	if !strings.Contains(err.Error(), "agg") || !strings.Contains(err.Error(), "eco") {
		t.Errorf("error message should mention both sources, got: %v", err)
	}
}

func TestMemoryQueryEmptyFreeTextRecoverable(t *testing.T) {
	c := &fakeMemoryClient{}
	flags := MemoryQueryFlags{FreeText: "  ", Format: "json"}
	var w bytes.Buffer
	err := RunMemoryQuery(context.Background(), c, flags, &w)
	if err == nil {
		t.Fatal("expected error for empty free-text")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable, got: %v", err)
	}
	if c.aggQueryCalled {
		t.Error("aggregator should not be called when free-text is empty")
	}
}

func TestMemoryQueryTextFormat(t *testing.T) {
	c := &fakeMemoryClient{
		aggResp: &client.AggQueryResponse{Results: []client.AggQueryResultRow{
			{NoteID: "internal-platform-x/M0", Title: "max-scope doctrine", Snippet: "max-scope means..."},
		}},
	}
	flags := MemoryQueryFlags{FreeText: "doctrine"}
	var w bytes.Buffer
	if err := RunMemoryQuery(context.Background(), c, flags, &w); err != nil {
		t.Fatalf("RunMemoryQuery: %v", err)
	}
	out := w.String()
	if !strings.Contains(out, "max-scope doctrine") {
		t.Errorf("expected title in text output: %s", out)
	}
	if !strings.Contains(out, "aggregator") {
		t.Errorf("expected source label: %s", out)
	}
}

func TestMemoryQueryLimitCapsResults(t *testing.T) {
	c := &fakeMemoryClient{
		aggResp: &client.AggQueryResponse{Results: []client.AggQueryResultRow{
			{NoteID: "n1", Title: "a"},
			{NoteID: "n2", Title: "b"},
			{NoteID: "n3", Title: "c"},
		}},
	}
	flags := MemoryQueryFlags{FreeText: "x", Limit: 2, Format: "json"}
	var w bytes.Buffer
	if err := RunMemoryQuery(context.Background(), c, flags, &w); err != nil {
		t.Fatalf("RunMemoryQuery: %v", err)
	}
	var hits []MemoryHit
	if err := json.Unmarshal(w.Bytes(), &hits); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(hits) != 2 {
		t.Errorf("Limit=2 should cap; got %d hits", len(hits))
	}
}

func TestMemoryQueryOverFetchesForRRF(t *testing.T) {
	c := &fakeMemoryClient{}
	flags := MemoryQueryFlags{FreeText: "x", Limit: 5}
	var w bytes.Buffer
	if err := RunMemoryQuery(context.Background(), c, flags, &w); err != nil {
		t.Fatalf("RunMemoryQuery: %v", err)
	}
	if c.lastAggReq.Limit != 20 {
		t.Errorf("aggregator over-fetch: want Limit=20, got %d", c.lastAggReq.Limit)
	}
}

func TestRRFFuseMemoryK60Formula(t *testing.T) {
	agg := []client.AggQueryResultRow{
		{NoteID: "a", Title: "Title A"},
		{NoteID: "b", Title: "Title B"},
	}
	eco := &client.EcosystemQueryResponse{Chunks: []client.EcosystemChunk{
		{SymbolPath: "sym.X", Version: "v1", SourceURL: "u1", ContentText: "x"},
	}}
	hits := rrfFuseMemory(agg, eco, 60)
	if len(hits) != 3 {
		t.Fatalf("want 3 hits, got %d", len(hits))
	}

	wantRank0 := 1.0 / 61.0
	wantRank1 := 1.0 / 62.0

	last := hits[len(hits)-1]
	if last.RRFScore != wantRank1 {
		t.Errorf("last hit RRFScore = %v, want %v", last.RRFScore, wantRank1)
	}
	if last.Key != "agg:b" {
		t.Errorf("last hit Key = %q, want agg:b", last.Key)
	}

	for i, h := range hits[:2] {
		if h.RRFScore != wantRank0 {
			t.Errorf("hit[%d].RRFScore = %v, want %v", i, h.RRFScore, wantRank0)
		}
	}
}

func TestRRFFuseMemoryNilEcosystem(t *testing.T) {
	agg := []client.AggQueryResultRow{{NoteID: "a", Title: "x"}}
	hits := rrfFuseMemory(agg, nil, 60)
	if len(hits) != 1 {
		t.Fatalf("want 1 hit, got %d", len(hits))
	}
	if hits[0].Source != "aggregator" {
		t.Errorf("Source = %q, want aggregator", hits[0].Source)
	}
}

func TestRRFFuseMemoryEmpty(t *testing.T) {
	hits := rrfFuseMemory(nil, nil, 60)
	if len(hits) != 0 {
		t.Errorf("empty input should produce 0 hits, got %d", len(hits))
	}
}

func TestMemoryListRenders(t *testing.T) {
	c := &fakeMemoryClient{
		listResp: []client.AggPinNote{
			{NoteID: "internal-platform-x/M0", ProjectID: "internal-platform-x", Title: "max-scope doctrine", PromotedBy: "testuser"},
			{NoteID: "zen-swarm/m1", ProjectID: "zen-swarm", Title: "tdd discipline", PromotedBy: "testuser"},
		},
	}
	flags := MemoryListFlags{Limit: 10, Format: "text"}
	var w bytes.Buffer
	if err := RunMemoryList(context.Background(), c, flags, &w); err != nil {
		t.Fatalf("RunMemoryList: %v", err)
	}
	out := w.String()
	for _, want := range []string{"internal-platform-x/M0", "zen-swarm/m1", "max-scope doctrine", "tdd discipline"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output: %s", want, out)
		}
	}
}

func TestMemoryListEmpty(t *testing.T) {
	c := &fakeMemoryClient{}
	flags := MemoryListFlags{Limit: 10, Format: "text"}
	var w bytes.Buffer
	if err := RunMemoryList(context.Background(), c, flags, &w); err != nil {
		t.Fatalf("RunMemoryList: %v", err)
	}
	if !strings.Contains(w.String(), "no pinned notes") {
		t.Errorf("expected empty placeholder, got: %s", w.String())
	}
}

func TestMemoryListJSONFormat(t *testing.T) {
	c := &fakeMemoryClient{
		listResp: []client.AggPinNote{
			{NoteID: "a/b", ProjectID: "a", Title: "t"},
		},
	}
	flags := MemoryListFlags{Limit: 10, Format: "json"}
	var w bytes.Buffer
	if err := RunMemoryList(context.Background(), c, flags, &w); err != nil {
		t.Fatalf("RunMemoryList: %v", err)
	}

	var notes []client.AggPinNote
	if err := json.Unmarshal(w.Bytes(), &notes); err != nil {
		t.Fatalf("JSON decode: %v\noutput: %s", err, w.String())
	}
	if len(notes) != 1 || notes[0].NoteID != "a/b" {
		t.Errorf("decoded notes = %+v", notes)
	}
}

func TestMemoryListPassesLimitAndOffset(t *testing.T) {
	c := &fakeMemoryClient{}
	flags := MemoryListFlags{Limit: 50, Offset: 100}
	var w bytes.Buffer
	if err := RunMemoryList(context.Background(), c, flags, &w); err != nil {
		t.Fatalf("RunMemoryList: %v", err)
	}
	if c.lastListLimit != 50 {
		t.Errorf("lastListLimit = %d, want 50", c.lastListLimit)
	}
	if c.lastListOffset != 100 {
		t.Errorf("lastListOffset = %d, want 100", c.lastListOffset)
	}
}

func TestMemoryPinCallsPromote(t *testing.T) {
	c := &fakeMemoryClient{}
	flags := MemoryPinFlags{NoteID: "internal-platform-x/M0", Reason: "important for max-scope", OperatorID: "testuser"}
	var w bytes.Buffer
	if err := RunMemoryPin(context.Background(), c, flags, &w); err != nil {
		t.Fatalf("RunMemoryPin: %v", err)
	}
	if !c.promoteCalled {
		t.Error("pin must call MemoryPromote (alias)")
	}
	if c.lastPromoteReq.NoteID != "internal-platform-x/M0" {
		t.Errorf("NoteID forwarded = %q, want internal-platform-x/M0", c.lastPromoteReq.NoteID)
	}
	if c.lastPromoteReq.Reason != "important for max-scope" {
		t.Errorf("Reason forwarded = %q", c.lastPromoteReq.Reason)
	}
	if c.lastPromoteReq.OperatorID != "testuser" {
		t.Errorf("OperatorID forwarded = %q", c.lastPromoteReq.OperatorID)
	}
}

func TestMemoryPromoteCallsPromote(t *testing.T) {
	c := &fakeMemoryClient{}
	flags := MemoryPromoteFlags{NoteID: "zen-swarm/m1", Reason: "doctrine reference", OperatorID: "testuser"}
	var w bytes.Buffer
	if err := RunMemoryPromote(context.Background(), c, flags, &w); err != nil {
		t.Fatalf("RunMemoryPromote: %v", err)
	}
	if !c.promoteCalled {
		t.Error("promote must call MemoryPromote")
	}
	if c.lastPromoteReq.NoteID != "zen-swarm/m1" {
		t.Errorf("NoteID forwarded = %q", c.lastPromoteReq.NoteID)
	}
}

func TestMemoryPinRequiresReason(t *testing.T) {
	c := &fakeMemoryClient{}
	flags := MemoryPinFlags{NoteID: "n1", Reason: "   "}
	var w bytes.Buffer
	err := RunMemoryPin(context.Background(), c, flags, &w)
	if err == nil {
		t.Fatal("expected error for empty reason")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable, got: %v", err)
	}
	if c.promoteCalled {
		t.Error("MemoryPromote should not be called with empty reason")
	}
}

func TestMemoryPromoteRequiresReason(t *testing.T) {
	c := &fakeMemoryClient{}
	flags := MemoryPromoteFlags{NoteID: "n1", Reason: ""}
	var w bytes.Buffer
	err := RunMemoryPromote(context.Background(), c, flags, &w)
	if err == nil {
		t.Fatal("expected error for empty reason")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable, got: %v", err)
	}
}

func TestMemoryPinRequiresNoteID(t *testing.T) {
	c := &fakeMemoryClient{}
	flags := MemoryPinFlags{NoteID: "", Reason: "x"}
	var w bytes.Buffer
	err := RunMemoryPin(context.Background(), c, flags, &w)
	if err == nil {
		t.Fatal("expected error for empty note-id")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable, got: %v", err)
	}
}

func TestMemoryUnpinCallsUnpromote(t *testing.T) {
	c := &fakeMemoryClient{}
	flags := MemoryUnpinFlags{NoteID: "internal-platform-x/M0", Reason: "obsolete"}
	var w bytes.Buffer
	if err := RunMemoryUnpin(context.Background(), c, flags, &w); err != nil {
		t.Fatalf("RunMemoryUnpin: %v", err)
	}
	if !c.unpinCalled {
		t.Error("unpin must call MemoryUnpin")
	}
	if c.lastUnpinID != "internal-platform-x/M0" {
		t.Errorf("NoteID forwarded = %q", c.lastUnpinID)
	}
}

func TestMemoryUnpinRequiresNoteID(t *testing.T) {
	c := &fakeMemoryClient{}
	flags := MemoryUnpinFlags{NoteID: ""}
	var w bytes.Buffer
	err := RunMemoryUnpin(context.Background(), c, flags, &w)
	if err == nil {
		t.Fatal("expected error for empty note-id")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable, got: %v", err)
	}
}

func TestMemoryPin422Recoverable(t *testing.T) {
	c := &fakeMemoryClient{
		promoteErr: &client.HTTPError{Status: http.StatusUnprocessableEntity, Path: "/v1/knowledge/aggregator/promote"},
	}
	flags := MemoryPinFlags{NoteID: "x", Reason: "y"}
	var w bytes.Buffer
	err := RunMemoryPin(context.Background(), c, flags, &w)
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsRecoverable(err) {
		t.Errorf("422 should be recoverable, got: %v", err)
	}
}

func TestMemoryPin503Unrecoverable(t *testing.T) {
	c := &fakeMemoryClient{
		promoteErr: &client.HTTPError{Status: http.StatusServiceUnavailable, Path: "/v1/knowledge/aggregator/promote"},
	}
	flags := MemoryPinFlags{NoteID: "x", Reason: "y"}
	var w bytes.Buffer
	err := RunMemoryPin(context.Background(), c, flags, &w)
	if err == nil {
		t.Fatal("expected error")
	}
	if IsRecoverable(err) {
		t.Errorf("503 should NOT be recoverable, got: %v", err)
	}
}

func TestMemoryQueryCmdParsesFlagsViaCobra(t *testing.T) {
	prev := memoryClientFactory
	c := &fakeMemoryClient{
		aggResp: &client.AggQueryResponse{Results: []client.AggQueryResultRow{
			{NoteID: "n1", Title: "hit", Snippet: "snippet"},
		}},
	}
	memoryClientFactory = func(_ *cobra.Command) MemoryClient { return c }
	t.Cleanup(func() { memoryClientFactory = prev })

	cmd := NewMemoryCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"query", "context cancellation", "--limit", "5", "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if c.lastAggReq.Text != "context cancellation" {
		t.Errorf("FreeText forwarded = %q", c.lastAggReq.Text)
	}
}

func TestMemoryListCmdParsesFlagsViaCobra(t *testing.T) {
	prev := memoryClientFactory
	c := &fakeMemoryClient{
		listResp: []client.AggPinNote{{NoteID: "x", ProjectID: "p", Title: "t"}},
	}
	memoryClientFactory = func(_ *cobra.Command) MemoryClient { return c }
	t.Cleanup(func() { memoryClientFactory = prev })

	cmd := NewMemoryCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"list", "--limit", "25", "--format", "text"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if c.lastListLimit != 25 {
		t.Errorf("Limit forwarded = %d", c.lastListLimit)
	}
}

func TestMemoryPinCmdParsesFlagsViaCobra(t *testing.T) {
	prev := memoryClientFactory
	c := &fakeMemoryClient{}
	memoryClientFactory = func(_ *cobra.Command) MemoryClient { return c }
	t.Cleanup(func() { memoryClientFactory = prev })

	cmd := NewMemoryCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"pin", "internal-platform-x/M0", "--reason", "doctrine reference"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if c.lastPromoteReq.NoteID != "internal-platform-x/M0" {
		t.Errorf("NoteID forwarded = %q", c.lastPromoteReq.NoteID)
	}
	if c.lastPromoteReq.Reason != "doctrine reference" {
		t.Errorf("Reason forwarded = %q", c.lastPromoteReq.Reason)
	}
}

func TestMemoryPinCmdMissingReasonFailsCobra(t *testing.T) {
	prev := memoryClientFactory
	c := &fakeMemoryClient{}
	memoryClientFactory = func(_ *cobra.Command) MemoryClient { return c }
	t.Cleanup(func() { memoryClientFactory = prev })

	cmd := NewMemoryCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"pin", "internal-platform-x/M0"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --reason")
	}
	if c.promoteCalled {
		t.Error("MemoryPromote should not be called when --reason is missing")
	}
}

type memoryServerCapture struct {
	path   string
	method string
	body   []byte
}

func newProductionMemoryClientServer(t *testing.T, capture *memoryServerCapture) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.path = r.URL.Path
		capture.method = r.Method
		if b, err := io.ReadAll(r.Body); err == nil {
			capture.body = b
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/knowledge/aggregator/query":
			_, _ = w.Write([]byte(`{"results":[{"note_id":"n1","title":"hit","score":0.9,"snippet":"snip","source":"aggregator"}]}`))
		case "/v1/knowledge/aggregator/list":
			_, _ = w.Write([]byte(`{"notes":[{"note_id":"a/b","project_id":"a","title":"t","promoted_by":"testuser"}]}`))
		case "/v1/knowledge/aggregator/promote":
			_, _ = w.Write([]byte(`{"note_id":"x","audit_chain_anchor":"sha256:abc","promoted_at":"2026-05-18T10:00:00Z"}`))
		case "/v1/knowledge/aggregator/unpromote":
			_, _ = w.Write([]byte(`{"note_id":"x","unpromoted_at":"2026-05-18T10:00:00Z"}`))
		case "/v1/knowledge/ecosystem/query":
			_, _ = w.Write([]byte(`{"chunks":[{"symbol_path":"context.Context","source_url":"u","reranker_score":0.9,"version":"1.22","content_text":"x"}],"abstained":false,"provenance":{"detection_layer":1,"routing_method":"router","fresh_dispatch":false,"doctrine_applied":"max-scope"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestProductionMemoryClient_MemoryQueryRoutesToAggregator(t *testing.T) {
	t.Parallel()
	cap := &memoryServerCapture{}
	srv := newProductionMemoryClientServer(t, cap)
	p := &productionMemoryClient{c: client.NewWithBaseURL(srv.URL)}
	resp, err := p.MemoryQuery(context.Background(), client.AggQueryRequest{Text: "context", Limit: 5})
	if err != nil {
		t.Fatalf("MemoryQuery: %v", err)
	}
	if cap.path != "/v1/knowledge/aggregator/query" {
		t.Errorf("path = %q, want /v1/knowledge/aggregator/query", cap.path)
	}
	if cap.method != http.MethodPost {
		t.Errorf("method = %q, want POST", cap.method)
	}
	if !strings.Contains(string(cap.body), `"text":"context"`) {
		t.Errorf("request body missing text field: %s", string(cap.body))
	}
	if resp == nil || len(resp.Results) != 1 || resp.Results[0].NoteID != "n1" {
		t.Errorf("decoded response unexpected: %+v", resp)
	}
}

func TestProductionMemoryClient_MemoryListRoutesToAggregator(t *testing.T) {
	t.Parallel()
	cap := &memoryServerCapture{}
	srv := newProductionMemoryClientServer(t, cap)
	p := &productionMemoryClient{c: client.NewWithBaseURL(srv.URL)}
	resp, err := p.MemoryList(context.Background(), 25, 0)
	if err != nil {
		t.Fatalf("MemoryList: %v", err)
	}
	if cap.path != "/v1/knowledge/aggregator/list" {
		t.Errorf("path = %q, want /v1/knowledge/aggregator/list", cap.path)
	}
	if cap.method != http.MethodGet {
		t.Errorf("method = %q, want GET", cap.method)
	}
	if resp == nil || len(resp.Notes) != 1 || resp.Notes[0].NoteID != "a/b" {
		t.Errorf("decoded response unexpected: %+v", resp)
	}
}

func TestProductionMemoryClient_MemoryPinRoutesToPromote(t *testing.T) {
	t.Parallel()
	cap := &memoryServerCapture{}
	srv := newProductionMemoryClientServer(t, cap)
	p := &productionMemoryClient{c: client.NewWithBaseURL(srv.URL)}
	if err := p.MemoryPin(context.Background(), "internal-platform-x/M0"); err != nil {
		t.Fatalf("MemoryPin: %v", err)
	}
	if cap.path != "/v1/knowledge/aggregator/promote" {
		t.Errorf("path = %q, want /v1/knowledge/aggregator/promote", cap.path)
	}
	if cap.method != http.MethodPost {
		t.Errorf("method = %q, want POST", cap.method)
	}
	if !strings.Contains(string(cap.body), `"note_id":"internal-platform-x/M0"`) {
		t.Errorf("request body missing note_id: %s", string(cap.body))
	}
}

func TestProductionMemoryClient_MemoryUnpinRoutesToUnpromote(t *testing.T) {
	t.Parallel()
	cap := &memoryServerCapture{}
	srv := newProductionMemoryClientServer(t, cap)
	p := &productionMemoryClient{c: client.NewWithBaseURL(srv.URL)}
	if err := p.MemoryUnpin(context.Background(), "internal-platform-x/M0"); err != nil {
		t.Fatalf("MemoryUnpin: %v", err)
	}
	if cap.path != "/v1/knowledge/aggregator/unpromote" {
		t.Errorf("path = %q, want /v1/knowledge/aggregator/unpromote", cap.path)
	}
	if cap.method != http.MethodPost {
		t.Errorf("method = %q, want POST", cap.method)
	}
	if !strings.Contains(string(cap.body), `"note_id":"internal-platform-x/M0"`) {
		t.Errorf("request body missing note_id: %s", string(cap.body))
	}
}

func TestProductionMemoryClient_MemoryPromoteRoutesToPromote(t *testing.T) {
	t.Parallel()
	cap := &memoryServerCapture{}
	srv := newProductionMemoryClientServer(t, cap)
	p := &productionMemoryClient{c: client.NewWithBaseURL(srv.URL)}
	req := client.AggPromoteRequest{
		NoteID:     "zen-swarm/m1",
		OperatorID: "testuser",
		Reason:     "max-scope reference",
	}
	if err := p.MemoryPromote(context.Background(), req); err != nil {
		t.Fatalf("MemoryPromote: %v", err)
	}
	if cap.path != "/v1/knowledge/aggregator/promote" {
		t.Errorf("path = %q, want /v1/knowledge/aggregator/promote", cap.path)
	}
	body := string(cap.body)
	if !strings.Contains(body, `"note_id":"zen-swarm/m1"`) {
		t.Errorf("body missing note_id: %s", body)
	}
	if !strings.Contains(body, `"reason":"max-scope reference"`) {
		t.Errorf("body missing reason: %s", body)
	}
	if !strings.Contains(body, `"operator_id":"testuser"`) {
		t.Errorf("body missing operator_id: %s", body)
	}
}

func TestProductionMemoryClient_MemoryQueryError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	t.Cleanup(srv.Close)
	p := &productionMemoryClient{c: client.NewWithBaseURL(srv.URL)}
	resp, err := p.MemoryQuery(context.Background(), client.AggQueryRequest{Text: "x"})
	if err == nil {
		t.Fatal("expected daemon 500 to surface as adapter error")
	}
	if resp != nil {
		t.Errorf("expected nil response on error, got: %+v", resp)
	}
}

func TestProductionMemoryClient_MemoryListError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	t.Cleanup(srv.Close)
	p := &productionMemoryClient{c: client.NewWithBaseURL(srv.URL)}
	resp, err := p.MemoryList(context.Background(), 10, 0)
	if err == nil {
		t.Fatal("expected daemon 500 to surface as adapter error")
	}
	if resp != nil {
		t.Errorf("expected nil response on error, got: %+v", resp)
	}
}

func TestProductionMemoryClient_EcosystemQueryRoutesToEcosystem(t *testing.T) {
	t.Parallel()
	cap := &memoryServerCapture{}
	srv := newProductionMemoryClientServer(t, cap)
	p := &productionMemoryClient{c: client.NewWithBaseURL(srv.URL)}
	req := client.EcosystemQueryRequest{
		Query:     "context cancellation",
		Ecosystem: "go",
		Doctrine:  "max-scope",
	}
	resp, err := p.EcosystemQuery(context.Background(), req)
	if err != nil {
		t.Fatalf("EcosystemQuery: %v", err)
	}
	if cap.path != "/v1/knowledge/ecosystem/query" {
		t.Errorf("path = %q, want /v1/knowledge/ecosystem/query", cap.path)
	}
	if cap.method != http.MethodPost {
		t.Errorf("method = %q, want POST", cap.method)
	}
	body := string(cap.body)
	if !strings.Contains(body, `"query":"context cancellation"`) {
		t.Errorf("body missing query: %s", body)
	}
	if !strings.Contains(body, `"ecosystem":"go"`) {
		t.Errorf("body missing ecosystem: %s", body)
	}
	if resp == nil || len(resp.Chunks) != 1 || resp.Chunks[0].SymbolPath != "context.Context" {
		t.Errorf("decoded response unexpected: %+v", resp)
	}
}

func TestProductionMemoryClient_SatisfiesInterface(t *testing.T) {
	t.Parallel()
	var _ MemoryClient = (*productionMemoryClient)(nil)
	p := &productionMemoryClient{c: client.NewWithBaseURL("http://127.0.0.1:0")}
	if p == nil {
		t.Fatal("productionMemoryClient construction returned nil")
	}
}

func TestMemoryClientFactory_BuildsProductionAdapter(t *testing.T) {
	t.Parallel()
	carrier := &cobra.Command{}
	carrier.PersistentFlags().String("uds", "", "")
	mc := memoryClientFactory(carrier)
	if mc == nil {
		t.Fatal("memoryClientFactory returned nil")
	}
	if _, ok := mc.(*productionMemoryClient); !ok {
		t.Errorf("memoryClientFactory returned %T, want *productionMemoryClient", mc)
	}
}

func TestFormatMemoryTimeZeroPath(t *testing.T) {
	t.Parallel()
	got := formatMemoryTime(time.Time{})
	if got != "" {
		t.Errorf("formatMemoryTime(zero) = %q, want empty string", got)
	}
}

func TestFormatMemoryTimeNonZeroPath(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 5, 18, 10, 30, 0, 0, time.UTC)
	got := formatMemoryTime(ts)
	if got != "2026-05-18T10:30Z" {
		t.Errorf("formatMemoryTime(non-zero) = %q, want 2026-05-18T10:30Z", got)
	}
}

func TestMemoryListRejectsBadFormat(t *testing.T) {
	t.Parallel()
	c := &fakeMemoryClient{}
	var w bytes.Buffer
	err := RunMemoryList(context.Background(), c, MemoryListFlags{Format: "xml"}, &w)
	if err == nil {
		t.Fatal("expected error for unsupported --format")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable, got %v", err)
	}
	if c.listCalled {
		t.Error("MemoryList should not be invoked when --format is invalid")
	}
}

func TestMemoryListDefaultLimitWhenZero(t *testing.T) {
	t.Parallel()
	c := &fakeMemoryClient{}
	var w bytes.Buffer
	if err := RunMemoryList(context.Background(), c, MemoryListFlags{Limit: 0}, &w); err != nil {
		t.Fatalf("RunMemoryList: %v", err)
	}
	if c.lastListLimit != defaultMemoryListLimit {
		t.Errorf("default limit not applied: got %d, want %d", c.lastListLimit, defaultMemoryListLimit)
	}
}

func TestMemoryListRejectsNegativeLimit(t *testing.T) {
	t.Parallel()
	c := &fakeMemoryClient{}
	var w bytes.Buffer
	err := RunMemoryList(context.Background(), c, MemoryListFlags{Limit: -1}, &w)
	if err == nil {
		t.Fatal("expected error for negative --limit")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable, got %v", err)
	}
	if c.listCalled {
		t.Error("MemoryList should not be invoked with negative limit")
	}
}

func TestMemoryListRejectsNegativeOffset(t *testing.T) {
	t.Parallel()
	c := &fakeMemoryClient{}
	var w bytes.Buffer
	err := RunMemoryList(context.Background(), c, MemoryListFlags{Offset: -1}, &w)
	if err == nil {
		t.Fatal("expected error for negative --offset")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable, got %v", err)
	}
}

func TestMemoryListTransportError(t *testing.T) {
	t.Parallel()
	c := &fakeMemoryClient{listErr: errors.New("transport boom")}
	var w bytes.Buffer
	err := RunMemoryList(context.Background(), c, MemoryListFlags{Limit: 10}, &w)
	if err == nil {
		t.Fatal("expected transport error")
	}
	if IsRecoverable(err) {
		t.Errorf("transport error should NOT be recoverable: %v", err)
	}
	if !strings.Contains(err.Error(), "memory list") {
		t.Errorf("expected `memory list` prefix in error: %v", err)
	}
}

func TestMemoryListJSONFormatNilNotes(t *testing.T) {
	t.Parallel()
	c := &fakeMemoryClient{listResp: nil}
	var w bytes.Buffer
	if err := RunMemoryList(context.Background(), c, MemoryListFlags{Format: "json", Limit: 10}, &w); err != nil {
		t.Fatalf("RunMemoryList: %v", err)
	}
	out := strings.TrimSpace(w.String())
	if out == "null" {
		t.Errorf("nil notes should render as `[]`, got: %s", out)
	}
	if !strings.HasPrefix(out, "[") || !strings.HasSuffix(out, "]") {
		t.Errorf("expected JSON array, got: %s", out)
	}
}

func TestMemoryPromoteRejectsEmptyNoteID(t *testing.T) {
	t.Parallel()
	c := &fakeMemoryClient{}
	var w bytes.Buffer
	err := RunMemoryPromote(context.Background(), c, MemoryPromoteFlags{NoteID: "  ", Reason: "x"}, &w)
	if err == nil {
		t.Fatal("expected error for empty note-id")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable, got %v", err)
	}
	if c.promoteCalled {
		t.Error("MemoryPromote should not be called when note-id is empty")
	}
}

func TestMemoryPromoteTransportError(t *testing.T) {
	t.Parallel()
	c := &fakeMemoryClient{promoteErr: errors.New("transport boom")}
	var w bytes.Buffer
	err := RunMemoryPromote(context.Background(), c, MemoryPromoteFlags{NoteID: "x", Reason: "y"}, &w)
	if err == nil {
		t.Fatal("expected transport error")
	}
	if IsRecoverable(err) {
		t.Errorf("transport error should NOT be recoverable: %v", err)
	}
	if !strings.Contains(err.Error(), "memory promote") {
		t.Errorf("expected `memory promote` prefix in error: %v", err)
	}
}

func TestMemoryUnpinTransportError(t *testing.T) {
	t.Parallel()
	c := &fakeMemoryClient{unpinErr: errors.New("transport boom")}
	var w bytes.Buffer
	err := RunMemoryUnpin(context.Background(), c, MemoryUnpinFlags{NoteID: "x"}, &w)
	if err == nil {
		t.Fatal("expected transport error")
	}
	if IsRecoverable(err) {
		t.Errorf("transport error should NOT be recoverable: %v", err)
	}
	if !strings.Contains(err.Error(), "memory unpin") {
		t.Errorf("expected `memory unpin` prefix in error: %v", err)
	}
}

func TestClassifyMemoryMutateErrorNil(t *testing.T) {
	t.Parallel()
	got := classifyMemoryMutateError(nil, "pin")
	if got != nil {
		t.Errorf("classifyMemoryMutateError(nil) = %v, want nil", got)
	}
}

func TestMemoryPromoteCmdParsesFlagsViaCobra(t *testing.T) {
	prev := memoryClientFactory
	c := &fakeMemoryClient{}
	memoryClientFactory = func(_ *cobra.Command) MemoryClient { return c }
	t.Cleanup(func() { memoryClientFactory = prev })

	cmd := NewMemoryCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"promote", "internal-platform-x/M0", "--reason", "doctrine load-bearing", "--operator", "testuser"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !c.promoteCalled {
		t.Fatal("MemoryPromote not called")
	}
	if c.lastPromoteReq.NoteID != "internal-platform-x/M0" {
		t.Errorf("NoteID forwarded = %q", c.lastPromoteReq.NoteID)
	}
	if c.lastPromoteReq.Reason != "doctrine load-bearing" {
		t.Errorf("Reason forwarded = %q", c.lastPromoteReq.Reason)
	}
	if c.lastPromoteReq.OperatorID != "testuser" {
		t.Errorf("OperatorID forwarded = %q", c.lastPromoteReq.OperatorID)
	}
	if !strings.Contains(buf.String(), "promoted: note_id=internal-platform-x/M0") {
		t.Errorf("expected success message: %s", buf.String())
	}
}

func TestMemoryPromoteCmdMissingReasonFailsCobra(t *testing.T) {
	prev := memoryClientFactory
	c := &fakeMemoryClient{}
	memoryClientFactory = func(_ *cobra.Command) MemoryClient { return c }
	t.Cleanup(func() { memoryClientFactory = prev })

	cmd := NewMemoryCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"promote", "internal-platform-x/M0"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --reason")
	}
	if c.promoteCalled {
		t.Error("MemoryPromote should not be called when --reason is missing")
	}
}

func TestMemoryUnpinCmdParsesFlagsViaCobra(t *testing.T) {
	prev := memoryClientFactory
	c := &fakeMemoryClient{}
	memoryClientFactory = func(_ *cobra.Command) MemoryClient { return c }
	t.Cleanup(func() { memoryClientFactory = prev })

	cmd := NewMemoryCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"unpin", "internal-platform-x/M0", "--reason", "obsolete", "--operator", "testuser"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !c.unpinCalled {
		t.Fatal("MemoryUnpin not called")
	}
	if c.lastUnpinID != "internal-platform-x/M0" {
		t.Errorf("NoteID forwarded = %q", c.lastUnpinID)
	}
	if !strings.Contains(buf.String(), "unpinned: note_id=internal-platform-x/M0") {
		t.Errorf("expected success message: %s", buf.String())
	}
}
