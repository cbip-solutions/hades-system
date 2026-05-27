// SPDX-License-Identifier: MIT
// Package handlers — ratelimit.go.
//
// Token-bucket rate limiter middleware daemon endpoints.
//
// Algorithm classic token bucket.
// - Capacity = rate (burst = 1 second of capacity; doctrine can cap at 2×).
// - Refill: one token per (1s / rate); refill computed lazily on each request.
// - 429 Too Many Requests when bucket empty; Retry-After header = ms until next token.
//
// Per-endpoint thresholds are doctrine-tunable via RateLimitCtx.RateLimitThreshold().
// The canonical default thresholds live in DefaultRateLimits below; *daemon.Server
// wires those through RateLimitThreshold() until doctrine loader replaces
// them with operator-tunable values (post-review I-6 fix).
//
// # Registry lifetime + per-Server isolation (post-review C-1 fix)
//
// Each *Server owns its own *BucketRegistry constructed in handlers.NewBucketRegistry().
// The registry's get() lazy-creates one bucket per endpoint key on first request and
// caches it for the life of the Server. RateLimitMiddleware now takes the registry as
// a parameter so:
//
// 1. Test isolation: each test that needs a fresh limiter constructs a new registry,
// so no leftover state bleeds across runs (`go test -count=10` passes deterministically).
// 2. Doctrine reload invalidates buckets: Server.DoctrineReload() calls
// registry.InvalidateAll() after the atomic-swap so the next request observes the
// new threshold (no stale capacity cached forever).
//
// invariant: Unix socket is the transport layer; rate limiter is an additional
// per-endpoint guard (defense-in-depth).
package handlers

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

var DefaultRateLimits = map[string]int{
	"research_cache_get":     200,
	"research_cache_set":     100,
	"audit_emit":             500,
	"budget_cap_status":      50,
	"budget_record":          50,
	"budget_axes":            50,
	"budget_anomaly":         20,
	"budget_events":          20,
	"budget_pause":           10,
	"budget_resume":          10,
	"workforce_specs":        20,
	"workforce_workers":      20,
	"workforce_checkpoints":  20,
	"workforce_fix_prompts":  20,
	"workforce_aggregations": 10,
	"gate_state":             50,
	"gate_pause":             10,
	"gate_resume":            10,
	"doctrine_state":         20,
	"doctrine_validate":      10,
	"doctrine_reload":        2,
}

func Defaults() map[string]int {
	return DefaultRateLimits
}

type RateLimitCtx interface {
	RateLimitThreshold(endpoint string) int
}

type tokenBucket struct {
	mu         sync.Mutex
	tokens     float64
	capacity   float64
	refillPer  time.Duration
	lastRefill time.Time
}

func newTokenBucket(ratePerSec int) *tokenBucket {
	capacity := float64(ratePerSec)
	if capacity < 1 {
		capacity = 1
	}
	refillInterval := time.Second
	if ratePerSec > 0 {
		refillInterval = time.Second / time.Duration(ratePerSec)
	}
	return &tokenBucket{
		tokens:     capacity,
		capacity:   capacity,
		refillPer:  refillInterval,
		lastRefill: time.Now(),
	}
}

func (b *tokenBucket) tryConsume() (ok bool, retryAfter time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastRefill)
	newTokens := float64(elapsed) / float64(b.refillPer)
	if newTokens > 0 {
		b.tokens = min(b.tokens+newTokens, b.capacity)
		b.lastRefill = now
	}

	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true, 0
	}

	deficit := 1.0 - b.tokens
	wait := time.Duration(float64(b.refillPer) * deficit)
	return false, wait
}

type BucketRegistry struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
}

func NewBucketRegistry() *BucketRegistry {
	return &BucketRegistry{
		buckets: make(map[string]*tokenBucket),
	}
}

func (reg *BucketRegistry) get(ctx RateLimitCtx, endpoint string) *tokenBucket {

	reg.mu.Lock()
	if b, ok := reg.buckets[endpoint]; ok {
		reg.mu.Unlock()
		return b
	}
	reg.mu.Unlock()

	rate := ctx.RateLimitThreshold(endpoint)
	if rate <= 0 {
		rate = 100
	}

	reg.mu.Lock()
	defer reg.mu.Unlock()

	if b, ok := reg.buckets[endpoint]; ok {
		return b
	}
	b := newTokenBucket(rate)
	reg.buckets[endpoint] = b
	return b
}

func (reg *BucketRegistry) InvalidateAll() {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.buckets = make(map[string]*tokenBucket)
}

func (reg *BucketRegistry) InvalidateEndpoint(endpoint string) bool {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if _, ok := reg.buckets[endpoint]; !ok {
		return false
	}
	delete(reg.buckets, endpoint)
	return true
}

func (reg *BucketRegistry) Len() int {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	return len(reg.buckets)
}

func RateLimitMiddleware(ctx RateLimitCtx, registry *BucketRegistry, endpoint string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bucket := registry.get(ctx, endpoint)
		ok, retryAfter := bucket.tryConsume()
		if !ok {
			retryMs := retryAfter.Milliseconds()
			if retryMs < 1 {
				retryMs = 1
			}
			w.Header().Set("Retry-After", fmt.Sprintf("%d", retryMs))
			w.Header().Set("X-HADES-Rate-Limit-Endpoint", endpoint)
			writeJSON(w, http.StatusTooManyRequests, map[string]string{
				"error":       "rate limit exceeded",
				"endpoint":    endpoint,
				"retry_after": fmt.Sprintf("%dms", retryMs),
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}
