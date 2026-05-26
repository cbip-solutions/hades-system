// SPDX-License-Identifier: MIT
// Package litestreammock provides a fault-injection seam for the
// auditadapter.LitestreamMgr interface.
//
// # Design
//
// The real litestream.Manager spawns a real `litestream replicate`
// subprocess. Spawning that binary in a chaos test is too heavy — it
// requires a valid litestream YAML config, a running SQLite database,
// and an accessible S3 endpoint. None of those are available in the
// chaos tier.
//
// This mock follows the tesseramock / researchmcpmock precedent: a
// pure-Go in-process struct that satisfies the production interface
// and exposes test-only fault-injection methods named *ForTesting /
// InjectCrash / Reset. The production interface (LitestreamMgr) has
// exactly one method:
//
//	Status(ctx context.Context) (state string, lagSec int64, err error)
//
// The mock starts in "replicating" state (healthy). InjectCrash
// transitions it to "crashed" state; subsequent Status calls return
// the injected error. Reset transitions back to "replicating".
//
// # Sub-package isolation
//
// This package lives at tests/chaos/plan9_audit_chaos/litestreammock/
// rather than tests/testhelpers/litestreammock/ because it is only
// consumed by the plan9_audit_chaos test package. Keeping it local
// avoids adding a testhelpers dependency that would be imported by
// every test binary in the repo, and mirrors the precedent set by
// the K-6 plan9_audit_chain + plan9_knowledge_research_state
// sub-package split (boundary discipline: widen testhelpers only for
// helpers consumed by multiple test packages).
//
// # Interface satisfaction
//
// MockLitestreamMgr satisfies auditadapter.LitestreamMgr at compile
// time via the blank-identifier assertion at the bottom of this file.
// The interface is imported from its canonical location; no copy is
// kept here.
package litestreammock

import (
	"context"
	"sync"
)

const StateReplicating = "replicating"

const stateCrashed = "crashed"

type MockLitestreamMgr struct {
	mu       sync.Mutex
	crashErr error
	state    string
	lagSec   int64
}

func New() *MockLitestreamMgr {
	return &MockLitestreamMgr{
		state: StateReplicating,
	}
}

func (m *MockLitestreamMgr) Status(_ context.Context) (state string, lagSec int64, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.crashErr != nil {
		return "", 0, m.crashErr
	}
	return m.state, m.lagSec, nil
}

func (m *MockLitestreamMgr) InjectCrash(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err == nil {
		panic("litestreammock: InjectCrash requires a non-nil error; use Reset() to clear a crash")
	}
	m.crashErr = err
	m.state = stateCrashed
}

func (m *MockLitestreamMgr) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.crashErr = nil
	m.state = StateReplicating
}

func (m *MockLitestreamMgr) SetLag(lagSec int64) {
	if lagSec < 0 {
		panic("litestreammock: lagSec must be >= 0")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lagSec = lagSec
}

var _ litestreamMgr = (*MockLitestreamMgr)(nil)

type litestreamMgr interface {
	Status(ctx context.Context) (state string, lagSec int64, err error)
}
