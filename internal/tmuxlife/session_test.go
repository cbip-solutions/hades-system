package tmuxlife

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSessionNameCanonical(t *testing.T) {
	cases := []struct {
		alias, sha8, want string
	}{
		{"internal-platform-x", "a3f5b2c8", "zen-internal-platform-x-a3f5b2c8"},
		{"zen-swarm", "b8e1c4d6", "zen-zen-swarm-b8e1c4d6"},
		{"reference-project", "d2a9f7b3", "zen-reference-project-d2a9f7b3"},
		{"x", "12345678", "zen-x-12345678"},
	}
	for _, tc := range cases {
		got := SessionName(tc.alias, tc.sha8)
		if got != tc.want {
			t.Errorf("SessionName(%q,%q) = %q, want %q", tc.alias, tc.sha8, got, tc.want)
		}
	}
}

func TestSessionNameRejectsEmpty(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("SessionName(\"\",\"a3f5b2c8\") did NOT panic; programmer error must surface")
		}
	}()
	_ = SessionName("", "a3f5b2c8")
}

func TestSessionNameRejectsEmptySha8(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("SessionName(\"internal-platform-x\",\"\") did NOT panic")
		}
	}()
	_ = SessionName("internal-platform-x", "")
}

func TestSessionNameSha8LengthValidated(t *testing.T) {
	cases := []string{
		"abc",
		"abcdefghi",
		"ABC12345",
		"abcdefgz",
		"abcd ef0",
	}
	for _, sha := range cases {
		t.Run(sha, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("SessionName(%q,%q) did NOT panic; sha8 invalid", "internal-platform-x", sha)
				}
			}()
			_ = SessionName("internal-platform-x", sha)
		})
	}
}

func TestSessionNameAcceptsLowerHex(t *testing.T) {
	got := SessionName("test", "0123abcd")
	want := "zen-test-0123abcd"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSessionNameAcceptsAllHexBoundaries(t *testing.T) {
	cases := []struct {
		sha8, want string
	}{
		{"00000000", "zen-x-00000000"},
		{"99999999", "zen-x-99999999"},
		{"aaaaaaaa", "zen-x-aaaaaaaa"},
		{"ffffffff", "zen-x-ffffffff"},
		{"0a1b2c3d", "zen-x-0a1b2c3d"},
		{"deadbeef", "zen-x-deadbeef"},
	}
	for _, tc := range cases {
		got := SessionName("x", tc.sha8)
		if got != tc.want {
			t.Errorf("SessionName(\"x\",%q) = %q, want %q", tc.sha8, got, tc.want)
		}
	}
}

func TestSessionStatusEnumValues(t *testing.T) {
	cases := []struct {
		status SessionStatus
		want   int
		name   string
	}{
		{StatusActive, 0, "active"},
		{StatusIdle, 1, "idle"},
		{StatusOrphaned, 2, "orphaned"},
		{StatusArchived, 3, "archived"},
	}
	for _, tc := range cases {
		if int(tc.status) != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, int(tc.status), tc.want)
		}
		if tc.status.String() != tc.name {
			t.Errorf("%v.String() = %q, want %q", tc.status, tc.status.String(), tc.name)
		}
	}
}

func TestSessionStatusUnknownString(t *testing.T) {
	bogus := SessionStatus(99)
	got := bogus.String()
	if !strings.Contains(got, "unknown") || !strings.Contains(got, "99") {
		t.Errorf("unknown SessionStatus(99).String() = %q; want \"unknown(99)\"-ish", got)
	}

	neg := SessionStatus(-7)
	gotNeg := neg.String()
	if !strings.Contains(gotNeg, "unknown") || !strings.Contains(gotNeg, "-7") {
		t.Errorf("unknown SessionStatus(-7).String() = %q; want \"unknown(-7)\"-ish", gotNeg)
	}
}

func TestSessionValueType(t *testing.T) {
	s := Session{
		Alias:        "internal-platform-x",
		Sha8:         "a3f5b2c8",
		Name:         "zen-internal-platform-x-a3f5b2c8",
		CreatedAt:    time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC),
		LastAttachAt: time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC),
		Status:       StatusActive,
	}
	if s.Alias != "internal-platform-x" || s.Status != StatusActive {
		t.Errorf("field round-trip broken: %+v", s)
	}

	cp := s
	cp.Status = StatusIdle
	if s.Status != StatusActive {
		t.Errorf("copy mutation leaked to original: s.Status = %v", s.Status)
	}

	if got := SessionName(s.Alias, s.Sha8); got != s.Name {
		t.Errorf("round-trip invariant broken: SessionName(%q,%q) = %q, want %q",
			s.Alias, s.Sha8, got, s.Name)
	}
}

func TestSessionStoreInterface(t *testing.T) {
	var _ SessionStore = (*fakeSessionStore)(nil)
}

func TestFakeSessionStoreRoundTrip(t *testing.T) {
	f := newFakeSessionStore()
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	s := Session{
		Alias:        "internal-platform-x",
		Sha8:         "a3f5b2c8",
		Name:         "zen-internal-platform-x-a3f5b2c8",
		CreatedAt:    now,
		LastAttachAt: now,
		Status:       StatusActive,
	}

	if err := f.UpsertSession(s); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	got, err := f.GetSession(s.Name)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Alias != s.Alias || got.Status != s.Status {
		t.Errorf("GetSession round-trip mismatch: got %+v, want %+v", got, s)
	}

	rows, err := f.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != s.Name {
		t.Errorf("ListSessions = %+v, want 1 row matching %q", rows, s.Name)
	}

	later := now.Add(time.Hour)
	if err := f.SetLastAttach(s.Name, later); err != nil {
		t.Fatalf("SetLastAttach: %v", err)
	}
	got2, _ := f.GetSession(s.Name)
	if !got2.LastAttachAt.Equal(later) {
		t.Errorf("LastAttachAt = %v, want %v", got2.LastAttachAt, later)
	}

	if err := f.SetStatus(s.Name, StatusIdle); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	got3, _ := f.GetSession(s.Name)
	if got3.Status != StatusIdle {
		t.Errorf("Status = %v, want %v", got3.Status, StatusIdle)
	}

	if err := f.DeleteSession(s.Name); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := f.GetSession(s.Name); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("GetSession after delete: err = %v, want ErrSessionNotFound", err)
	}
}

func TestFakeSessionStoreNotFound(t *testing.T) {
	f := newFakeSessionStore()
	if _, err := f.GetSession("nope"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("GetSession nope: err = %v, want ErrSessionNotFound", err)
	}
	if err := f.DeleteSession("nope"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("DeleteSession nope: err = %v, want ErrSessionNotFound", err)
	}
	if err := f.SetLastAttach("nope", time.Now()); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("SetLastAttach nope: err = %v, want ErrSessionNotFound", err)
	}
	if err := f.SetStatus("nope", StatusIdle); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("SetStatus nope: err = %v, want ErrSessionNotFound", err)
	}
}

type fakeSessionStore struct {
	rows          map[string]Session
	expectedPanes map[string]map[WindowName][]string
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{
		rows:          map[string]Session{},
		expectedPanes: map[string]map[WindowName][]string{},
	}
}

func (f *fakeSessionStore) UpsertSession(s Session) error {
	f.rows[s.Name] = s
	return nil
}

func (f *fakeSessionStore) GetSession(name string) (Session, error) {
	s, ok := f.rows[name]
	if !ok {
		return Session{}, ErrSessionNotFound
	}
	return s, nil
}

func (f *fakeSessionStore) ListSessions() ([]Session, error) {
	out := make([]Session, 0, len(f.rows))
	for _, s := range f.rows {
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeSessionStore) DeleteSession(name string) error {
	if _, ok := f.rows[name]; !ok {
		return ErrSessionNotFound
	}
	delete(f.rows, name)
	return nil
}

func (f *fakeSessionStore) SetLastAttach(name string, t time.Time) error {
	s, ok := f.rows[name]
	if !ok {
		return ErrSessionNotFound
	}
	s.LastAttachAt = t
	f.rows[name] = s
	return nil
}

func (f *fakeSessionStore) SetStatus(name string, st SessionStatus) error {
	s, ok := f.rows[name]
	if !ok {
		return ErrSessionNotFound
	}
	s.Status = st
	f.rows[name] = s
	return nil
}

func (f *fakeSessionStore) ExpectedPanesFor(sessionName string) (map[WindowName][]string, error) {
	m := f.expectedPanes[sessionName]
	if m == nil {
		return map[WindowName][]string{}, nil
	}

	out := make(map[WindowName][]string, len(m))
	for k, v := range m {
		copied := make([]string, len(v))
		copy(copied, v)
		out[k] = copied
	}
	return out, nil
}

func (f *fakeSessionStore) setExpectedPanes(sessionName string, panes map[WindowName][]string) {
	f.expectedPanes[sessionName] = panes
}

func TestManagerNewWiring(t *testing.T) {
	st := newFakeSessionStore()
	m := New(st)
	if m == nil {
		t.Fatalf("New returned nil")
	}
	if m.store != st {
		t.Errorf("Manager.store not wired")
	}
	if m.exec == nil {
		t.Errorf("Manager.exec not wired (should default to ExecTmux)")
	}
}

func TestManagerNewPanicsOnNilStore(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("New(nil) did not panic")
		}
	}()
	_ = New(nil)
}

func TestManagerSpawnIdempotentSecondCall(t *testing.T) {
	st := newFakeSessionStore()
	exec := &fakeExecutor{}
	m := New(st)
	m.exec = exec.Exec

	ctx := context.Background()
	exec.responses = map[string]execResp{
		"has-session-zen-test-12345678":   {err: errors.New("can't find session")},
		"new-session-zen-test-12345678":   {out: []byte(""), err: nil},
		"has-session-zen-test-12345678#2": {err: nil},
	}

	first, err := m.Spawn(ctx, "test", "12345678")
	if err != nil {
		t.Fatalf("first Spawn: %v", err)
	}
	if first.Name != "zen-test-12345678" {
		t.Errorf("Name = %q, want zen-test-12345678", first.Name)
	}
	if first.Status != StatusActive {
		t.Errorf("Status = %v, want StatusActive", first.Status)
	}
	if first.Alias != "test" || first.Sha8 != "12345678" {
		t.Errorf("Alias/Sha8 = %q/%q, want test/12345678", first.Alias, first.Sha8)
	}
	if first.CreatedAt.IsZero() {
		t.Errorf("CreatedAt is zero; expected timestamp")
	}
	if !first.LastAttachAt.IsZero() {
		t.Errorf("LastAttachAt = %v, want zero (never attached)", first.LastAttachAt)
	}

	_, err = m.Spawn(ctx, "test", "12345678")
	if !errors.Is(err, ErrSessionExists) {
		t.Errorf("second Spawn err = %v, want ErrSessionExists", err)
	}
}

func TestManagerSpawnPersistsRow(t *testing.T) {
	st := newFakeSessionStore()
	exec := &fakeExecutor{}
	exec.responses = map[string]execResp{
		"has-session-zen-foo-deadbeef": {err: errors.New("not found")},
		"new-session-zen-foo-deadbeef": {out: []byte(""), err: nil},
	}
	m := New(st)
	m.exec = exec.Exec
	ctx := context.Background()

	s, err := m.Spawn(ctx, "foo", "deadbeef")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	got, err := st.GetSession("zen-foo-deadbeef")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Alias != "foo" || got.Sha8 != "deadbeef" {
		t.Errorf("persisted row alias/sha8 = %q/%q, want foo/deadbeef",
			got.Alias, got.Sha8)
	}
	if got.Name != "zen-foo-deadbeef" {
		t.Errorf("persisted Name = %q, want zen-foo-deadbeef", got.Name)
	}
	if got.Status != StatusActive {
		t.Errorf("persisted Status = %v, want StatusActive", got.Status)
	}
	if !s.CreatedAt.Equal(got.CreatedAt) {
		t.Errorf("CreatedAt mismatch %v vs %v", s.CreatedAt, got.CreatedAt)
	}
}

func TestManagerSpawnTmuxFailureRollsBack(t *testing.T) {
	st := newFakeSessionStore()
	exec := &fakeExecutor{}
	exec.responses = map[string]execResp{
		"has-session-zen-bar-12345678": {err: errors.New("not found")},
		"new-session-zen-bar-12345678": {err: errors.New("tmux: out of resources")},
	}
	m := New(st)
	m.exec = exec.Exec
	ctx := context.Background()

	_, err := m.Spawn(ctx, "bar", "12345678")
	if err == nil {
		t.Fatalf("Spawn succeeded on tmux failure; expected error")
	}
	if !strings.Contains(err.Error(), "out of resources") {
		t.Errorf("error = %v; want wrap of underlying tmux error", err)
	}
	// Persisted row MUST be absent — Spawn rolled back on tmux failure.
	if _, getErr := st.GetSession("zen-bar-12345678"); !errors.Is(getErr, ErrSessionNotFound) {
		t.Errorf("row leaked after spawn failure; rollback violated (err=%v)", getErr)
	}
}

func TestManagerSpawnDuplicateSessionRace(t *testing.T) {
	st := newFakeSessionStore()
	exec := &fakeExecutor{}
	exec.responses = map[string]execResp{
		"has-session-zen-race-12345678": {err: errors.New("not found")},
		"new-session-zen-race-12345678": {err: errors.New("duplicate session: zen-race-12345678")},
	}
	m := New(st)
	m.exec = exec.Exec
	ctx := context.Background()

	_, err := m.Spawn(ctx, "race", "12345678")
	if !errors.Is(err, ErrSessionExists) {
		t.Errorf("duplicate-session err = %v, want ErrSessionExists", err)
	}

	if _, getErr := st.GetSession("zen-race-12345678"); !errors.Is(getErr, ErrSessionNotFound) {
		t.Errorf("row leaked after duplicate-session; rollback violated (err=%v)", getErr)
	}
}

func TestManagerSpawnHasSessionTransportError(t *testing.T) {
	st := newFakeSessionStore()
	exec := &fakeExecutor{}
	exec.responses = map[string]execResp{
		"has-session-zen-net-12345678": {err: errors.New("error connecting to /tmp/zen-swarm.sock")},
	}
	m := New(st)
	m.exec = exec.Exec
	ctx := context.Background()

	_, err := m.Spawn(ctx, "net", "12345678")
	if err == nil {
		t.Fatalf("Spawn succeeded on transport error; expected error")
	}
	if errors.Is(err, ErrSessionExists) || errors.Is(err, ErrSessionNotFound) {
		t.Errorf("transport err mapped to sentinel: %v", err)
	}

	if len(exec.calls) != 1 || exec.calls[0] != "has-session-zen-net-12345678" {
		t.Errorf("calls = %v; want exactly [has-session-...]", exec.calls)
	}
}

func TestManagerSpawnStoreFailureKillsTmuxSession(t *testing.T) {
	st := &failingStore{
		fakeSessionStore: newFakeSessionStore(),
		upsertErr:        errors.New("disk full"),
	}
	exec := &fakeExecutor{}
	exec.responses = map[string]execResp{
		"has-session-zen-disk-12345678":  {err: errors.New("not found")},
		"new-session-zen-disk-12345678":  {out: []byte(""), err: nil},
		"kill-session-zen-disk-12345678": {out: []byte(""), err: nil},
	}
	m := New(st)
	m.exec = exec.Exec
	ctx := context.Background()

	_, err := m.Spawn(ctx, "disk", "12345678")
	if err == nil {
		t.Fatalf("Spawn succeeded on store failure; expected error")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Errorf("error = %v; want wrap of underlying store error", err)
	}

	foundKill := false
	for _, op := range exec.calls {
		if op == "kill-session-zen-disk-12345678" {
			foundKill = true
			break
		}
	}
	if !foundKill {
		t.Errorf("kill-session not invoked after store failure; calls = %v", exec.calls)
	}
}

func TestManagerSpawnContextCanceled(t *testing.T) {
	st := newFakeSessionStore()
	exec := &fakeExecutor{honorCtx: true}
	m := New(st)
	m.exec = exec.Exec

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := m.Spawn(ctx, "cancel", "12345678")
	if err == nil {
		t.Fatalf("Spawn succeeded on canceled ctx; expected error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v; want errors.Is context.Canceled", err)
	}

	if _, getErr := st.GetSession("zen-cancel-12345678"); !errors.Is(getErr, ErrSessionNotFound) {
		t.Errorf("row leaked after ctx cancel; rollback violated (err=%v)", getErr)
	}
}

type failingStore struct {
	*fakeSessionStore
	upsertErr        error
	listErr          error
	setLastAttachErr error
	getErr           error
}

func (s *failingStore) UpsertSession(sess Session) error {
	if s.upsertErr != nil {
		return s.upsertErr
	}
	return s.fakeSessionStore.UpsertSession(sess)
}

func (s *failingStore) ListSessions() ([]Session, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.fakeSessionStore.ListSessions()
}

func (s *failingStore) GetSession(name string) (Session, error) {
	if s.getErr != nil {
		return Session{}, s.getErr
	}
	return s.fakeSessionStore.GetSession(name)
}

func (s *failingStore) SetLastAttach(name string, t time.Time) error {
	if s.setLastAttachErr != nil {
		return s.setLastAttachErr
	}
	return s.fakeSessionStore.SetLastAttach(name, t)
}

type fakeExecutor struct {
	calls     []string
	responses map[string]execResp

	honorCtx bool
}

type execResp struct {
	out []byte
	err error
}

func (f *fakeExecutor) Exec(ctx context.Context, args ...string) ([]byte, error) {
	if f.honorCtx {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	op := f.synthOp(args)
	occurrences := 0
	for _, prior := range f.calls {
		if prior == op {
			occurrences++
		}
	}
	f.calls = append(f.calls, op)
	key := op
	if occurrences > 0 {
		key = fmt.Sprintf("%s#%d", op, occurrences+1)
	}
	resp, ok := f.responses[key]
	if !ok {
		return nil, fmt.Errorf("fakeExecutor: no response for %q (calls so far: %v)", key, f.calls)
	}
	return resp.out, resp.err
}

func (f *fakeExecutor) synthOp(args []string) string {

	skipNext := false
	clean := make([]string, 0, len(args))
	for _, a := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if a == "-S" {
			skipNext = true
			continue
		}
		clean = append(clean, a)
	}
	if len(clean) == 0 {
		return ""
	}
	switch {
	case len(clean) >= 3 && clean[0] == "has-session" && clean[1] == "-t":
		return "has-session-" + clean[2]
	case len(clean) >= 4 && clean[0] == "new-session":

		for i, a := range clean {
			if a == "-s" && i+1 < len(clean) {
				return "new-session-" + clean[i+1]
			}
		}
	case len(clean) >= 3 && clean[0] == "kill-session" && clean[1] == "-t":
		return "kill-session-" + clean[2]
	case len(clean) >= 2 && clean[0] == "rename-window":

		var target, newname string
		for i, a := range clean {
			if a == "-t" && i+1 < len(clean) {
				target = clean[i+1]
			}
		}
		if len(clean) > 0 {
			newname = clean[len(clean)-1]
		}
		return "rename-window-" + strings.ReplaceAll(target, ":", "-") + "-" + newname
	case len(clean) >= 2 && clean[0] == "new-window":

		var session, name string
		for i, a := range clean {
			if a == "-t" && i+1 < len(clean) {
				session = clean[i+1]
			}
			if a == "-n" && i+1 < len(clean) {
				name = clean[i+1]
			}
		}
		return "new-window-" + session + "-" + name
	}
	return strings.Join(clean, " ")
}

func TestManagerAttachReturnsCommand(t *testing.T) {
	st := newFakeSessionStore()
	if err := st.UpsertSession(Session{
		Alias: "internal-platform-x", Sha8: "a3f5b2c8",
		Name: "zen-internal-platform-x-a3f5b2c8", Status: StatusActive,
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	exec := &fakeExecutor{}
	exec.responses = map[string]execResp{
		"has-session-zen-internal-platform-x-a3f5b2c8": {err: nil},
	}
	m := New(st)
	m.exec = exec.Exec

	cmd, err := m.Attach(context.Background(), "internal-platform-x", WindowOrch)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	expected := []string{
		"tmux", "-S", "/tmp/zen-swarm.sock",
		"attach", "-t", "zen-internal-platform-x-a3f5b2c8:orch",
	}
	if !reflect.DeepEqual(cmd, expected) {
		t.Errorf("cmd = %v\nwant %v", cmd, expected)
	}

	got, _ := st.GetSession("zen-internal-platform-x-a3f5b2c8")
	if got.LastAttachAt.IsZero() {
		t.Errorf("LastAttachAt not updated")
	}
}

func TestManagerAttachInvalidWindowRejected(t *testing.T) {
	st := newFakeSessionStore()
	if err := st.UpsertSession(Session{Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	m := New(st)
	m.exec = (&fakeExecutor{}).Exec

	_, err := m.Attach(context.Background(), "x", WindowName("bogus"))
	if err == nil {
		t.Fatalf("Attach with invalid window did not error")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("err = %v; missing window name in message", err)
	}
}

func TestManagerAttachUnknownAlias(t *testing.T) {
	m := New(newFakeSessionStore())
	m.exec = (&fakeExecutor{}).Exec
	_, err := m.Attach(context.Background(), "missing", WindowOrch)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("err = %v, want ErrSessionNotFound", err)
	}
}

func TestManagerAttachOrphanedSession(t *testing.T) {
	st := newFakeSessionStore()
	if err := st.UpsertSession(Session{Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	exec := &fakeExecutor{}
	exec.responses = map[string]execResp{
		"has-session-zen-x-12345678": {err: errors.New("can't find session")},
	}
	m := New(st)
	m.exec = exec.Exec

	_, err := m.Attach(context.Background(), "x", WindowOrch)
	if err == nil {
		t.Fatalf("expected error on orphaned session")
	}

	got, _ := st.GetSession("zen-x-12345678")
	if got.Status != StatusOrphaned {
		t.Errorf("Status = %v, want StatusOrphaned", got.Status)
	}
}

func TestManagerAttachHasSessionTransportError(t *testing.T) {
	st := newFakeSessionStore()
	if err := st.UpsertSession(Session{Alias: "net", Sha8: "12345678", Name: "zen-net-12345678", Status: StatusActive}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	exec := &fakeExecutor{}
	exec.responses = map[string]execResp{
		"has-session-zen-net-12345678": {err: errors.New("error connecting to /tmp/zen-swarm.sock")},
	}
	m := New(st)
	m.exec = exec.Exec

	_, err := m.Attach(context.Background(), "net", WindowOrch)
	if err == nil {
		t.Fatalf("Attach succeeded on transport error; expected error")
	}
	if errors.Is(err, ErrSessionNotFound) {
		t.Errorf("transport err mapped to ErrSessionNotFound: %v", err)
	}

	got, _ := st.GetSession("zen-net-12345678")
	if got.Status != StatusActive {
		t.Errorf("Status = %v, want StatusActive (transport error must not flip to Orphaned)", got.Status)
	}
}

func TestManagerListReturnsAllRows(t *testing.T) {
	st := newFakeSessionStore()
	if err := st.UpsertSession(Session{Alias: "a", Sha8: "11111111", Name: "zen-a-11111111", Status: StatusActive}); err != nil {
		t.Fatalf("UpsertSession a: %v", err)
	}
	if err := st.UpsertSession(Session{Alias: "b", Sha8: "22222222", Name: "zen-b-22222222", Status: StatusIdle}); err != nil {
		t.Fatalf("UpsertSession b: %v", err)
	}
	if err := st.UpsertSession(Session{Alias: "c", Sha8: "33333333", Name: "zen-c-33333333", Status: StatusOrphaned}); err != nil {
		t.Fatalf("UpsertSession c: %v", err)
	}

	m := New(st)
	m.exec = (&fakeExecutor{}).Exec

	got, err := m.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("len = %d, want 3", len(got))
	}
}

func TestManagerListEmpty(t *testing.T) {
	m := New(newFakeSessionStore())
	m.exec = (&fakeExecutor{}).Exec
	got, err := m.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestManagerListPropagatesStoreError(t *testing.T) {
	st := &failingStore{
		fakeSessionStore: newFakeSessionStore(),
		listErr:          errors.New("disk read error"),
	}
	m := New(st)
	m.exec = (&fakeExecutor{}).Exec
	_, err := m.List(context.Background())
	if err == nil {
		t.Fatalf("List succeeded on store error; expected error")
	}
	if !strings.Contains(err.Error(), "disk read error") {
		t.Errorf("err = %v; want wrap of underlying store error", err)
	}
}

func TestManagerTeardownNoSnapshotKillsAndArchives(t *testing.T) {
	st := newFakeSessionStore()
	if err := st.UpsertSession(Session{Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	exec := &fakeExecutor{}
	exec.responses = map[string]execResp{
		"has-session-zen-x-12345678":  {err: nil},
		"kill-session-zen-x-12345678": {err: nil},
	}
	m := New(st)
	m.exec = exec.Exec

	if err := m.Teardown(context.Background(), "x", false); err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	got, _ := st.GetSession("zen-x-12345678")
	if got.Status != StatusArchived {
		t.Errorf("Status = %v, want StatusArchived", got.Status)
	}

	found := false
	for _, c := range exec.calls {
		if c == "kill-session-zen-x-12345678" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("kill-session not called; calls: %v", exec.calls)
	}
}

func TestManagerTeardownUnknownAlias(t *testing.T) {
	m := New(newFakeSessionStore())
	m.exec = (&fakeExecutor{}).Exec
	err := m.Teardown(context.Background(), "missing", false)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("err = %v, want ErrSessionNotFound", err)
	}
}

func TestManagerTeardownAlreadyArchivedNoop(t *testing.T) {
	st := newFakeSessionStore()
	if err := st.UpsertSession(Session{Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusArchived}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	exec := &fakeExecutor{}
	m := New(st)
	m.exec = exec.Exec

	if err := m.Teardown(context.Background(), "x", false); err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if len(exec.calls) != 0 {
		t.Errorf("unexpected exec calls on already-archived: %v", exec.calls)
	}
}

func TestManagerTeardownKillSessionFailureStillArchives(t *testing.T) {
	st := newFakeSessionStore()
	if err := st.UpsertSession(Session{Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	exec := &fakeExecutor{}
	exec.responses = map[string]execResp{
		"kill-session-zen-x-12345678": {err: errors.New("server not found on socket")},
	}
	m := New(st)
	m.exec = exec.Exec

	err := m.Teardown(context.Background(), "x", false)
	if err == nil {
		t.Fatalf("Teardown succeeded on kill failure; expected wrapped error")
	}
	if !strings.Contains(err.Error(), "server not found") {
		t.Errorf("err = %v; want wrap of underlying tmux error", err)
	}

	got, _ := st.GetSession("zen-x-12345678")
	if got.Status != StatusArchived {
		t.Errorf("Status = %v, want StatusArchived even on kill failure", got.Status)
	}
}

func TestManagerTeardownSnapshotTrueSucceedsWithFakeResurrect(t *testing.T) {
	st := newFakeSessionStore()
	if err := st.UpsertSession(Session{Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	exec := &fakeExecutor{}
	exec.responses = map[string]execResp{
		"kill-session-zen-x-12345678": {err: nil},
	}
	m := New(st)
	m.exec = exec.Exec
	m.resurrect = &fakeResurrectExec{}
	m.snapshotDir = t.TempDir()

	if err := m.Teardown(context.Background(), "x", true); err != nil {
		t.Fatalf("Teardown(snapshot=true): %v", err)
	}
	got, _ := st.GetSession("zen-x-12345678")
	if got.Status != StatusArchived {
		t.Errorf("Status = %v, want StatusArchived after snapshot+kill success", got.Status)
	}

	killCount := 0
	for _, c := range exec.calls {
		if c == "kill-session-zen-x-12345678" {
			killCount++
		}
	}
	if killCount != 1 {
		t.Errorf("kill-session calls = %d, want 1; calls = %v", killCount, exec.calls)
	}
}

func TestManagerTeardownSnapshotTrueWrapsSaveError(t *testing.T) {
	st := newFakeSessionStore()
	if err := st.UpsertSession(Session{Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	exec := &fakeExecutor{}
	m := New(st)
	m.exec = exec.Exec
	m.resurrect = &fakeResurrectExec{saveErr: errors.New("simulated plugin failure")}
	m.snapshotDir = t.TempDir()

	err := m.Teardown(context.Background(), "x", true)
	if err == nil {
		t.Fatalf("Teardown(snapshot=true) succeeded; expected wrapped Save error")
	}
	if !strings.Contains(err.Error(), "snapshot first") {
		t.Errorf("err = %v; want 'snapshot first' wrap", err)
	}
	if !strings.Contains(err.Error(), "simulated plugin failure") {
		t.Errorf("err = %v; want underlying 'simulated plugin failure'", err)
	}

	for _, c := range exec.calls {
		if strings.HasPrefix(c, "kill-session") {
			t.Errorf("kill-session called after snapshot failure: %v", exec.calls)
		}
	}

	got, _ := st.GetSession("zen-x-12345678")
	if got.Status != StatusActive {
		t.Errorf("Status = %v, want StatusActive (teardown aborted on snapshot failure)", got.Status)
	}
}

func TestManagerTeardownSnapshotTrueDefaultManagerFailsCleanly(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	st := newFakeSessionStore()
	if err := st.UpsertSession(Session{Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	exec := &fakeExecutor{}
	m := New(st)
	m.exec = exec.Exec

	err := m.Teardown(context.Background(), "x", true)
	if err == nil {
		t.Fatalf("Teardown succeeded; expected plugin-not-installed error")
	}
	if !strings.Contains(err.Error(), "snapshot first") {
		t.Errorf("err = %v; want 'snapshot first' wrap", err)
	}

	got, _ := st.GetSession("zen-x-12345678")
	if got.Status != StatusActive {
		t.Errorf("Status = %v, want StatusActive", got.Status)
	}
}

func TestManagerResolveAliasLinearScanFindsAcrossRows(t *testing.T) {
	st := newFakeSessionStore()
	if err := st.UpsertSession(Session{Alias: "a", Sha8: "11111111", Name: "zen-a-11111111", Status: StatusActive}); err != nil {
		t.Fatalf("UpsertSession a: %v", err)
	}
	if err := st.UpsertSession(Session{Alias: "b", Sha8: "22222222", Name: "zen-b-22222222", Status: StatusActive}); err != nil {
		t.Fatalf("UpsertSession b: %v", err)
	}
	exec := &fakeExecutor{}
	exec.responses = map[string]execResp{
		"has-session-zen-b-22222222": {err: nil},
	}
	m := New(st)
	m.exec = exec.Exec

	cmd, err := m.Attach(context.Background(), "b", WindowOrch)
	if err != nil {
		t.Fatalf("Attach b: %v", err)
	}
	if !strings.Contains(strings.Join(cmd, " "), "zen-b-22222222") {
		t.Errorf("cmd = %v; want target referencing zen-b-22222222", cmd)
	}
}

func TestManagerResolveAliasStoreFailure(t *testing.T) {
	st := &failingStore{
		fakeSessionStore: newFakeSessionStore(),
		listErr:          errors.New("disk read error"),
	}
	m := New(st)
	m.exec = (&fakeExecutor{}).Exec

	_, err := m.Attach(context.Background(), "x", WindowOrch)
	if err == nil {
		t.Fatalf("Attach succeeded on store list failure; expected error")
	}
	if !strings.Contains(err.Error(), "disk read error") {
		t.Errorf("err = %v; want wrap of underlying store error", err)
	}
}

func TestManagerAttachSetLastAttachFailureBestEffort(t *testing.T) {
	inner := newFakeSessionStore()
	if err := inner.UpsertSession(Session{Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	st := &failingStore{
		fakeSessionStore: inner,
		setLastAttachErr: errors.New("disk full"),
	}
	exec := &fakeExecutor{}
	exec.responses = map[string]execResp{
		"has-session-zen-x-12345678": {err: nil},
	}
	m := New(st)
	m.exec = exec.Exec

	cmd, err := m.Attach(context.Background(), "x", WindowOrch)
	if err != nil {
		t.Fatalf("Attach failed despite best-effort SetLastAttach: %v", err)
	}
	if len(cmd) == 0 || cmd[0] != "tmux" {
		t.Errorf("cmd = %v; want non-empty tmux args even on metadata failure", cmd)
	}
}
