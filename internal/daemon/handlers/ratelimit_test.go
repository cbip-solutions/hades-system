package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type rateLimitServer struct {
	thresholds map[string]int
}

func (r *rateLimitServer) RateLimitThreshold(endpoint string) int {
	if t, ok := r.thresholds[endpoint]; ok {
		return t
	}
	return 100
}

var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func wrapBucket(bucket *tokenBucket, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok, retryAfter := bucket.tryConsume()
		if !ok {
			retryMs := retryAfter.Milliseconds()
			if retryMs < 1 {
				retryMs = 1
			}
			w.Header().Set("Retry-After", fmt.Sprintf("%d", retryMs))
			w.Header().Set("X-Zen-Rate-Limit-Endpoint", "test")
			writeJSON(w, http.StatusTooManyRequests, map[string]string{
				"error":       "rate limit exceeded",
				"retry_after": fmt.Sprintf("%dms", retryMs),
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func TestRateLimiter_AllowsUnderLimit(t *testing.T) {
	srv := &rateLimitServer{thresholds: map[string]int{"test-allow": 10}}

	bucket := newTokenBucket(10)
	limited := wrapBucket(bucket, okHandler)

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		limited.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: want 200, got %d", i+1, w.Code)
		}
	}
	_ = srv
}

func TestRateLimiter_Blocks429WhenExhausted(t *testing.T) {

	bucket := newTokenBucket(1)
	limited := wrapBucket(bucket, okHandler)

	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	w1 := httptest.NewRecorder()
	limited.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request: want 200, got %d", w1.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	w2 := httptest.NewRecorder()
	limited.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: want 429, got %d", w2.Code)
	}
}

func TestRateLimiter_RefillsOverTime(t *testing.T) {

	bucket := newTokenBucket(10)
	limited := wrapBucket(bucket, okHandler)

	for i := 0; i < 15; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		limited.ServeHTTP(w, req)
	}

	time.Sleep(200 * time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	limited.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("after refill: want 200, got %d", w.Code)
	}
}

func TestRateLimiter_RetryAfterHeader(t *testing.T) {
	bucket := newTokenBucket(1)
	limited := wrapBucket(bucket, okHandler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	limited.ServeHTTP(w, req)

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	w2 := httptest.NewRecorder()
	limited.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d", w2.Code)
	}
	if w2.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header must be present on 429")
	}
}

func TestRateLimiter_DefaultThreshold(t *testing.T) {

	srv := &rateLimitServer{thresholds: map[string]int{}}
	registry := NewBucketRegistry()
	limited := RateLimitMiddleware(srv, registry, "test-default-threshold-unique-key", okHandler)

	for i := 0; i < 50; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		limited.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: want 200, got %d (default threshold)", i+1, w.Code)
		}
	}
}

// TestRateLimiter_PerServerIsolation verifies that two distinct registries
// do NOT share state — the post-review C-1 regression test.
//
// Prior to the fix this was impossible to express (registry was a process
// global). Now each *daemon.Server owns its own; tests construct one per
// case and verify that exhausting registry A does not affect registry B.
func TestRateLimiter_PerServerIsolation(t *testing.T) {
	srv := &rateLimitServer{thresholds: map[string]int{"shared-key": 1}}
	regA := NewBucketRegistry()
	regB := NewBucketRegistry()

	limitedA := RateLimitMiddleware(srv, regA, "shared-key", okHandler)
	limitedB := RateLimitMiddleware(srv, regB, "shared-key", okHandler)

	{
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		limitedA.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("regA first: want 200, got %d", w.Code)
		}
	}
	{
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		limitedA.ServeHTTP(w, req)
		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("regA exhausted: want 429, got %d", w.Code)
		}
	}

	// Registry B's bucket for the same key MUST be untouched.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	limitedB.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("regB independent: want 200, got %d", w.Code)
	}
}

func TestRateLimiter_InvalidateAll(t *testing.T) {
	srv := &rateLimitServer{thresholds: map[string]int{"reload-key": 1}}
	registry := NewBucketRegistry()
	limited := RateLimitMiddleware(srv, registry, "reload-key", okHandler)

	{
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		limited.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("first: want 200, got %d", w.Code)
		}
	}

	{
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		limited.ServeHTTP(w, req)
		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("exhausted: want 429, got %d", w.Code)
		}
	}

	srv.thresholds["reload-key"] = 100
	registry.InvalidateAll()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	limited.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("after invalidate: want 200, got %d", w.Code)
	}
}

func TestRateLimiter_InvalidateEndpoint(t *testing.T) {
	srv := &rateLimitServer{thresholds: map[string]int{"a": 1, "b": 1}}
	registry := NewBucketRegistry()

	limitedA := RateLimitMiddleware(srv, registry, "a", okHandler)
	limitedB := RateLimitMiddleware(srv, registry, "b", okHandler)

	for _, lim := range []http.Handler{limitedA, limitedB} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		lim.ServeHTTP(w, req)
	}

	if !registry.InvalidateEndpoint("a") {
		t.Fatal("InvalidateEndpoint must report true when bucket present")
	}
	if registry.InvalidateEndpoint("a") {
		t.Fatal("InvalidateEndpoint must report false when bucket absent")
	}

	srv.thresholds["a"] = 100
	{
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		limitedA.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("a after invalidate: want 200, got %d", w.Code)
		}
	}
	{
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		limitedB.ServeHTTP(w, req)
		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("b untouched: want 429, got %d", w.Code)
		}
	}
}

func TestBucketRegistry_GetReadsThresholdOutsideLock(t *testing.T) {
	registry := NewBucketRegistry()

	gate := make(chan struct{})
	enteredA := make(chan struct{})

	ctx := &slowThresholdCtx{
		thresholds: map[string]int{"slow": 10, "fast": 10},
		onCall: func(endpoint string) {
			if endpoint == "slow" {
				close(enteredA)
				<-gate
			}
		},
	}

	done1 := make(chan struct{})
	go func() {
		defer close(done1)
		registry.get(ctx, "slow")
	}()

	<-enteredA

	done2 := make(chan struct{})
	go func() {
		defer close(done2)
		registry.get(ctx, "fast")
	}()

	select {
	case <-done2:

	case <-time.After(2 * time.Second):
		t.Fatal("get(\"fast\") blocked while get(\"slow\") was inside callback (I-5 regression)")
	}

	close(gate)
	<-done1
}

type slowThresholdCtx struct {
	mu         sync.Mutex
	thresholds map[string]int
	onCall     func(endpoint string)
}

func (s *slowThresholdCtx) RateLimitThreshold(endpoint string) int {
	if s.onCall != nil {
		s.onCall(endpoint)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.thresholds[endpoint]; ok {
		return t
	}
	return 100
}

func TestDefaults_ReturnsDefaultRateLimitsMap(t *testing.T) {
	got := Defaults()
	if len(got) == 0 {
		t.Fatal("Defaults() returned empty map; expected the populated DefaultRateLimits")
	}

	expected := map[string]int{
		"research_cache_get": 200,
		"audit_emit":         500,
		"budget_cap_status":  50,
		"gate_pause":         10,
	}
	for endpoint, want := range expected {
		if v := got[endpoint]; v != want {
			t.Errorf("Defaults()[%q] = %d, want %d", endpoint, v, want)
		}
	}
}

func TestBucketRegistry_GetCachesPerEndpoint(t *testing.T) {
	ctx := &slowThresholdCtx{thresholds: map[string]int{"/v1/test": 50}}
	reg := NewBucketRegistry()
	b1 := reg.get(ctx, "/v1/test")
	b2 := reg.get(ctx, "/v1/test")
	if b1 != b2 {
		t.Errorf("registry.get returned different buckets for same endpoint")
	}
}
