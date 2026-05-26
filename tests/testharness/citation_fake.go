// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT

package testharness

import (
	"context"
	"sync"

	"github.com/cbip-solutions/hades-system/internal/citation"
)

type FakeAuditEmitter struct {
	mu     sync.Mutex
	Events []citation.CitationRenderedEvent
	Err    error
}

func (f *FakeAuditEmitter) EmitCitationRendered(ctx context.Context, ev citation.CitationRenderedEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Events = append(f.Events, ev)
	return f.Err
}

func (f *FakeAuditEmitter) Snapshot() []citation.CitationRenderedEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]citation.CitationRenderedEvent, len(f.Events))
	copy(out, f.Events)
	return out
}

type FakeRenderer struct {
	PlatformName string
	mu           sync.Mutex
	Calls        []citation.SessionContext
	Output       string
	Err          error
}

func (f *FakeRenderer) Render(env *citation.Envelope, sess citation.SessionContext) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, sess)
	return f.Output, f.Err
}

func (f *FakeRenderer) Platform() string { return f.PlatformName }

func (f *FakeRenderer) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Calls)
}
