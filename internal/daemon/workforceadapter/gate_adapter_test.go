package workforceadapter_test

import (
	"context"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/workforceadapter"
	"github.com/cbip-solutions/hades-system/internal/workforce/gate"
)

func TestGateAdapterLoadDefaultIsRunning(t *testing.T) {
	a := workforceadapter.NewGateAdapter(openTestStore(t))
	s, err := a.LoadState(context.Background())
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if s != gate.StateRunning {
		t.Errorf("LoadState (fresh db) = %v, want StateRunning", s)
	}
}

func TestGateAdapterSaveAndLoad(t *testing.T) {
	a := workforceadapter.NewGateAdapter(openTestStore(t))
	ctx := context.Background()

	if err := a.SaveState(ctx, gate.StatePausedDescriptive, "operator test"); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	got, err := a.LoadState(ctx)
	if err != nil {
		t.Fatalf("LoadState after save: %v", err)
	}
	if got != gate.StatePausedDescriptive {
		t.Errorf("LoadState = %v, want StatePausedDescriptive", got)
	}
}

func TestGateAdapterSaveIdempotent(t *testing.T) {

	a := workforceadapter.NewGateAdapter(openTestStore(t))
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := a.SaveState(ctx, gate.StatePausedQuiet, "repeated"); err != nil {
			t.Fatalf("SaveState iteration %d: %v", i, err)
		}
	}
	got, _ := a.LoadState(ctx)
	if got != gate.StatePausedQuiet {
		t.Errorf("LoadState after 5 saves = %v, want StatePausedQuiet", got)
	}
}

func TestGateAdapterSchemaVersion(t *testing.T) {
	s := openTestStore(t)
	var v int
	if err := s.DB().QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&v); err != nil {
		t.Fatalf("query schema_version: %v", err)
	}
	if v < 15 {
		t.Errorf("schemaVersion = %d, want >= 15", v)
	}
}

func TestGateAdapterAllStateValues(t *testing.T) {
	states := []gate.State{
		gate.StateRunning,
		gate.StatePausedDescriptive,
		gate.StatePausedQuiet,
		gate.StatePausedAfterApply,
	}
	for _, st := range states {
		t.Run(string(st), func(t *testing.T) {
			a := workforceadapter.NewGateAdapter(openTestStore(t))
			ctx := context.Background()
			if err := a.SaveState(ctx, st, "test"); err != nil {
				t.Fatalf("SaveState(%v): %v", st, err)
			}
			got, err := a.LoadState(ctx)
			if err != nil {
				t.Fatalf("LoadState: %v", err)
			}
			if got != st {
				t.Errorf("roundtrip: got %v, want %v", got, st)
			}
		})
	}
}

func TestGateAdapterNilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewGateAdapter(nil) should panic")
		}
	}()
	workforceadapter.NewGateAdapter(nil)
}

func TestGateAdapterLoadStateError(t *testing.T) {
	a := workforceadapter.ExportNewGateAdapterWithFailLoadState(openTestStore(t))
	_, err := a.LoadState(context.Background())
	if err == nil {
		t.Error("expected error from LoadState with injected failure")
	}
}

func TestGateAdapterSaveStateError(t *testing.T) {
	a := workforceadapter.ExportNewGateAdapterWithFailSaveState(openTestStore(t))
	err := a.SaveState(context.Background(), gate.StatePausedDescriptive, "test")
	if err == nil {
		t.Error("expected error from SaveState with injected failure")
	}
}

func TestGateAdapterClosedDBErrors(t *testing.T) {
	t.Run("LoadState SQL error", func(t *testing.T) {
		s := openTestStoreNoCleanup(t)
		a := workforceadapter.NewGateAdapter(s)
		_ = s.Close()
		_, err := a.LoadState(context.Background())
		if err == nil {
			t.Error("expected error from LoadState with closed DB")
		}
	})
	t.Run("SaveState SQL error", func(t *testing.T) {
		s := openTestStoreNoCleanup(t)
		a := workforceadapter.NewGateAdapter(s)
		_ = s.Close()
		err := a.SaveState(context.Background(), gate.StatePausedQuiet, "test")
		if err == nil {
			t.Error("expected error from SaveState with closed DB")
		}
	})
}

func TestGateAdapterLoadStateUnrecognized(t *testing.T) {

	s := openTestStore(t)
	_, err := s.DB().Exec(
		`INSERT INTO operator_gate_state (id, state, reason, updated_at) VALUES (1, 'running', 'boot', 0)
		 ON CONFLICT(id) DO UPDATE SET state='running', reason='boot', updated_at=0`,
	)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	a := workforceadapter.NewGateAdapter(s)
	got, err := a.LoadState(context.Background())
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if got != gate.StateRunning {
		t.Errorf("LoadState = %v, want StateRunning", got)
	}
}

func TestGateAdapterLoadStateUnrecognizedViaScan(t *testing.T) {

	s := openTestStore(t)

	_, err := s.DB().Exec(`PRAGMA ignore_check_constraints = ON`)
	if err != nil {

		_, err = s.DB().Exec(
			`INSERT OR REPLACE INTO operator_gate_state (id, state, reason, updated_at)
			 VALUES (1, 'unknown_xyz', 'bad data', 0)`)
		if err != nil {

			t.Skipf("cannot bypass CHECK constraint to test unrecognised state: %v", err)
		}
	} else {
		_, err = s.DB().Exec(
			`INSERT OR REPLACE INTO operator_gate_state (id, state, reason, updated_at)
			 VALUES (1, 'unknown_xyz', 'bad data', 0)`)
		if err != nil {
			t.Skipf("cannot insert bad state even with constraint disabled: %v", err)
		}
		_, _ = s.DB().Exec(`PRAGMA ignore_check_constraints = OFF`)
	}

	a := workforceadapter.NewGateAdapter(s)
	got, loadErr := a.LoadState(context.Background())
	if loadErr != nil {
		t.Fatalf("LoadState with unrecognised state: %v", loadErr)
	}
	if got != gate.StateRunning {
		t.Errorf("LoadState with unrecognised state = %v, want StateRunning", got)
	}
}

func TestGateAdapterLoadStateUnrecognizedViaSeam(t *testing.T) {

	a := workforceadapter.ExportNewGateAdapterWithUnrecognizedStateSeam(openTestStore(t))
	got, err := a.LoadState(context.Background())
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if got != gate.StateRunning {
		t.Errorf("LoadState with unrecognised state seam = %v, want StateRunning", got)
	}
}
