// SPDX-License-Identifier: MIT
// internal/daemon/orchestrator/tier_health.go
//
// TierHealth tracks recent outcomes per tier in a rolling time-window.
// Used by CircuitBreaker (Phase D-2) to decide state transitions:
// closed → suspect → open.
//
// Boundary (inv-zen-031): this file imports only stdlib (sync + time).
// The orchestrator package MUST NOT import internal/store.
//
// Design notes:
//   - Outcomes are stored in insertion order (chronological, assuming
//     monotonic clock-forward calls). evictExpiredLocked scans from the
//     front — a linear scan O(n) on the expired prefix — then re-slices.
//     n is bounded by the request rate × window duration; for typical
//     circuit-breaker windows (1–5 min) and typical LLM traffic rates
//     this is at most a few hundred entries and the linear cost is negligible.
//   - consecF (ConsecutiveFailures) is an independent counter: it counts
//     consecutive failures since the last success. Crucially, it is NOT
//     reset when outcomes evict from the window. Rationale: a tier that
//     had N consecutive failures and then went quiet for longer than the
//     window is still in a failure streak from the breaker's perspective.
//     The breaker may decide to probe it anyway; that probe's result will
//     reset or advance consecF. Phase D-2 documents this choice.

package orchestrator

import (
	"sync"
	"time"
)

type outcome struct {
	t       time.Time
	success bool
}

type TierHealth struct {
	window   time.Duration
	mu       sync.Mutex
	outcomes []outcome
	consecF  int
}

func NewTierHealth(window time.Duration) *TierHealth {
	if window <= 0 {
		window = 5 * time.Minute
	}
	return &TierHealth{window: window}
}

func (t *TierHealth) RecordSuccess() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.outcomes = append(t.outcomes, outcome{t: time.Now(), success: true})
	t.consecF = 0
	t.evictExpiredLocked()
}

func (t *TierHealth) RecordFailure() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.outcomes = append(t.outcomes, outcome{t: time.Now(), success: false})
	t.consecF++
	t.evictExpiredLocked()
}

func (t *TierHealth) ErrorRate() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.evictExpiredLocked()
	if len(t.outcomes) == 0 {
		return 0
	}
	var failures int
	for _, o := range t.outcomes {
		if !o.success {
			failures++
		}
	}
	return float64(failures) / float64(len(t.outcomes))
}

func (t *TierHealth) ConsecutiveFailures() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.consecF
}

// evictExpiredLocked removes outcomes that fall outside the rolling window.
// Caller MUST hold t.mu.
//
// Boundary semantics: threshold = now - window. Outcomes with ts strictly
// after the threshold survive; outcomes with ts <= threshold are pruned.
// This matches WindowCounter's strict edge-prune convention (F-4):
// "ts exactly at the boundary" is considered expired and dropped.
//
// Implementation scan from the front (oldest first) until we find the first
// outcome inside the window, then re-slice from that index. Outcomes are
// appended in call order (monotonic insertion), so all expired outcomes are
// at the front of the slice.
func (t *TierHealth) evictExpiredLocked() {
	threshold := time.Now().Add(-t.window)
	i := 0
	for ; i < len(t.outcomes); i++ {
		if t.outcomes[i].t.After(threshold) {
			break
		}
	}
	t.outcomes = t.outcomes[i:]
}
