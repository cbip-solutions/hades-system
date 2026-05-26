// SPDX-License-Identifier: MIT
// Test infrastructure for Plan 11 Phase B integration tests. Provides a
// recording fake dispatcher and a fake AuditAnchor; the production
// MessagesHandler wraps these so tests can assert the dispatcher saw the
// expected TierRequest shape after the Python ZenSwarmTransport posts.
//
// Used by:
//   - tests/integration/zenswarm_transport_test.go (Phase B-7)
//   - tests/compliance/inv_zen_164_*_test.go (Phase B-8)
//   - Phase F tests
package testharness

import (
	"context"
	"sync"

	"github.com/cbip-solutions/hades-system/internal/providers"
)

type ZenSwarmRecordingDispatcher struct {
	mu    sync.Mutex
	Calls []providers.TierRequest
	Resp  *providers.TierResponse
	Err   error
}

func (r *ZenSwarmRecordingDispatcher) Forward(_ context.Context, req providers.TierRequest) (*providers.TierResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Calls = append(r.Calls, req)
	return r.Resp, r.Err
}

func (r *ZenSwarmRecordingDispatcher) CallCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.Calls)
}

func (r *ZenSwarmRecordingDispatcher) LastCall() providers.TierRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.Calls) == 0 {
		return providers.TierRequest{}
	}
	return r.Calls[len(r.Calls)-1]
}

type ZenSwarmRecordingAnchor struct {
	mu     sync.Mutex
	Events []ZenSwarmAnchorEvent
	ID     string
	Err    error
}

type ZenSwarmAnchorEvent struct {
	Type    string
	Payload map[string]any
}

func (r *ZenSwarmRecordingAnchor) Emit(_ context.Context, eventType string, payload map[string]any) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Events = append(r.Events, ZenSwarmAnchorEvent{Type: eventType, Payload: payload})
	if r.Err != nil {
		return "", r.Err
	}
	return r.ID, nil
}

func (r *ZenSwarmRecordingAnchor) EventCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.Events)
}
