package tmuxlife

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDriftTypeEnumExhaustive(t *testing.T) {
	cases := []struct {
		dt   DriftType
		name string
	}{
		{DriftPaneAdded, "PaneAdded"},
		{DriftPaneRemoved, "PaneRemoved"},
		{DriftWindowKilled, "WindowKilled"},
		{DriftWindowRenamed, "WindowRenamed"},
	}
	seen := map[DriftType]bool{}
	for _, c := range cases {
		if seen[c.dt] {
			t.Errorf("DriftType %v reused", c.dt)
		}
		seen[c.dt] = true
		if got := c.dt.String(); got != c.name {
			t.Errorf("%v.String() = %q, want %q", c.dt, got, c.name)
		}
	}
}

func TestDriftTypeUnknownStringFormat(t *testing.T) {
	got := DriftType(99).String()
	if got != "DriftType(99)" {
		t.Errorf("DriftType(99).String() = %q, want %q", got, "DriftType(99)")
	}
}

func TestLayoutDriftZeroValueInvalid(t *testing.T) {
	var d LayoutDrift
	if d.IsValid() {
		t.Error("zero LayoutDrift reports IsValid() = true; should be false")
	}
	d2 := LayoutDrift{
		SessionName: "zen-internal-platform-x-deadbeef",
		Window:      WindowOrch,
		Type:        DriftPaneAdded,
		ObservedAt:  time.Now(),
	}
	if !d2.IsValid() {
		t.Error("populated LayoutDrift reports IsValid() = false")
	}
}

func TestLayoutDriftIsValidRejectsOutOfRangeType(t *testing.T) {
	d := LayoutDrift{
		SessionName: "zen-internal-platform-x-deadbeef",
		Window:      WindowOrch,
		Type:        DriftType(42),
	}
	if d.IsValid() {
		t.Error("LayoutDrift with out-of-range Type reports IsValid() = true; should be false")
	}
}

func TestLayoutDriftIsValidRejectsEmptyWindow(t *testing.T) {
	d := LayoutDrift{
		SessionName: "zen-internal-platform-x-deadbeef",
		Type:        DriftPaneAdded,
	}
	if d.IsValid() {
		t.Error("LayoutDrift with empty Window reports IsValid() = true; should be false")
	}
}

func TestLayoutDriftStringFormat(t *testing.T) {
	d := LayoutDrift{
		SessionName: "zen-internal-platform-x-deadbeef",
		Window:      WindowWorkers,
		Type:        DriftPaneRemoved,
	}
	got := d.String()
	want := "zen-internal-platform-x-deadbeef:workers PaneRemoved"
	if got != want {
		t.Errorf("LayoutDrift.String() = %q, want %q", got, want)
	}
}

type fakeDriftEmitter struct {
	mu     sync.Mutex
	events []LayoutDrift
}

func (f *fakeDriftEmitter) Emit(d LayoutDrift) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, d)
}
func (f *fakeDriftEmitter) Events() []LayoutDrift {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]LayoutDrift, len(f.events))
	copy(out, f.events)
	return out
}

type fakePaneLister struct {
	mu       sync.Mutex
	byWindow map[string][]paneRecord
	err      error

	queriedKeys []string
}

func (f *fakePaneLister) ListPanes(_ context.Context, sessionName string, window WindowName) ([]paneRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := sessionName + ":" + string(window)
	f.queriedKeys = append(f.queriedKeys, key)
	if f.err != nil {
		return nil, f.err
	}
	return f.byWindow[key], nil
}

func (f *fakePaneLister) queries() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.queriedKeys))
	copy(out, f.queriedKeys)
	return out
}

func TestDriftPollerScratchExcluded(t *testing.T) {
	emitter := &fakeDriftEmitter{}
	store := newFakeSessionStore()
	_ = store.UpsertSession(Session{
		Alias:  "internal-platform-x",
		Sha8:   "deadbeef",
		Name:   "zen-internal-platform-x-deadbeef",
		Status: StatusActive,
	})
	lister := &fakePaneLister{
		byWindow: map[string][]paneRecord{

			"zen-internal-platform-x-deadbeef:orch":    {{ID: "%0", Title: "orch.0"}},
			"zen-internal-platform-x-deadbeef:leads":   {{ID: "%1", Title: "leads.0"}},
			"zen-internal-platform-x-deadbeef:workers": {{ID: "%2", Title: "workers.0"}},
			"zen-internal-platform-x-deadbeef:hra":     {{ID: "%3", Title: "hra.0"}},
			"zen-internal-platform-x-deadbeef:logs":    {{ID: "%4", Title: "logs.0"}},
		},
	}
	store.setExpectedPanes("zen-internal-platform-x-deadbeef", map[WindowName][]string{
		WindowOrch:    {"%0"},
		WindowLeads:   {"%1"},
		WindowWorkers: {"%2"},
		WindowHRA:     {"%3"},
		WindowLogs:    {"%4"},
	})
	poller := NewDriftPoller(store, lister, emitter, 5*time.Second)

	if err := poller.tick(context.Background()); err != nil {
		t.Fatalf("tick err: %v", err)
	}

	for _, k := range lister.queries() {
		if strings.HasSuffix(k, ":scratch") {
			t.Errorf("ListPanes called for scratch %q; inv-zen-118 violated", k)
		}
	}
	if got := emitter.Events(); len(got) != 0 {
		t.Errorf("expected 0 drift events on healthy layout, got %d: %+v", len(got), got)
	}
}

func TestDriftPollerDetectsPaneAdded(t *testing.T) {
	emitter := &fakeDriftEmitter{}
	store := newFakeSessionStore()
	_ = store.UpsertSession(Session{
		Alias: "internal-platform-x", Sha8: "deadbeef",
		Name: "zen-internal-platform-x-deadbeef", Status: StatusActive,
	})
	lister := &fakePaneLister{
		byWindow: map[string][]paneRecord{
			"zen-internal-platform-x-deadbeef:orch":    {{ID: "%0"}, {ID: "%99", Title: "operator-extra"}},
			"zen-internal-platform-x-deadbeef:leads":   {{ID: "%1"}},
			"zen-internal-platform-x-deadbeef:workers": {{ID: "%2"}},
			"zen-internal-platform-x-deadbeef:hra":     {{ID: "%3"}},
			"zen-internal-platform-x-deadbeef:logs":    {{ID: "%4"}},
		},
	}
	store.setExpectedPanes("zen-internal-platform-x-deadbeef", map[WindowName][]string{
		WindowOrch: {"%0"}, WindowLeads: {"%1"}, WindowWorkers: {"%2"},
		WindowHRA: {"%3"}, WindowLogs: {"%4"},
	})
	poller := NewDriftPoller(store, lister, emitter, time.Second)

	if err := poller.tick(context.Background()); err != nil {
		t.Fatalf("tick err: %v", err)
	}
	events := emitter.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 drift event, got %d: %+v", len(events), events)
	}
	if events[0].Type != DriftPaneAdded {
		t.Errorf("type = %v, want DriftPaneAdded", events[0].Type)
	}
	if events[0].Window != WindowOrch {
		t.Errorf("window = %v, want orch", events[0].Window)
	}
	if events[0].PaneTitle != "operator-extra" {
		t.Errorf("paneTitle = %q, want operator-extra", events[0].PaneTitle)
	}
	if events[0].SessionName != "zen-internal-platform-x-deadbeef" {
		t.Errorf("sessionName = %q, want zen-internal-platform-x-deadbeef", events[0].SessionName)
	}
	if events[0].ObservedAt.IsZero() {
		t.Error("ObservedAt is zero; expected synthetic clock value")
	}
}

func TestDriftPollerDetectsPaneRemoved(t *testing.T) {
	emitter := &fakeDriftEmitter{}
	store := newFakeSessionStore()
	_ = store.UpsertSession(Session{
		Alias: "internal-platform-x", Sha8: "deadbeef",
		Name: "zen-internal-platform-x-deadbeef", Status: StatusActive,
	})
	lister := &fakePaneLister{
		byWindow: map[string][]paneRecord{

			"zen-internal-platform-x-deadbeef:orch":    {{ID: "%0"}},
			"zen-internal-platform-x-deadbeef:leads":   {{ID: "%1"}},
			"zen-internal-platform-x-deadbeef:workers": {{ID: "%2"}},
			"zen-internal-platform-x-deadbeef:hra":     {{ID: "%3"}},
			"zen-internal-platform-x-deadbeef:logs":    {{ID: "%4"}},
		},
	}
	store.setExpectedPanes("zen-internal-platform-x-deadbeef", map[WindowName][]string{
		WindowOrch:    {"%0", "%5"},
		WindowLeads:   {"%1"},
		WindowWorkers: {"%2"},
		WindowHRA:     {"%3"},
		WindowLogs:    {"%4"},
	})
	poller := NewDriftPoller(store, lister, emitter, time.Second)
	if err := poller.tick(context.Background()); err != nil {
		t.Fatalf("tick err: %v", err)
	}
	events := emitter.Events()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 drift event, got %d: %+v", len(events), events)
	}
	if events[0].Type != DriftPaneRemoved {
		t.Errorf("type = %v, want DriftPaneRemoved", events[0].Type)
	}
	if events[0].Window != WindowOrch {
		t.Errorf("window = %v, want orch", events[0].Window)
	}
	if events[0].PaneTitle != "%5" {
		t.Errorf("paneTitle = %q, want %%5 (expected pane id surfaced)", events[0].PaneTitle)
	}
}

func TestDriftPollerDetectsWindowKilled(t *testing.T) {
	emitter := &fakeDriftEmitter{}
	store := newFakeSessionStore()
	_ = store.UpsertSession(Session{
		Alias: "internal-platform-x", Sha8: "deadbeef",
		Name: "zen-internal-platform-x-deadbeef", Status: StatusActive,
	})
	lister := &fakePaneLister{
		byWindow: map[string][]paneRecord{

			"zen-internal-platform-x-deadbeef:orch":    {{ID: "%0"}},
			"zen-internal-platform-x-deadbeef:leads":   {{ID: "%1"}},
			"zen-internal-platform-x-deadbeef:workers": {{ID: "%2"}},
			"zen-internal-platform-x-deadbeef:logs":    {{ID: "%4"}},
		},
	}

	lister.byWindow["zen-internal-platform-x-deadbeef:hra"] = nil
	store.setExpectedPanes("zen-internal-platform-x-deadbeef", map[WindowName][]string{
		WindowOrch: {"%0"}, WindowLeads: {"%1"}, WindowWorkers: {"%2"},
		WindowHRA: {"%3"}, WindowLogs: {"%4"},
	})
	poller := NewDriftPoller(store, lister, emitter, time.Second)
	if err := poller.tick(context.Background()); err != nil {
		t.Fatalf("tick err: %v", err)
	}
	events := emitter.Events()
	found := false
	for _, e := range events {
		if e.Type == DriftWindowKilled && e.Window == WindowHRA {
			found = true
			if e.SessionName != "zen-internal-platform-x-deadbeef" {
				t.Errorf("sessionName = %q, want zen-internal-platform-x-deadbeef", e.SessionName)
			}
		}
	}
	if !found {
		t.Errorf("expected DriftWindowKilled for hra; got %+v", events)
	}
}

func TestDriftPollerListerErrorContinues(t *testing.T) {
	emitter := &fakeDriftEmitter{}
	store := newFakeSessionStore()
	_ = store.UpsertSession(Session{
		Alias: "internal-platform-x", Sha8: "deadbeef",
		Name: "zen-internal-platform-x-deadbeef", Status: StatusActive,
	})
	_ = store.UpsertSession(Session{
		Alias: "nexus", Sha8: "12345678",
		Name: "zen-nexus-12345678", Status: StatusActive,
	})
	lister := &fakePaneLister{err: errors.New("tmux server unavailable")}
	poller := NewDriftPoller(store, lister, emitter, time.Second)
	sink := &testLogSink{}
	poller.logger = sink.logger()

	if err := poller.tick(context.Background()); err != nil {
		t.Fatalf("tick err: %v", err)
	}

	if got := emitter.Events(); len(got) != 0 {
		t.Errorf("expected 0 drift events when lister errors, got %d", len(got))
	}

	if !sink.contains("ListPanes") {
		t.Errorf("expected log to mention ListPanes, got %q", sink.string())
	}
	if !sink.contains("tmux server unavailable") {
		t.Errorf("expected log to wrap underlying error, got %q", sink.string())
	}
}

func TestDriftPollerSkipsNonActiveSessions(t *testing.T) {
	emitter := &fakeDriftEmitter{}
	store := newFakeSessionStore()
	_ = store.UpsertSession(Session{
		Alias: "archived", Sha8: "aaaaaaaa",
		Name: "zen-archived-aaaaaaaa", Status: StatusArchived,
	})
	_ = store.UpsertSession(Session{
		Alias: "idle", Sha8: "bbbbbbbb",
		Name: "zen-idle-bbbbbbbb", Status: StatusIdle,
	})
	_ = store.UpsertSession(Session{
		Alias: "orphaned", Sha8: "cccccccc",
		Name: "zen-orphaned-cccccccc", Status: StatusOrphaned,
	})
	lister := &fakePaneLister{}
	poller := NewDriftPoller(store, lister, emitter, time.Second)
	if err := poller.tick(context.Background()); err != nil {
		t.Fatalf("tick err: %v", err)
	}

	if q := lister.queries(); len(q) != 0 {
		t.Errorf("ListPanes called %d times for non-Active sessions; want 0; queries: %v", len(q), q)
	}
	if got := emitter.Events(); len(got) != 0 {
		t.Errorf("expected 0 drift events for non-Active sessions, got %d: %+v", len(got), got)
	}
}

func TestDriftPollerExpectedPanesForErrorContinues(t *testing.T) {
	emitter := &fakeDriftEmitter{}
	inner := newFakeSessionStore()
	_ = inner.UpsertSession(Session{
		Alias: "internal-platform-x", Sha8: "deadbeef",
		Name: "zen-internal-platform-x-deadbeef", Status: StatusActive,
	})
	_ = inner.UpsertSession(Session{
		Alias: "nexus", Sha8: "12345678",
		Name: "zen-nexus-12345678", Status: StatusActive,
	})

	inner.setExpectedPanes("zen-nexus-12345678", map[WindowName][]string{
		WindowOrch: {"%10"}, WindowLeads: {"%11"}, WindowWorkers: {"%12"},
		WindowHRA: {"%13"}, WindowLogs: {"%14"},
	})
	failing := &expectedPanesFailingStore{
		fakeSessionStore: inner,
		expectedPanesFor: "zen-internal-platform-x-deadbeef",
		expectedPanesErr: errors.New("daemon.db disk full"),
	}
	lister := &fakePaneLister{
		byWindow: map[string][]paneRecord{
			"zen-nexus-12345678:orch":    {{ID: "%10"}},
			"zen-nexus-12345678:leads":   {{ID: "%11"}},
			"zen-nexus-12345678:workers": {{ID: "%12"}},
			"zen-nexus-12345678:hra":     {{ID: "%13"}},
			"zen-nexus-12345678:logs":    {{ID: "%14"}},
		},
	}
	poller := NewDriftPoller(failing, lister, emitter, time.Second)
	sink := &testLogSink{}
	poller.logger = sink.logger()

	if err := poller.tick(context.Background()); err != nil {
		t.Fatalf("tick err: %v", err)
	}

	if got := emitter.Events(); len(got) != 0 {
		t.Errorf("expected 0 drift events (nexus healthy + internal-platform-x skipped), got %d: %+v", len(got), got)
	}
	if !sink.contains("ExpectedPanesFor") {
		t.Errorf("expected log to mention ExpectedPanesFor, got %q", sink.string())
	}
	if !sink.contains("daemon.db disk full") {
		t.Errorf("expected log to wrap underlying error, got %q", sink.string())
	}
}

func TestDriftPollerListSessionsErrorReturned(t *testing.T) {
	emitter := &fakeDriftEmitter{}
	failing := &failingStore{
		fakeSessionStore: newFakeSessionStore(),
		listErr:          errors.New("disk full"),
	}
	lister := &fakePaneLister{}
	poller := NewDriftPoller(failing, lister, emitter, time.Second)
	err := poller.tick(context.Background())
	if err == nil {
		t.Fatalf("tick: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "ListSessions") {
		t.Errorf("err = %v; expected wrap referencing ListSessions", err)
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Errorf("err = %v; expected wrap of underlying %q", err, "disk full")
	}
}

func TestDriftPollerObservedAtUsesSyntheticClock(t *testing.T) {
	emitter := &fakeDriftEmitter{}
	store := newFakeSessionStore()
	_ = store.UpsertSession(Session{
		Alias: "internal-platform-x", Sha8: "deadbeef",
		Name: "zen-internal-platform-x-deadbeef", Status: StatusActive,
	})
	lister := &fakePaneLister{
		byWindow: map[string][]paneRecord{
			"zen-internal-platform-x-deadbeef:orch":    {{ID: "%0"}, {ID: "%9", Title: "extra"}},
			"zen-internal-platform-x-deadbeef:leads":   {{ID: "%1"}},
			"zen-internal-platform-x-deadbeef:workers": {{ID: "%2"}},
			"zen-internal-platform-x-deadbeef:hra":     {{ID: "%3"}},
			"zen-internal-platform-x-deadbeef:logs":    {{ID: "%4"}},
		},
	}
	store.setExpectedPanes("zen-internal-platform-x-deadbeef", map[WindowName][]string{
		WindowOrch: {"%0"}, WindowLeads: {"%1"}, WindowWorkers: {"%2"},
		WindowHRA: {"%3"}, WindowLogs: {"%4"},
	})
	frozen := time.Date(2026, 5, 6, 12, 30, 0, 0, time.UTC)
	poller := NewDriftPoller(store, lister, emitter, time.Second)
	poller.now = func() time.Time { return frozen }

	if err := poller.tick(context.Background()); err != nil {
		t.Fatalf("tick err: %v", err)
	}
	events := emitter.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 drift event, got %d", len(events))
	}
	if !events[0].ObservedAt.Equal(frozen) {
		t.Errorf("ObservedAt = %v, want frozen %v", events[0].ObservedAt, frozen)
	}
}

func TestDriftPollerMultipleAddedSortedDeterministic(t *testing.T) {
	emitter := &fakeDriftEmitter{}
	store := newFakeSessionStore()
	_ = store.UpsertSession(Session{
		Alias: "internal-platform-x", Sha8: "deadbeef",
		Name: "zen-internal-platform-x-deadbeef", Status: StatusActive,
	})
	lister := &fakePaneLister{
		byWindow: map[string][]paneRecord{
			"zen-internal-platform-x-deadbeef:orch":    {{ID: "%0"}, {ID: "%30", Title: "c"}, {ID: "%10", Title: "a"}, {ID: "%20", Title: "b"}},
			"zen-internal-platform-x-deadbeef:leads":   {{ID: "%1"}},
			"zen-internal-platform-x-deadbeef:workers": {{ID: "%2"}},
			"zen-internal-platform-x-deadbeef:hra":     {{ID: "%3"}},
			"zen-internal-platform-x-deadbeef:logs":    {{ID: "%4"}},
		},
	}
	store.setExpectedPanes("zen-internal-platform-x-deadbeef", map[WindowName][]string{
		WindowOrch: {"%0"}, WindowLeads: {"%1"}, WindowWorkers: {"%2"},
		WindowHRA: {"%3"}, WindowLogs: {"%4"},
	})
	poller := NewDriftPoller(store, lister, emitter, time.Second)
	if err := poller.tick(context.Background()); err != nil {
		t.Fatalf("tick err: %v", err)
	}
	events := emitter.Events()
	if len(events) != 3 {
		t.Fatalf("expected 3 PaneAdded events, got %d: %+v", len(events), events)
	}
	wantTitles := []string{"a", "b", "c"}
	for i, w := range wantTitles {
		if events[i].PaneTitle != w {
			t.Errorf("events[%d].PaneTitle = %q, want %q (deterministic order)", i, events[i].PaneTitle, w)
		}
	}
}

func TestDriftPollerMultipleRemovedSortedDeterministic(t *testing.T) {
	emitter := &fakeDriftEmitter{}
	store := newFakeSessionStore()
	_ = store.UpsertSession(Session{
		Alias: "internal-platform-x", Sha8: "deadbeef",
		Name: "zen-internal-platform-x-deadbeef", Status: StatusActive,
	})
	lister := &fakePaneLister{
		byWindow: map[string][]paneRecord{

			"zen-internal-platform-x-deadbeef:orch":    {{ID: "%0"}},
			"zen-internal-platform-x-deadbeef:leads":   {{ID: "%1"}},
			"zen-internal-platform-x-deadbeef:workers": {{ID: "%2"}},
			"zen-internal-platform-x-deadbeef:hra":     {{ID: "%3"}},
			"zen-internal-platform-x-deadbeef:logs":    {{ID: "%4"}},
		},
	}
	store.setExpectedPanes("zen-internal-platform-x-deadbeef", map[WindowName][]string{
		WindowOrch:    {"%0", "%30", "%10", "%20"},
		WindowLeads:   {"%1"},
		WindowWorkers: {"%2"},
		WindowHRA:     {"%3"},
		WindowLogs:    {"%4"},
	})
	poller := NewDriftPoller(store, lister, emitter, time.Second)
	if err := poller.tick(context.Background()); err != nil {
		t.Fatalf("tick err: %v", err)
	}
	events := emitter.Events()
	if len(events) != 3 {
		t.Fatalf("expected 3 PaneRemoved events, got %d: %+v", len(events), events)
	}
	wantTitles := []string{"%10", "%20", "%30"}
	for i, w := range wantTitles {
		if events[i].PaneTitle != w {
			t.Errorf("events[%d].PaneTitle = %q, want %q (deterministic order)", i, events[i].PaneTitle, w)
		}
	}
}

func TestDriftPollerHealthyNoEvents(t *testing.T) {
	emitter := &fakeDriftEmitter{}
	store := newFakeSessionStore()
	_ = store.UpsertSession(Session{
		Alias: "internal-platform-x", Sha8: "deadbeef",
		Name: "zen-internal-platform-x-deadbeef", Status: StatusActive,
	})
	lister := &fakePaneLister{
		byWindow: map[string][]paneRecord{

			"zen-internal-platform-x-deadbeef:orch":    {{ID: "%2"}, {ID: "%0"}, {ID: "%1"}},
			"zen-internal-platform-x-deadbeef:leads":   {{ID: "%5"}},
			"zen-internal-platform-x-deadbeef:workers": {{ID: "%6"}},
			"zen-internal-platform-x-deadbeef:hra":     {{ID: "%7"}},
			"zen-internal-platform-x-deadbeef:logs":    {{ID: "%9"}, {ID: "%8"}},
		},
	}
	store.setExpectedPanes("zen-internal-platform-x-deadbeef", map[WindowName][]string{
		WindowOrch:    {"%0", "%1", "%2"},
		WindowLeads:   {"%5"},
		WindowWorkers: {"%6"},
		WindowHRA:     {"%7"},
		WindowLogs:    {"%8", "%9"},
	})
	poller := NewDriftPoller(store, lister, emitter, time.Second)
	if err := poller.tick(context.Background()); err != nil {
		t.Fatalf("tick err: %v", err)
	}
	if got := emitter.Events(); len(got) != 0 {
		t.Errorf("expected 0 drift events on healthy multi-pane layout, got %d: %+v", len(got), got)
	}
}

// TestDriftPollerWindowKilledNotEmittedWhenExpectedEmpty asserts the
// edge case: if expected is also empty (no panes registered for a
// daemon-owned window — pre-CreateWindows or stale row), we do NOT
// emit DriftWindowKilled. The poller's contract: only emit when
// expected non-empty AND actual empty.
func TestDriftPollerWindowKilledNotEmittedWhenExpectedEmpty(t *testing.T) {
	emitter := &fakeDriftEmitter{}
	store := newFakeSessionStore()
	_ = store.UpsertSession(Session{
		Alias: "internal-platform-x", Sha8: "deadbeef",
		Name: "zen-internal-platform-x-deadbeef", Status: StatusActive,
	})
	lister := &fakePaneLister{
		byWindow: map[string][]paneRecord{},
	}

	poller := NewDriftPoller(store, lister, emitter, time.Second)
	if err := poller.tick(context.Background()); err != nil {
		t.Fatalf("tick err: %v", err)
	}
	if got := emitter.Events(); len(got) != 0 {
		t.Errorf("expected 0 drift events when both expected and actual are empty, got %d: %+v", len(got), got)
	}
}

func TestDriftPollerRunCycleStops(t *testing.T) {
	emitter := &fakeDriftEmitter{}
	store := newFakeSessionStore()
	lister := &fakePaneLister{}
	poller := NewDriftPoller(store, lister, emitter, 10*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		poller.Run(ctx)
		close(done)
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case <-done:

	case <-time.After(time.Second):
		t.Error("DriftPoller.Run did not stop within 1s of ctx cancel")
	}
}

func TestDriftPollerRunLogsTickError(t *testing.T) {
	emitter := &fakeDriftEmitter{}
	failing := &failingStore{
		fakeSessionStore: newFakeSessionStore(),
		listErr:          errors.New("transient"),
	}
	lister := &fakePaneLister{}
	poller := NewDriftPoller(failing, lister, emitter, 10*time.Millisecond)
	sink := &testLogSink{}
	poller.logger = sink.logger()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		poller.Run(ctx)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("DriftPoller.Run did not stop within 1s of ctx cancel")
	}
	if !sink.contains("DriftPoller") {
		t.Errorf("expected log to contain 'DriftPoller', got %q", sink.string())
	}
	if !sink.contains("transient") {
		t.Errorf("expected log to contain underlying error 'transient', got %q", sink.string())
	}
}

func TestNewDriftPollerDefaultsZeroIntervalTo5s(t *testing.T) {
	emitter := &fakeDriftEmitter{}
	store := newFakeSessionStore()
	lister := &fakePaneLister{}
	p := NewDriftPoller(store, lister, emitter, 0)
	if p.interval != 5*time.Second {
		t.Errorf("interval = %v, want 5s (spec §4.1 default)", p.interval)
	}
}

func TestNewDriftPollerDefaultsNegativeIntervalTo5s(t *testing.T) {
	emitter := &fakeDriftEmitter{}
	store := newFakeSessionStore()
	lister := &fakePaneLister{}
	p := NewDriftPoller(store, lister, emitter, -1*time.Second)
	if p.interval != 5*time.Second {
		t.Errorf("interval = %v, want 5s (negative coerced to default)", p.interval)
	}
}

func TestRealPaneListerImplementsPaneLister(t *testing.T) {
	var _ PaneLister = RealPaneLister{}
}

func TestRealPaneListerListPanesParsesFakeOutput(t *testing.T) {
	tmp := t.TempDir()

	script := "#!/bin/sh\nprintf '%%0|orch.0\\n%%1|orch.1\\n'\nexit 0\n"
	if err := writeExec(tmp+"/tmux", script); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", tmp+":"+pathEnv())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got, err := RealPaneLister{}.ListPanes(ctx, "zen-test-12345678", WindowOrch)
	if err != nil {
		t.Fatalf("ListPanes happy = %v; want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(records) = %d; want 2; got %+v", len(got), got)
	}
	if got[0].ID != "%0" || got[0].Title != "orch.0" {
		t.Errorf("records[0] = %+v; want {ID:%%0, Title:orch.0}", got[0])
	}
	if got[1].ID != "%1" || got[1].Title != "orch.1" {
		t.Errorf("records[1] = %+v; want {ID:%%1, Title:orch.1}", got[1])
	}
}

func TestRealPaneListerListPanesReturnsNilOnTmuxError(t *testing.T) {
	tmp := t.TempDir()
	script := "#!/bin/sh\necho 'no current session' >&2\nexit 1\n"
	if err := writeExec(tmp+"/tmux", script); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", tmp+":"+pathEnv())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got, err := RealPaneLister{}.ListPanes(ctx, "zen-missing-deadbeef", WindowHRA)
	if err != nil {
		t.Errorf("ListPanes on tmux-error returned err = %v; want nil (window gone)", err)
	}
	if got != nil {
		t.Errorf("records = %+v; want nil on tmux-error", got)
	}
}

// TestRealPaneListerListPanesEmptyOutput covers the case where tmux
// exits 0 but emits an empty body (no panes, but the call succeeded —
// rare but possible if a window exists with zero panes). Records slice
// MUST be non-nil-but-empty so callers distinguish from the "tmux
// errored" branch above.
func TestRealPaneListerListPanesEmptyOutput(t *testing.T) {
	tmp := t.TempDir()
	script := "#!/bin/sh\nexit 0\n"
	if err := writeExec(tmp+"/tmux", script); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", tmp+":"+pathEnv())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got, err := RealPaneListerListPanesEmpty(ctx)
	if err != nil {
		t.Fatalf("ListPanes empty-output = %v; want nil", err)
	}
	if len(got) != 0 {
		t.Errorf("records = %+v; want empty", got)
	}
}

func RealPaneListerListPanesEmpty(ctx context.Context) ([]paneRecord, error) {
	return RealPaneLister{}.ListPanes(ctx, "zen-empty-deadbeef", WindowOrch)
}

// TestRealPaneListerListPanesIDOnlyTitleAbsent covers the SplitN edge
// case where tmux emits a record without a pipe (pane_title empty in
// some tmux build configurations). Records[i].Title MUST be empty
// without producing a parser error.
func TestRealPaneListerListPanesIDOnlyTitleAbsent(t *testing.T) {
	tmp := t.TempDir()
	script := "#!/bin/sh\nprintf '%%0\\n'\nexit 0\n"
	if err := writeExec(tmp+"/tmux", script); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", tmp+":"+pathEnv())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got, err := RealPaneLister{}.ListPanes(ctx, "zen-test-deadbeef", WindowOrch)
	if err != nil {
		t.Fatalf("ListPanes id-only = %v; want nil", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(records) = %d; want 1; got %+v", len(got), got)
	}
	if got[0].ID != "%0" || got[0].Title != "" {
		t.Errorf("records[0] = %+v; want {ID:%%0, Title:\"\"}", got[0])
	}
}

func TestDriftPollerScratchGuardDefenseInDepth(t *testing.T) {

	saved := append([]WindowName(nil), DaemonOwnedWindows...)
	t.Cleanup(func() { DaemonOwnedWindows = saved })
	DaemonOwnedWindows = []WindowName{WindowScratch}

	emitter := &fakeDriftEmitter{}
	store := newFakeSessionStore()
	_ = store.UpsertSession(Session{
		Alias: "internal-platform-x", Sha8: "deadbeef",
		Name: "zen-internal-platform-x-deadbeef", Status: StatusActive,
	})
	lister := &fakePaneLister{
		byWindow: map[string][]paneRecord{
			"zen-internal-platform-x-deadbeef:scratch": {{ID: "%99", Title: "private"}},
		},
	}
	poller := NewDriftPoller(store, lister, emitter, time.Second)
	sink := &testLogSink{}
	poller.logger = sink.logger()

	if err := poller.tick(context.Background()); err != nil {
		t.Fatalf("tick err: %v", err)
	}

	if !sink.contains("inv-zen-118 violated") {
		t.Errorf("expected guard log to mention inv-zen-118, got %q", sink.string())
	}

	for _, k := range lister.queries() {
		if strings.HasSuffix(k, ":scratch") {
			t.Errorf("ListPanes called for scratch %q; inv-zen-118 violated by guard bypass", k)
		}
	}

	if got := emitter.Events(); len(got) != 0 {
		t.Errorf("expected 0 drift events under scratch-guard, got %d: %+v", len(got), got)
	}
}

type expectedPanesFailingStore struct {
	*fakeSessionStore
	expectedPanesFor string
	expectedPanesErr error
}

func (s *expectedPanesFailingStore) ExpectedPanesFor(sessionName string) (map[WindowName][]string, error) {
	if sessionName == s.expectedPanesFor && s.expectedPanesErr != nil {
		return nil, s.expectedPanesErr
	}
	return s.fakeSessionStore.ExpectedPanesFor(sessionName)
}
