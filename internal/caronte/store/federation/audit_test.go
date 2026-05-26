package federation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
)

type fakeTesseraAdapter struct {
	mu       sync.Mutex
	calls    int
	lastLeaf fakeAuditLeaf
	failWith error
}

type fakeAuditLeaf struct {
	EventID, EventType, ProjectID string
	PayloadHash, RecordHash       []byte
}

type dummyAdapter struct{}

func TestEmitAuditRejectsUnknownEventType(t *testing.T) {
	_, err := EmitAudit(context.Background(), nil, Event{Type: "BOGUS", WorkspaceID: "ws-1", OccurredAt: 1})
	if !errors.Is(err, ErrUnknownEventType) {
		t.Errorf("EmitAudit(BOGUS) err = %v; want ErrUnknownEventType", err)
	}
}

func TestEmitAuditRejectsNilAdapter(t *testing.T) {
	id, err := EmitAudit(context.Background(), nil, Event{
		Type: EvtCrossRepoLink, WorkspaceID: "ws-1", OccurredAt: 1,
	})
	if err != nil {
		t.Errorf("EmitAudit(nil) err = %v; want nil (graceful degradation)", err)
	}
	if id != "" {
		t.Errorf("EmitAudit(nil) returned non-empty LeafID %q; want \"\"", id)
	}
}

func TestEmitAuditPayloadHashDeterministic(t *testing.T) {
	e := Event{
		Type:        EvtBreakingChange,
		WorkspaceID: "ws-1",
		Payload:     []byte(`{"change_id":"bc-1"}`),
		OccurredAt:  1_700_000_000_000,
	}
	h1, err := computeAuditPayloadHash(e)
	if err != nil {
		t.Fatalf("computeAuditPayloadHash 1: %v", err)
	}
	h2, err := computeAuditPayloadHash(e)
	if err != nil {
		t.Fatalf("computeAuditPayloadHash 2: %v", err)
	}
	if !bytesEqual(h1, h2) {
		t.Errorf("payload hash not deterministic: %s vs %s", hex.EncodeToString(h1), hex.EncodeToString(h2))
	}
	if len(h1) != sha256.Size {
		t.Errorf("payload hash len = %d; want %d (sha256)", len(h1), sha256.Size)
	}
}

func TestEmitAuditRecordHashDeterministic(t *testing.T) {
	e := Event{
		Type:        EvtBreakingChange,
		WorkspaceID: "ws-1",
		Payload:     []byte(`{"change_id":"bc-1"}`),
		OccurredAt:  1_700_000_000_000,
	}
	ph, err := computeAuditPayloadHash(e)
	if err != nil {
		t.Fatalf("computeAuditPayloadHash: %v", err)
	}
	rh1 := computeAuditRecordHash(e, ph)
	rh2 := computeAuditRecordHash(e, ph)
	if !bytesEqual(rh1, rh2) {
		t.Errorf("record hash not deterministic")
	}
	if len(rh1) != sha256.Size {
		t.Errorf("record hash len = %d; want %d (sha256)", len(rh1), sha256.Size)
	}
	// Mutating any identity field MUST change the record hash.
	mutated := e
	mutated.WorkspaceID = "ws-2"
	rh3 := computeAuditRecordHash(mutated, ph)
	if bytesEqual(rh1, rh3) {
		t.Error("record hash unchanged when WorkspaceID mutated; identity not anchored")
	}
}

func TestEmitAuditViaFakeAppender(t *testing.T) {
	fake := &fakeTesseraAdapter{}
	prev := appendLeafFn
	appendLeafFn = func(_ context.Context, _ tesseraAdapterShim, leaf tesseraLeafShim) (tesseraLeafIDShim, error) {
		fake.mu.Lock()
		fake.calls++
		fake.lastLeaf = fakeAuditLeaf{
			EventID:     leaf.EventID,
			EventType:   leaf.EventType,
			ProjectID:   leaf.ProjectID,
			PayloadHash: append([]byte(nil), leaf.PayloadHash...),
			RecordHash:  append([]byte(nil), leaf.RecordHash...),
		}
		fake.mu.Unlock()
		return tesseraLeafIDShim("leaf-id-stub"), nil
	}
	t.Cleanup(func() { appendLeafFn = prev })

	dummy := newDummyTesseraPtr(t)
	id, err := EmitAudit(context.Background(), dummy, Event{
		Type:        EvtCrossRepoLink,
		WorkspaceID: "ws-1",
		Payload:     []byte(`{"call":"c-1"}`),
		OccurredAt:  1_700_000_000_000,
	})
	if err != nil {
		t.Fatalf("EmitAudit: %v", err)
	}
	if id == "" {
		t.Error("EmitAudit returned empty LeafID")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.calls != 1 {
		t.Errorf("appendLeafFn calls = %d; want 1", fake.calls)
	}
	if fake.lastLeaf.EventType != string(EvtCrossRepoLink) {
		t.Errorf("lastLeaf.EventType = %q; want %q", fake.lastLeaf.EventType, EvtCrossRepoLink)
	}
	if !strings.Contains(fake.lastLeaf.EventID, "ws-1") {
		t.Errorf("lastLeaf.EventID %q does not contain workspace id", fake.lastLeaf.EventID)
	}
	if len(fake.lastLeaf.PayloadHash) != sha256.Size {
		t.Errorf("lastLeaf.PayloadHash len = %d; want %d", len(fake.lastLeaf.PayloadHash), sha256.Size)
	}
	if len(fake.lastLeaf.RecordHash) != sha256.Size {
		t.Errorf("lastLeaf.RecordHash len = %d; want %d", len(fake.lastLeaf.RecordHash), sha256.Size)
	}
}

func TestEmitAuditWrapsAppenderError(t *testing.T) {
	prev := appendLeafFn
	appendLeafFn = func(_ context.Context, _ tesseraAdapterShim, _ tesseraLeafShim) (tesseraLeafIDShim, error) {
		return "", errors.New("appender exploded")
	}
	t.Cleanup(func() { appendLeafFn = prev })
	dummy := newDummyTesseraPtr(t)
	_, err := EmitAudit(context.Background(), dummy, Event{
		Type: EvtCoordinatedDispatch, WorkspaceID: "ws-1",
		Payload: []byte(`{}`), OccurredAt: 1,
	})
	if err == nil {
		t.Fatal("EmitAudit returned nil err on appender failure")
	}
	if !errors.Is(err, ErrCorruptAuditLeaf) {
		t.Errorf("EmitAudit err = %v; want ErrCorruptAuditLeaf", err)
	}
}

func TestNewAuditEmitterTransitsAppendLeaf(t *testing.T) {
	fake := &fakeTesseraAdapter{}
	prev := appendLeafFn
	appendLeafFn = func(_ context.Context, _ tesseraAdapterShim, leaf tesseraLeafShim) (tesseraLeafIDShim, error) {
		fake.mu.Lock()
		fake.calls++
		fake.lastLeaf = fakeAuditLeaf{
			EventID:     leaf.EventID,
			EventType:   leaf.EventType,
			ProjectID:   leaf.ProjectID,
			PayloadHash: append([]byte(nil), leaf.PayloadHash...),
			RecordHash:  append([]byte(nil), leaf.RecordHash...),
		}
		fake.mu.Unlock()
		return tesseraLeafIDShim("leaf-id-emitter"), nil
	}
	t.Cleanup(func() { appendLeafFn = prev })

	dummy := newDummyTesseraPtr(t)
	emitter := NewAuditEmitter(dummy, "ws-1")
	if err := emitter.Emit(context.Background(), EvtWorkspacePolicySet, []byte(`{"k":"v"}`)); err != nil {
		t.Fatalf("AuditEmitter.Emit: %v", err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.calls != 1 {
		t.Errorf("appendLeafFn calls = %d; want 1", fake.calls)
	}
	if fake.lastLeaf.EventType != string(EvtWorkspacePolicySet) {
		t.Errorf("lastLeaf.EventType = %q; want %q", fake.lastLeaf.EventType, EvtWorkspacePolicySet)
	}
	if !strings.Contains(fake.lastLeaf.EventID, "ws-1") {
		t.Errorf("lastLeaf.EventID %q does not contain workspace id", fake.lastLeaf.EventID)
	}
}

func TestNewAuditEmitterNilAdapterIsNoop(t *testing.T) {
	emitter := NewAuditEmitter(nil, "ws-1")
	if err := emitter.Emit(context.Background(), EvtCrossRepoLink, []byte("{}")); err != nil {
		t.Errorf("AuditEmitter.Emit(nil adapter) err = %v; want nil (no-op)", err)
	}
}

func newDummyTesseraPtr(t *testing.T) *tessera.Adapter {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "audit-root")
	a, err := tessera.NewProjectAdapter(context.Background(), "test-project", tmp, tessera.Config{
		BatchMaxAge:         50_000_000,
		BatchMaxSize:        1,
		RotationCadenceDays: 365,
	})
	if err != nil {
		t.Fatalf("tessera.NewProjectAdapter: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
