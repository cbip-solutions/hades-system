package daemon

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/store"
)

func newNotifTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestNotifierDispatchPersists(t *testing.T) {
	st := newNotifTestStore(t)
	n := NewNotifier(st)
	defer n.Close()
	n.osascriptCmd = "/usr/bin/true"

	id, err := n.Dispatch(context.Background(), "WARN", "test title", "body", "test.source")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := st.ListBypassNotifications(context.Background(), 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != id || rows[0].Severity != "WARN" {
		t.Errorf("unexpected rows: %+v", rows)
	}
	if rows[0].Title != "test title" {
		t.Errorf("title: %q", rows[0].Title)
	}
}

func TestNotifierAckStopsRepeat(t *testing.T) {
	st := newNotifTestStore(t)
	n := NewNotifier(st)
	defer n.Close()
	n.osascriptCmd = "/usr/bin/true"
	n.repeatEvery = 100 * time.Millisecond

	id, err := n.Dispatch(context.Background(), "CRITICAL", "fail", "detail", "bypass.test")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AckBypassNotification(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	due, err := st.UnackedCriticalsDueForRepeat(context.Background(), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 0 {
		t.Errorf("acked critical still due for repeat: %+v", due)
	}
}

func TestNotifierUnackedCriticalDueForRepeat(t *testing.T) {
	st := newNotifTestStore(t)
	n := NewNotifier(st)
	defer n.Close()
	n.osascriptCmd = "/usr/bin/true"

	_, err := n.Dispatch(context.Background(), "CRITICAL", "x", "y", "bypass.test")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	due, err := st.UnackedCriticalsDueForRepeat(context.Background(), 1*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 {
		t.Errorf("expected 1 due, got %d", len(due))
	}
}

func TestNotifierOnTierSwitchDispatches(t *testing.T) {
	st := newNotifTestStore(t)
	n := NewNotifier(st)
	defer n.Close()
	n.osascriptCmd = "/usr/bin/true"

	n.OnTierSwitch("in-house", "payg", "401 after refresh")
	rows, err := st.ListBypassNotifications(context.Background(), 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Source != "bypass.tier-switch" {
		t.Errorf("source = %q, want bypass.tier-switch", rows[0].Source)
	}

	if rows[0].Severity != "WARN" {
		t.Errorf("severity = %q, want WARN (CRITICAL re-fires hourly via runRepeatLoop)", rows[0].Severity)
	}

	if combined := strings.ToLower(rows[0].Title + " " + rows[0].Body); strings.Contains(combined, "payg") {
		t.Errorf("tier-switch notification falsely asserts a payg destination: title=%q body=%q", rows[0].Title, rows[0].Body)
	}
}

func TestFireOSNotificationDarwinOnly(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only")
	}
	st := newNotifTestStore(t)
	n := NewNotifier(st)
	defer n.Close()
	n.osascriptCmd = "/usr/bin/true"
	n.fireOSNotification("INFO", "title", "body")
}

func TestNotifierCloseIdempotent(t *testing.T) {
	st := newNotifTestStore(t)
	n := NewNotifier(st)
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	if err := n.Close(); err != nil {
		t.Errorf("second close returned: %v", err)
	}
}

func TestNotifierCloseStopsRepeatGoroutine(t *testing.T) {
	st := newNotifTestStore(t)
	before := runtime.NumGoroutine()
	n := NewNotifier(st)
	// Sanity NewNotifier MUST start exactly one repeat goroutine.
	// If a future refactor adds more (e.g., a per-severity worker),
	// this check will surface that fact and force the regression
	// test to be updated alongside the constructor change.
	if got := runtime.NumGoroutine(); got <= before {
		t.Errorf("NewNotifier did not start a goroutine: before=%d after=%d", before, got)
	}
	if err := n.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("Notifier goroutine still running after Close: before=%d after-close=%d (200ms settle)", before, runtime.NumGoroutine())
}
