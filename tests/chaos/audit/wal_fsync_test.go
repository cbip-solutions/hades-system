//go:build chaos

// modes.
//
// "WAL fsync" in the inv-zen-054 contract aliases to the leaf-append
// durability boundary in chain.SealPartition: the AppendSeal call to
// the underlying tessera tile-log. A failure here MUST:
//
//   1. Wrap the upstream error with the chain.SealPartition prefix
//      so callers see a clear cause.
//   2. NOT advance partition state (no half-written seal row in the
//      store; subsequent reads see ErrPartitionSealNotFound).
//   3. Be idempotent on retry: once the underlying AppendSeal
//      succeeds, the seal record materialises and a third call
//      observes the now-sealed state.
//
// The DST harness (F-5) drives the per-step fault stream; each fault
// is an AppendSeal failure followed by a clean retry.

package audit

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	auditchain "github.com/cbip-solutions/hades-system/internal/audit/chain"
	"github.com/cbip-solutions/hades-system/tests/chaos/dst"
)

const (
	testProjectID   = "proj-test"
	testPartitionID = "2026_05"
)

func seedPartition(t *testing.T, store *chainStore) {
	t.Helper()
	store.AddEvent("evt-1", `{"k":"v"}`, "", "rec-hash-1", testPartitionID)
}

func TestWALFsyncFailureWrapsAndDoesNotAdvance(t *testing.T) {
	store := newChainStore()
	tessera := newSealAppender()
	seedPartition(t, store)

	injected := errors.New("synthetic WAL fsync failure")
	tessera.FailAppendNext(injected)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := auditchain.SealPartition(ctx, store, tessera, testProjectID, testPartitionID, time.Now().Unix())
	if err == nil {
		t.Fatal("SealPartition: got nil err, want wrapped failure")
	}
	if !errors.Is(err, injected) {
		t.Errorf("SealPartition: err does not wrap injected sentinel: %v", err)
	}
	if !strings.Contains(err.Error(), "chain.SealPartition") {
		t.Errorf("SealPartition: err missing 'chain.SealPartition' prefix: %v", err)
	}

	if _, getErr := store.GetPartitionSeal(ctx, testPartitionID); !errors.Is(getErr, auditchain.ErrPartitionSealNotFound) {
		t.Errorf("post-failure GetPartitionSeal: got %v, want ErrPartitionSealNotFound", getErr)
	}
}

// TestWALFsyncRetryAfterClearSucceeds pins the idempotent-retry
// contract: a one-shot AppendSeal failure followed by a clean call
// MUST produce the canonical seal record.
func TestWALFsyncRetryAfterClearSucceeds(t *testing.T) {
	store := newChainStore()
	tessera := newSealAppender()
	seedPartition(t, store)

	tessera.FailAppendNext(errors.New("one-shot"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := auditchain.SealPartition(ctx, store, tessera, testProjectID, testPartitionID, time.Now().Unix()); err == nil {
		t.Fatal("expected first call to error")
	}

	seal, err := auditchain.SealPartition(ctx, store, tessera, testProjectID, testPartitionID, time.Now().Unix())
	if err != nil {
		t.Fatalf("retry SealPartition: %v", err)
	}
	if seal.PartitionID != testPartitionID {
		t.Errorf("seal.PartitionID = %q, want %q", seal.PartitionID, testPartitionID)
	}
	if seal.TesseraSealLeafID == "" {
		t.Error("seal.TesseraSealLeafID empty after retry")
	}
}

func TestWALFsyncRepeatedSealsAreIdempotent(t *testing.T) {
	store := newChainStore()
	tessera := newSealAppender()
	seedPartition(t, store)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	first, err := auditchain.SealPartition(ctx, store, tessera, testProjectID, testPartitionID, time.Now().Unix())
	if err != nil {
		t.Fatalf("first seal: %v", err)
	}
	second, err := auditchain.SealPartition(ctx, store, tessera, testProjectID, testPartitionID, time.Now().Unix())
	if err != nil {
		t.Fatalf("second seal: %v", err)
	}
	if first.TesseraSealLeafID != second.TesseraSealLeafID {
		t.Errorf("leaf ID drift across idempotent calls: %q vs %q",
			first.TesseraSealLeafID, second.TesseraSealLeafID)
	}
}

func TestWALFsyncDSTDrivesRetryUntilSuccess(t *testing.T) {
	store := newChainStore()
	tessera := newSealAppender()
	seedPartition(t, store)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sut := &walFsyncDriver{
		t:       t,
		ctx:     ctx,
		store:   store,
		tessera: tessera,
	}
	cfg := dst.RunConfig{
		Seed: 17,

		Mix:     dst.Mix{Inject: 3, Recover: 1, Sleep: 1, MaxSleep: 1 * time.Millisecond},
		Steps:   40,
		SkipBub: true,
	}
	result, err := dst.Run(t, cfg, sut)
	if err != nil {
		t.Fatalf("DST run: %v\nresult: %s", err, result)
	}

	if !sut.sealedOnce {
		t.Errorf("DST drove %d injects + %d recovers but never produced a seal (seed=%d)",
			result.Injects, result.Recovers, cfg.Seed)
	}
}

type walFsyncDriver struct {
	t          *testing.T
	ctx        context.Context
	store      *chainStore
	tessera    *sealAppender
	sealedOnce bool
}

func (d *walFsyncDriver) OnSleep(_ context.Context, _ time.Duration) error { return nil }
func (d *walFsyncDriver) OnYield(_ context.Context) error                  { return nil }
func (d *walFsyncDriver) OnInject(_ context.Context) error {
	d.tessera.FailAppendNext(errors.New("DST one-shot WAL fsync"))
	return nil
}
func (d *walFsyncDriver) OnRecover(_ context.Context) error {
	seal, err := auditchain.SealPartition(d.ctx, d.store, d.tessera, testProjectID, testPartitionID, time.Now().Unix())
	if err != nil {

		return nil
	}
	if seal.TesseraSealLeafID != "" {
		d.sealedOnce = true
	}
	return nil
}
