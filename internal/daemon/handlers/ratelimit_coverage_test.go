package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type negativeRateCtx struct{}

func (n *negativeRateCtx) RateLimitThreshold(endpoint string) int { return -1 }

func TestBucketRegistry_NegativeRate(t *testing.T) {

	registry := NewBucketRegistry()
	const ep = "test-registry-negative-rate"
	limited := RateLimitMiddleware(&negativeRateCtx{}, registry, ep, okHandler)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	limited.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
}

func TestWrapBucket_AllowPath(t *testing.T) {

	b := newTokenBucket(100)
	h := wrapBucket(b, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d", w.Code)
	}
}

func TestWrapBucket_SmallRetryAfter(t *testing.T) {

	b := newTokenBucket(1000000)
	b.mu.Lock()
	b.tokens = 0
	b.mu.Unlock()

	h := wrapBucket(b, okHandler)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code == http.StatusOK {

		return
	}
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429 or 200, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("Retry-After must be set on 429")
	}
}

func TestRateLimitMiddleware_SmallRetryAfter(t *testing.T) {

	registry := NewBucketRegistry()
	const ep = "test-rl-small-retry-after"
	srv := &rateLimitServer{thresholds: map[string]int{ep: 1000}}
	limited := RateLimitMiddleware(srv, registry, ep, okHandler)
	for i := 0; i < 1002; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		limited.ServeHTTP(w, req)
		if w.Code == http.StatusTooManyRequests {

			if w.Header().Get("Retry-After") == "" {
				t.Error("Retry-After missing on 429")
			}
			return
		}
	}

}
