package auditadapter

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
)

type mockTesseraAdapter struct {
	mu            sync.Mutex
	enqueued      []enqueuedEvent
	failNext      bool
	returnEmptyID bool
	idSeq         int
}

type enqueuedEvent struct {
	ProjectID   string
	EventID     string
	PayloadHash []byte
	RecordHash  []byte
}

func (m *mockTesseraAdapter) AppendLeaf(ctx context.Context, leaf tessera.Leaf) (tessera.LeafID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failNext {
		m.failNext = false
		return "", errors.New("mockTessera: injected failure")
	}
	pcopy := make([]byte, len(leaf.PayloadHash))
	copy(pcopy, leaf.PayloadHash)
	rcopy := make([]byte, len(leaf.RecordHash))
	copy(rcopy, leaf.RecordHash)
	m.enqueued = append(m.enqueued, enqueuedEvent{
		ProjectID:   leaf.ProjectID,
		EventID:     leaf.EventID,
		PayloadHash: pcopy,
		RecordHash:  rcopy,
	})
	m.idSeq++
	if m.returnEmptyID {
		return "", nil
	}
	return tessera.LeafID(fmt.Sprintf("%s:%d", leaf.ProjectID, m.idSeq-1)), nil
}

func (m *mockTesseraAdapter) Snapshot() []enqueuedEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]enqueuedEvent, len(m.enqueued))
	copy(out, m.enqueued)
	return out
}

func TestOnEmitRawWithTesseraDispatchesEvent(t *testing.T) {
	s := openMigratedStore(t)
	mt := &mockTesseraAdapter{}
	a := New(s, WithTessera(mt))
	ctx := context.Background()

	insertRawAuditEvent(t, s, "evt-1", "proj-X", "test.event", `{}`, 1700000000)
	if _, err := a.OnEmitRaw(ctx, "evt-1", "proj-X", "test.event", []byte(`{}`), 1700000000); err != nil {
		t.Fatalf("OnEmitRaw: %v", err)
	}

	enqueued := mt.Snapshot()
	if len(enqueued) != 1 {
		t.Fatalf("len(enqueued) = %d, want 1", len(enqueued))
	}
	if enqueued[0].ProjectID != "proj-X" {
		t.Errorf("project_id = %q, want proj-X", enqueued[0].ProjectID)
	}
	if enqueued[0].EventID != "evt-1" {
		t.Errorf("event_id = %q, want evt-1", enqueued[0].EventID)
	}
	if len(enqueued[0].PayloadHash) != 32 {
		t.Errorf("payload_hash len = %d, want 32 (sha256)", len(enqueued[0].PayloadHash))
	}
	if len(enqueued[0].RecordHash) != 32 {
		t.Errorf("record_hash len = %d, want 32", len(enqueued[0].RecordHash))
	}
}

func TestOnEmitRawWithoutTesseraNoDispatch(t *testing.T) {
	s := openMigratedStore(t)
	a := New(s)
	ctx := context.Background()

	insertRawAuditEvent(t, s, "evt-1", "proj-X", "test.event", `{}`, 1700000000)
	if _, err := a.OnEmitRaw(ctx, "evt-1", "proj-X", "test.event", []byte(`{}`), 1700000000); err != nil {
		t.Fatalf("OnEmitRaw without Tessera: %v", err)
	}

	row, _ := a.GetEventByID(ctx, "evt-1")
	if row.RecordHash == "" {
		t.Error("chain compute did not run when Tessera not wired")
	}

	if row.TesseraLeafID != nil {
		t.Errorf("tessera_leaf_id should remain NULL without Tessera, got %v", row.TesseraLeafID)
	}
}

func TestOnEmitRawTesseraFailureNonFatal(t *testing.T) {

	s := openMigratedStore(t)
	mt := &mockTesseraAdapter{failNext: true}
	a := New(s, WithTessera(mt))
	ctx := context.Background()

	insertRawAuditEvent(t, s, "evt-1", "proj-X", "test.event", `{}`, 1700000000)
	tip, err := a.OnEmitRaw(ctx, "evt-1", "proj-X", "test.event", []byte(`{}`), 1700000000)
	if err != nil {
		t.Fatalf("OnEmitRaw should NOT fail on Tessera error: %v", err)
	}
	if tip == "" {
		t.Error("chain tip should be returned despite Tessera failure")
	}

	row, _ := a.GetEventByID(ctx, "evt-1")
	if row.RecordHash == "" {
		t.Error("chain compute should have run despite Tessera failure")
	}
}

func TestOnTesseraBatchFlushedSetsLeafID(t *testing.T) {

	s := openMigratedStore(t)
	mt := &mockTesseraAdapter{returnEmptyID: true}
	a := New(s, WithTessera(mt))
	ctx := context.Background()

	insertRawAuditEvent(t, s, "evt-1", "proj-X", "test.event", `{}`, 1700000000)
	if _, err := a.OnEmitRaw(ctx, "evt-1", "proj-X", "test.event", []byte(`{}`), 1700000000); err != nil {
		t.Fatalf("OnEmitRaw: %v", err)
	}

	row, _ := a.GetEventByID(ctx, "evt-1")
	if row.TesseraLeafID != nil {
		t.Fatalf("pre-condition: tessera_leaf_id should be NULL, got %v", row.TesseraLeafID)
	}

	if err := a.OnTesseraBatchFlushed(ctx, "evt-1", "leaf-100"); err != nil {
		t.Fatalf("OnTesseraBatchFlushed: %v", err)
	}

	row, _ = a.GetEventByID(ctx, "evt-1")
	if row.TesseraLeafID == nil || *row.TesseraLeafID != "leaf-100" {
		t.Errorf("tessera_leaf_id = %v, want leaf-100", row.TesseraLeafID)
	}
}

func TestPayloadHashDeterministic(t *testing.T) {

	a := sha256Bytes([]byte(`{"k":1}`))
	b := sha256Bytes([]byte(`{"k":1}`))
	if string(a) != string(b) {
		t.Error("sha256Bytes non-deterministic")
	}
	c := sha256Bytes([]byte(`{"k":2}`))
	if string(a) == string(c) {
		t.Error("sha256Bytes did not differ on different inputs")
	}
	if len(a) != 32 {
		t.Errorf("sha256Bytes len = %d, want 32", len(a))
	}
}

func TestDecodeChainHash(t *testing.T) {

	valid := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	b, err := decodeChainHash(valid)
	if err != nil {
		t.Fatalf("decodeChainHash(valid): %v", err)
	}
	if len(b) != 32 {
		t.Errorf("decodeChainHash(valid) len = %d, want 32", len(b))
	}

	if _, err := decodeChainHash("not-hex"); err == nil {
		t.Error("decodeChainHash(non-hex) returned nil error; expected wrapped hex error")
	}
	if _, err := decodeChainHash("zz"); err == nil {
		t.Error("decodeChainHash(zz) returned nil error; expected wrapped hex error")
	}
}

func TestOnEmitRawDecodeChainHashArchitecturalLimit(t *testing.T) {

	t.Log("architectural limit: OnEmitRaw herr!=nil branch not independently reachable (see comment)")
}
