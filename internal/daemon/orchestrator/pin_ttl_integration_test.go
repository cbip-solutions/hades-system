package orchestrator

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type observableAdapter struct {
	*pinStoreAdapter
	purges atomic.Int32
}

func (o *observableAdapter) PurgeExpired(now time.Time) (int, error) {
	o.purges.Add(1)
	return o.pinStoreAdapter.PurgeExpired(now)
}

func TestPinTTLExpiryMidConversation(t *testing.T) {

	s := openTestStore(t)
	obs := &observableAdapter{pinStoreAdapter: newPinStoreAdapter(s)}

	pins := NewPinOverrides(obs)

	const ttl = 2 * time.Second
	const sessionID = "sess-ephemeral"

	if err := pins.Set("session", sessionID, "tier_openclaude", "", ttl, "ttl-integration-test"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	row, err := pins.Resolve(sessionID, "")
	if err != nil {
		t.Fatalf("Phase1 Resolve: %v", err)
	}
	if row == nil {
		t.Fatal("Phase1: expected pinned tier within TTL, got nil")
	}
	if row.Tier != "tier_openclaude" {
		t.Errorf("Phase1: Tier = %q, want %q", row.Tier, "tier_openclaude")
	}

	time.Sleep(ttl + 200*time.Millisecond)

	rowExpired, err := pins.Resolve(sessionID, "")
	if err != nil {
		t.Fatalf("Phase2 Resolve: %v", err)
	}
	if rowExpired != nil {
		t.Errorf("Phase2: expected nil (expired by filter-on-read), got %+v", rowExpired)
	}

	storeRows, err := s.ListAllPins()
	if err != nil {
		t.Fatalf("Phase2 s.ListAllPins: %v", err)
	}
	if len(storeRows) != 1 {
		t.Errorf("Phase2: s.ListAllPins len = %d, want 1 (row present before sweep)", len(storeRows))
	}

	pins.tickInterval = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := pins.StartTTLSweep(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := s.ListAllPins()
		if err != nil {
			t.Fatalf("Phase3 s.ListAllPins poll: %v", err)
		}
		if len(rows) == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	finalRows, err := s.ListAllPins()
	if err != nil {
		t.Fatalf("Phase3 s.ListAllPins final: %v", err)
	}
	if len(finalRows) != 0 {
		t.Errorf("Phase3: expected 0 rows after sweep, got %d", len(finalRows))
	}
	if got := obs.purges.Load(); got < 1 {
		t.Errorf("Phase3: purges counter = %d, want ≥1", got)
	}

	rowPostSweep, err := pins.Resolve(sessionID, "")
	if err != nil {
		t.Fatalf("Phase4 Resolve: %v", err)
	}
	if rowPostSweep != nil {
		t.Errorf("Phase4: expected nil post-sweep, got %+v", rowPostSweep)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("Phase4: sweep goroutine did not exit within 1s of cancel")
	}
}

func TestPinTTLExpiryWithin5MinSweep(t *testing.T) {
	if got, want := defaultTTLSweepInterval, 5*time.Minute; got != want {
		t.Errorf("defaultTTLSweepInterval = %v, want %v (spec §4.1)", got, want)
	}
}
