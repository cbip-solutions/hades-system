//go:build chaos

// failure modes.
//
// "Anchor write" in the inv-zen-054 contract aliases to
// chain.Backfill's UpdateChainColumns call (the per-row chain-anchor
// commit). A failure here MUST:
//
//   1. Surface a wrapped error with the chain.Backfill prefix.
//   2. Leave the PRIOR rows committed (chain.UpdateChainColumns is
//      atomic per row — atomic-by-store).
//   3. Allow a clean restart: Backfill called again resumes from the
//      chain tip without re-processing committed rows.

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

func seedUnchainedEvents(t *testing.T, store *chainStore, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		id := "evt-unchained-" + intToStr(i)
		store.AddEvent(id, `{"k":"v"}`, "", "", "")
	}
}

func TestAnchorWriteFailureWrapsAndStopsAtFailingRow(t *testing.T) {
	store := newChainStore()

	seedUnchainedEvents(t, store, 3)
	injected := errors.New("synthetic anchor-write failure")
	store.FailUpdateChainNext(injected)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	report, err := auditchain.Backfill(ctx, store, 10)
	if err == nil {
		t.Fatal("Backfill: got nil err, want wrapped failure")
	}
	if !errors.Is(err, injected) {
		t.Errorf("Backfill: err does not wrap injected sentinel: %v", err)
	}
	if !strings.Contains(err.Error(), "chain.Backfill") {
		t.Errorf("Backfill: err missing 'chain.Backfill' prefix: %v", err)
	}
	if report.RowsBackfilled != 0 {
		t.Errorf("RowsBackfilled = %d on failed-row-0, want 0", report.RowsBackfilled)
	}
}

// TestAnchorWriteRestartResumesFromTip pins the restart-contract:
// after a Backfill failure mid-batch, a subsequent call (with no
// injected fault) MUST resume cleanly. Concretely the second call
// processes ALL rows because the first call committed zero (the
// first-row failure aborted before any UpdateChainColumns landed).
func TestAnchorWriteRestartResumesFromTip(t *testing.T) {
	store := newChainStore()
	seedUnchainedEvents(t, store, 3)
	store.FailUpdateChainNext(errors.New("one-shot anchor write"))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := auditchain.Backfill(ctx, store, 10)
	if err == nil {
		t.Fatal("first Backfill: got nil err, want fault")
	}

	report, err := auditchain.Backfill(ctx, store, 10)
	if err != nil {
		t.Fatalf("restart Backfill: %v", err)
	}
	if report.RowsBackfilled != 3 {
		t.Errorf("restart Backfill: RowsBackfilled = %d, want 3", report.RowsBackfilled)
	}
}

func TestAnchorWriteIdempotentReRunOnFullyChainedStoreIsNoOp(t *testing.T) {
	store := newChainStore()
	seedUnchainedEvents(t, store, 5)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	first, err := auditchain.Backfill(ctx, store, 10)
	if err != nil {
		t.Fatalf("first Backfill: %v", err)
	}
	if first.RowsBackfilled != 5 {
		t.Fatalf("first Backfill: rows=%d, want 5", first.RowsBackfilled)
	}

	second, err := auditchain.Backfill(ctx, store, 10)
	if err != nil {
		t.Fatalf("rerun Backfill: %v", err)
	}
	if second.RowsBackfilled != 0 {
		t.Errorf("rerun Backfill: rows=%d, want 0 (idempotent no-op)", second.RowsBackfilled)
	}
}

// TestAnchorWriteScanFailureSurfaces pins the BackfillScan path: a
// scan-side error (DB unavailable) MUST bubble out of Backfill with
// the canonical prefix, allowing the caller to retry the whole run.
func TestAnchorWriteScanFailureSurfaces(t *testing.T) {
	store := newChainStore()
	seedUnchainedEvents(t, store, 1)
	store.failBackfill = errors.New("synthetic scan failure")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := auditchain.Backfill(ctx, store, 10)
	if err == nil {
		t.Fatal("Backfill: got nil err, want scan-failure surface")
	}
	if !strings.Contains(err.Error(), "chain.Backfill") {
		t.Errorf("err missing 'chain.Backfill' prefix: %v", err)
	}
}

func TestAnchorWriteDSTDrivesRestartLoop(t *testing.T) {
	store := newChainStore()
	const rowCount = 5
	seedUnchainedEvents(t, store, rowCount)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sut := &anchorDriver{t: t, ctx: ctx, store: store}
	cfg := dst.RunConfig{
		Seed:    23,
		Mix:     dst.Mix{Inject: 2, Recover: 3, Sleep: 1, MaxSleep: 1 * time.Millisecond},
		Steps:   60,
		SkipBub: true,
	}
	if _, err := dst.Run(t, cfg, sut); err != nil {
		t.Fatalf("DST run: %v", err)
	}

	parts, err := store.ListPartitions(ctx)
	if err != nil {
		t.Fatalf("ListPartitions: %v", err)
	}
	var total int64
	for _, p := range parts {
		total += p.EventCount
	}

	if total != rowCount {
		t.Errorf("anchored rows = %d, want %d (seed=%d)", total, rowCount, cfg.Seed)
	}
}

type anchorDriver struct {
	t     *testing.T
	ctx   context.Context
	store *chainStore
}

func (d *anchorDriver) OnSleep(_ context.Context, _ time.Duration) error { return nil }
func (d *anchorDriver) OnYield(_ context.Context) error                  { return nil }
func (d *anchorDriver) OnInject(_ context.Context) error {
	d.store.FailUpdateChainNext(errors.New("DST one-shot anchor write"))
	return nil
}
func (d *anchorDriver) OnRecover(_ context.Context) error {

	_, _ = auditchain.Backfill(d.ctx, d.store, 10)
	return nil
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
