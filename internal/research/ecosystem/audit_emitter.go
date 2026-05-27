// SPDX-License-Identifier: MIT
// internal/research/ecosystem/audit_emitter.go
//
// RAGAuditEmitter + RAGAuditChainEmitter interface (
// Task A-7; master §3.6).
//
// Wraps internal/audit/chain/ primitives indirectly via a
// narrow RAGAuditChainEmitter interface ( B-6
// pattern). does NOT import internal/daemon/budget; declaring
// the interface HERE lets honour invariant boundary while
// still leveraging the same chain hashing/sealing primitives.
//
// Production implementation lands at Task D-12 — a thin
// wrapper that constructs the chain row + delegates parent_hash /
// self_hash computation to chain primitives. ships the
// interface declaration + RAGAuditEmitter struct + InMemoryRAGAuditChain
// test impl (mock_chain.go) that exercises the SAME chain-link hashing
// logic locally so wiring is verifiable in isolation.
//
// D-13: added SetProfile rebind hook +
// atomic.Pointer-backed profile storage so the Dispatcher can swap the
// constructor-time `default` profile for the per-query resolved
// doctrine at step 2 of Query() without racing concurrent Emit calls.

package ecosystem

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type RAGAuditChainEmitter interface {
	// Append assigns a new monotonically-increasing seq, computes the
	// chain-link hashes (parent_hash + self_hash per spec §4.6), and
	// persists the record. Returns the assigned seq.
	//
	// Concurrency implementations MUST serialize seq assignment + hash
	// computation atomically (typically mutex-protected;
	// production wraps chain.Append which uses SQLite txn).
	Append(ctx context.Context, evt eventlog.EventType, payload []byte, doctrine string) (seq int64, err error)

	LastHash(ctx context.Context) (parentHash string, err error)

	SealPartition(ctx context.Context, partitionID string) error
}

type RAGAuditEmitter struct {
	chain           RAGAuditChainEmitter
	doctrineProfile atomic.Pointer[DoctrineProfile]
}

func NewRAGAuditEmitter(chain RAGAuditChainEmitter, profile *DoctrineProfile) *RAGAuditEmitter {
	if chain == nil {
		panic("research/ecosystem: NewRAGAuditEmitter: chain must be non-nil")
	}
	if profile == nil {
		panic("research/ecosystem: NewRAGAuditEmitter: profile must be non-nil")
	}
	e := &RAGAuditEmitter{chain: chain}
	e.doctrineProfile.Store(profile)
	return e
}

func (e *RAGAuditEmitter) SetProfile(profile *DoctrineProfile) {
	if profile == nil {
		panic("research/ecosystem: RAGAuditEmitter.SetProfile: profile must be non-nil")
	}
	e.doctrineProfile.Store(profile)
}

func (e *RAGAuditEmitter) activeProfile() *DoctrineProfile {
	return e.doctrineProfile.Load()
}

func (e *RAGAuditEmitter) Emit(ctx context.Context, evt eventlog.EventType, payload interface{}) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	profile := e.activeProfile()
	if !shouldEmitUnderProfile(profile, evt) {
		return 0, nil
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("research/ecosystem: audit emit marshal: %w", err)
	}
	return e.chain.Append(ctx, evt, payloadJSON, profile.Name)
}

func (e *RAGAuditEmitter) shouldEmit(evt eventlog.EventType) bool {
	return shouldEmitUnderProfile(e.activeProfile(), evt)
}

func shouldEmitUnderProfile(profile *DoctrineProfile, evt eventlog.EventType) bool {
	switch profile.AuditEmissionLevel {
	case AuditAll8Events, "":
		return true
	case AuditQueryAbstainVerifyFailureAnswer:
		switch evt {
		case eventlog.EvtRAGQuery, eventlog.EvtRAGAbstain,
			eventlog.EvtRAGVerify, eventlog.EvtRAGAnswer:
			return true
		}
		return false
	case AuditMinimal:
		switch evt {
		case eventlog.EvtRAGQuery, eventlog.EvtRAGAbstain:
			return true
		}
		return false
	default:

		return true
	}
}
