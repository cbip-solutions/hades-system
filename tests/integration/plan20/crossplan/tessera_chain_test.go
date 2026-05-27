// go:build integration
package crossplan

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation"
)

func TestTesseraChainSpansPlan14_19_20(t *testing.T) {
	disableKeychain(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	tmp := t.TempDir()
	adapter := newTesseraAdapter(t, ctx, "chain-itest", tmp)

	events := []struct {
		eventID, eventType, payload string
	}{
		{"plan14-evt-001", "audit.partition_sealed", `{"partition":"a"}`},
		{"plan14-evt-002", "audit.partition_sealed", `{"partition":"b"}`},
		{"plan14-evt-003", "audit.partition_sealed", `{"partition":"c"}`},
		{"plan14-evt-004", "audit.partition_sealed", `{"partition":"d"}`},

		{"plan19-evt-001", "caronte.engine_initialized", `{"project":"p1"}`},
		{"plan19-evt-002", "caronte.index_completed", `{"project":"p1","nodes":42}`},
		{"plan19-evt-003", "caronte.blast_radius_scored", `{"symbol":"F1","level":"high"}`},
		{"plan20-evt-001", string(federation.EvtCrossRepoLink), `{"workspace":"ws1","call":"c1","endpoint":"e1"}`},
		{"plan20-evt-002", string(federation.EvtBreakingChange), `{"workspace":"ws1","change":"chg1"}`},
		{"plan20-evt-003", string(federation.EvtCoordinatedDispatch), `{"workspace":"ws1","mode":"surface"}`},
	}

	leafIDs := make([]tessera.LeafID, 0, len(events))
	for _, ev := range events {
		payloadHash := sha256.Sum256([]byte(ev.payload))
		recordHash := sha256.Sum256([]byte(ev.eventID + ev.eventType))
		id, err := adapter.AppendLeaf(ctx, tessera.Leaf{
			EventID:     ev.eventID,
			EventType:   ev.eventType,
			PayloadHash: payloadHash[:],
			RecordHash:  recordHash[:],
		})
		if err != nil {
			t.Fatalf("AppendLeaf %s: %v", ev.eventID, err)
		}
		if id == "" {
			t.Errorf("AppendLeaf %s returned empty LeafID", ev.eventID)
		}
		leafIDs = append(leafIDs, id)
	}
	if got, want := len(leafIDs), 10; got != want {
		t.Fatalf("appended %d leaves; want %d", got, want)
	}

	for i, id := range leafIDs {
		if !verifyInclusionWithPoll(t, ctx, adapter, id, 10*time.Second) {
			t.Errorf("VerifyMerkleInclusion[%d] (%s) never returned true within 10s; want true (chain integrity for mixed Plan-14/19/20 leaves)", i, id)
		}
	}
}

func verifyInclusionWithPoll(t *testing.T, ctx context.Context, adapter *tessera.Adapter, id tessera.LeafID, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ok, err := adapter.VerifyMerkleInclusion(ctx, id)
		switch {
		case err == nil && ok:
			return true
		case err == nil && !ok:

		case errors.Is(err, tessera.ErrLeafNotFound):

		case err != nil:

			t.Logf("VerifyMerkleInclusion(%s) transient err: %v (will retry)", id, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}
