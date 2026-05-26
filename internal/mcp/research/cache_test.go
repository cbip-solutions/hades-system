package research

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCacheKeyStableAcrossSourceOrder(t *testing.T) {
	a := CacheKey("q", []string{"web", "arxiv"}, 0)
	b := CacheKey("q", []string{"arxiv", "web"}, 0)
	if a != b {
		t.Errorf("hash differs across source order: %s vs %s", a, b)
	}
}

func TestCacheKeyDistinctIteration(t *testing.T) {
	a := CacheKey("q", nil, 0)
	b := CacheKey("q", nil, 1)
	if a == b {
		t.Errorf("hash collision across iteration")
	}
}

func TestCacheKeyDistinctQuery(t *testing.T) {
	a := CacheKey("q1", nil, 0)
	b := CacheKey("q2", nil, 0)
	if a == b {
		t.Errorf("hash collision across query")
	}
}

func TestCacheAdapterRoundTrip(t *testing.T) {
	store := map[string][]byte{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/research/cache/set":
			body := make([]byte, 4096)
			n, _ := r.Body.Read(body)
			body = body[:n]

			var p struct {
				Hash     string `json:"hash"`
				Response any    `json:"response"`
			}
			_ = jsonUnmarshalGeneric(body, &p)
			respBytes, _ := jsonMarshalGeneric(p.Response)
			store[p.Hash] = respBytes
			w.WriteHeader(http.StatusNoContent)
		case "/v1/research/cache/get":
			h := r.URL.Query().Get("hash")
			if v, ok := store[h]; ok {
				_, _ = w.Write(v)
			} else {
				http.NotFound(w, r)
			}
		}
	}))
	defer srv.Close()
	c := NewCacheAdapter(CacheAdapterOptions{DaemonURL: srv.URL})
	hash := "h1"
	if err := c.Set(context.Background(), hash, CacheEntry{
		Hash: hash, Response: []byte(`{"x":1}`),
	}, 60); err != nil {
		t.Fatal(err)
	}
	entry, ok, err := c.Get(context.Background(), hash)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected hit")
	}
	if !strings.Contains(string(entry.Response), `"x":1`) {
		t.Errorf("payload = %s", entry.Response)
	}
}

func TestCacheAdapterMiss(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer srv.Close()
	c := NewCacheAdapter(CacheAdapterOptions{DaemonURL: srv.URL})
	_, ok, err := c.Get(context.Background(), "missing")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected miss")
	}
}

func TestCacheAdapterDaemonError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", 500)
	}))
	defer srv.Close()
	c := NewCacheAdapter(CacheAdapterOptions{DaemonURL: srv.URL})
	if _, _, err := c.Get(context.Background(), "x"); err == nil {
		t.Fatal("expected 500 error")
	}
	if err := c.Set(context.Background(), "x", CacheEntry{}, 60); err == nil {
		t.Fatal("expected 500 error on set")
	}
}

func TestCacheAdapterEmptyDaemonURLNoOp(t *testing.T) {
	c := NewCacheAdapter(CacheAdapterOptions{})
	_, ok, err := c.Get(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Errorf("expected miss on empty DaemonURL")
	}
	if err := c.Set(context.Background(), "x", CacheEntry{}, 60); err != nil {
		t.Errorf("Set returned error on empty DaemonURL: %v", err)
	}
}

func TestCacheAdapterAuthHeader(t *testing.T) {
	gotAuth := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := NewCacheAdapter(CacheAdapterOptions{
		DaemonURL: srv.URL,
		AuthToken: "sekret",
	})
	_, _, _ = c.Get(context.Background(), "x")
	if !strings.Contains(gotAuth, "sekret") {
		t.Errorf("auth = %q", gotAuth)
	}
}

func jsonUnmarshalGeneric(b []byte, v any) error {
	return json.Unmarshal(b, v)
}
func jsonMarshalGeneric(v any) ([]byte, error) {
	return json.Marshal(v)
}
