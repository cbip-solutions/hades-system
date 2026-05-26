//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package researchmcpmock

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/research/cache"
)

var _ cache.MCPClient = (*MockResearchMCP)(nil)

type MockResearchMCP struct {
	mu         sync.Mutex
	registered map[string]cache.FreshFindings
	errors     map[string]error
	latency    time.Duration
	calls      []string
}

func New() *MockResearchMCP {
	return &MockResearchMCP{
		registered: make(map[string]cache.FreshFindings),
		errors:     make(map[string]error),
	}
}

func (m *MockResearchMCP) Register(query string, findings cache.FreshFindings) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registered[query] = findings
}

func (m *MockResearchMCP) SetError(query string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors[query] = err
}

func (m *MockResearchMCP) SetLatency(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latency = d
}

func (m *MockResearchMCP) Calls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.calls))
	copy(out, m.calls)
	return out
}

func (m *MockResearchMCP) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registered = make(map[string]cache.FreshFindings)
	m.errors = make(map[string]error)
	m.calls = nil
	m.latency = 0
}

func (m *MockResearchMCP) Dispatch(ctx context.Context, query string) (cache.FreshFindings, error) {
	m.mu.Lock()
	m.calls = append(m.calls, query)
	registered, hasReg := m.registered[query]
	wantErr, hasErr := m.errors[query]
	latency := m.latency
	m.mu.Unlock()

	if latency > 0 {

		t := time.NewTimer(latency)
		defer t.Stop()
		select {
		case <-t.C:

		case <-ctx.Done():
			return cache.FreshFindings{}, ctx.Err()
		}
	}

	if hasErr {
		return cache.FreshFindings{}, wantErr
	}
	if hasReg {
		return registered, nil
	}

	return defaultFreshFindings(query), nil
}

func defaultFreshFindings(query string) cache.FreshFindings {
	const numFindings = 3
	baseTime := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	prefix := hashKey(query)

	out := make([]cache.FreshFinding, 0, numFindings)
	for i := 0; i < numFindings; i++ {
		url := fmt.Sprintf("https://mock.example/%s/%d", prefix, i)
		body := []byte(fmt.Sprintf("mock body for %q (idx %d)", query, i))
		out = append(out, cache.FreshFinding{
			SourceURL:          url,
			SourceURLCanonical: url,
			Ext:                ".html",
			Body:               body,
			RetrievedAt:        baseTime.Add(time.Duration(i) * time.Minute),
		})
	}
	return cache.FreshFindings{Query: query, Findings: out}
}

func hashKey(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}
