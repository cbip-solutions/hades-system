package tmuxlife

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

type fakeProberExec struct {
	calls  [][]string
	stdout string
	err    error
}

func (f *fakeProberExec) run(ctx context.Context, args ...string) ([]byte, error) {
	f.calls = append(f.calls, args)
	if f.err != nil {
		return nil, f.err
	}
	return []byte(f.stdout), nil
}

type fakeFileInfo struct {
	mode os.FileMode
}

func (f *fakeFileInfo) Name() string       { return "" }
func (f *fakeFileInfo) Size() int64        { return 0 }
func (f *fakeFileInfo) Mode() os.FileMode  { return f.mode }
func (f *fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f *fakeFileInfo) IsDir() bool        { return false }
func (f *fakeFileInfo) Sys() any           { return nil }

func TestProberBinaryVersionParsesPlain(t *testing.T) {
	exec := &fakeProberExec{stdout: "tmux 3.5a\n"}
	p := &Prober{store: newFakeSessionStore(), exec: exec.run}
	v, meets, err := p.BinaryVersion(context.Background())
	if err != nil {
		t.Fatalf("BinaryVersion: %v", err)
	}
	if v != "3.5a" {
		t.Errorf("version = %q, want 3.5a", v)
	}
	if !meets {
		t.Errorf("meetsMin = false, want true (3.5a >= 3.4)")
	}
}

func TestProberBinaryVersionExactlyMin(t *testing.T) {
	exec := &fakeProberExec{stdout: "tmux 3.4\n"}
	p := &Prober{store: newFakeSessionStore(), exec: exec.run}
	v, meets, err := p.BinaryVersion(context.Background())
	if err != nil {
		t.Fatalf("BinaryVersion: %v", err)
	}
	if v != "3.4" {
		t.Errorf("version = %q, want 3.4", v)
	}
	if !meets {
		t.Errorf("meetsMin = false, want true (3.4 >= 3.4)")
	}
}

func TestProberBinaryVersionTooOld(t *testing.T) {
	exec := &fakeProberExec{stdout: "tmux 3.0\n"}
	p := &Prober{store: newFakeSessionStore(), exec: exec.run}
	_, meets, err := p.BinaryVersion(context.Background())
	if err != nil {
		t.Fatalf("BinaryVersion: %v", err)
	}
	if meets {
		t.Errorf("meetsMin = true, want false (3.0 < 3.4)")
	}
}

func TestProberBinaryVersionMissing(t *testing.T) {
	exec := &fakeProberExec{err: errors.New(`exec: "tmux": not found`)}
	p := &Prober{store: newFakeSessionStore(), exec: exec.run}
	_, _, err := p.BinaryVersion(context.Background())
	if err == nil {
		t.Error("expected error when tmux missing")
	}
}

func TestProberBinaryVersionUnparseable(t *testing.T) {
	exec := &fakeProberExec{stdout: "tmux\n"}
	p := &Prober{store: newFakeSessionStore(), exec: exec.run}
	_, _, err := p.BinaryVersion(context.Background())
	if err == nil {
		t.Error("expected parse error on malformed output")
	}
}

func TestProberServerReachableHealthy(t *testing.T) {
	exec := &fakeProberExec{stdout: ""}
	p := &Prober{store: newFakeSessionStore(), exec: exec.run}
	if err := p.ServerReachable(context.Background()); err != nil {
		t.Errorf("ServerReachable: %v", err)
	}

	if len(exec.calls) == 0 {
		t.Fatal("no exec calls")
	}
	hasS := false
	hasSocket := false
	for _, arg := range exec.calls[0] {
		if arg == "-S" {
			hasS = true
		}
		if arg == SocketPath {
			hasSocket = true
		}
	}
	if !hasS {
		t.Errorf("inv-zen-117: -S flag missing in args %v", exec.calls[0])
	}
	if !hasSocket {
		t.Errorf("inv-zen-117: SocketPath missing in args %v", exec.calls[0])
	}
}

func TestProberServerReachableEmptyOK(t *testing.T) {

	exec := &fakeProberExec{err: errors.New("no server running on /tmp/zen-swarm.sock")}
	p := &Prober{store: newFakeSessionStore(), exec: exec.run}
	if err := p.ServerReachable(context.Background()); err != nil {
		t.Errorf("ServerReachable empty: %v", err)
	}
}

func TestProberServerReachableHardError(t *testing.T) {
	exec := &fakeProberExec{err: errors.New("permission denied")}
	p := &Prober{store: newFakeSessionStore(), exec: exec.run}
	if err := p.ServerReachable(context.Background()); err == nil {
		t.Error("ServerReachable: expected hard error to surface")
	}
}

func TestProberSessionCount(t *testing.T) {
	store := newFakeSessionStore()
	store.UpsertSession(Session{Alias: "a", Sha8: "12345678", Name: "zen-a-12345678", Status: StatusActive})
	store.UpsertSession(Session{Alias: "b", Sha8: "abcdef12", Name: "zen-b-abcdef12", Status: StatusActive})
	store.UpsertSession(Session{Alias: "c", Sha8: "deadbeef", Name: "zen-c-deadbeef", Status: StatusOrphaned})
	p := &Prober{store: store, exec: (&fakeProberExec{}).run}
	n, err := p.SessionCount(context.Background())
	if err != nil {
		t.Fatalf("SessionCount: %v", err)
	}
	if n != 2 {
		t.Errorf("count = %d, want 2 (only Active)", n)
	}
}

func TestProberSessionCountEmpty(t *testing.T) {
	p := &Prober{store: newFakeSessionStore(), exec: (&fakeProberExec{}).run}
	n, err := p.SessionCount(context.Background())
	if err != nil {
		t.Fatalf("SessionCount: %v", err)
	}
	if n != 0 {
		t.Errorf("count = %d, want 0", n)
	}
}

func TestProberDriftCount(t *testing.T) {
	store := newFakeSessionStore()
	store.UpsertSession(Session{Alias: "a", Sha8: "12345678", Name: "zen-a-12345678", Status: StatusActive})
	store.UpsertSession(Session{Alias: "b", Sha8: "abcdef12", Name: "zen-b-abcdef12", Status: StatusOrphaned})
	store.UpsertSession(Session{Alias: "c", Sha8: "deadbeef", Name: "zen-c-deadbeef", Status: StatusOrphaned})
	p := &Prober{store: store, exec: (&fakeProberExec{}).run}
	n, err := p.DriftCount(context.Background())
	if err != nil {
		t.Fatalf("DriftCount: %v", err)
	}
	if n != 2 {
		t.Errorf("count = %d, want 2 (Orphaned)", n)
	}
}

func TestProberSocketPermissions0600(t *testing.T) {
	p := &Prober{
		store: newFakeSessionStore(),
		exec:  (&fakeProberExec{}).run,
		statFile: func(path string) (os.FileInfo, error) {
			if path != SocketPath {
				t.Errorf("statFile called with %q, want %q (inv-zen-117)", path, SocketPath)
			}
			return &fakeFileInfo{mode: 0o600}, nil
		},
	}
	mode, err := p.SocketPermissions(context.Background())
	if err != nil {
		t.Fatalf("SocketPermissions: %v", err)
	}
	if mode != "0600" {
		t.Errorf("mode = %q, want 0600", mode)
	}
}

func TestProberSocketPermissions0644(t *testing.T) {
	p := &Prober{
		store: newFakeSessionStore(),
		exec:  (&fakeProberExec{}).run,
		statFile: func(path string) (os.FileInfo, error) {
			return &fakeFileInfo{mode: 0o644}, nil
		},
	}
	mode, err := p.SocketPermissions(context.Background())
	if err != nil {
		t.Fatalf("SocketPermissions: %v", err)
	}
	if mode != "0644" {
		t.Errorf("mode = %q, want 0644", mode)
	}
}

func TestProberSocketPermissionsMissing(t *testing.T) {
	p := &Prober{
		store: newFakeSessionStore(),
		exec:  (&fakeProberExec{}).run,
		statFile: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	}
	_, err := p.SocketPermissions(context.Background())
	if err == nil {
		t.Error("expected error for missing socket")
	}
}

func TestProberSocketPermissionsOtherStatError(t *testing.T) {
	p := &Prober{
		store: newFakeSessionStore(),
		exec:  (&fakeProberExec{}).run,
		statFile: func(path string) (os.FileInfo, error) {
			return nil, errors.New("permission denied")
		},
	}
	_, err := p.SocketPermissions(context.Background())
	if err == nil {
		t.Error("expected error on stat failure")
	}
}

func TestProberNewPanicsOnNilStore(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewProber(nil, *) should panic")
		}
	}()
	_ = NewProber(nil, (&fakeProberExec{}).run)
}

func TestProberNewPanicsOnNilExec(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewProber(*, nil) should panic")
		}
	}()
	_ = NewProber(newFakeSessionStore(), nil)
}

func TestProberNewWiresStatToOSStat(t *testing.T) {

	p := NewProber(newFakeSessionStore(), (&fakeProberExec{}).run)
	if p.statFile == nil {
		t.Error("NewProber: statFile not defaulted")
	}
}

func TestProberSessionListErrorPropagates(t *testing.T) {
	store := failingListStore{}
	p := &Prober{store: store, exec: (&fakeProberExec{}).run}
	if _, err := p.SessionCount(context.Background()); err == nil {
		t.Error("SessionCount: expected error")
	}
	if _, err := p.DriftCount(context.Background()); err == nil {
		t.Error("DriftCount: expected error")
	}
}

func TestTmuxVersionAtLeastMajorBump(t *testing.T) {
	got, err := tmuxVersionAtLeast("4.0", "3.4")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if !got {
		t.Errorf("4.0 >= 3.4: got false, want true")
	}
	got, err = tmuxVersionAtLeast("2.9", "3.4")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got {
		t.Errorf("2.9 >= 3.4: got true, want false")
	}
}

func TestTmuxVersionAtLeastBadGot(t *testing.T) {
	_, err := tmuxVersionAtLeast("notaversion", "3.4")
	if err == nil {
		t.Error("expected parse error on bad got")
	}
}

func TestTmuxVersionAtLeastBadMin(t *testing.T) {
	_, err := tmuxVersionAtLeast("3.5", "notaversion")
	if err == nil {
		t.Error("expected parse error on bad min")
	}
}

func TestSplitTmuxProberVersionNonMajorMinor(t *testing.T) {
	_, _, err := splitTmuxProberVersion("3")
	if err == nil {
		t.Error("expected error on missing minor")
	}
}

func TestSplitTmuxProberVersionBadMajor(t *testing.T) {
	_, _, err := splitTmuxProberVersion(".4")
	if err == nil {
		t.Error("expected error on empty major")
	}
}

func TestSplitTmuxProberVersionBadMinor(t *testing.T) {
	_, _, err := splitTmuxProberVersion("3.")
	if err == nil {
		t.Error("expected error on empty minor")
	}
}

func TestAtoiEmpty(t *testing.T) {
	_, err := atoi("")
	if err == nil {
		t.Error("expected error on empty")
	}
}

func TestAtoiNonDigit(t *testing.T) {
	_, err := atoi("12a")
	if err == nil {
		t.Error("expected error on non-digit")
	}
}

func TestAtoiValid(t *testing.T) {
	n, err := atoi("123")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if n != 123 {
		t.Errorf("got %d, want 123", n)
	}
}
