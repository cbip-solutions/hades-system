package tmuxlife

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
)

func TestDoctrineValues(t *testing.T) {
	cases := []struct {
		got, want string
	}{
		{string(doctrine.NameMaxScope), "max-scope"},
		{string(doctrine.NameDefault), "default"},
		{string(doctrine.NameCapaFirewall), "capa-firewall"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
}

// TestDoctrineIsValid accepts the three canonical names; rejects
// everything else including empty, uppercase, alternate separators,
// title case, and trimming-sensitive variants.
//
// IsValid is the centralised gate; tmuxlife's DoctrineIdleTTL panics
// for unknown names (programmer-error-must-surface), so callers that
// receive doctrine.Name from untrusted input (zenswarm.toml load)
// MUST IsValid-check before invoking DoctrineIdleTTL.
func TestDoctrineIsValid(t *testing.T) {
	valid := []string{"max-scope", "default", "capa-firewall"}
	for _, v := range valid {
		if !doctrine.IsValid(doctrine.Name(v)) {
			t.Errorf("doctrine.IsValid(%q) = false, want true", v)
		}
	}
	invalid := []string{"", "MAX-SCOPE", "max_scope", "Default", "capafirewall", "max-scope ", " default"}
	for _, v := range invalid {
		if doctrine.IsValid(doctrine.Name(v)) {
			t.Errorf("doctrine.IsValid(%q) = true, want false", v)
		}
	}
}

func TestDoctrineIdleTTLMatrix(t *testing.T) {
	cases := []struct {
		d    doctrine.Name
		want IdleTTL
	}{
		{doctrine.NameMaxScope, IdleTTL(IdleTTLInfinity)},
		{doctrine.NameDefault, IdleTTL(24)},
		{doctrine.NameCapaFirewall, IdleTTL(4)},
	}
	for _, tc := range cases {
		got := DoctrineIdleTTL(tc.d)
		if got != tc.want {
			t.Errorf("DoctrineIdleTTL(%q) = %d, want %d", tc.d, got, tc.want)
		}
	}
}

func TestDoctrineIdleTTLUnknownPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("DoctrineIdleTTL(unknown) did not panic")
		}
	}()
	_ = DoctrineIdleTTL(doctrine.Name("phantom"))
}

func TestDoctrineIdleTTLEmptyPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("DoctrineIdleTTL(\"\") did not panic")
		}
	}()
	_ = DoctrineIdleTTL(doctrine.Name(""))
}

func TestIdleTTLIsInfinityHelper(t *testing.T) {
	if !IdleTTLIsInfinity(IdleTTL(IdleTTLInfinity)) {
		t.Errorf("IdleTTLIsInfinity(-1) = false, want true")
	}
	if IdleTTLIsInfinity(IdleTTL(24)) {
		t.Errorf("IdleTTLIsInfinity(24) = true, want false")
	}
	if IdleTTLIsInfinity(IdleTTL(0)) {
		t.Errorf("IdleTTLIsInfinity(0) = true, want false (0 means reap immediately)")
	}
	if IdleTTLIsInfinity(IdleTTL(4)) {
		t.Errorf("IdleTTLIsInfinity(4) = true, want false (capa-firewall TTL)")
	}
}

func TestTriggerValues(t *testing.T) {
	cases := []struct {
		got  Trigger
		want int
		s    string
	}{
		{TriggerCwd, 0, "cwd"},
		{TriggerExplicitAttach, 1, "explicit-attach"},
		{TriggerScheduledJob, 2, "scheduled-job"},
		{TriggerAutonomousResume, 3, "autonomous-resume"},
	}
	for _, tc := range cases {
		if int(tc.got) != tc.want {
			t.Errorf("%v = %d, want %d", tc.got, int(tc.got), tc.want)
		}
		if tc.got.String() != tc.s {
			t.Errorf("%v.String() = %q, want %q", tc.got, tc.got.String(), tc.s)
		}
	}
}

func TestTriggerStringUnknown(t *testing.T) {
	bogus := Trigger(99)
	if !strings.Contains(bogus.String(), "unknown") {
		t.Errorf("bogus String = %q; expected unknown(N)", bogus.String())
	}
	if !strings.Contains(bogus.String(), "99") {
		t.Errorf("bogus String = %q; expected to include numeric value", bogus.String())
	}

	neg := Trigger(-3)
	if !strings.Contains(neg.String(), "unknown") || !strings.Contains(neg.String(), "-3") {
		t.Errorf("neg String = %q; want unknown(-3)", neg.String())
	}
}

func TestIsValidTrigger(t *testing.T) {
	valid := []Trigger{TriggerCwd, TriggerExplicitAttach, TriggerScheduledJob, TriggerAutonomousResume}
	for _, v := range valid {
		if !IsValidTrigger(v) {
			t.Errorf("IsValidTrigger(%v) = false, want true", v)
		}
	}
	invalid := []Trigger{Trigger(-1), Trigger(4), Trigger(99)}
	for _, v := range invalid {
		if IsValidTrigger(v) {
			t.Errorf("IsValidTrigger(%v) = true, want false", v)
		}
	}
}

func TestHandleTriggerFirstTimeSpawnsAndCreatesWindows(t *testing.T) {
	st := newFakeSessionStore()
	exec := &fakeExecutor{}
	exec.responses = map[string]execResp{
		"has-session-zen-foo-deadbeef":          {err: errors.New("can't find session")},
		"new-session-zen-foo-deadbeef":          {},
		"rename-window-zen-foo-deadbeef-0-orch": {},
		"new-window-zen-foo-deadbeef-leads":     {},
		"new-window-zen-foo-deadbeef-workers":   {},
		"new-window-zen-foo-deadbeef-hra":       {},
		"new-window-zen-foo-deadbeef-logs":      {},
		"new-window-zen-foo-deadbeef-scratch":   {},
	}

	m := New(st)
	m.exec = exec.Exec
	ctx := context.Background()

	s, err := m.HandleTrigger(ctx, TriggerCwd, "foo", "deadbeef")
	if err != nil {
		t.Fatalf("HandleTrigger: %v", err)
	}
	if s.Status != StatusActive {
		t.Errorf("Status = %v, want StatusActive", s.Status)
	}
	if s.Alias != "foo" || s.Sha8 != "deadbeef" {
		t.Errorf("session fields wrong: %+v", s)
	}
	if s.Name != "zen-foo-deadbeef" {
		t.Errorf("Name = %q, want zen-foo-deadbeef", s.Name)
	}
	if s.LastAttachAt.IsZero() {
		t.Errorf("LastAttachAt not updated on first-time activation")
	}

	got, gerr := st.GetSession("zen-foo-deadbeef")
	if gerr != nil {
		t.Fatalf("GetSession: %v", gerr)
	}
	if got.Status != StatusActive {
		t.Errorf("persisted Status = %v, want StatusActive", got.Status)
	}
	if got.LastAttachAt.IsZero() {
		t.Errorf("persisted LastAttachAt is zero")
	}
}

func TestHandleTriggerExistingActiveAlive(t *testing.T) {
	st := newFakeSessionStore()
	if err := st.UpsertSession(Session{
		Alias: "foo", Sha8: "deadbeef", Name: "zen-foo-deadbeef",
		Status: StatusActive, CreatedAt: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	exec := &fakeExecutor{}
	exec.responses = map[string]execResp{
		"has-session-zen-foo-deadbeef": {err: nil},
	}
	m := New(st)
	m.exec = exec.Exec

	s, err := m.HandleTrigger(context.Background(), TriggerExplicitAttach, "foo", "deadbeef")
	if err != nil {
		t.Fatalf("HandleTrigger: %v", err)
	}
	if s.Status != StatusActive {
		t.Errorf("Status = %v, want StatusActive", s.Status)
	}

	for _, c := range exec.calls {
		if strings.HasPrefix(c, "new-session-") {
			t.Errorf("unexpected new-session call: %v", exec.calls)
		}
	}
	got, _ := st.GetSession("zen-foo-deadbeef")
	if got.LastAttachAt.IsZero() {
		t.Errorf("LastAttachAt not updated")
	}
}

func TestHandleTriggerExistingActiveDead(t *testing.T) {
	st := newFakeSessionStore()
	if err := st.UpsertSession(Session{
		Alias: "foo", Sha8: "deadbeef", Name: "zen-foo-deadbeef",
		Status: StatusActive, CreatedAt: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	exec := &fakeExecutor{}

	exec.responses = map[string]execResp{
		"has-session-zen-foo-deadbeef":          {err: errors.New("can't find session")},
		"has-session-zen-foo-deadbeef#2":        {err: errors.New("can't find session")},
		"new-session-zen-foo-deadbeef":          {},
		"rename-window-zen-foo-deadbeef-0-orch": {},
		"new-window-zen-foo-deadbeef-leads":     {},
		"new-window-zen-foo-deadbeef-workers":   {},
		"new-window-zen-foo-deadbeef-hra":       {},
		"new-window-zen-foo-deadbeef-logs":      {},
		"new-window-zen-foo-deadbeef-scratch":   {},
	}

	m := New(st)
	m.exec = exec.Exec
	s, err := m.HandleTrigger(context.Background(), TriggerScheduledJob, "foo", "deadbeef")
	if err != nil {
		t.Fatalf("HandleTrigger: %v", err)
	}
	if s.Status != StatusActive {
		t.Errorf("after recovery Status = %v, want StatusActive", s.Status)
	}

	got, _ := st.GetSession("zen-foo-deadbeef")
	if got.LastAttachAt.IsZero() {
		t.Errorf("LastAttachAt not updated post-recovery")
	}
}

func TestHandleTriggerArchivedNoSnapshotFreshSpawn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	st := newFakeSessionStore()
	if err := st.UpsertSession(Session{
		Alias: "foo", Sha8: "deadbeef", Name: "zen-foo-deadbeef",
		Status: StatusArchived, CreatedAt: time.Now().Add(-48 * time.Hour),
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	exec := &fakeExecutor{}

	exec.responses = map[string]execResp{
		"has-session-zen-foo-deadbeef":          {err: errors.New("can't find session")},
		"new-session-zen-foo-deadbeef":          {},
		"rename-window-zen-foo-deadbeef-0-orch": {},
		"new-window-zen-foo-deadbeef-leads":     {},
		"new-window-zen-foo-deadbeef-workers":   {},
		"new-window-zen-foo-deadbeef-hra":       {},
		"new-window-zen-foo-deadbeef-logs":      {},
		"new-window-zen-foo-deadbeef-scratch":   {},
	}

	m := New(st)
	m.exec = exec.Exec

	s, err := m.HandleTrigger(context.Background(), TriggerAutonomousResume, "foo", "deadbeef")
	if err != nil {
		t.Fatalf("HandleTrigger: %v", err)
	}
	if s.Status != StatusActive {
		t.Errorf("Status = %v, want StatusActive", s.Status)
	}
	got, _ := st.GetSession("zen-foo-deadbeef")
	if got.Status != StatusActive {
		t.Errorf("persisted Status = %v, want StatusActive", got.Status)
	}
}

func TestHandleTriggerIdleNoSnapshotFreshSpawn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	st := newFakeSessionStore()
	if err := st.UpsertSession(Session{
		Alias: "foo", Sha8: "deadbeef", Name: "zen-foo-deadbeef",
		Status: StatusIdle, CreatedAt: time.Now().Add(-2 * time.Hour),
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	exec := &fakeExecutor{}
	exec.responses = map[string]execResp{
		"has-session-zen-foo-deadbeef":          {err: errors.New("can't find session")},
		"new-session-zen-foo-deadbeef":          {},
		"rename-window-zen-foo-deadbeef-0-orch": {},
		"new-window-zen-foo-deadbeef-leads":     {},
		"new-window-zen-foo-deadbeef-workers":   {},
		"new-window-zen-foo-deadbeef-hra":       {},
		"new-window-zen-foo-deadbeef-logs":      {},
		"new-window-zen-foo-deadbeef-scratch":   {},
	}

	m := New(st)
	m.exec = exec.Exec

	s, err := m.HandleTrigger(context.Background(), TriggerCwd, "foo", "deadbeef")
	if err != nil {
		t.Fatalf("HandleTrigger: %v", err)
	}
	if s.Status != StatusActive {
		t.Errorf("Status = %v, want StatusActive", s.Status)
	}
}

func TestHandleTriggerOrphanedRespawnsFresh(t *testing.T) {
	st := newFakeSessionStore()
	if err := st.UpsertSession(Session{
		Alias: "foo", Sha8: "deadbeef", Name: "zen-foo-deadbeef",
		Status: StatusOrphaned, CreatedAt: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	exec := &fakeExecutor{}
	exec.responses = map[string]execResp{
		"has-session-zen-foo-deadbeef":          {err: errors.New("can't find session")},
		"new-session-zen-foo-deadbeef":          {},
		"rename-window-zen-foo-deadbeef-0-orch": {},
		"new-window-zen-foo-deadbeef-leads":     {},
		"new-window-zen-foo-deadbeef-workers":   {},
		"new-window-zen-foo-deadbeef-hra":       {},
		"new-window-zen-foo-deadbeef-logs":      {},
		"new-window-zen-foo-deadbeef-scratch":   {},
	}

	m := New(st)
	m.exec = exec.Exec
	s, err := m.HandleTrigger(context.Background(), TriggerCwd, "foo", "deadbeef")
	if err != nil {
		t.Fatalf("HandleTrigger: %v", err)
	}
	if s.Status != StatusActive {
		t.Errorf("Status = %v, want StatusActive", s.Status)
	}
	got, _ := st.GetSession("zen-foo-deadbeef")
	if got.Status != StatusActive {
		t.Errorf("persisted Status = %v, want StatusActive", got.Status)
	}
}

func TestHandleTriggerActiveTransportError(t *testing.T) {
	st := newFakeSessionStore()
	if err := st.UpsertSession(Session{
		Alias: "foo", Sha8: "deadbeef", Name: "zen-foo-deadbeef",
		Status: StatusActive, CreatedAt: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	exec := &fakeExecutor{}
	exec.responses = map[string]execResp{
		"has-session-zen-foo-deadbeef": {err: errors.New("error connecting to /tmp/zen-swarm.sock")},
	}
	m := New(st)
	m.exec = exec.Exec

	_, err := m.HandleTrigger(context.Background(), TriggerCwd, "foo", "deadbeef")
	if err == nil {
		t.Fatalf("HandleTrigger succeeded on transport error; expected error")
	}
	if errors.Is(err, ErrSessionNotFound) {
		t.Errorf("transport err mapped to ErrSessionNotFound: %v", err)
	}

	got, _ := st.GetSession("zen-foo-deadbeef")
	if got.Status != StatusActive {
		t.Errorf("Status = %v, want StatusActive (transport must not flip Orphaned)", got.Status)
	}
}

func TestHandleTriggerStoreLookupError(t *testing.T) {
	st := &failingStore{
		fakeSessionStore: newFakeSessionStore(),
		getErr:           errors.New("disk read failure"),
	}
	m := New(st)
	m.exec = (&fakeExecutor{}).Exec
	_, err := m.HandleTrigger(context.Background(), TriggerCwd, "foo", "deadbeef")
	if err == nil {
		t.Fatalf("HandleTrigger succeeded on store lookup error; expected error")
	}
	if !strings.Contains(err.Error(), "disk read failure") {
		t.Errorf("err = %v; want wrap of underlying store error", err)
	}
}

func TestHandleTriggerInvalidTrigger(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("HandleTrigger with bogus Trigger did not panic")
		}
	}()
	st := newFakeSessionStore()
	m := New(st)
	m.exec = (&fakeExecutor{}).Exec
	_, _ = m.HandleTrigger(context.Background(), Trigger(99), "x", "12345678")
}

func TestHandleTriggerFirstTimeSpawnFails(t *testing.T) {
	st := newFakeSessionStore()
	exec := &fakeExecutor{}
	exec.responses = map[string]execResp{
		"has-session-zen-foo-deadbeef": {err: errors.New("can't find session")},
		"new-session-zen-foo-deadbeef": {err: errors.New("tmux: out of resources")},
	}
	m := New(st)
	m.exec = exec.Exec
	_, err := m.HandleTrigger(context.Background(), TriggerCwd, "foo", "deadbeef")
	if err == nil {
		t.Fatalf("HandleTrigger succeeded despite Spawn failure")
	}
	if !strings.Contains(err.Error(), "out of resources") {
		t.Errorf("err = %v; want wrap of underlying tmux error", err)
	}
	if _, gerr := st.GetSession("zen-foo-deadbeef"); !errors.Is(gerr, ErrSessionNotFound) {
		t.Errorf("row leaked after Spawn failure: %v", gerr)
	}
}

func TestHandleTriggerFirstTimeCreateWindowsFails(t *testing.T) {
	st := newFakeSessionStore()
	exec := &fakeExecutor{}
	exec.responses = map[string]execResp{
		"has-session-zen-foo-deadbeef":          {err: errors.New("can't find session")},
		"new-session-zen-foo-deadbeef":          {},
		"rename-window-zen-foo-deadbeef-0-orch": {err: errors.New("tmux: pty alloc failed")},
		"kill-session-zen-foo-deadbeef":         {},
	}
	m := New(st)
	m.exec = exec.Exec
	_, err := m.HandleTrigger(context.Background(), TriggerCwd, "foo", "deadbeef")
	if err == nil {
		t.Fatalf("HandleTrigger succeeded despite CreateWindows failure")
	}
	if !strings.Contains(err.Error(), "pty alloc failed") {
		t.Errorf("err = %v; want wrap of underlying tmux error", err)
	}

	if _, gerr := st.GetSession("zen-foo-deadbeef"); !errors.Is(gerr, ErrSessionNotFound) {
		t.Errorf("row leaked after CreateWindows failure: %v", gerr)
	}

	foundKill := false
	for _, c := range exec.calls {
		if c == "kill-session-zen-foo-deadbeef" {
			foundKill = true
		}
	}
	if !foundKill {
		t.Errorf("kill-session not invoked as rollback; calls = %v", exec.calls)
	}
}

func TestHandleTriggerOrphanedRespawnSpawnFails(t *testing.T) {
	st := newFakeSessionStore()
	if err := st.UpsertSession(Session{
		Alias: "foo", Sha8: "deadbeef", Name: "zen-foo-deadbeef",
		Status: StatusOrphaned, CreatedAt: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	exec := &fakeExecutor{}
	exec.responses = map[string]execResp{
		"has-session-zen-foo-deadbeef": {err: errors.New("can't find session")},
		"new-session-zen-foo-deadbeef": {err: errors.New("tmux: out of resources")},
	}
	m := New(st)
	m.exec = exec.Exec
	_, err := m.HandleTrigger(context.Background(), TriggerCwd, "foo", "deadbeef")
	if err == nil {
		t.Fatalf("HandleTrigger succeeded despite respawn Spawn failure")
	}
	if !strings.Contains(err.Error(), "out of resources") {
		t.Errorf("err = %v; want wrap of underlying tmux error", err)
	}
}

func TestHandleTriggerOrphanedRespawnSessionExistsRecovers(t *testing.T) {
	st := newFakeSessionStore()
	if err := st.UpsertSession(Session{
		Alias: "foo", Sha8: "deadbeef", Name: "zen-foo-deadbeef",
		Status: StatusOrphaned, CreatedAt: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	exec := &fakeExecutor{}

	exec.responses = map[string]execResp{
		"has-session-zen-foo-deadbeef":          {err: nil},
		"rename-window-zen-foo-deadbeef-0-orch": {},
		"new-window-zen-foo-deadbeef-leads":     {},
		"new-window-zen-foo-deadbeef-workers":   {},
		"new-window-zen-foo-deadbeef-hra":       {},
		"new-window-zen-foo-deadbeef-logs":      {},
		"new-window-zen-foo-deadbeef-scratch":   {},
	}
	m := New(st)
	m.exec = exec.Exec
	s, err := m.HandleTrigger(context.Background(), TriggerCwd, "foo", "deadbeef")
	if err != nil {
		t.Fatalf("HandleTrigger: %v", err)
	}
	if s.Status != StatusActive {
		t.Errorf("Status = %v, want StatusActive", s.Status)
	}
}

func TestHandleTriggerOrphanedRespawnCreateWindowsFails(t *testing.T) {
	st := newFakeSessionStore()
	if err := st.UpsertSession(Session{
		Alias: "foo", Sha8: "deadbeef", Name: "zen-foo-deadbeef",
		Status: StatusOrphaned, CreatedAt: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	exec := &fakeExecutor{}
	exec.responses = map[string]execResp{
		"has-session-zen-foo-deadbeef":          {err: errors.New("can't find session")},
		"new-session-zen-foo-deadbeef":          {},
		"rename-window-zen-foo-deadbeef-0-orch": {err: errors.New("tmux: pty alloc failed")},
	}
	m := New(st)
	m.exec = exec.Exec
	_, err := m.HandleTrigger(context.Background(), TriggerCwd, "foo", "deadbeef")
	if err == nil {
		t.Fatalf("HandleTrigger succeeded despite respawn CreateWindows failure")
	}
	if !strings.Contains(err.Error(), "pty alloc failed") {
		t.Errorf("err = %v; want wrap of underlying tmux error", err)
	}
}

func TestHandleTriggerUnexpectedStatusErrors(t *testing.T) {
	st := newFakeSessionStore()
	if err := st.UpsertSession(Session{
		Alias: "foo", Sha8: "deadbeef", Name: "zen-foo-deadbeef",
		Status: SessionStatus(99), CreatedAt: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	m := New(st)
	m.exec = (&fakeExecutor{}).Exec
	_, err := m.HandleTrigger(context.Background(), TriggerCwd, "foo", "deadbeef")
	if err == nil {
		t.Fatalf("HandleTrigger succeeded on unknown status; expected error")
	}
	if !strings.Contains(err.Error(), "unexpected status") {
		t.Errorf("err = %v; want mention of unexpected status", err)
	}
}

func TestRestoreEmptySnapshotDirDefaultsAndReturnsSessionNotFound(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := New(newFakeSessionStore())
	m.exec = (&fakeExecutor{}).Exec
	err := m.Restore(context.Background(), "any-alias")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("Restore err = %v; want ErrSessionNotFound (no snapshots in default dir)", err)
	}
}

func TestRestoreNonEmptySnapshotDirNoMatchingArchives(t *testing.T) {
	m := New(newFakeSessionStore())
	m.exec = (&fakeExecutor{}).Exec
	m.snapshotDir = t.TempDir()
	if err := m.Restore(context.Background(), "any-alias"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("Restore err = %v; want ErrSessionNotFound (empty snapshot dir)", err)
	}
}

func TestHandleTriggerArchivedRestoreSucceeds(t *testing.T) {
	st := newFakeSessionStore()
	if err := st.UpsertSession(Session{
		Alias: "foo", Sha8: "deadbeef", Name: "zen-foo-deadbeef",
		Status: StatusArchived, CreatedAt: time.Now().Add(-48 * time.Hour),
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	m := New(st)
	m.exec = (&fakeExecutor{}).Exec

	m.restoreFn = func(ctx context.Context, alias string) error {
		return nil
	}

	s, err := m.HandleTrigger(context.Background(), TriggerAutonomousResume, "foo", "deadbeef")
	if err != nil {
		t.Fatalf("HandleTrigger: %v", err)
	}
	if s.Status != StatusActive {
		t.Errorf("Status = %v, want StatusActive after restore success", s.Status)
	}
	if s.LastAttachAt.IsZero() {
		t.Errorf("LastAttachAt not updated post-restore")
	}
	got, _ := st.GetSession("zen-foo-deadbeef")
	if got.Status != StatusActive {
		t.Errorf("persisted Status = %v, want StatusActive", got.Status)
	}
	if got.LastAttachAt.IsZero() {
		t.Errorf("persisted LastAttachAt not updated")
	}
}

func TestHandleTriggerIdleRestoreSucceeds(t *testing.T) {
	st := newFakeSessionStore()
	if err := st.UpsertSession(Session{
		Alias: "foo", Sha8: "deadbeef", Name: "zen-foo-deadbeef",
		Status: StatusIdle, CreatedAt: time.Now().Add(-2 * time.Hour),
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	m := New(st)
	m.exec = (&fakeExecutor{}).Exec
	m.restoreFn = func(ctx context.Context, alias string) error { return nil }

	s, err := m.HandleTrigger(context.Background(), TriggerCwd, "foo", "deadbeef")
	if err != nil {
		t.Fatalf("HandleTrigger: %v", err)
	}
	if s.Status != StatusActive {
		t.Errorf("Status = %v, want StatusActive after restore success", s.Status)
	}
}
