// SPDX-License-Identifier: MIT
// tessera.Adapter public surface.
//
// Why a separate sub-package?
//
// tests/testhelpers/store.go imports internal/store which uses
// ncruces/go-sqlite3 (pure-Go WASM). internal/audit/tessera does not pull
// a sqlite driver today, but the boundary discipline still applies —
// future-proofing per researchmcpmock/ pattern. Sub-package isolation
// prevents accidental driver double-registration and keeps test binaries
// linking only what each tier needs.
//
// Capabilities (mirror the real Adapter — verified at HEAD against
// internal/audit/tessera/adapter.go):
//
// - AppendLeaf, AppendSeal, VerifyMerkleInclusion, SubscribeSTH,
// Attach, WitnessCoSignSeal, VerifySealSignature, Close — same
// signatures + same error sentinels.
// - FlushAndPublishSTH — explicit STH publish (faster than real
// Tessera's batch-cadence-driven publication).
// - SetCorruption — fault injection to simulate disk-level corruption
// on a stored leaf; later VerifyMerkleInclusion returns (false, nil)
// to surface as a verify-failure rather than a missing-leaf error.
// - SetClock — replace the wall clock with a fixed function so STH
// timestamps are deterministic in tests.
//
// In-memory storage:
//
// - leaves []tessera.Leaf — append-only slice.
// - leafIDs map[LeafID]int — LeafID to index, deterministic format
// "mock-<projectID>-<index>" where index = len(leaves) BEFORE append.
// - sealCache map[partitionID]LeafID — backs AppendSeal idempotence on
// (projectID, partitionID); cross-project rejected before this map
// is consulted so the key is unambiguous.
// - corrupted map[LeafID]bool — simulated disk-level corruption marks.
//
// Merkle root + inclusion: encodeLeaf serializes a Leaf to canonical
// bytes (NUL-separated fields, parity-shape to production's
// zen-swarm-leaf-v1 format but simplified for mock semantics). RFC 6962
// hash domains apply: leaf hash = sha256(0x00 || encodeLeaf), internal
// node hash = sha256(0x01 || left || right). VerifyMerkleInclusion
// recomputes the audit path and asserts the leaf hash combines to the
// stored root — same semantic as production verifyInclusion.
//
// Concurrency: all public methods are safe for concurrent use. The fast
// path checks closed.Load() (atomic.Bool) before acquiring the mutex,
// mirroring the real Adapter's hot-path pattern.
//
// Boundary: this file lives in tests/testhelpers/tesseramock/ —
// testhelpers may import internal/* freely (the invariant boundary
// forbids the OPPOSITE direction; assertion enforced at K-17).
package tesseramock

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
)

type sthSubscriber interface {
	OnSTH(ctx context.Context, sth tessera.STH) error
}

type SubscriberFunc func(ctx context.Context, sth tessera.STH) error

func (f SubscriberFunc) OnSTH(ctx context.Context, sth tessera.STH) error {
	return f(ctx, sth)
}

type MockTesseraAdapter struct {
	projectID string
	dir       string

	closed atomic.Bool
	mu     sync.Mutex

	leaves    []tessera.Leaf
	leafIDs   map[tessera.LeafID]int
	corrupted map[tessera.LeafID]bool
	sealCache map[string]tessera.LeafID

	subscribers []sthSubscriber
	witness     *MockWitness
	clock       func() time.Time
}

func New(projectID string) *MockTesseraAdapter {
	if projectID == "" {
		panic("tesseramock: projectID must be non-empty")
	}
	return &MockTesseraAdapter{
		projectID: projectID,
		dir:       "mem:///" + projectID,
		leafIDs:   make(map[tessera.LeafID]int),
		corrupted: make(map[tessera.LeafID]bool),
		sealCache: make(map[string]tessera.LeafID),
		clock:     time.Now,
	}
}

func (m *MockTesseraAdapter) ProjectID() string { return m.projectID }

func (m *MockTesseraAdapter) Dir() string { return m.dir }

func (m *MockTesseraAdapter) AppendLeaf(_ context.Context, leaf tessera.Leaf) (tessera.LeafID, error) {
	if m.closed.Load() {
		return "", tessera.ErrAdapterClosed
	}
	if leaf.ProjectID != "" && leaf.ProjectID != m.projectID {
		return "", fmt.Errorf("%w: adapter=%s leaf=%s", tessera.ErrCrossProjectAccess, m.projectID, leaf.ProjectID)
	}
	if len(leaf.PayloadHash) != sha256.Size {
		return "", fmt.Errorf("tesseramock: PayloadHash must be %d bytes, got %d", sha256.Size, len(leaf.PayloadHash))
	}
	if len(leaf.RecordHash) != sha256.Size {
		return "", fmt.Errorf("tesseramock: RecordHash must be %d bytes, got %d", sha256.Size, len(leaf.RecordHash))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed.Load() {
		return "", tessera.ErrAdapterClosed
	}
	leaf.ProjectID = m.projectID
	id := tessera.LeafID(fmt.Sprintf("mock-%s-%d", m.projectID, len(m.leaves)))
	m.leafIDs[id] = len(m.leaves)
	m.leaves = append(m.leaves, leaf)
	return id, nil
}

func (m *MockTesseraAdapter) AppendSeal(_ context.Context, projectID, partitionID string, payload []byte) (tessera.LeafID, error) {
	if m.closed.Load() {
		return "", tessera.ErrAdapterClosed
	}
	if projectID != m.projectID {
		return "", fmt.Errorf("%w: adapter=%s seal=%s", tessera.ErrCrossProjectAccess, m.projectID, projectID)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed.Load() {
		return "", tessera.ErrAdapterClosed
	}
	if cached, ok := m.sealCache[partitionID]; ok {
		return cached, nil
	}
	digest := sha256.Sum256(payload)
	seal := tessera.Leaf{
		EventID:     "seal:" + partitionID,
		EventType:   "audit.partition_sealed",
		PayloadHash: digest[:],
		RecordHash:  digest[:],
		ProjectID:   m.projectID,
	}
	id := tessera.LeafID(fmt.Sprintf("mock-%s-%d", m.projectID, len(m.leaves)))
	m.leafIDs[id] = len(m.leaves)
	m.leaves = append(m.leaves, seal)
	m.sealCache[partitionID] = id
	return id, nil
}

func (m *MockTesseraAdapter) VerifyMerkleInclusion(_ context.Context, leafID tessera.LeafID) (bool, error) {
	if m.closed.Load() {
		return false, tessera.ErrAdapterClosed
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed.Load() {
		return false, tessera.ErrAdapterClosed
	}
	idx, ok := m.leafIDs[leafID]
	if !ok {
		return false, fmt.Errorf("%w: %s", tessera.ErrLeafNotFound, leafID)
	}
	if m.corrupted[leafID] {
		return false, nil
	}
	root := merkleRoot(m.leaves)
	proof := inclusionProof(m.leaves, idx)
	leafHash := hashLeaf(m.leaves[idx])
	recomputed := recomputeRoot(leafHash, idx, len(m.leaves), proof)
	if !bytes.Equal(recomputed, root) {
		return false, nil
	}
	return true, nil
}

func (m *MockTesseraAdapter) SubscribeSTH(sub sthSubscriber) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed.Load() {
		return tessera.ErrAdapterClosed
	}
	m.subscribers = append(m.subscribers, sub)
	return nil
}

func (m *MockTesseraAdapter) FlushAndPublishSTH(ctx context.Context) error {
	if m.closed.Load() {
		return tessera.ErrAdapterClosed
	}
	m.mu.Lock()
	if m.closed.Load() {
		m.mu.Unlock()
		return tessera.ErrAdapterClosed
	}
	sth := tessera.STH{
		ProjectID: m.projectID,
		Size:      uint64(len(m.leaves)),
		RootHash:  merkleRoot(m.leaves),
		Timestamp: m.clock(),
	}
	subs := make([]sthSubscriber, len(m.subscribers))
	copy(subs, m.subscribers)
	m.mu.Unlock()

	var firstErr error
	for _, s := range subs {
		if err := s.OnSTH(ctx, sth); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m *MockTesseraAdapter) Attach(w *MockWitness) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.witness = w
}

func (m *MockTesseraAdapter) WitnessCoSignSeal(_ context.Context, leafID tessera.LeafID, payload []byte) ([]byte, error) {
	_ = leafID
	if m.closed.Load() {
		return nil, tessera.ErrAdapterClosed
	}
	m.mu.Lock()
	w := m.witness
	m.mu.Unlock()
	if w == nil {
		return nil, tessera.ErrWitnessKeyMissing
	}
	digest := sha256.Sum256(payload)
	sig, err := w.Sign(digest[:])
	if err != nil {
		return nil, fmt.Errorf("tesseramock: witness sign seal: %w", err)
	}
	return sig, nil
}

func (m *MockTesseraAdapter) VerifySealSignature(_ context.Context, payload, sig []byte) (bool, error) {
	if m.closed.Load() {
		return false, tessera.ErrAdapterClosed
	}
	m.mu.Lock()
	w := m.witness
	m.mu.Unlock()
	if w == nil {
		return false, tessera.ErrWitnessKeyMissing
	}
	pub, err := w.Load()
	if err != nil {
		return false, fmt.Errorf("tesseramock: verify seal sig load pubkey: %w", err)
	}
	digest := sha256.Sum256(payload)
	return VerifyWithPubkey(pub, digest[:], sig), nil
}

func (m *MockTesseraAdapter) Close() error {
	if !m.closed.CompareAndSwap(false, true) {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscribers = nil
	m.witness = nil
	return nil
}

func (m *MockTesseraAdapter) SetCorruption(leafID tessera.LeafID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.corrupted[leafID] = true
}

func (m *MockTesseraAdapter) SetClock(fn func() time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if fn == nil {
		m.clock = time.Now
		return
	}
	m.clock = fn
}

func encodeLeaf(leaf tessera.Leaf) []byte {

	out := make([]byte, 0, len(leaf.EventID)+len(leaf.EventType)+len(leaf.PayloadHash)+len(leaf.RecordHash)+len(leaf.ProjectID)+5)
	out = append(out, leaf.EventID...)
	out = append(out, 0)
	out = append(out, leaf.EventType...)
	out = append(out, 0)
	out = append(out, leaf.PayloadHash...)
	out = append(out, 0)
	out = append(out, leaf.RecordHash...)
	out = append(out, 0)
	out = append(out, leaf.ProjectID...)
	return out
}

func hashLeaf(leaf tessera.Leaf) []byte {
	h := sha256.New()
	h.Write([]byte{0x00})
	h.Write(encodeLeaf(leaf))
	return h.Sum(nil)
}

func hashNode(left, right []byte) []byte {
	h := sha256.New()
	h.Write([]byte{0x01})
	h.Write(left)
	h.Write(right)
	return h.Sum(nil)
}

func merkleRoot(leaves []tessera.Leaf) []byte {
	if len(leaves) == 0 {
		zero := make([]byte, sha256.Size)
		return zero
	}
	level := make([][]byte, len(leaves))
	for i, l := range leaves {
		level[i] = hashLeaf(l)
	}
	for len(level) > 1 {
		next := make([][]byte, 0, (len(level)+1)/2)
		for i := 0; i < len(level); i += 2 {
			if i+1 == len(level) {

				next = append(next, level[i])
				continue
			}
			next = append(next, hashNode(level[i], level[i+1]))
		}
		level = next
	}
	return level[0]
}

func inclusionProof(leaves []tessera.Leaf, idx int) [][]byte {
	if len(leaves) == 0 || idx < 0 || idx >= len(leaves) {
		return nil
	}
	level := make([][]byte, len(leaves))
	for i, l := range leaves {
		level[i] = hashLeaf(l)
	}
	proof := [][]byte(nil)
	pos := idx
	for len(level) > 1 {
		if pos%2 == 1 {
			proof = append(proof, level[pos-1])
		} else if pos+1 < len(level) {
			proof = append(proof, level[pos+1])
		}

		next := make([][]byte, 0, (len(level)+1)/2)
		for i := 0; i < len(level); i += 2 {
			if i+1 == len(level) {
				next = append(next, level[i])
				continue
			}
			next = append(next, hashNode(level[i], level[i+1]))
		}
		level = next
		pos /= 2
	}
	return proof
}

func recomputeRoot(leafHash []byte, idx, treeSize int, proof [][]byte) []byte {
	if treeSize == 0 {
		return nil
	}
	hash := leafHash
	levelSize := treeSize
	pos := idx
	pi := 0
	for levelSize > 1 {

		isLast := pos == levelSize-1 && pos%2 == 0
		if !isLast {
			if pi >= len(proof) {
				return nil
			}
			sibling := proof[pi]
			pi++
			if pos%2 == 0 {
				hash = hashNode(hash, sibling)
			} else {
				hash = hashNode(sibling, hash)
			}
		}
		pos /= 2
		levelSize = (levelSize + 1) / 2
	}
	return hash
}
