// TDD this file is written BEFORE tamper_dispatcher_activation.go.
// Tests use local fakes that satisfy DoctrineActive, EventAppender, InboxNotifier
// interfaces declared in tamper_dispatcher_activation.go.
//
// Coverage target: 100% on tamper_dispatcher_activation.go (security-critical).
package recovery

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/inbox"
)

type fakeDoctrineActive struct {
	mu          sync.Mutex
	modes       map[string]string
	defaultMode string
	allIDs      []string
}

func (f *fakeDoctrineActive) TamperMode(projectID string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m, ok := f.modes[projectID]; ok {
		return m
	}
	return f.defaultMode
}

func (f *fakeDoctrineActive) AllProjectIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.allIDs))
	copy(out, f.allIDs)
	return out
}

type fakeEventAppender struct {
	mu     sync.Mutex
	events []emittedEvent
}

type emittedEvent struct {
	EventType string
	ProjectID string
	Payload   map[string]any
}

func (f *fakeEventAppender) Append(_ context.Context, eventType, projectID string, payload map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, emittedEvent{
		EventType: eventType,
		ProjectID: projectID,
		Payload:   payload,
	})
	return nil
}

func (f *fakeEventAppender) byType(et string) []emittedEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []emittedEvent
	for _, e := range f.events {
		if e.EventType == et {
			out = append(out, e)
		}
	}
	return out
}

type fakeInboxNotifier struct {
	mu       sync.Mutex
	notified []inboxCall
}

type inboxCall struct {
	Severity  inbox.Severity
	ProjectID string
	Message   string
}

func (f *fakeInboxNotifier) Notify(_ context.Context, sev inbox.Severity, projectID, message string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notified = append(f.notified, inboxCall{Severity: sev, ProjectID: projectID, Message: message})
	return nil
}

func TestNewDoctrineDispatcher_NilActive_Error(t *testing.T) {
	_, err := NewDoctrineDispatcher(nil, &fakeEventAppender{}, &fakeInboxNotifier{})
	if err == nil {
		t.Error("nil DoctrineActive should return error")
	}
}

func TestNewDoctrineDispatcher_NilEventlog_Error(t *testing.T) {
	active := &fakeDoctrineActive{defaultMode: "halt-per-project"}
	_, err := NewDoctrineDispatcher(active, nil, &fakeInboxNotifier{})
	if err == nil {
		t.Error("nil EventAppender should return error")
	}
}

func TestNewDoctrineDispatcher_NilInbox_Error(t *testing.T) {
	active := &fakeDoctrineActive{defaultMode: "halt-per-project"}
	_, err := NewDoctrineDispatcher(active, &fakeEventAppender{}, nil)
	if err == nil {
		t.Error("nil InboxNotifier should return error")
	}
}

func TestDispatch_MaxScope_HaltPerProject(t *testing.T) {
	active := &fakeDoctrineActive{
		modes: map[string]string{
			"zen-swarm":           "halt-per-project",
			"internal-platform-x": "halt-per-project",
		},
		allIDs: []string{"zen-swarm", "internal-platform-x"},
	}
	evlog := &fakeEventAppender{}
	ibx := &fakeInboxNotifier{}
	d, err := NewDoctrineDispatcher(active, evlog, ibx)
	if err != nil {
		t.Fatalf("NewDoctrineDispatcher: %v", err)
	}

	ev := TamperEvent{
		ProjectID:         "zen-swarm",
		LastCleanRecordID: 847238,
		DetectionPath:     "chain_hash_mismatch",
		Severity:          "URGENT",
		Timestamp:         time.Now(),
	}
	if err := d.Dispatch(context.Background(), ev); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	tamperEvts := evlog.byType("audit.tamper_detected")
	if len(tamperEvts) != 1 {
		t.Errorf("audit.tamper_detected count = %d, want 1", len(tamperEvts))
	}

	recoveryEvts := evlog.byType("audit.recovery_initiated")
	if len(recoveryEvts) != 1 {
		t.Errorf("audit.recovery_initiated count = %d, want 1", len(recoveryEvts))
	}

	if len(ibx.notified) != 1 || ibx.notified[0].Severity != inbox.SeverityUrgent {
		t.Errorf("inbox calls = %v, want 1 URGENT", ibx.notified)
	}

	if !d.IsHalted("zen-swarm") {
		t.Error("zen-swarm should be halted after halt-per-project tamper")
	}

	if d.IsHalted("internal-platform-x") {
		t.Error("internal-platform-x must NOT be halted (per-project blast radius, halt-per-project mode)")
	}
}

func TestDispatch_Default_LogContinue(t *testing.T) {
	active := &fakeDoctrineActive{
		modes: map[string]string{
			"zen-swarm": "log-continue",
		},
		allIDs: []string{"zen-swarm"},
	}
	evlog := &fakeEventAppender{}
	ibx := &fakeInboxNotifier{}
	d, err := NewDoctrineDispatcher(active, evlog, ibx)
	if err != nil {
		t.Fatalf("NewDoctrineDispatcher: %v", err)
	}

	ev := TamperEvent{
		ProjectID:         "zen-swarm",
		LastCleanRecordID: 100,
		DetectionPath:     "chain_hash_mismatch",
		Severity:          "URGENT",
		Timestamp:         time.Now(),
	}
	if err := d.Dispatch(context.Background(), ev); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	if d.IsHalted("zen-swarm") {
		t.Error("log-continue: project must NOT be halted")
	}

	tamperEvts := evlog.byType("audit.tamper_detected")
	if len(tamperEvts) != 1 {
		t.Errorf("audit.tamper_detected count = %d, want 1 (logged)", len(tamperEvts))
	}

	recoveryEvts := evlog.byType("audit.recovery_initiated")
	if len(recoveryEvts) != 0 {
		t.Errorf("audit.recovery_initiated count = %d, want 0 (log-continue)", len(recoveryEvts))
	}
	if len(ibx.notified) != 1 || ibx.notified[0].Severity != inbox.SeverityUrgent {
		t.Errorf("inbox calls = %v, want 1 URGENT", ibx.notified)
	}
}

func TestDispatch_CapaFirewall_CascadeAllProjects(t *testing.T) {
	active := &fakeDoctrineActive{
		modes: map[string]string{
			"zen-swarm":           "cascade-halt-all",
			"internal-platform-x": "cascade-halt-all",
			"reference-project":   "cascade-halt-all",
		},
		allIDs: []string{"zen-swarm", "internal-platform-x", "reference-project"},
	}
	evlog := &fakeEventAppender{}
	ibx := &fakeInboxNotifier{}
	d, err := NewDoctrineDispatcher(active, evlog, ibx)
	if err != nil {
		t.Fatalf("NewDoctrineDispatcher: %v", err)
	}

	ev := TamperEvent{
		ProjectID:         "zen-swarm",
		LastCleanRecordID: 847238,
		DetectionPath:     "chain_hash_mismatch",
		Severity:          "URGENT",
		Timestamp:         time.Now(),
	}
	if err := d.Dispatch(context.Background(), ev); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	for _, pid := range []string{"zen-swarm", "internal-platform-x", "reference-project"} {
		if !d.IsHalted(pid) {
			t.Errorf("project %s should be halted in cascade-halt-all", pid)
		}
	}

	tamperEvts := evlog.byType("audit.tamper_detected")
	if len(tamperEvts) != 3 {
		t.Errorf("audit.tamper_detected count = %d, want 3 (one per project)", len(tamperEvts))
	}

	recoveryEvts := evlog.byType("audit.recovery_initiated")
	if len(recoveryEvts) != 1 {
		t.Errorf("audit.recovery_initiated count = %d, want 1", len(recoveryEvts))
	}

	if len(ibx.notified) != 3 {
		t.Errorf("inbox calls = %d, want 3 URGENT (cascade)", len(ibx.notified))
	}
	for i, call := range ibx.notified {
		if call.Severity != inbox.SeverityUrgent {
			t.Errorf("inbox[%d].Severity = %q, want urgent", i, call.Severity)
		}
	}
}

func TestDispatch_EmptyProjectID_Rejected(t *testing.T) {
	active := &fakeDoctrineActive{defaultMode: "halt-per-project"}
	d, err := NewDoctrineDispatcher(active, &fakeEventAppender{}, &fakeInboxNotifier{})
	if err != nil {
		t.Fatalf("NewDoctrineDispatcher: %v", err)
	}
	ev := TamperEvent{ProjectID: "", DetectionPath: "x"}
	if err := d.Dispatch(context.Background(), ev); err == nil {
		t.Error("empty project_id must be rejected (inv-zen-150)")
	}
}

func TestDispatch_UnknownMode_Error(t *testing.T) {
	active := &fakeDoctrineActive{
		modes:  map[string]string{"p1": "unknown-mode-xyz"},
		allIDs: []string{"p1"},
	}
	evlog := &fakeEventAppender{}
	ibx := &fakeInboxNotifier{}
	d, err := NewDoctrineDispatcher(active, evlog, ibx)
	if err != nil {
		t.Fatalf("NewDoctrineDispatcher: %v", err)
	}
	ev := TamperEvent{
		ProjectID:     "p1",
		DetectionPath: "chain_hash_mismatch",
		Timestamp:     time.Now(),
	}
	if err := d.Dispatch(context.Background(), ev); err == nil {
		t.Error("unknown tamper_response mode should return error")
	}
}

func TestDispatch_RecursiveChainAnchor(t *testing.T) {
	active := &fakeDoctrineActive{
		modes:  map[string]string{"zen-swarm": "halt-per-project"},
		allIDs: []string{"zen-swarm"},
	}
	evlog := &fakeEventAppender{}
	d, err := NewDoctrineDispatcher(active, evlog, &fakeInboxNotifier{})
	if err != nil {
		t.Fatalf("NewDoctrineDispatcher: %v", err)
	}
	ev := TamperEvent{
		ProjectID:         "zen-swarm",
		LastCleanRecordID: 100,
		DetectionPath:     "tessera_missing",
		Timestamp:         time.Now(),
	}
	if err := d.Dispatch(context.Background(), ev); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(evlog.byType("audit.tamper_detected")) != 1 {
		t.Error("audit.tamper_detected not emitted (required for chain anchor)")
	}
	if len(evlog.byType("audit.recovery_initiated")) != 1 {
		t.Error("audit.recovery_initiated not emitted (required for chain anchor)")
	}
}

func TestIsHalted_Untriggered_False(t *testing.T) {
	active := &fakeDoctrineActive{defaultMode: "halt-per-project"}
	d, err := NewDoctrineDispatcher(active, &fakeEventAppender{}, &fakeInboxNotifier{})
	if err != nil {
		t.Fatalf("NewDoctrineDispatcher: %v", err)
	}
	if d.IsHalted("never-touched") {
		t.Error("IsHalted returned true for project that was never dispatched")
	}
}

type errEventAppender struct {
	fakeEventAppender
	failOn int
	count  int
}

func (e *errEventAppender) Append(ctx context.Context, eventType, projectID string, payload map[string]any) error {
	e.count++
	if e.failOn > 0 && e.count == e.failOn {
		return errors.New("injected eventlog error")
	}
	return e.fakeEventAppender.Append(ctx, eventType, projectID, payload)
}

type errInboxNotifier struct {
	fakeInboxNotifier
}

func (e *errInboxNotifier) Notify(ctx context.Context, sev inbox.Severity, projectID, message string) error {
	return errors.New("injected inbox error")
}

func TestDispatch_HaltPerProject_EventlogError_TamperDetected(t *testing.T) {
	active := &fakeDoctrineActive{
		modes:  map[string]string{"p1": "halt-per-project"},
		allIDs: []string{"p1"},
	}
	evlog := &errEventAppender{failOn: 1}
	d, _ := NewDoctrineDispatcher(active, evlog, &fakeInboxNotifier{})
	ev := TamperEvent{ProjectID: "p1", DetectionPath: "chain_hash_mismatch", Timestamp: time.Now()}
	if err := d.Dispatch(context.Background(), ev); err == nil {
		t.Error("expected error when eventlog.Append fails on tamper_detected")
	}
}

func TestDispatch_HaltPerProject_InboxError(t *testing.T) {
	active := &fakeDoctrineActive{
		modes:  map[string]string{"p1": "halt-per-project"},
		allIDs: []string{"p1"},
	}
	d, _ := NewDoctrineDispatcher(active, &fakeEventAppender{}, &errInboxNotifier{})
	ev := TamperEvent{ProjectID: "p1", DetectionPath: "chain_hash_mismatch", Timestamp: time.Now()}
	if err := d.Dispatch(context.Background(), ev); err == nil {
		t.Error("expected error when inbox.Notify fails")
	}
}

func TestDispatch_HaltPerProject_EventlogError_RecoveryInitiated(t *testing.T) {
	active := &fakeDoctrineActive{
		modes:  map[string]string{"p1": "halt-per-project"},
		allIDs: []string{"p1"},
	}
	evlog := &errEventAppender{failOn: 2}
	d, _ := NewDoctrineDispatcher(active, evlog, &fakeInboxNotifier{})
	ev := TamperEvent{ProjectID: "p1", DetectionPath: "chain_hash_mismatch", Timestamp: time.Now()}
	if err := d.Dispatch(context.Background(), ev); err == nil {
		t.Error("expected error when eventlog.Append fails on recovery_initiated")
	}
}

func TestDispatch_LogContinue_EventlogError(t *testing.T) {
	active := &fakeDoctrineActive{
		modes:  map[string]string{"p1": "log-continue"},
		allIDs: []string{"p1"},
	}
	evlog := &errEventAppender{failOn: 1}
	d, _ := NewDoctrineDispatcher(active, evlog, &fakeInboxNotifier{})
	ev := TamperEvent{ProjectID: "p1", DetectionPath: "x", Timestamp: time.Now()}
	if err := d.Dispatch(context.Background(), ev); err == nil {
		t.Error("expected error when eventlog.Append fails in log-continue mode")
	}
}

func TestDispatch_LogContinue_InboxError(t *testing.T) {
	active := &fakeDoctrineActive{
		modes:  map[string]string{"p1": "log-continue"},
		allIDs: []string{"p1"},
	}
	d, _ := NewDoctrineDispatcher(active, &fakeEventAppender{}, &errInboxNotifier{})
	ev := TamperEvent{ProjectID: "p1", DetectionPath: "x", Timestamp: time.Now()}
	if err := d.Dispatch(context.Background(), ev); err == nil {
		t.Error("expected error when inbox.Notify fails in log-continue mode")
	}
}

func TestDispatch_Cascade_EventlogError(t *testing.T) {
	active := &fakeDoctrineActive{
		modes:  map[string]string{"p1": "cascade-halt-all"},
		allIDs: []string{"p1"},
	}
	evlog := &errEventAppender{failOn: 1}
	d, _ := NewDoctrineDispatcher(active, evlog, &fakeInboxNotifier{})
	ev := TamperEvent{ProjectID: "p1", DetectionPath: "x", Timestamp: time.Now()}
	if err := d.Dispatch(context.Background(), ev); err == nil {
		t.Error("expected error when eventlog.Append fails in cascade mode")
	}
}

func TestDispatch_Cascade_InboxError(t *testing.T) {
	active := &fakeDoctrineActive{
		modes:  map[string]string{"p1": "cascade-halt-all"},
		allIDs: []string{"p1"},
	}
	d, _ := NewDoctrineDispatcher(active, &fakeEventAppender{}, &errInboxNotifier{})
	ev := TamperEvent{ProjectID: "p1", DetectionPath: "x", Timestamp: time.Now()}
	if err := d.Dispatch(context.Background(), ev); err == nil {
		t.Error("expected error when inbox.Notify fails in cascade mode")
	}
}

func TestDispatch_Cascade_RecoveryInitiatedError(t *testing.T) {
	active := &fakeDoctrineActive{
		modes:  map[string]string{"p1": "cascade-halt-all"},
		allIDs: []string{"p1"},
	}
	evlog := &errEventAppender{failOn: 2}
	d, _ := NewDoctrineDispatcher(active, evlog, &fakeInboxNotifier{})
	ev := TamperEvent{ProjectID: "p1", DetectionPath: "x", Timestamp: time.Now()}
	if err := d.Dispatch(context.Background(), ev); err == nil {
		t.Error("expected error when eventlog.Append fails on cascade recovery_initiated")
	}
}

func TestBuildTamperPayload_EmptySeverityAndZeroTimestamp(t *testing.T) {
	active := &fakeDoctrineActive{
		modes:  map[string]string{"p1": "halt-per-project"},
		allIDs: []string{"p1"},
	}
	evlog := &fakeEventAppender{}
	d, _ := NewDoctrineDispatcher(active, evlog, &fakeInboxNotifier{})

	ev := TamperEvent{
		ProjectID:     "p1",
		DetectionPath: "chain_hash_mismatch",
	}
	if err := d.Dispatch(context.Background(), ev); err != nil {
		t.Fatalf("Dispatch with zero timestamp / empty severity: %v", err)
	}

	evts := evlog.byType("audit.tamper_detected")
	if len(evts) != 1 {
		t.Fatalf("want 1 tamper_detected event, got %d", len(evts))
	}

	if evts[0].Payload["severity"] != "URGENT" {
		t.Errorf("payload severity = %q, want URGENT (default)", evts[0].Payload["severity"])
	}

	ts, _ := evts[0].Payload["timestamp"].(string)
	if ts == "" {
		t.Error("payload timestamp should be non-empty (defaulted to time.Now())")
	}
}
