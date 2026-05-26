package tessera

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"sync"
	"testing"
	"time"
)

type memCheckpointSink struct {
	mu    sync.Mutex
	items []SignedSTH
}

func (m *memCheckpointSink) Append(ctx context.Context, signed SignedSTH) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(signed.Signature) == 0 {
		return ErrUnsignedSTH
	}
	m.items = append(m.items, signed)
	return nil
}

func (m *memCheckpointSink) snapshot() []SignedSTH {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]SignedSTH, len(m.items))
	copy(out, m.items)
	return out
}

func TestCoSignerSignsSTH(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	pub, err := w.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	sink := &memCheckpointSink{}
	cs := NewCoSigner(w, sink)
	sth := STH{
		ProjectID: "p1",
		Size:      1,
		RootHash:  bytes32(0xab),
		Timestamp: time.Now().UTC(),
	}
	signed, err := cs.Sign(context.Background(), sth)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if len(signed.Signature) == 0 {
		t.Fatal("Sign returned empty signature")
	}
	digest := sth.Digest()
	if !ecdsa.VerifyASN1(pub, digest[:], signed.Signature) {
		t.Error("VerifyASN1 rejected our own cosigner signature")
	}
	if signed.PubkeyFingerprint == "" {
		t.Error("Sign returned empty PubkeyFingerprint")
	}
	if len(signed.PubkeyFingerprint) != 16 {
		t.Errorf("PubkeyFingerprint len = %d, want 16 (8 bytes hex-encoded)",
			len(signed.PubkeyFingerprint))
	}
	if signed.STH.ProjectID != "p1" {
		t.Errorf("signed.STH.ProjectID = %q, want p1", signed.STH.ProjectID)
	}
}

func TestCoSignerSignFailsWhenWitnessMissing(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	sink := &memCheckpointSink{}
	cs := NewCoSigner(w, sink)
	_, err := cs.Sign(context.Background(), STH{ProjectID: "p1", RootHash: bytes32(0)})
	if !errors.Is(err, ErrWitnessKeyMissing) {
		t.Fatalf("Sign without key: want ErrWitnessKeyMissing, got %v", err)
	}
}

func TestCoSignerOnSTHPropagatesSignError(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	sink := &memCheckpointSink{}
	cs := NewCoSigner(w, sink)
	err := cs.OnSTH(context.Background(), STH{ProjectID: "p1", RootHash: bytes32(0)})
	if !errors.Is(err, ErrWitnessKeyMissing) {
		t.Fatalf("OnSTH without key: want ErrWitnessKeyMissing, got %v", err)
	}
	if got := sink.snapshot(); len(got) != 0 {
		t.Errorf("sink got %d items after Sign error; expected zero forwards", len(got))
	}
}

func TestCoSignerOnSTHForwardsToSink(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	if _, err := w.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	sink := &memCheckpointSink{}
	cs := NewCoSigner(w, sink)
	sth := STH{
		ProjectID: "p1",
		Size:      1,
		RootHash:  bytes32(0xab),
		Timestamp: time.Now().UTC(),
	}
	if err := cs.OnSTH(context.Background(), sth); err != nil {
		t.Fatalf("OnSTH: %v", err)
	}
	got := sink.snapshot()
	if len(got) != 1 {
		t.Fatalf("sink got %d items, want 1", len(got))
	}
	if got[0].STH.ProjectID != "p1" {
		t.Errorf("sink got ProjectID %q, want p1", got[0].STH.ProjectID)
	}
	if len(got[0].Signature) == 0 {
		t.Error("sink got empty signature; inv-zen-145 violation")
	}
}

func TestCoSignerOnSTHFailFastOnSinkError(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	if _, err := w.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	wantErr := errors.New("disk full")
	sink := failingSink{err: wantErr}
	cs := NewCoSigner(w, sink)
	err := cs.OnSTH(context.Background(), STH{ProjectID: "p1", RootHash: bytes32(0xff)})
	if err == nil {
		t.Fatal("OnSTH ignored sink error")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("OnSTH returned %v, want sink error %v", err, wantErr)
	}
}

func TestCoSignerSubscribeWiresAdapter(t *testing.T) {
	withTestKeychain(t)
	w := NewWitness()
	if _, err := w.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	sink := &memCheckpointSink{}
	cs := NewCoSigner(w, sink)

	a, _ := newTempAdapter(t, "p1")
	if err := cs.SubscribeAdapter(a); err != nil {
		t.Fatalf("SubscribeAdapter: %v", err)
	}
	rec := bytes32(0xcd)
	leaf := Leaf{EventID: "evt-1", EventType: "e", PayloadHash: rec, RecordHash: rec}
	if _, err := a.AppendLeaf(context.Background(), leaf); err != nil {
		t.Fatalf("AppendLeaf: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(sink.snapshot()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	got := sink.snapshot()
	if len(got) == 0 {
		t.Fatal("cosigner did not forward STH from adapter within 2s")
	}
	if got[0].STH.ProjectID != "p1" {
		t.Errorf("forwarded STH ProjectID = %q, want p1", got[0].STH.ProjectID)
	}
	if len(got[0].Signature) == 0 {
		t.Error("forwarded SignedSTH has empty Signature; inv-zen-145 violation")
	}
}

func TestCoSignerSubscribeAdapterPropagatesClosedError(t *testing.T) {
	// Adapter.SubscribeSTH returns ErrAdapterClosed when the adapter
	// has been closed (post-A-3 fix-pass I2). SubscribeAdapter MUST
	// surface this error rather than swallow it; the daemon wiring
	// in cmd/zen-swarm-ctld depends on observing subscription failures
	// to fail-fast on a misconfigured project lifecycle.
	withTestKeychain(t)
	w := NewWitness()
	if _, err := w.Generate(); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	sink := &memCheckpointSink{}
	cs := NewCoSigner(w, sink)
	a, _ := newTempAdapter(t, "p1")
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	err := cs.SubscribeAdapter(a)
	if !errors.Is(err, ErrAdapterClosed) {
		t.Fatalf("SubscribeAdapter on closed adapter: want ErrAdapterClosed, got %v", err)
	}
}

type failingSink struct{ err error }

func (f failingSink) Append(ctx context.Context, signed SignedSTH) error { return f.err }
