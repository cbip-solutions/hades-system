// go:build integration && cgo
//go:build integration && cgo
// +build integration,cgo

package plan9_audit_chain

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/chain"
	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/internal/daemon/auditadapter"
	"github.com/cbip-solutions/hades-system/internal/store"
)

type memCheckpointSink struct {
	mu      sync.Mutex
	signed  []tessera.SignedSTH
	signal  chan struct{}
	signalO sync.Once
}

func newMemCheckpointSink() *memCheckpointSink {
	return &memCheckpointSink{signal: make(chan struct{})}
}

func (s *memCheckpointSink) Append(_ context.Context, signed tessera.SignedSTH) error {
	if len(signed.Signature) == 0 {
		return tessera.ErrUnsignedSTH
	}
	s.mu.Lock()
	s.signed = append(s.signed, signed)
	s.mu.Unlock()
	s.signalO.Do(func() { close(s.signal) })
	return nil
}

func (s *memCheckpointSink) Snapshot() []tessera.SignedSTH {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]tessera.SignedSTH, len(s.signed))
	copy(out, s.signed)
	return out
}

type sthSubscriberFunc func(ctx context.Context, sth tessera.STH) error

func (f sthSubscriberFunc) OnSTH(ctx context.Context, sth tessera.STH) error {
	return f(ctx, sth)
}

func openMigratedStore(t *testing.T, tmpDir string) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(tmpDir, "state.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	return s
}

func TestPlan9_AuditChainE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped under -short")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	tmp := t.TempDir()

	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	tessRoot := filepath.Join(tmp, "tessera-root")
	cfg := tessera.Config{
		BatchMaxAge:         50 * time.Millisecond,
		BatchMaxSize:        10,
		RotationCadenceDays: 365,
	}
	tessAdap, err := tessera.NewProjectAdapter(ctx, "proj-A", tessRoot, cfg)
	if err != nil {
		t.Fatalf("tessera.NewProjectAdapter: %v", err)
	}
	t.Cleanup(func() { _ = tessAdap.Close() })

	witness := tessera.NewWitness()
	pub, err := witness.Generate()
	if err != nil && !errors.Is(err, tessera.ErrWitnessKeyAlreadyExists) {
		t.Fatalf("witness.Generate: %v", err)
	}
	if pub == nil {

		pub, err = witness.Load()
		if err != nil {
			t.Fatalf("witness.Load: %v", err)
		}
	}
	tessAdap.Attach(witness)

	sthCh := make(chan tessera.STH, 20)
	if err := tessAdap.SubscribeSTH(sthSubscriberFunc(func(_ context.Context, sth tessera.STH) error {
		select {
		case sthCh <- sth:
		default:
		}
		return nil
	})); err != nil {
		t.Fatalf("SubscribeSTH(sthCh): %v", err)
	}

	sink := newMemCheckpointSink()
	cosigner := tessera.NewCoSigner(witness, sink)
	if err := tessAdap.SubscribeSTH(cosigner); err != nil {
		t.Fatalf("SubscribeSTH(cosigner): %v", err)
	}

	st := openMigratedStore(t, tmp)
	aa := auditadapter.New(st, auditadapter.WithTessera(tessAdap))

	const N = 100
	const projectID = "proj-A"
	const eventType = "research.findings_returned"
	for i := 0; i < N; i++ {
		eventID := fmt.Sprintf("evt-%03d", i)
		payload := []byte(fmt.Sprintf(`{"i":%d}`, i))
		emittedAt := time.Now().UTC().Unix()
		if _, err := st.DB().ExecContext(ctx,
			`INSERT INTO audit_events_raw (id, project_id, type, payload_json, emitted_at) VALUES (?, ?, ?, ?, ?)`,
			eventID, projectID, eventType, string(payload), emittedAt,
		); err != nil {
			t.Fatalf("insert raw event[%d]: %v", i, err)
		}
		if _, err := aa.OnEmitRaw(ctx, eventID, projectID, eventType, payload, emittedAt); err != nil {
			t.Fatalf("OnEmitRaw[%d]: %v", i, err)
		}
	}

	report, err := chain.Walk(ctx, aa, projectID)
	if err != nil {
		t.Fatalf("chain.Walk: %v", err)
	}
	if report.EventsWalked != int64(N) {
		t.Fatalf("EventsWalked = %d, want %d", report.EventsWalked, N)
	}
	if len(report.Tampered) != 0 {
		t.Errorf("Tampered = %d, want 0: %+v", len(report.Tampered), report.Tampered)
	}
	if len(report.GapsDetected) != 0 {
		t.Errorf("GapsDetected = %d, want 0: %+v", len(report.GapsDetected), report.GapsDetected)
	}

	for i := 0; i < N; i++ {
		eventID := fmt.Sprintf("evt-%03d", i)
		row, err := aa.GetByEventID(ctx, eventID)
		if err != nil {
			t.Fatalf("GetByEventID[%s]: %v", eventID, err)
		}
		if row.TesseraLeafID == nil || *row.TesseraLeafID == "" {
			t.Errorf("event %s: tessera_leaf_id NULL after OnEmitRaw", eventID)
		}
	}

	select {
	case sth := <-sthCh:
		if sth.Size == 0 {
			t.Errorf("STH.Size = 0; expected non-zero after appending %d leaves", N)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("no STH fired within 5s after appending %d leaves", N)
	}

	select {
	case <-sink.signal:

	case <-time.After(5 * time.Second):
		t.Fatalf("CoSigner did not forward any SignedSTH within 5s")
	}
	captured := sink.Snapshot()
	if len(captured) == 0 {
		t.Fatalf("sink captured 0 SignedSTHs")
	}

	for _, s := range captured {
		if len(s.Signature) == 0 {
			t.Errorf("captured SignedSTH has empty signature")
			continue
		}
		digest := s.STH.Digest()
		if !tessera.VerifyWithPubkey(pub, digest[:], s.Signature) {
			t.Errorf("VerifyWithPubkey rejected captured SignedSTH (size=%d)", s.STH.Size)
		}
	}
}
