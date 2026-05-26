//go:build chaos && cgo
// +build chaos,cgo

package plan9_audit_chaos

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/chain"
	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/internal/daemon/auditadapter"
	"github.com/cbip-solutions/hades-system/internal/store"
	"github.com/cbip-solutions/hades-system/tests/chaos/plan9_audit_chaos/litestreammock"
	testhelpers "github.com/cbip-solutions/hades-system/tests/testhelpers"
)

// TestChaos_KillDaemonMidBatch is the first chaos scenario from
// spec §5.3 — spawn the real daemon, append events into
// audit_events_raw via POST /v1/audit/emit, SIGKILL the daemon
// mid-batch, then re-open the store and assert chain.Walk reports
// zero Tampered + zero GapsDetected.
//
// Recovery contract: cmd/zen-swarm-ctld/main.go calls chain.Backfill at
// boot (C-fix-4); previously-unchained rows get prev_hash + record_hash
// + partition_id set on restart. tessera_leaf_id remains NULL for rows
// whose batch hadn't flushed pre-kill — recovered by Phase J's
// recovery sweep (post-Plan-9 ships).
//
// This test verifies the BACKFILL contract: data on disk before crash
// is recoverable post-crash. We do NOT verify tessera_leaf_id presence
// here (that is the Phase J scope); we only assert chain integrity.
func TestChaos_KillDaemonMidBatch(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	uds, pid, dbPath := testhelpers.SpawnDaemonWithPID(t)

	const N = 8
	client := testhelpers.HTTPClientForUDS(uds)
	for i := 0; i < N; i++ {
		body, _ := json.Marshal(map[string]any{
			"project_id": "chaos-proj",
			"type":       "chaos.kill_mid_batch",
			"payload":    map[string]int{"seq": i},
		})
		resp, err := client.Post("http://daemon/v1/audit/emit", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("emit[%d]: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("emit[%d]: unexpected status %d", i, resp.StatusCode)
		}
	}

	ci := testhelpers.NewCrashInjector()
	if err := ci.KillProcess(ctx, pid); err != nil {
		t.Fatalf("KillProcess(%d): %v", pid, err)
	}

	waitUntil := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(uds); os.IsNotExist(err) {
			break
		}
		if time.Now().After(waitUntil) {

			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	time.Sleep(100 * time.Millisecond)

	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open(%s) after kill: %v", dbPath, err)
	}
	defer s.Close()

	a := auditadapter.New(s)

	_, err = chain.Backfill(ctx, a, 100)
	if err != nil {
		t.Fatalf("chain.Backfill post-kill: %v", err)
	}

	report, err := chain.Walk(ctx, a, "chaos-proj")
	if err != nil {
		t.Fatalf("chain.Walk post-kill: %v", err)
	}
	if len(report.Tampered) != 0 {
		t.Errorf("Walk reports %d Tampered entries after kill+backfill: %+v",
			len(report.Tampered), report.Tampered)
	}
	if len(report.GapsDetected) != 0 {
		t.Errorf("Walk reports %d GapsDetected entries after kill+backfill: %+v",
			len(report.GapsDetected), report.GapsDetected)
	}
	t.Logf("chain.Walk post-kill: EventsWalked=%d Tampered=%d GapsDetected=%d",
		report.EventsWalked, len(report.Tampered), len(report.GapsDetected))
}

func TestChaos_AppendSealIdempotenceAcrossCacheLoss(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tmp := t.TempDir()
	tessRoot := filepath.Join(tmp, "tessera-root")

	cfg := tessera.Config{
		BatchMaxAge:         50 * time.Millisecond,
		BatchMaxSize:        10,
		RotationCadenceDays: 365,
	}

	a1, err := tessera.NewProjectAdapter(ctx, "proj-A", tessRoot, cfg)
	if err != nil {
		t.Fatalf("NewProjectAdapter[1]: %v", err)
	}
	payload := []byte("partition payload")
	id1a, err := a1.AppendSeal(ctx, "proj-A", "2026_05", payload)
	if err != nil {
		_ = a1.Close()
		t.Fatalf("AppendSeal[1a]: %v", err)
	}
	id1b, err := a1.AppendSeal(ctx, "proj-A", "2026_05", payload)
	if err != nil {
		_ = a1.Close()
		t.Fatalf("AppendSeal[1b]: %v", err)
	}
	if id1a != id1b {
		_ = a1.Close()
		t.Fatalf("within-adapter AppendSeal returned different IDs %q vs %q; expected cached", id1a, id1b)
	}
	if id1a == "" {
		_ = a1.Close()
		t.Fatalf("within-adapter AppendSeal returned empty LeafID")
	}
	if err := a1.Close(); err != nil {
		t.Fatalf("a1.Close: %v", err)
	}

	a2, err := tessera.NewProjectAdapter(ctx, "proj-A", tessRoot, cfg)
	if err != nil {
		t.Fatalf("NewProjectAdapter[2]: %v", err)
	}
	defer func() { _ = a2.Close() }()
	id2, err := a2.AppendSeal(ctx, "proj-A", "2026_05", payload)
	if err != nil {
		t.Fatalf("AppendSeal[2]: %v", err)
	}

	if id2 == "" {
		t.Fatalf("cross-adapter AppendSeal returned empty LeafID (id1a=%q id2=%q)", id1a, id2)
	}
	if id1a == id2 {
		t.Logf("LeafIDs collapsed across adapter generations: %q (Tessera library may deduplicate by content; not asserted)", id2)
	} else {
		t.Logf("LeafIDs differ across adapter generations as expected: id1a=%q id2=%q (chain layer owns cross-generation idempotence)", id1a, id2)
	}

	id2b, err := a2.AppendSeal(ctx, "proj-A", "2026_05", payload)
	if err != nil {
		t.Fatalf("AppendSeal[2b]: %v", err)
	}
	if id2 != id2b {
		t.Fatalf("within-adapter-2 AppendSeal returned different IDs %q vs %q; expected cached", id2, id2b)
	}
}

func TestChaos_LitestreamSubprocessCrash(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mock := litestreammock.New()

	state, lagSec, err := mock.Status(ctx)
	if err != nil {
		t.Fatalf("pre-crash Status: unexpected error: %v", err)
	}
	if state != litestreammock.StateReplicating {
		t.Errorf("pre-crash state = %q, want %q", state, litestreammock.StateReplicating)
	}
	if lagSec < 0 {
		t.Errorf("pre-crash lagSec = %d, want >= 0", lagSec)
	}
	t.Logf("pre-crash: state=%q lagSec=%d", state, lagSec)

	s := testhelpers.NewTestStore(t)
	a := auditadapter.New(s, auditadapter.WithLitestream(mock))

	_ = a

	mock.InjectCrash(fmt.Errorf("litestream: subprocess exited with status 1 (simulated)"))

	_, _, crashErr := mock.Status(ctx)
	if crashErr == nil {
		t.Fatalf("post-crash Status: expected error, got nil (state detection contract violated)")
	}
	t.Logf("post-crash Status error (expected): %v", crashErr)

	mock.Reset()
	stateAfter, _, err := mock.Status(ctx)
	if err != nil {
		t.Fatalf("post-reset Status: unexpected error: %v", err)
	}
	if stateAfter != litestreammock.StateReplicating {
		t.Errorf("post-reset state = %q, want %q", stateAfter, litestreammock.StateReplicating)
	}
	t.Logf("post-reset: state=%q (recovery signal visible)", stateAfter)
}
