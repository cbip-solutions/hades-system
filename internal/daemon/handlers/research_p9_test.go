package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type fakeResearchStoreP9 struct {
	mu sync.Mutex

	historyArgs    []ResearchHistoryFilterP9
	historyResults [][]ResearchHistoryEntryP9
	historyErr     error

	statsArgs    []string
	statsResults []ResearchCacheStatsP9
	statsErr     error

	invalidateArgs    []string
	invalidateResults []int
	invalidateErr     error

	listArgs    []researchListArgsP9
	listResults [][]ResearchCacheEntryP9
	listErr     error
}

type researchListArgsP9 struct {
	ProjectID    string
	SourcePrefix string
}

func (f *fakeResearchStoreP9) History(_ context.Context, filter ResearchHistoryFilterP9) ([]ResearchHistoryEntryP9, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.historyArgs = append(f.historyArgs, filter)
	if f.historyErr != nil {
		return nil, f.historyErr
	}
	if len(f.historyResults) == 0 {
		return nil, nil
	}
	r := f.historyResults[0]
	f.historyResults = f.historyResults[1:]
	return r, nil
}

func (f *fakeResearchStoreP9) CacheStats(_ context.Context, projectID string) (ResearchCacheStatsP9, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statsArgs = append(f.statsArgs, projectID)
	if f.statsErr != nil {
		return ResearchCacheStatsP9{}, f.statsErr
	}
	if len(f.statsResults) == 0 {
		return ResearchCacheStatsP9{}, nil
	}
	r := f.statsResults[0]
	f.statsResults = f.statsResults[1:]
	return r, nil
}

func (f *fakeResearchStoreP9) CacheInvalidate(_ context.Context, query string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invalidateArgs = append(f.invalidateArgs, query)
	if f.invalidateErr != nil {
		return 0, f.invalidateErr
	}
	if len(f.invalidateResults) == 0 {
		return 0, nil
	}
	r := f.invalidateResults[0]
	f.invalidateResults = f.invalidateResults[1:]
	return r, nil
}

func (f *fakeResearchStoreP9) CacheList(_ context.Context, projectID, sourcePrefix string) ([]ResearchCacheEntryP9, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listArgs = append(f.listArgs, researchListArgsP9{projectID, sourcePrefix})
	if f.listErr != nil {
		return nil, f.listErr
	}
	if len(f.listResults) == 0 {
		return nil, nil
	}
	r := f.listResults[0]
	f.listResults = f.listResults[1:]
	return r, nil
}

func TestResearchP9_History_Happy(t *testing.T) {
	fake := &fakeResearchStoreP9{
		historyResults: [][]ResearchHistoryEntryP9{{
			{Query: "sqlite-vec", DispatchedAt: 100, FindingsCount: 5, Source: "cache_hit_exact"},
		}},
	}
	h := ResearchP9History(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/research/history?filter=cache_hit&since=50&project_id=proj-1", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp struct {
		Items []ResearchHistoryEntryP9 `json:"items"`
		Count int                      `json:"count"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Count != 1 {
		t.Errorf("count: got %d, want 1", resp.Count)
	}
	if resp.Items[0].Query != "sqlite-vec" {
		t.Errorf("query: got %q", resp.Items[0].Query)
	}

	if fake.historyArgs[0].Filter != "cache_hit" {
		t.Errorf("filter forwarded: got %q", fake.historyArgs[0].Filter)
	}
	if fake.historyArgs[0].Since != 50 {
		t.Errorf("since forwarded: got %d", fake.historyArgs[0].Since)
	}
	if fake.historyArgs[0].ProjectID != "proj-1" {
		t.Errorf("project_id forwarded: got %q", fake.historyArgs[0].ProjectID)
	}
}

func TestResearchP9_History_AdapterError(t *testing.T) {
	fake := &fakeResearchStoreP9{historyErr: errors.New("db offline")}
	h := ResearchP9History(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/research/history", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
	if !strings.Contains(w.Body.String(), "db offline") {
		t.Errorf("body missing error: %s", w.Body.String())
	}
}

func TestResearchP9_History_NilAdapter(t *testing.T) {
	h := ResearchP9History(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/research/history", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
	if !strings.Contains(w.Body.String(), "plan9_research_unavailable") {
		t.Errorf("body missing code: %s", w.Body.String())
	}
}

func TestResearchP9_CacheStats_Happy(t *testing.T) {
	fake := &fakeResearchStoreP9{
		statsResults: []ResearchCacheStatsP9{{
			TotalEntries:           100,
			TotalBytes:             1_000_000,
			FreshnessLagSeconds:    300,
			RevalidationQueueDepth: 5,
			StuckQueriesCount:      0,
		}},
	}
	h := ResearchP9CacheStats(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/research/cache/stats?project_id=proj-1", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp ResearchCacheStatsP9
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.FreshnessLagSeconds != 300 {
		t.Errorf("freshness_lag_seconds: got %d, want 300", resp.FreshnessLagSeconds)
	}
	if resp.RevalidationQueueDepth != 5 {
		t.Errorf("revalidation_queue_depth: got %d, want 5", resp.RevalidationQueueDepth)
	}
	if resp.TotalEntries != 100 {
		t.Errorf("total_entries: got %d, want 100", resp.TotalEntries)
	}

	if fake.statsArgs[0] != "proj-1" {
		t.Errorf("project_id: got %q", fake.statsArgs[0])
	}
}

func TestResearchP9_CacheStats_MissingProjectID(t *testing.T) {

	fake := &fakeResearchStoreP9{
		statsResults: []ResearchCacheStatsP9{{TotalEntries: 42}},
	}
	h := ResearchP9CacheStats(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/research/cache/stats", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}

	if fake.statsArgs[0] != "" {
		t.Errorf("project_id should be empty for global: got %q", fake.statsArgs[0])
	}
}

func TestResearchP9_CacheStats_AdapterError(t *testing.T) {
	fake := &fakeResearchStoreP9{statsErr: errors.New("stats unavailable")}
	h := ResearchP9CacheStats(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/research/cache/stats", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestResearchP9_CacheStats_NilAdapter(t *testing.T) {
	h := ResearchP9CacheStats(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/research/cache/stats", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
	if !strings.Contains(w.Body.String(), "plan9_research_unavailable") {
		t.Errorf("body missing code: %s", w.Body.String())
	}
}

func TestResearchP9_CacheInvalidate_Happy(t *testing.T) {
	fake := &fakeResearchStoreP9{invalidateResults: []int{12}}
	h := ResearchP9CacheInvalidate(fake)
	body, _ := json.Marshal(map[string]any{"query": "sqlite-vec performance"})
	req := httptest.NewRequest(http.MethodPost, "/v1/research/cache/invalidate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp struct {
		Invalidated int `json:"invalidated"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Invalidated != 12 {
		t.Errorf("invalidated: got %d, want 12", resp.Invalidated)
	}
	if fake.invalidateArgs[0] != "sqlite-vec performance" {
		t.Errorf("query forwarded: got %q", fake.invalidateArgs[0])
	}
}

func TestResearchP9_CacheInvalidate_MissingQuery(t *testing.T) {

	fake := &fakeResearchStoreP9{}
	h := ResearchP9CacheInvalidate(fake)
	body, _ := json.Marshal(map[string]any{"query": ""})
	req := httptest.NewRequest(http.MethodPost, "/v1/research/cache/invalidate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "query required") {
		t.Errorf("body missing 'query required': %s", w.Body.String())
	}
}

func TestResearchP9_CacheInvalidate_AdapterError(t *testing.T) {
	fake := &fakeResearchStoreP9{invalidateErr: errors.New("store error")}
	h := ResearchP9CacheInvalidate(fake)
	body, _ := json.Marshal(map[string]any{"query": "broken"})
	req := httptest.NewRequest(http.MethodPost, "/v1/research/cache/invalidate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestResearchP9_CacheInvalidate_NilAdapter(t *testing.T) {
	h := ResearchP9CacheInvalidate(nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/research/cache/invalidate", bytes.NewReader([]byte(`{"query":"x"}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
	if !strings.Contains(w.Body.String(), "plan9_research_unavailable") {
		t.Errorf("body missing code: %s", w.Body.String())
	}
}

func TestResearchP9_CacheList_Happy(t *testing.T) {
	fake := &fakeResearchStoreP9{
		listResults: [][]ResearchCacheEntryP9{{
			{Hash: "abc123", SourceURL: "https://arxiv.org/abs/2401.00001", ContentHash: "sha256:def456"},
		}},
	}
	h := ResearchP9CacheList(fake)
	req := httptest.NewRequest(http.MethodGet,
		"/v1/research/cache/list?project_id=proj-1&source=https://arxiv.org", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp struct {
		Items []ResearchCacheEntryP9 `json:"items"`
		Count int                    `json:"count"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Count != 1 {
		t.Errorf("count: got %d, want 1", resp.Count)
	}
	if resp.Items[0].SourceURL != "https://arxiv.org/abs/2401.00001" {
		t.Errorf("source_url: got %q", resp.Items[0].SourceURL)
	}
	if resp.Items[0].ContentHash != "sha256:def456" {
		t.Errorf("content_hash: got %q", resp.Items[0].ContentHash)
	}
	if fake.listArgs[0].ProjectID != "proj-1" {
		t.Errorf("project_id: got %q", fake.listArgs[0].ProjectID)
	}
	if fake.listArgs[0].SourcePrefix != "https://arxiv.org" {
		t.Errorf("source_prefix: got %q", fake.listArgs[0].SourcePrefix)
	}
}

func TestResearchP9_CacheList_MissingProjectID(t *testing.T) {

	fake := &fakeResearchStoreP9{
		listResults: [][]ResearchCacheEntryP9{{}},
	}
	h := ResearchP9CacheList(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/research/cache/list", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	if fake.listArgs[0].ProjectID != "" {
		t.Errorf("project_id should be empty: got %q", fake.listArgs[0].ProjectID)
	}
}

func TestResearchP9_CacheList_AdapterError(t *testing.T) {
	fake := &fakeResearchStoreP9{listErr: errors.New("list failed")}
	h := ResearchP9CacheList(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/research/cache/list", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestResearchP9_CacheList_NilAdapter(t *testing.T) {
	h := ResearchP9CacheList(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/research/cache/list", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
	if !strings.Contains(w.Body.String(), "plan9_research_unavailable") {
		t.Errorf("body missing code: %s", w.Body.String())
	}
}
