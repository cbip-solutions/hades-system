package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type researchCacheServer struct {
	db map[string]researchCacheRow
}

type researchCacheRow struct {
	responseJSON string
	ttlUnix      int64
}

func (s *researchCacheServer) ResearchCacheGet(hash string) (string, int64, bool, error) {
	row, ok := s.db[hash]
	if !ok {
		return "", 0, false, nil
	}
	if row.ttlUnix < time.Now().Unix() {
		return "", 0, false, nil
	}
	return row.responseJSON, row.ttlUnix, true, nil
}

func (s *researchCacheServer) ResearchCacheSet(hash, responseJSON string, ttlUnix int64) error {
	if s.db == nil {
		s.db = make(map[string]researchCacheRow)
	}
	s.db[hash] = researchCacheRow{responseJSON: responseJSON, ttlUnix: ttlUnix}
	return nil
}

func (s *researchCacheServer) ResearchCacheTTL() time.Duration {
	return 7 * 24 * time.Hour
}

func TestResearchCacheGet_Miss(t *testing.T) {
	srv := &researchCacheServer{}
	h := ResearchCacheGet(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/research/cache/get?hash=abc123", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func TestResearchCacheGet_Hit(t *testing.T) {
	srv := &researchCacheServer{
		db: map[string]researchCacheRow{
			"deadbeef": {responseJSON: `{"result":"ok"}`, ttlUnix: time.Now().Add(time.Hour).Unix()},
		},
	}
	h := ResearchCacheGet(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/research/cache/get?hash=deadbeef", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["hit"] != true {
		t.Errorf("want hit=true, got %v", resp["hit"])
	}
}

func TestResearchCacheGet_Expired(t *testing.T) {
	srv := &researchCacheServer{
		db: map[string]researchCacheRow{
			"expired": {responseJSON: `{}`, ttlUnix: time.Now().Add(-time.Hour).Unix()},
		},
	}
	h := ResearchCacheGet(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/research/cache/get?hash=expired", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 for expired entry, got %d", w.Code)
	}
}

func TestResearchCacheGet_MissingHash(t *testing.T) {
	srv := &researchCacheServer{}
	h := ResearchCacheGet(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/research/cache/get", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestResearchCacheSet_OK(t *testing.T) {
	srv := &researchCacheServer{}
	h := ResearchCacheSet(srv)
	body := map[string]any{
		"hash":          "newkey",
		"response_json": `{"data":"result"}`,
		"ttl_seconds":   604800,
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/research/cache/set", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", w.Code, w.Body.String())
	}

	resp, ttlUnix, hit, err := srv.ResearchCacheGet("newkey")
	if err != nil || !hit {
		t.Fatalf("expected cache hit after set; hit=%v err=%v", hit, err)
	}
	if resp != `{"data":"result"}` {
		t.Errorf("stored response mismatch: %q", resp)
	}
	if ttlUnix == 0 {
		t.Error("ttl_unix must be returned by ResearchCacheGet (post-review I-1)")
	}
}

func TestResearchCacheGet_TTLUnixInResponse(t *testing.T) {
	want := time.Now().Add(2 * time.Hour).Unix()
	srv := &researchCacheServer{
		db: map[string]researchCacheRow{
			"ttl-key": {responseJSON: `{}`, ttlUnix: want},
		},
	}
	h := ResearchCacheGet(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/research/cache/get?hash=ttl-key", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	got, ok := resp["ttl_unix"].(float64)
	if !ok {
		t.Fatalf("ttl_unix missing or non-numeric: %#v", resp["ttl_unix"])
	}
	if int64(got) != want {
		t.Errorf("ttl_unix: want %d, got %d", want, int64(got))
	}
}

func TestResearchCacheSet_InvalidBody(t *testing.T) {
	srv := &researchCacheServer{}
	h := ResearchCacheSet(srv)
	req := httptest.NewRequest(http.MethodPost, "/v1/research/cache/set",
		bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestResearchCacheSet_MissingFields(t *testing.T) {
	srv := &researchCacheServer{}
	h := ResearchCacheSet(srv)

	body := map[string]any{"hash": "only-hash"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/research/cache/set", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for missing response_json, got %d", w.Code)
	}
}
