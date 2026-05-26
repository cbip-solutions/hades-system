package workforceadapter_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/workforceadapter"
	"github.com/cbip-solutions/hades-system/internal/store"
	"github.com/cbip-solutions/hades-system/internal/workforce/stream"
)

func TestStreamAdapterOpenCloseWindow(t *testing.T) {
	a := workforceadapter.NewStreamAdapter(openTestStore(t))
	ctx := context.Background()

	id, err := a.OpenWindow(ctx, stream.LayerL2, time.Now().UTC())
	if err != nil {
		t.Fatalf("OpenWindow: %v", err)
	}
	if id <= 0 {
		t.Errorf("OpenWindow returned id=%d, want >0", id)
	}

	ev := stream.Event{
		Type:        "checkpoint",
		Payload:     []byte(`{"step":1}`),
		PublishedAt: time.Now().UTC(),
	}
	if err := a.AppendEvent(ctx, id, ev); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if err := a.CloseWindow(ctx, id, time.Now().UTC(), 1); err != nil {
		t.Fatalf("CloseWindow: %v", err)
	}
}

func TestStreamAdapterLoadOpenWindows(t *testing.T) {
	a := workforceadapter.NewStreamAdapter(openTestStore(t))
	ctx := context.Background()

	id1, _ := a.OpenWindow(ctx, stream.LayerL2, time.Now().UTC())
	id2, _ := a.OpenWindow(ctx, stream.LayerL3, time.Now().UTC())
	_ = a.CloseWindow(ctx, id1, time.Now().UTC(), 0)

	open, err := a.LoadOpenWindows(ctx)
	if err != nil {
		t.Fatalf("LoadOpenWindows: %v", err)
	}
	found := false
	for _, w := range open {
		if w.WindowID == id2 {
			found = true
			if w.Layer != stream.LayerL3 {
				t.Errorf("Layer = %v, want L3", w.Layer)
			}
		}
		if w.WindowID == id1 {
			t.Errorf("closed window id1=%d should not appear in LoadOpenWindows", id1)
		}
	}
	if !found {
		t.Errorf("open window id2=%d not found in LoadOpenWindows", id2)
	}
}

func TestStreamAdapterPayloadJsonValidCheckEnforced(t *testing.T) {
	a := workforceadapter.NewStreamAdapter(openTestStore(t))
	ctx := context.Background()

	id, err := a.OpenWindow(ctx, stream.LayerL2, time.Now().UTC())
	if err != nil {
		t.Fatalf("OpenWindow: %v", err)
	}

	cases := []struct {
		name      string
		payload   []byte
		shouldErr bool
	}{
		{"valid json object", []byte(`{"step":1}`), false},
		{"valid json empty object", []byte(`{}`), false},
		{"valid json array", []byte(`[1,2,3]`), false},
		{"valid json string", []byte(`"hello"`), false},
		{"malformed json missing brace", []byte(`{"step":1`), true},
		{"empty payload", []byte(``), true},
		{"raw bytes", []byte(`not-json-at-all`), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := a.AppendEvent(ctx, id, stream.Event{
				Type:        "checkpoint",
				Payload:     tc.payload,
				PublishedAt: time.Now().UTC(),
			})
			if tc.shouldErr && err == nil {
				t.Errorf("AppendEvent with malformed payload %q should fail json_valid CHECK", tc.payload)
			}
			if !tc.shouldErr && err != nil {
				t.Errorf("AppendEvent with valid payload %q failed: %v", tc.payload, err)
			}
		})
	}
}

func TestStreamAdapterSchemaVersion(t *testing.T) {
	s := openTestStore(t)
	var v int
	if err := s.DB().QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&v); err != nil {
		t.Fatalf("query schema_version: %v", err)
	}
	if v < 16 {
		t.Errorf("schemaVersion = %d, want >= 16", v)
	}
}

func TestStreamAdapterRestartRecovery(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "recovery.db")

	// Session 1: open windows, add events, do NOT close
	{
		s, _ := store.Open(dbPath)
		_ = s.Migrate()
		a := workforceadapter.NewStreamAdapter(s)
		ctx := context.Background()
		id, _ := a.OpenWindow(ctx, stream.LayerL2, time.Now().UTC())
		_ = a.AppendEvent(ctx, id, stream.Event{Type: "checkpoint", Payload: []byte(`{}`), PublishedAt: time.Now().UTC()})
		_ = s.Close()
	}

	{
		s, _ := store.Open(dbPath)
		_ = s.Migrate()
		a := workforceadapter.NewStreamAdapter(s)
		open, err := a.LoadOpenWindows(context.Background())
		if err != nil {
			t.Fatalf("LoadOpenWindows after restart: %v", err)
		}
		if len(open) == 0 {
			t.Error("expected at least 1 open window after simulated restart, got 0")
		}
		_ = s.Close()
	}
}

func TestStreamAdapterNilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewStreamAdapter(nil) should panic")
		}
	}()
	workforceadapter.NewStreamAdapter(nil)
}

func TestStreamAdapterCloseWindowNotFound(t *testing.T) {
	a := workforceadapter.NewStreamAdapter(openTestStore(t))
	ctx := context.Background()

	err := a.CloseWindow(ctx, 99999, time.Now().UTC(), 0)
	if err == nil {
		t.Fatal("CloseWindow on non-existent window should return error")
	}
	if !errors.Is(err, workforceadapter.ErrWindowNotFound) {
		t.Errorf("CloseWindow non-existent error = %v, want errors.Is(ErrWindowNotFound)", err)
	}
}

func TestStreamAdapterCloseAlreadyClosed(t *testing.T) {
	a := workforceadapter.NewStreamAdapter(openTestStore(t))
	ctx := context.Background()
	id, _ := a.OpenWindow(ctx, stream.LayerL2, time.Now().UTC())
	_ = a.CloseWindow(ctx, id, time.Now().UTC(), 0)

	err := a.CloseWindow(ctx, id, time.Now().UTC(), 0)
	if err == nil {
		t.Fatal("CloseWindow on already-closed window should return error")
	}
	if !errors.Is(err, workforceadapter.ErrWindowAlreadyClosed) {
		t.Errorf("CloseWindow already-closed error = %v, want errors.Is(ErrWindowAlreadyClosed)", err)
	}

	if errors.Is(err, workforceadapter.ErrWindowNotFound) {
		t.Errorf("CloseWindow already-closed error = %v also matches ErrWindowNotFound; sentinels must be distinct", err)
	}
}

func TestStreamAdapterLoadOpenWindowsEmpty(t *testing.T) {
	a := workforceadapter.NewStreamAdapter(openTestStore(t))
	ctx := context.Background()
	open, err := a.LoadOpenWindows(ctx)
	if err != nil {
		t.Fatalf("LoadOpenWindows on empty db: %v", err)
	}
	if len(open) != 0 {
		t.Errorf("expected 0 open windows on empty db, got %d", len(open))
	}
}

func TestStreamAdapterCloseWindowSeamSuccess(t *testing.T) {

	a := workforceadapter.ExportNewStreamAdapterWithCloseWindowSuccess(openTestStore(t))
	err := a.CloseWindow(context.Background(), 1, time.Now().UTC(), 0)
	if err != nil {
		t.Errorf("expected nil from CloseWindow with n=1 seam: %v", err)
	}
}

func TestStreamAdapterLoadWindowsRealScanError(t *testing.T) {

	a := workforceadapter.ExportNewStreamAdapterWithRealScanError(openTestStore(t))
	_, err := a.LoadOpenWindows(context.Background())
	if err == nil {
		t.Error("expected error from LoadOpenWindows real scan error path")
	}
}

func TestStreamAdapterLoadWindowsScanRowError(t *testing.T) {

	s := openTestStore(t)

	normalA := workforceadapter.NewStreamAdapter(s)
	ctx := context.Background()
	_, err := normalA.OpenWindow(ctx, stream.LayerL2, time.Now().UTC())
	if err != nil {
		t.Fatalf("OpenWindow: %v", err)
	}

	a := workforceadapter.ExportNewStreamAdapterWithScanRowError(s)
	_, err = a.LoadOpenWindows(ctx)
	if err == nil {
		t.Error("expected error from LoadOpenWindows with scan row error")
	}
}

func TestStreamAdapterLoadWindowsRowsErrPath(t *testing.T) {

	s := openTestStore(t)
	a := workforceadapter.NewStreamAdapter(s)
	ctx := context.Background()

	_, err := a.OpenWindow(ctx, stream.LayerL2, time.Now().UTC())
	if err != nil {
		t.Fatalf("OpenWindow: %v", err)
	}

	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()

	_, _ = a.LoadOpenWindows(cancelCtx)
}

func TestStreamAdapterOpenWindowError(t *testing.T) {
	a := workforceadapter.ExportNewStreamAdapterWithFailOpenWindow(openTestStore(t))
	_, err := a.OpenWindow(context.Background(), stream.LayerL2, time.Now().UTC())
	if err == nil {
		t.Error("expected error from OpenWindow with injected failure")
	}
}

func TestStreamAdapterAppendEventError(t *testing.T) {
	a := workforceadapter.ExportNewStreamAdapterWithFailAppendEvent(openTestStore(t))
	err := a.AppendEvent(context.Background(), 1, stream.Event{
		Type: "x", Payload: []byte(`{}`), PublishedAt: time.Now().UTC(),
	})
	if err == nil {
		t.Error("expected error from AppendEvent with injected failure")
	}
}

func TestStreamAdapterCloseWindowError(t *testing.T) {
	a := workforceadapter.ExportNewStreamAdapterWithFailCloseWindow(openTestStore(t))
	err := a.CloseWindow(context.Background(), 1, time.Now().UTC(), 0)
	if err == nil {
		t.Error("expected error from CloseWindow with injected exec failure")
	}
}

func TestStreamAdapterCloseWindowZeroRows(t *testing.T) {
	a := workforceadapter.ExportNewStreamAdapterWithZeroCloseRows(openTestStore(t))
	err := a.CloseWindow(context.Background(), 1, time.Now().UTC(), 0)
	if err == nil {
		t.Error("expected error from CloseWindow with 0 rows affected")
	}
}

func TestStreamAdapterLoadOpenWindowsError(t *testing.T) {
	a := workforceadapter.ExportNewStreamAdapterWithFailLoadWindows(openTestStore(t))
	_, err := a.LoadOpenWindows(context.Background())
	if err == nil {
		t.Error("expected error from LoadOpenWindows with injected failure")
	}
}

func TestStreamAdapterLoadWindowsScanError(t *testing.T) {
	a := workforceadapter.ExportNewStreamAdapterWithScanError(openTestStore(t))
	_, err := a.LoadOpenWindows(context.Background())
	if err == nil {
		t.Error("expected error from LoadOpenWindows with scan error")
	}
}

func TestStreamAdapterCloseDisambiguateSelectError(t *testing.T) {
	a := workforceadapter.ExportNewStreamAdapterWithDisambiguateSelectError(openTestStore(t))
	ctx := context.Background()

	err := a.CloseWindow(ctx, 99999, time.Now().UTC(), 0)
	if err == nil {
		t.Fatal("CloseWindow with disambiguate select error should return error")
	}
	if errors.Is(err, workforceadapter.ErrWindowNotFound) || errors.Is(err, workforceadapter.ErrWindowAlreadyClosed) {
		t.Errorf("CloseWindow with select-error returned sentinel %v; expected raw wrapped scan error", err)
	}
}

func TestStreamAdapterCloseDisambiguateUnexpectedStatus(t *testing.T) {
	a := workforceadapter.ExportNewStreamAdapterWithDisambiguateUnexpectedStatus(openTestStore(t))
	ctx := context.Background()
	err := a.CloseWindow(ctx, 99999, time.Now().UTC(), 0)
	if err == nil {
		t.Fatal("CloseWindow with unexpected status should return error")
	}
	if !errors.Is(err, workforceadapter.ErrWindowNotFound) {
		t.Errorf("CloseWindow with unexpected-status returned %v; want errors.Is(ErrWindowNotFound)", err)
	}
}

func TestStreamAdapterClosedDBErrors(t *testing.T) {
	t.Run("OpenWindow SQL error", func(t *testing.T) {
		s := openTestStoreNoCleanup(t)
		a := workforceadapter.NewStreamAdapter(s)
		_ = s.Close()
		_, err := a.OpenWindow(context.Background(), stream.LayerL2, time.Now().UTC())
		if err == nil {
			t.Error("expected error from OpenWindow with closed DB")
		}
	})
	t.Run("AppendEvent SQL error", func(t *testing.T) {
		s := openTestStoreNoCleanup(t)
		a := workforceadapter.NewStreamAdapter(s)
		_ = s.Close()
		err := a.AppendEvent(context.Background(), 1, stream.Event{
			Type: "x", Payload: []byte(`{}`), PublishedAt: time.Now().UTC(),
		})
		if err == nil {
			t.Error("expected error from AppendEvent with closed DB")
		}
	})
	t.Run("CloseWindow SQL error", func(t *testing.T) {
		s := openTestStoreNoCleanup(t)
		a := workforceadapter.NewStreamAdapter(s)
		_ = s.Close()
		err := a.CloseWindow(context.Background(), 1, time.Now().UTC(), 0)
		if err == nil {
			t.Error("expected error from CloseWindow with closed DB")
		}
	})
	t.Run("LoadOpenWindows SQL error", func(t *testing.T) {
		s := openTestStoreNoCleanup(t)
		a := workforceadapter.NewStreamAdapter(s)
		_ = s.Close()
		_, err := a.LoadOpenWindows(context.Background())
		if err == nil {
			t.Error("expected error from LoadOpenWindows with closed DB")
		}
	})
}
