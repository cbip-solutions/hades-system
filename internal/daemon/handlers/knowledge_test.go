package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/knowledge"
)

type fakeKnowledgeIndex struct {
	mu sync.Mutex

	queryRows   []knowledge.Result
	queryErr    error
	stats       KnowledgeStats
	statsErr    error
	reindexResp ReindexResult
	reindexErr  error

	lastQuery   knowledge.Query
	lastReindex ReindexRequest
}

func (f *fakeKnowledgeIndex) Query(_ context.Context, q knowledge.Query) ([]knowledge.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastQuery = q
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return f.queryRows, nil
}

func (f *fakeKnowledgeIndex) Reindex(_ context.Context, req ReindexRequest) (ReindexResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastReindex = req
	if f.reindexErr != nil {
		return ReindexResult{}, f.reindexErr
	}
	return f.reindexResp, nil
}

func (f *fakeKnowledgeIndex) Stats(_ context.Context) (KnowledgeStats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.statsErr != nil {
		return KnowledgeStats{}, f.statsErr
	}
	return f.stats, nil
}

type fakeKnowledgeAccessor struct{ idx KnowledgeIndex }

func (f *fakeKnowledgeAccessor) KnowledgeIndex() KnowledgeIndex { return f.idx }

type nilKnowledgeAccessor struct{}

func (nilKnowledgeAccessor) KnowledgeIndex() KnowledgeIndex { return nil }

func TestKnowledgeQuery503WhenNotConfigured(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/query", strings.NewReader(`{}`))
	KnowledgeQueryHandler(nilKnowledgeAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestKnowledgeReindex503WhenNotConfigured(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/reindex", strings.NewReader(`{}`))
	KnowledgeReindexHandler(nilKnowledgeAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestKnowledgeStats503WhenNotConfigured(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/knowledge/stats", nil)
	KnowledgeStatsHandler(nilKnowledgeAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestKnowledgeQueryBadJSON(t *testing.T) {
	idx := &fakeKnowledgeIndex{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/query", strings.NewReader(`not json`))
	KnowledgeQueryHandler(&fakeKnowledgeAccessor{idx: idx}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestKnowledgeReindexBadJSON(t *testing.T) {
	idx := &fakeKnowledgeIndex{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/reindex", strings.NewReader(`not json`))
	KnowledgeReindexHandler(&fakeKnowledgeAccessor{idx: idx}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestKnowledgeQueryInvalidFileType422(t *testing.T) {
	idx := &fakeKnowledgeIndex{}
	rec := httptest.NewRecorder()
	body := `{"type":["banana"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/query", strings.NewReader(body))
	KnowledgeQueryHandler(&fakeKnowledgeAccessor{idx: idx}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestKnowledgeQueryHappyPath(t *testing.T) {
	idx := &fakeKnowledgeIndex{
		queryRows: []knowledge.Result{
			{
				Doc: knowledge.Doc{
					FilePath:     "/tmp/x.md",
					ProjectAlias: "internal-platform-x",
					FileType:     knowledge.FileTypeMemory,
					Title:        "X",
					LastModified: time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC),
				},
				Score:   0.5,
				Snippet: "[hi] world",
			},
		},
	}
	rec := httptest.NewRecorder()
	body := `{"free_text":"hi","limit":5}`
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/query", strings.NewReader(body))
	KnowledgeQueryHandler(&fakeKnowledgeAccessor{idx: idx}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp KnowledgeQueryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(resp.Rows))
	}
	if resp.Rows[0].Title != "X" {
		t.Errorf("Title = %q, want X", resp.Rows[0].Title)
	}
	if resp.Rows[0].FileType != "memory" {
		t.Errorf("FileType = %q, want memory", resp.Rows[0].FileType)
	}

	if idx.lastQuery.FreeText != "hi" {
		t.Errorf("FreeText = %q, want hi", idx.lastQuery.FreeText)
	}
	if idx.lastQuery.Limit != 5 {
		t.Errorf("Limit = %d, want 5", idx.lastQuery.Limit)
	}
}

func TestKnowledgeQueryPropagatesAllFilters(t *testing.T) {
	idx := &fakeKnowledgeIndex{}
	rec := httptest.NewRecorder()
	body := `{"free_text":"abc","project_alias":["a","b"],"type":["memory","adr"],"since_seconds":86400,"limit":7,"code_symbol":"fooFn"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/query", strings.NewReader(body))
	KnowledgeQueryHandler(&fakeKnowledgeAccessor{idx: idx}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if idx.lastQuery.FreeText != "abc" {
		t.Errorf("FreeText = %q", idx.lastQuery.FreeText)
	}
	if len(idx.lastQuery.ProjectFilter) != 2 || idx.lastQuery.ProjectFilter[0] != "a" {
		t.Errorf("ProjectFilter = %v", idx.lastQuery.ProjectFilter)
	}
	if len(idx.lastQuery.TypeFilter) != 2 || idx.lastQuery.TypeFilter[0] != knowledge.FileTypeMemory {
		t.Errorf("TypeFilter = %v", idx.lastQuery.TypeFilter)
	}
	if idx.lastQuery.SinceFilter == nil {
		t.Fatal("SinceFilter not set")
	}
	if *idx.lastQuery.SinceFilter != 24*time.Hour {
		t.Errorf("SinceFilter = %v, want 24h", *idx.lastQuery.SinceFilter)
	}
	if idx.lastQuery.CodeSymbol != "fooFn" {
		t.Errorf("CodeSymbol = %q", idx.lastQuery.CodeSymbol)
	}
}

func TestKnowledgeReindexHappyPath(t *testing.T) {
	idx := &fakeKnowledgeIndex{reindexResp: ReindexResult{Indexed: 7, Errors: 0}}
	rec := httptest.NewRecorder()
	body := `{"full":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/reindex", strings.NewReader(body))
	KnowledgeReindexHandler(&fakeKnowledgeAccessor{idx: idx}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp KnowledgeReindexResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.OK {
		t.Errorf("OK = false, want true")
	}
	if resp.Indexed != 7 {
		t.Errorf("Indexed = %d, want 7", resp.Indexed)
	}
	if !idx.lastReindex.Full {
		t.Error("Full not propagated")
	}
}

func TestKnowledgeReindexPerProject(t *testing.T) {
	idx := &fakeKnowledgeIndex{}
	rec := httptest.NewRecorder()
	body := `{"project_alias":"internal-platform-x"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/reindex", strings.NewReader(body))
	KnowledgeReindexHandler(&fakeKnowledgeAccessor{idx: idx}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if idx.lastReindex.ProjectAlias != "internal-platform-x" {
		t.Errorf("ProjectAlias = %q, want internal-platform-x", idx.lastReindex.ProjectAlias)
	}
	if idx.lastReindex.Full {
		t.Error("Full = true, want false (per-project)")
	}
}

func TestKnowledgeStatsHappyPath(t *testing.T) {
	idx := &fakeKnowledgeIndex{
		stats: KnowledgeStats{
			TotalDocs:       100,
			ByType:          map[string]int{"memory": 80, "adr": 20},
			LastIndexedUnix: 1715000000,
		},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/knowledge/stats", nil)
	KnowledgeStatsHandler(&fakeKnowledgeAccessor{idx: idx}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp KnowledgeStatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.TotalDocs != 100 {
		t.Errorf("TotalDocs = %d, want 100", resp.TotalDocs)
	}
	if resp.ByType["memory"] != 80 {
		t.Errorf("ByType[memory] = %d, want 80", resp.ByType["memory"])
	}
	if resp.LastIndexedUnix != 1715000000 {
		t.Errorf("LastIndexedUnix = %d, want 1715000000", resp.LastIndexedUnix)
	}
}

func TestKnowledgeQuery500OnOpaqueBackend(t *testing.T) {
	idx := &fakeKnowledgeIndex{queryErr: errFakeKnowledgeIndex}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/query", strings.NewReader(`{}`))
	KnowledgeQueryHandler(&fakeKnowledgeAccessor{idx: idx}).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestKnowledgeReindex500OnOpaqueBackend(t *testing.T) {
	idx := &fakeKnowledgeIndex{reindexErr: errFakeKnowledgeIndex}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/reindex", strings.NewReader(`{}`))
	KnowledgeReindexHandler(&fakeKnowledgeAccessor{idx: idx}).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestKnowledgeStats500OnOpaqueBackend(t *testing.T) {
	idx := &fakeKnowledgeIndex{statsErr: errFakeKnowledgeIndex}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/knowledge/stats", nil)
	KnowledgeStatsHandler(&fakeKnowledgeAccessor{idx: idx}).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestKnowledgeResolveNonAccessorYields503(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/query", strings.NewReader(`{}`))
	type notAnAccessor struct{}
	KnowledgeQueryHandler(notAnAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestKnowledgeStatsNilByTypeNormalised(t *testing.T) {
	idx := &fakeKnowledgeIndex{stats: KnowledgeStats{TotalDocs: 0, ByType: nil}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/knowledge/stats", nil)
	KnowledgeStatsHandler(&fakeKnowledgeAccessor{idx: idx}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp KnowledgeStatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.ByType == nil {
		t.Error("ByType is nil; handler should normalise to empty map")
	}
}

var errFakeKnowledgeIndex = &knowledgeHandlerErr{msg: "fake knowledge index error"}

type knowledgeHandlerErr struct{ msg string }

func (e *knowledgeHandlerErr) Error() string { return e.msg }
