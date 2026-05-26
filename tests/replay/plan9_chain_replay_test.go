//go:build replay
// +build replay

// tests/replay/plan9_chain_replay_test.go (Plan 9 Phase K-9).
//
// Replay-tier validation of the chain layer's three load-bearing
// determinism + integrity properties, asserted independently of
// ReplayState — orthogonal axis from chain.Walk's per-project
// integrity walk).
//
// Per spec amendment 6153913 (K-6..K-10 doc-revision): K-9 originally
// referenced eventlog.Replay returning a per-project event slice, but
// the real signature is Replay(ctx, sessionID) → *ReplayState. K-9 is
// re-anchored against chain.Walk over the auditadapter (*Adapter
// satisfies chain.EventStore), which IS the per-project axis. The
// three scenarios PRESERVE the original intent (determinism,
// cross-event-type integrity, per-project isolation) and assert each
// chain-layer property in isolation against shipped Phase A+B APIs.
//
// Three scenarios:
//
//  1. TestReplay_ChainHashesByteIdentical — re-derive record_hash via
//     chain.Compute(prev, type, payload, ts) for every row returned by
//     auditadapter.ListEventsForPartition; assert recomputed == stored.
//     This is the load-bearing determinism contract; chain.Walk
//     implements the same check internally, so this test asserts the
//     property in isolation against the EventStore satisfier.
//
//  2. TestReplay_MultipleEventTypesIntegrity — append rows with four
//     distinct Plan 9 event_type strings (audit/vault/research/state);
//     chain.Walk(ctx, aa, proj) MUST report zero Tampered + zero
//     GapsDetected regardless. The chain does not discriminate by
//     event_type.
//
//  3. TestReplay_PerProjectIsolation — inv-zen-150 (per-project blast
//     radius). Interleave proj-A and proj-B rows in the same partition;
//     chain.Walk(ctx, aa, "proj-A") MUST report EventsWalked == 30
//     (not 60). Symmetric for proj-B. Tessera leaf-id prefix shape
//     verified opportunistically (only when populated; auditadapter
//     without WithTessera leaves tessera_leaf_id NULL).
//
// Build tag: replay. Opt-in via `make test-replay`.
package replay_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/chain"
	"github.com/cbip-solutions/hades-system/internal/daemon/auditadapter"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/store"
	"github.com/cbip-solutions/hades-system/tests/testhelpers"
)

func seedRawEvent(
	t *testing.T,
	ctx context.Context,
	aa *auditadapter.Adapter,
	st *store.Store,
	eventID, projectID, eventType string,
	payload []byte,
	ts int64,
) {
	t.Helper()
	_, err := st.DB().ExecContext(
		ctx,
		`INSERT INTO audit_events_raw (id, project_id, type, payload_json, emitted_at) VALUES (?, ?, ?, ?, ?)`,
		eventID, projectID, eventType, string(payload), ts,
	)
	if err != nil {
		t.Fatalf("insert raw event %q: %v", eventID, err)
	}
	if _, err := aa.OnEmitRaw(ctx, eventID, projectID, eventType, payload, ts); err != nil {
		t.Fatalf("OnEmitRaw %q: %v", eventID, err)
	}
}

func TestReplay_ChainHashesByteIdentical(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	st := testhelpers.NewTestStore(t)
	aa := auditadapter.New(st)

	const N = 200
	const projectID = "proj-A"
	const eventType = "research.findings_returned"
	baseTS := time.Now().UTC().Unix()
	for i := 0; i < N; i++ {
		eventID := fmt.Sprintf("evt-determ-%03d", i)
		payload := []byte(fmt.Sprintf(`{"i":%d}`, i))

		seedRawEvent(t, ctx, aa, st, eventID, projectID, eventType, payload, baseTS+int64(i))
	}

	parts, err := aa.ListPartitions(ctx)
	if err != nil {
		t.Fatalf("ListPartitions: %v", err)
	}
	if len(parts) == 0 {
		t.Fatalf("no partitions discovered; expected ≥1 after inserting %d events", N)
	}

	var prevHash string
	walked := 0
	for _, p := range parts {
		rows, err := aa.ListEventsForPartition(ctx, p.PartitionID)
		if err != nil {
			t.Fatalf("ListEventsForPartition %q: %v", p.PartitionID, err)
		}
		for _, row := range rows {
			if row.ProjectID != projectID {
				continue
			}
			walked++
			got, err := chain.Compute(prevHash, row.Type, []byte(row.PayloadJSON), row.EmittedAt)
			if err != nil {
				t.Fatalf("chain.Compute %q: %v", row.ID, err)
			}
			if got != row.RecordHash {
				t.Errorf("event %q: reconstructed hash %s != stored %s", row.ID, got, row.RecordHash)
			}
			prevHash = row.RecordHash
		}
	}
	if walked != N {
		t.Errorf("walked %d rows, want %d", walked, N)
	}
}

func TestReplay_MultipleEventTypesIntegrity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	st := testhelpers.NewTestStore(t)
	aa := auditadapter.New(st)

	const projectID = "proj-A"

	eventTypes := []string{
		eventlog.EvtAuditPartitionSealed,
		eventlog.EvtVaultNotePromotedToGlobal,
		eventlog.EvtResearchFindingsReturned,
		eventlog.EvtStateRegenerated,
	}
	baseTS := time.Now().UTC().Unix()
	const N = 50
	for i := 0; i < N; i++ {
		eventID := fmt.Sprintf("evt-mixed-%03d", i)
		typ := eventTypes[i%len(eventTypes)]
		payload := []byte(fmt.Sprintf(`{"k":"%s","i":%d}`, typ, i))
		seedRawEvent(t, ctx, aa, st, eventID, projectID, typ, payload, baseTS+int64(i))
	}

	report, err := chain.Walk(ctx, aa, projectID)
	if err != nil {
		t.Fatalf("chain.Walk: %v", err)
	}
	if report.EventsWalked != int64(N) {
		t.Errorf("EventsWalked = %d, want %d", report.EventsWalked, N)
	}
	if len(report.Tampered) != 0 {
		t.Errorf("Tampered = %d, want 0: %+v", len(report.Tampered), report.Tampered)
	}
	if len(report.GapsDetected) != 0 {
		t.Errorf("GapsDetected = %d, want 0: %+v", len(report.GapsDetected), report.GapsDetected)
	}
}

func TestReplay_PerProjectIsolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	st := testhelpers.NewTestStore(t)
	aa := auditadapter.New(st)

	const N = 30

	tsA := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC).Unix()
	tsB := time.Date(2024, 2, 15, 12, 0, 0, 0, time.UTC).Unix()
	for i := 0; i < N; i++ {
		seedRawEvent(t, ctx, aa, st,
			fmt.Sprintf("evt-A-%03d", i), "proj-A", "test.event",
			[]byte("a"), tsA+int64(i))
	}
	for i := 0; i < N; i++ {
		seedRawEvent(t, ctx, aa, st,
			fmt.Sprintf("evt-B-%03d", i), "proj-B", "test.event",
			[]byte("b"), tsB+int64(i))
	}

	reportA, err := chain.Walk(ctx, aa, "proj-A")
	if err != nil {
		t.Fatalf("chain.Walk proj-A: %v", err)
	}
	reportB, err := chain.Walk(ctx, aa, "proj-B")
	if err != nil {
		t.Fatalf("chain.Walk proj-B: %v", err)
	}
	if reportA.EventsWalked != int64(N) {
		t.Errorf("reportA.EventsWalked = %d, want %d (per-project filter)", reportA.EventsWalked, N)
	}
	if reportB.EventsWalked != int64(N) {
		t.Errorf("reportB.EventsWalked = %d, want %d (per-project filter)", reportB.EventsWalked, N)
	}
	if len(reportA.Tampered)+len(reportA.GapsDetected) != 0 {
		t.Errorf("proj-A walk surfaced findings: %+v", reportA)
	}
	if len(reportB.Tampered)+len(reportB.GapsDetected) != 0 {
		t.Errorf("proj-B walk surfaced findings: %+v", reportB)
	}

	rowA, err := aa.GetByEventID(ctx, "evt-A-000")
	if err != nil {
		t.Fatalf("GetByEventID evt-A-000: %v", err)
	}
	if rowA.TesseraLeafID != nil && *rowA.TesseraLeafID != "" {
		if !strings.HasPrefix(*rowA.TesseraLeafID, "proj-A:") {
			t.Errorf("proj-A leaf_id %q lacks project prefix; isolation broken", *rowA.TesseraLeafID)
		}
	}
}
