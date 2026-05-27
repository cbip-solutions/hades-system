// SPDX-License-Identifier: MIT
// internal/research/ecosystem/mock_chain.go
//
// InMemoryRAGAuditChain — production-visible test utility (
// Task A-7).
//
// The production wrapper (D-12) targets chain primitives
// via SQLite. Phases B/C/E/F/G/H/I need to drive the RAGAuditEmitter in
// isolation (unit + integration tests) WITHOUT a SQLite chain
// instance — that is what InMemoryRAGAuditChain provides.
//
// Implements the SAME chain-link hash semantics as the
// production wrapper: sha256(seq || evt_int || payload || parent_hash)
// per spec §4.6 canonical chain-link formula.
//
// NOT a stub per project doctrine `feedback_no_stubs_complete_code.md`
// — fully implemented chain hashing with race-safe Append/Get/LastHash/
// SealPartition + introspection helpers (Len/Get/AllSeqs/Seals).

package ecosystem

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type InMemoryRAGAuditChainRecord struct {
	Seq         int64
	EventType   eventlog.EventType
	Payload     []byte
	ParentHash  string
	SelfHash    string
	Doctrine    string
	EmittedAt   time.Time
	PartitionID string
}

type InMemoryRAGAuditChain struct {
	mu      sync.Mutex
	records []InMemoryRAGAuditChainRecord
	tip     string
	seals   map[string]time.Time
}

func NewInMemoryRAGAuditChain() *InMemoryRAGAuditChain {
	return &InMemoryRAGAuditChain{
		records: make([]InMemoryRAGAuditChainRecord, 0, 16),
		seals:   make(map[string]time.Time),
	}
}

func (c *InMemoryRAGAuditChain) Append(ctx context.Context, evt eventlog.EventType, payload []byte, doctrine string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	seq := int64(len(c.records) + 1)
	parent := c.tip
	selfHash := chainHashFormula(seq, evt, payload, parent)
	now := time.Now().UTC()
	partID := now.Format("2006-01")

	rec := InMemoryRAGAuditChainRecord{
		Seq:         seq,
		EventType:   evt,
		Payload:     append([]byte(nil), payload...),
		ParentHash:  parent,
		SelfHash:    selfHash,
		Doctrine:    doctrine,
		EmittedAt:   now,
		PartitionID: partID,
	}
	c.records = append(c.records, rec)
	c.tip = selfHash
	return seq, nil
}

func (c *InMemoryRAGAuditChain) LastHash(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tip, nil
}

func (c *InMemoryRAGAuditChain) SealPartition(ctx context.Context, partitionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seals[partitionID] = time.Now().UTC()
	return nil
}

func (c *InMemoryRAGAuditChain) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.records)
}

func (c *InMemoryRAGAuditChain) Get(seq int64) *InMemoryRAGAuditChainRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	if seq < 1 || seq > int64(len(c.records)) {
		return nil
	}
	rec := c.records[seq-1]
	return &rec
}

func (c *InMemoryRAGAuditChain) AllSeqs() []int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]int64, len(c.records))
	for i, r := range c.records {
		out[i] = r.Seq
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (c *InMemoryRAGAuditChain) Seals() map[string]time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make(map[string]time.Time, len(c.seals))
	for k, v := range c.seals {
		cp[k] = v
	}
	return cp
}

// chainHashFormula computes sha256(seq || evt || payload || parent)
// per spec §4.6 canonical chain-link.
//
// Encoding "seq|evt_int|" prefix + raw payload bytes + "|parent" suffix
// before sha256. Stable across implementations; production
// wrapper MUST use the same encoding to maintain chain compatibility.
func chainHashFormula(seq int64, evt eventlog.EventType, payload []byte, parent string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%d|%d|", seq, int(evt))
	h.Write(payload)
	fmt.Fprintf(h, "|%s", parent)
	return hex.EncodeToString(h.Sum(nil))
}
