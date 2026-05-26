package client_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/mcp/client"
)

func fakeResearchCacheServer(t *testing.T) *httptest.Server {
	t.Helper()
	type entry struct {
		Response  string    `json:"response"`
		Hash      string    `json:"hash"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	store := map[string]entry{}

	mux := http.NewServeMux()

	mux.HandleFunc("/v1/research/cache/get", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		hash := r.URL.Query().Get("hash")
		if hash == "" {
			http.Error(w, "missing hash", http.StatusBadRequest)
			return
		}
		e, ok := store[hash]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(e)
	})

	mux.HandleFunc("/v1/research/cache/set", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Hash     string `json:"hash"`
			Response string `json:"response"`
			TTLNS    int64  `json:"ttl_ns"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		store[req.Hash] = entry{
			Response:  req.Response,
			Hash:      req.Hash,
			ExpiresAt: time.Now().Add(time.Duration(req.TTLNS)),
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	return httptest.NewServer(mux)
}

func newCacheClient(t *testing.T, srv *httptest.Server) *client.CacheClient {
	t.Helper()
	c := newTestClient(t, srv)
	return client.NewCacheClient(c)
}

func TestCacheGet_Miss(t *testing.T) {
	srv := fakeResearchCacheServer(t)
	defer srv.Close()

	cc := newCacheClient(t, srv)
	hit, err := cc.Get(context.Background(), "nonexistent query")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil hit on miss, got %+v", hit)
	}
}

func TestCacheSetThenGet_Hit(t *testing.T) {
	srv := fakeResearchCacheServer(t)
	defer srv.Close()

	cc := newCacheClient(t, srv)
	query := "golang concurrent map access"
	response := `{"findings":"use sync.Map or RWMutex"}`

	err := cc.Set(context.Background(), client.CacheSetRequest{
		Query:    query,
		Response: response,
		TTL:      24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("Set: %v", err)
	}

	hit, err := cc.Get(context.Background(), query)
	if err != nil {
		t.Fatalf("Get after Set: %v", err)
	}
	if hit == nil {
		t.Fatal("expected cache hit, got nil")
	}
	if hit.Response != response {
		t.Errorf("Response = %q, want %q", hit.Response, response)
	}

	expectedHash := fmt.Sprintf("%x", sha256.Sum256([]byte(query)))
	if hit.Hash != expectedHash {
		t.Errorf("Hash = %q, want %q", hit.Hash, expectedHash)
	}
}

func TestCacheGet_DifferentQueriesDontCollide(t *testing.T) {
	srv := fakeResearchCacheServer(t)
	defer srv.Close()

	cc := newCacheClient(t, srv)

	if err := cc.Set(context.Background(), client.CacheSetRequest{
		Query:    "query A",
		Response: `{"result":"A"}`,
		TTL:      time.Hour,
	}); err != nil {
		t.Fatalf("Set A: %v", err)
	}
	if err := cc.Set(context.Background(), client.CacheSetRequest{
		Query:    "query B",
		Response: `{"result":"B"}`,
		TTL:      time.Hour,
	}); err != nil {
		t.Fatalf("Set B: %v", err)
	}

	hitA, _ := cc.Get(context.Background(), "query A")
	hitB, _ := cc.Get(context.Background(), "query B")
	if hitA == nil || hitB == nil {
		t.Fatal("both queries must hit")
	}
	if hitA.Response == hitB.Response {
		t.Error("different queries returned same cached response (hash collision)")
	}
}

func TestCacheSet_HashDerivedFromQuery(t *testing.T) {

	srv := fakeResearchCacheServer(t)
	defer srv.Close()

	cc := newCacheClient(t, srv)
	const query = "sha256 derivation test"
	err := cc.Set(context.Background(), client.CacheSetRequest{
		Query:    query,
		Response: `{"ok":true}`,
		TTL:      time.Hour,
	})
	if err != nil {
		t.Fatalf("Set: %v", err)
	}

	hit, err := cc.Get(context.Background(), query)
	if err != nil || hit == nil {
		t.Fatalf("expected hit, err=%v hit=%v", err, hit)
	}
	expected := fmt.Sprintf("%x", sha256.Sum256([]byte(query)))
	if hit.Hash != expected {
		t.Errorf("Hash = %q, want %q", hit.Hash, expected)
	}
}
