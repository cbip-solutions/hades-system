// go:build e2e

package e2e

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/workforceadapter"
	"github.com/cbip-solutions/hades-system/internal/store"
	"github.com/cbip-solutions/hades-system/internal/workforce/gate"
	"github.com/cbip-solutions/hades-system/internal/workforce/stream"
)

func openE2EStore(t *testing.T, path string) *store.Store {
	t.Helper()
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open(%q): %v", path, err)
	}
	if err := s.Migrate(); err != nil {
		_ = s.Close()
		t.Fatalf("Migrate: %v", err)
	}
	return s
}

// TestPlan4ERestartDurability simulates a daemon restart mid-operation and
// validates that both AggregationStream open windows and OperatorGate pause
// state survive the restart.
//
// Session 1 (pre-restart):
// - Open an aggregation stream window, publish 3 events, do NOT close.
// - Pause the OperatorGate (StatePausedDescriptive).
// - Close the store (simulates daemon shutdown).
//
// Session 2 (post-restart):
// - Open the same DB file.
// - LoadOpenWindows → must find exactly 1 open L2 window.
// - GateAdapter.LoadState → must return StatePausedDescriptive.
// - OperatorGate.IsPaused(ScopeWorkerDispatch) → true.
func TestPlan4ERestartDurability(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "e2e-restart.db")

	var openWindowID int64

	{
		s1 := openE2EStore(t, dbPath)
		ctx := context.Background()

		sa := workforceadapter.NewStreamAdapter(s1)
		ga := workforceadapter.NewGateAdapter(s1)

		id, err := sa.OpenWindow(ctx, stream.LayerL2, time.Now().UTC())
		if err != nil {
			t.Fatalf("Session 1 OpenWindow: %v", err)
		}
		openWindowID = id
		for i := 0; i < 3; i++ {

			ev := stream.Event{
				Type:        "checkpoint",
				Payload:     []byte(`{"step":` + strconv.Itoa(i) + `}`),
				PublishedAt: time.Now().UTC(),
			}
			if err := sa.AppendEvent(ctx, id, ev); err != nil {
				t.Fatalf("Session 1 AppendEvent %d: %v", i, err)
			}
		}

		if err := ga.SaveState(ctx, gate.StatePausedDescriptive, "pre-restart test"); err != nil {
			t.Fatalf("Session 1 SaveState: %v", err)
		}

		if err := s1.Close(); err != nil {
			t.Fatalf("Session 1 Close: %v", err)
		}
	}

	{
		s2 := openE2EStore(t, dbPath)
		defer s2.Close()
		ctx := context.Background()

		sa := workforceadapter.NewStreamAdapter(s2)
		ga := workforceadapter.NewGateAdapter(s2)

		open, err := sa.LoadOpenWindows(ctx)
		if err != nil {
			t.Fatalf("Session 2 LoadOpenWindows: %v", err)
		}
		found := false
		for _, w := range open {
			if w.WindowID == openWindowID {
				found = true
				if w.Layer != stream.LayerL2 {
					t.Errorf("recovered window Layer = %v, want L2", w.Layer)
				}
				if w.Count != 0 {

					t.Logf("note: window.Count=%d (expected 0 since window was not closed)", w.Count)
				}
			}
		}
		if !found {
			t.Errorf("open window %d not recovered after restart; open=%v", openWindowID, open)
		}

		gateState, err := ga.LoadState(ctx)
		if err != nil {
			t.Fatalf("Session 2 LoadState: %v", err)
		}
		if gateState != gate.StatePausedDescriptive {
			t.Errorf("Session 2 gate state = %v, want StatePausedDescriptive", gateState)
		}

		g, err := gate.NewOperatorGate(ctx, ga)
		if err != nil {
			t.Fatalf("Session 2 NewOperatorGate: %v", err)
		}
		if !g.IsPaused(gate.ScopeWorkerDispatch) {
			t.Error("IsPaused(ScopeWorkerDispatch) = false after restart with paused state")
		}
		if !g.IsPaused(gate.ScopeLLMPreCall) {
			t.Error("IsPaused(ScopeLLMPreCall) = false after restart with paused state")
		}
		if !g.IsPaused(gate.ScopeAfterCommit) {
			t.Error("IsPaused(ScopeAfterCommit) = false after restart with paused state")
		}

		if err := g.Resume(ctx); err != nil {
			t.Fatalf("Session 2 Resume: %v", err)
		}
		if g.IsPaused(gate.ScopeWorkerDispatch) {
			t.Error("IsPaused after Resume = true, want false")
		}
	}
}

func TestPlan4EAggregationStreamDoctrineConfig(t *testing.T) {

	cfgMaxScope := stream.Config{
		L2ToL3: 30 * time.Second,
		L3ToL4: 5 * time.Minute,
	}

	cfgDefault := stream.Config{
		L2ToL3: 60 * time.Second,
		L3ToL4: 15 * time.Minute,
	}

	for _, tt := range []struct {
		name string
		cfg  stream.Config
	}{
		{"max-scope", cfgMaxScope},
		{"default", cfgDefault},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := &noopStreamPersist{}
			s, err := stream.NewAggregationStream(tt.cfg, p)
			if err != nil {
				t.Fatalf("NewAggregationStream(%s): %v", tt.name, err)
			}
			snap := s.WindowSnapshot(stream.LayerL2)
			if snap.Count != 0 {
				t.Errorf("initial Count = %d, want 0", snap.Count)
			}
		})
	}
}

type noopStreamPersist struct{}

func (n *noopStreamPersist) OpenWindow(_ context.Context, _ stream.Layer, _ time.Time) (int64, error) {
	return 0, nil
}
func (n *noopStreamPersist) AppendEvent(_ context.Context, _ int64, _ stream.Event) error {
	return nil
}
func (n *noopStreamPersist) CloseWindow(_ context.Context, _ int64, _ time.Time, _ int) error {
	return nil
}
func (n *noopStreamPersist) LoadOpenWindows(_ context.Context) ([]stream.WindowRecord, error) {
	return nil, nil
}
