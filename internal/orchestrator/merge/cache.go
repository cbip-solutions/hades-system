// SPDX-License-Identifier: MIT
// internal/orchestrator/merge/cache.go
package merge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"
)

type PassingSet []string

func (p PassingSet) Hash() string {
	cp := make([]string, len(p))
	copy(cp, p)
	sort.Strings(cp)
	h := sha256.New()
	for _, id := range cp {
		_, _ = h.Write([]byte(id))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (p PassingSet) Has(testID string) bool {
	for _, id := range p {
		if id == testID {
			return true
		}
	}
	return false
}

func (p PassingSet) HasAnyMissing(other PassingSet) bool {
	otherSet := make(map[string]struct{}, len(other))
	for _, id := range other {
		otherSet[id] = struct{}{}
	}
	for _, id := range p {
		if _, ok := otherSet[id]; !ok {
			return true
		}
	}
	return false
}

func CacheKey(req MergeRequest) string {
	heads := make([]string, len(req.Candidates))
	for i, c := range req.Candidates {
		heads[i] = c.HeadSHA
	}
	sort.Strings(heads)

	h := sha256.New()
	_, _ = h.Write([]byte(req.TargetBranch))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(req.BaseSHA))
	_, _ = h.Write([]byte{0})
	for _, head := range heads {
		_, _ = h.Write([]byte(head))
		_, _ = h.Write([]byte{0})
	}
	_, _ = h.Write([]byte(req.EngineVersion))
	return hex.EncodeToString(h.Sum(nil))
}

type Cache struct {
	mu sync.RWMutex
	m  map[string]MergeOutcome
}

func NewCache() *Cache {
	return &Cache{m: make(map[string]MergeOutcome)}
}

func (c *Cache) Lookup(req MergeRequest) (MergeOutcome, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out, ok := c.m[CacheKey(req)]
	return out, ok
}

func (c *Cache) Store(req MergeRequest, outcome MergeOutcome) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[CacheKey(req)] = outcome
}

func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.m)
}

func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m = make(map[string]MergeOutcome)
}

type EventReader interface {
	Each(ctx context.Context, fn func(e Event) error) error
}

type MergeCompletedPayload struct {
	WinnerCandidateID string       `json:"winner_candidate_id"`
	IntegrationSHA    string       `json:"integration_sha"`
	RequestHash       string       `json:"request_hash"`
	Outcome           MergeOutcome `json:"outcome"`
}

type MergeCacheHitPayload struct {
	RequestHash       string `json:"request_hash"`
	OriginalOutcomeID string `json:"original_outcome_id"`
}

type MergeCacheRebuiltPayload struct {
	EventsProcessed int    `json:"events_processed"`
	DurationMs      int64  `json:"duration_ms"`
	CacheSize       int    `json:"cache_size"`
	RebuildError    string `json:"rebuild_error,omitempty"`
}

func (c *Cache) Rebuild(ctx context.Context, r EventReader, em EventEmitter) error {
	start := time.Now()
	processed := 0

	err := r.Each(ctx, func(e Event) error {
		processed++
		if e.Type != EvtMergeCompleted {
			return nil
		}
		var p MergeCompletedPayload
		if uerr := json.Unmarshal(e.Payload, &p); uerr != nil {
			return fmt.Errorf("merge.Cache.Rebuild: decode EvtMergeCompleted at gen=%d: %w", e.GenerationID, uerr)
		}
		key := p.RequestHash
		if key == "" {
			key = e.RequestHash
		}
		c.mu.Lock()
		c.m[key] = p.Outcome
		c.mu.Unlock()
		return nil
	})

	durationMs := time.Since(start).Milliseconds()
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	payload, _ := json.Marshal(MergeCacheRebuiltPayload{
		EventsProcessed: processed,
		DurationMs:      durationMs,
		CacheSize:       c.Size(),
		RebuildError:    errMsg,
	})
	_ = em.Append(ctx, Event{
		Type:      EvtMergeCacheRebuilt,
		Payload:   payload,
		Timestamp: time.Now(),
	})
	return err
}
