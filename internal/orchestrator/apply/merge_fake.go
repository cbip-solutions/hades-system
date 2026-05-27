// go:build test

// SPDX-License-Identifier: MIT

package apply

import (
	"context"
	"sync"
)

type MergeEngineFake struct {
	mu    sync.Mutex
	calls []MergeRequest
}

func NewMergeEngineFake() *MergeEngineFake {
	mustBeTestRun()
	return &MergeEngineFake{}
}

func (f *MergeEngineFake) Merge(_ context.Context, req MergeRequest) (MergeOutcome, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	f.mu.Unlock()
	return MergeOutcome{}, ErrMergeNotImplemented
}

func (f *MergeEngineFake) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *MergeEngineFake) Calls() []MergeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]MergeRequest, len(f.calls))
	copy(out, f.calls)
	return out
}

var _ MergeEngine = (*MergeEngineFake)(nil)
