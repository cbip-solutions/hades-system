package tmuxlife

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestSnapshotPathConvention(t *testing.T) {
	dir := t.TempDir()
	alias := "internal-platform-x"
	ts := time.Date(2026, 5, 1, 14, 30, 45, 0, time.UTC)
	got := SnapshotPath(dir, alias, ts)
	want := filepath.Join(dir, "internal-platform-x-20260501T143045Z.tar.gz")
	if got != want {
		t.Errorf("SnapshotPath = %q, want %q", got, want)
	}
}

func TestSnapshotPathLocalToUTC(t *testing.T) {
	loc, err := time.LoadLocation("America/Argentina/Buenos_Aires")
	if err != nil {
		t.Skipf("Buenos_Aires zoneinfo unavailable: %v", err)
	}

	tsLocal := time.Date(2026, 5, 1, 11, 30, 45, 0, loc)
	got := SnapshotPath("", "x", tsLocal)
	if !strings.HasSuffix(got, "x-20260501T143045Z.tar.gz") {
		t.Errorf("SnapshotPath did not normalize to UTC: %q", got)
	}
}

func TestSnapshotZeroValueInvalid(t *testing.T) {
	var s Snapshot
	if s.IsValid() {
		t.Error("zero Snapshot reports IsValid=true")
	}
}

func TestSnapshotPopulatedValid(t *testing.T) {
	s := Snapshot{
		SessionName: "zen-internal-platform-x-deadbeef",
		Path:        "/tmp/some.tar.gz",
		CreatedAt:   time.Now(),
		SizeBytes:   1024,
	}
	if !s.IsValid() {
		t.Error("populated Snapshot reports IsValid=false")
	}
}

func TestSnapshotMissingFieldsInvalid(t *testing.T) {
	base := Snapshot{
		SessionName: "zen-x-12345678",
		Path:        "/tmp/x.tar.gz",
		CreatedAt:   time.Now(),
		SizeBytes:   1024,
	}
	cases := []struct {
		name string
		mut  func(*Snapshot)
	}{
		{"missing SessionName", func(s *Snapshot) { s.SessionName = "" }},
		{"missing Path", func(s *Snapshot) { s.Path = "" }},
		{"missing CreatedAt", func(s *Snapshot) { s.CreatedAt = time.Time{} }},
		{"missing SizeBytes", func(s *Snapshot) { s.SizeBytes = 0 }},
		{"negative SizeBytes", func(s *Snapshot) { s.SizeBytes = -1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := base
			tc.mut(&s)
			if s.IsValid() {
				t.Errorf("%s: IsValid=true, want false", tc.name)
			}
		})
	}
}

func TestDefaultSnapshotDirCreatesUnderHome(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	dir, err := DefaultSnapshotDir()
	if err != nil {
		t.Fatalf("DefaultSnapshotDir: %v", err)
	}
	want := filepath.Join(tempHome, ".config", "zen-swarm", "tmux-snapshots")
	if dir != want {
		t.Errorf("dir = %q, want %q", dir, want)
	}
	stat, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !stat.IsDir() {
		t.Errorf("path %q is not a directory", dir)
	}
}

func TestDefaultSnapshotDirIdempotent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := DefaultSnapshotDir(); err != nil {
		t.Fatalf("first DefaultSnapshotDir: %v", err)
	}
	if _, err := DefaultSnapshotDir(); err != nil {
		t.Fatalf("second DefaultSnapshotDir: %v", err)
	}
}

func TestDefaultSnapshotDirMkdirError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses 0o000 perms")
	}
	parent := t.TempDir()

	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })
	t.Setenv("HOME", parent)

	_, err := DefaultSnapshotDir()
	if err == nil {
		t.Fatalf("DefaultSnapshotDir succeeded; want mkdir error")
	}
	if !strings.Contains(err.Error(), "DefaultSnapshotDir") {
		t.Errorf("error = %v; want wrap of 'DefaultSnapshotDir'", err)
	}
}

type fakeResurrectExec struct {
	produced []byte

	saveErr error

	scratchInjected string

	restoreErr error

	restoreCalls []restoreCallRecord
}

type restoreCallRecord struct {
	sessionName string
	payloadLen  int
}

func (f *fakeResurrectExec) save(_ context.Context, _ string) ([]byte, error) {
	if f.saveErr != nil {
		return nil, f.saveErr
	}
	if f.produced != nil {
		return f.produced, nil
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	contents := "session-name: zen-internal-platform-x-deadbeef\nwindow: orch\n"
	if f.scratchInjected != "" {
		contents += "\n" + f.scratchInjected + "\n"
	}
	hdr := &tar.Header{Name: "resurrect.txt", Mode: 0o644, Size: int64(len(contents))}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write([]byte(contents))
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes(), nil
}

func (f *fakeResurrectExec) restore(_ context.Context, sessionName string, payload []byte) error {
	f.restoreCalls = append(f.restoreCalls, restoreCallRecord{
		sessionName: sessionName,
		payloadLen:  len(payload),
	})
	return f.restoreErr
}

func newManagerForTest(t *testing.T, store SessionStore, rex resurrectExec, dir string, nowFn func() time.Time) *Manager {
	t.Helper()
	m := New(store)
	m.resurrect = rex
	m.snapshotDir = dir
	if nowFn != nil {
		m.now = nowFn
	}
	return m
}

func TestSaveWritesTarballAtCanonicalPath(t *testing.T) {
	dir := t.TempDir()
	store := newFakeSessionStore()
	if err := store.UpsertSession(Session{
		Alias: "internal-platform-x", Sha8: "deadbeef", Name: "zen-internal-platform-x-deadbeef", Status: StatusActive,
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	now := time.Date(2026, 5, 1, 14, 30, 45, 0, time.UTC)
	mgr := newManagerForTest(t, store, &fakeResurrectExec{}, dir, func() time.Time { return now })

	snap, err := mgr.Save(context.Background(), "internal-platform-x")
	if err != nil {
		t.Fatalf("Save err: %v", err)
	}
	if snap == nil {
		t.Fatal("Save returned nil Snapshot")
	}
	if !snap.IsValid() {
		t.Errorf("Save returned invalid Snapshot: %+v", snap)
	}
	if !strings.HasSuffix(snap.Path, "internal-platform-x-20260501T143045Z.tar.gz") {
		t.Errorf("Path = %q, missing canonical suffix", snap.Path)
	}
	if _, err := os.Stat(snap.Path); err != nil {
		t.Errorf("snapshot file missing: %v", err)
	}
	if snap.SizeBytes <= 0 {
		t.Errorf("SizeBytes = %d, want > 0", snap.SizeBytes)
	}
	if snap.SessionName != "zen-internal-platform-x-deadbeef" {
		t.Errorf("SessionName = %q, want zen-internal-platform-x-deadbeef", snap.SessionName)
	}
	if !snap.CreatedAt.Equal(now.UTC()) {
		t.Errorf("CreatedAt = %v, want %v", snap.CreatedAt, now.UTC())
	}

	stat, err := os.Stat(snap.Path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := stat.Mode().Perm(); mode != 0o600 {
		t.Errorf("file mode = %o, want 0o600 (operator-only)", mode)
	}
}

func TestSaveScratchExclusionViolatedRejects(t *testing.T) {
	dir := t.TempDir()
	store := newFakeSessionStore()
	if err := store.UpsertSession(Session{
		Alias: "internal-platform-x", Sha8: "deadbeef", Name: "zen-internal-platform-x-deadbeef", Status: StatusActive,
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	rex := &fakeResurrectExec{scratchInjected: scratchSentinel}
	mgr := newManagerForTest(t, store, rex, dir, time.Now)

	_, err := mgr.Save(context.Background(), "internal-platform-x")
	if !errors.Is(err, ErrScratchExclusionViolated) {
		t.Fatalf("Save err = %v, want ErrScratchExclusionViolated", err)
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tar.gz") {
			t.Errorf("rejected tarball was written to disk: %s", e.Name())
		}
	}
}

func TestSaveScratchColumnMarkerRejects(t *testing.T) {
	dir := t.TempDir()
	store := newFakeSessionStore()
	if err := store.UpsertSession(Session{
		Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive,
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := "window\tzen-x-12345678\t1\tscratch\tactive\n"
	hdr := &tar.Header{Name: "resurrect.txt", Mode: 0o644, Size: int64(len(body))}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write([]byte(body))
	_ = tw.Close()
	_ = gz.Close()

	rex := &fakeResurrectExec{produced: buf.Bytes()}
	mgr := newManagerForTest(t, store, rex, dir, time.Now)

	_, err := mgr.Save(context.Background(), "x")
	if !errors.Is(err, ErrScratchExclusionViolated) {
		t.Errorf("err = %v, want ErrScratchExclusionViolated for column-marker leak", err)
	}
}

func TestSaveSessionNotFound(t *testing.T) {
	dir := t.TempDir()
	store := newFakeSessionStore()
	mgr := newManagerForTest(t, store, &fakeResurrectExec{}, dir, time.Now)

	_, err := mgr.Save(context.Background(), "nonexistent")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("err = %v, want ErrSessionNotFound", err)
	}
}

func TestSaveResolveAliasStoreError(t *testing.T) {
	dir := t.TempDir()
	st := &failingStore{
		fakeSessionStore: newFakeSessionStore(),
		listErr:          errors.New("disk read error"),
	}
	mgr := newManagerForTest(t, st, &fakeResurrectExec{}, dir, time.Now)

	_, err := mgr.Save(context.Background(), "anything")
	if err == nil {
		t.Fatalf("Save succeeded; expected store error")
	}
	if errors.Is(err, ErrSessionNotFound) {
		t.Errorf("non-sentinel store err mapped to ErrSessionNotFound: %v", err)
	}
	if !strings.Contains(err.Error(), "disk read error") {
		t.Errorf("err = %v; want wrap of 'disk read error'", err)
	}
}

func TestSaveResurrectError(t *testing.T) {
	dir := t.TempDir()
	store := newFakeSessionStore()
	if err := store.UpsertSession(Session{
		Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive,
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	rex := &fakeResurrectExec{saveErr: errors.New("plugin not installed")}
	mgr := newManagerForTest(t, store, rex, dir, time.Now)

	_, err := mgr.Save(context.Background(), "x")
	if err == nil {
		t.Fatalf("Save succeeded on resurrect error; expected error")
	}
	if !strings.Contains(err.Error(), "plugin not installed") {
		t.Errorf("err = %v; want wrap of 'plugin not installed'", err)
	}
	if !strings.Contains(err.Error(), "resurrect.save") {
		t.Errorf("err = %v; missing 'resurrect.save' wrap", err)
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tar.gz") {
			t.Errorf("tarball written despite resurrect error: %s", e.Name())
		}
	}
}

func TestSaveDefaultsSnapshotDirToHome(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	store := newFakeSessionStore()
	if err := store.UpsertSession(Session{
		Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive,
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	mgr := newManagerForTest(t, store, &fakeResurrectExec{}, "", time.Now)

	snap, err := mgr.Save(context.Background(), "x")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	wantDir := filepath.Join(tempHome, ".config", "zen-swarm", "tmux-snapshots")
	if !strings.HasPrefix(snap.Path, wantDir) {
		t.Errorf("Path = %q; expected prefix %q", snap.Path, wantDir)
	}

	if mgr.snapshotDir != wantDir {
		t.Errorf("m.snapshotDir = %q, want %q after default fallback", mgr.snapshotDir, wantDir)
	}
}

func TestSaveDefaultSnapshotDirError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses 0o000 perms")
	}
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })
	t.Setenv("HOME", parent)

	store := newFakeSessionStore()
	if err := store.UpsertSession(Session{
		Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive,
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	mgr := newManagerForTest(t, store, &fakeResurrectExec{}, "", time.Now)

	_, err := mgr.Save(context.Background(), "x")
	if err == nil {
		t.Fatalf("Save succeeded; expected DefaultSnapshotDir error")
	}
	if !strings.Contains(err.Error(), "Manager.Save") {
		t.Errorf("err = %v; want wrap of 'Manager.Save'", err)
	}
}

func TestSaveCallerSuppliedDirIsCreated(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "deeply", "nested", "snaps")
	store := newFakeSessionStore()
	if err := store.UpsertSession(Session{
		Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive,
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	mgr := newManagerForTest(t, store, &fakeResurrectExec{}, dir, time.Now)

	if _, err := mgr.Save(context.Background(), "x"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	stat, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !stat.IsDir() {
		t.Errorf("path %q is not a directory", dir)
	}
}

func TestSaveCallerSuppliedDirMkdirError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses 0o000 perms")
	}
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })
	dir := filepath.Join(parent, "no-can-do")

	store := newFakeSessionStore()
	if err := store.UpsertSession(Session{
		Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive,
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	mgr := newManagerForTest(t, store, &fakeResurrectExec{}, dir, time.Now)

	_, err := mgr.Save(context.Background(), "x")
	if err == nil {
		t.Fatalf("Save succeeded; expected mkdir error")
	}
	if !strings.Contains(err.Error(), "mkdir") {
		t.Errorf("err = %v; want 'mkdir' in error", err)
	}
}

func TestSaveStatErrorAfterWrite(t *testing.T) {
	dir := t.TempDir()
	store := newFakeSessionStore()
	if err := store.UpsertSession(Session{
		Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive,
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	mgr := newManagerForTest(t, store, &fakeResurrectExec{}, dir, time.Now)
	mgr.statFn = func(string) (os.FileInfo, error) {
		return nil, errors.New("simulated stat failure")
	}

	_, err := mgr.Save(context.Background(), "x")
	if err == nil {
		t.Fatalf("Save succeeded; expected stat failure")
	}
	if !strings.Contains(err.Error(), "Manager.Save: stat") {
		t.Errorf("err = %v; want 'Manager.Save: stat' wrap", err)
	}
	if !strings.Contains(err.Error(), "simulated stat failure") {
		t.Errorf("err = %v; want underlying 'simulated stat failure'", err)
	}
}

func TestSaveWriteFileError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses 0o000 perms")
	}
	dir := t.TempDir()
	store := newFakeSessionStore()
	if err := store.UpsertSession(Session{
		Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive,
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	mgr := newManagerForTest(t, store, &fakeResurrectExec{}, dir, time.Now)

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	_, err := mgr.Save(context.Background(), "x")
	if err == nil {
		t.Fatalf("Save succeeded; expected write error")
	}
	if !strings.Contains(err.Error(), "write") {
		t.Errorf("err = %v; want 'write' in error", err)
	}
}

func TestRestoreLatestSnapshot(t *testing.T) {
	dir := t.TempDir()
	store := newFakeSessionStore()
	if err := store.UpsertSession(Session{
		Alias: "internal-platform-x", Sha8: "deadbeef", Name: "zen-internal-platform-x-deadbeef", Status: StatusActive,
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	times := []time.Time{
		time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 1, 11, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
	}
	for _, ts := range times {
		path := SnapshotPath(dir, "internal-platform-x", ts)
		body := []byte("dummy-tarball-" + ts.Format(time.RFC3339))
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	rex := &fakeResurrectExec{}
	mgr := newManagerForTest(t, store, rex, dir, time.Now)

	if err := mgr.Restore(context.Background(), "internal-platform-x"); err != nil {
		t.Fatalf("Restore err: %v", err)
	}
	if len(rex.restoreCalls) != 1 {
		t.Fatalf("restoreCalls = %d, want 1", len(rex.restoreCalls))
	}

	wantBody := []byte("dummy-tarball-2026-05-01T12:00:00Z")
	if rex.restoreCalls[0].payloadLen != len(wantBody) {
		t.Errorf("payloadLen = %d, want %d (12:00 snapshot)",
			rex.restoreCalls[0].payloadLen, len(wantBody))
	}
	if rex.restoreCalls[0].sessionName != "zen-internal-platform-x-deadbeef" {
		t.Errorf("sessionName = %q, want zen-internal-platform-x-deadbeef",
			rex.restoreCalls[0].sessionName)
	}
}

func TestRestoreNoSnapshots(t *testing.T) {
	dir := t.TempDir()
	store := newFakeSessionStore()
	if err := store.UpsertSession(Session{
		Alias: "internal-platform-x", Sha8: "deadbeef", Name: "zen-internal-platform-x-deadbeef", Status: StatusActive,
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	mgr := newManagerForTest(t, store, &fakeResurrectExec{}, dir, time.Now)

	err := mgr.Restore(context.Background(), "internal-platform-x")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("err = %v; want ErrSessionNotFound", err)
	}
}

func TestRestoreIgnoresOtherAliases(t *testing.T) {
	dir := t.TempDir()
	store := newFakeSessionStore()
	if err := store.UpsertSession(Session{
		Alias: "internal-platform-x", Sha8: "deadbeef", Name: "zen-internal-platform-x-deadbeef", Status: StatusActive,
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	other := SnapshotPath(dir, "other-project", time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC))
	if err := os.WriteFile(other, []byte("not-mine"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	mgr := newManagerForTest(t, store, &fakeResurrectExec{}, dir, time.Now)
	err := mgr.Restore(context.Background(), "internal-platform-x")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("err = %v; want ErrSessionNotFound (other-project must not match)", err)
	}
}

func TestRestoreIgnoresNonTarballs(t *testing.T) {
	dir := t.TempDir()
	store := newFakeSessionStore()
	if err := store.UpsertSession(Session{
		Alias: "internal-platform-x", Sha8: "deadbeef", Name: "zen-internal-platform-x-deadbeef", Status: StatusActive,
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	stray := filepath.Join(dir, "internal-platform-x-stray.txt")
	if err := os.WriteFile(stray, []byte("not-a-tarball"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	mgr := newManagerForTest(t, store, &fakeResurrectExec{}, dir, time.Now)
	err := mgr.Restore(context.Background(), "internal-platform-x")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("err = %v; want ErrSessionNotFound (stray .txt must not match)", err)
	}
}

func TestRestoreIgnoresSubdirectories(t *testing.T) {
	dir := t.TempDir()
	store := newFakeSessionStore()
	if err := store.UpsertSession(Session{
		Alias: "internal-platform-x", Sha8: "deadbeef", Name: "zen-internal-platform-x-deadbeef", Status: StatusActive,
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	subdir := filepath.Join(dir, "internal-platform-x-snapshots-folder")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	mgr := newManagerForTest(t, store, &fakeResurrectExec{}, dir, time.Now)
	err := mgr.Restore(context.Background(), "internal-platform-x")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("err = %v; want ErrSessionNotFound (subdir must not match)", err)
	}
}

func TestRestoreReadDirError(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "does-not-exist")
	store := newFakeSessionStore()
	mgr := newManagerForTest(t, store, &fakeResurrectExec{}, dir, time.Now)

	err := mgr.Restore(context.Background(), "internal-platform-x")
	if err == nil {
		t.Fatalf("Restore succeeded; expected ReadDir error")
	}
	if !strings.Contains(err.Error(), "ReadDir") {
		t.Errorf("err = %v; want 'ReadDir' in error", err)
	}
}

func TestRestoreCorruptUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses 0o000 perms")
	}
	dir := t.TempDir()
	store := newFakeSessionStore()
	if err := store.UpsertSession(Session{
		Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive,
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	path := SnapshotPath(dir, "x", time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC))
	if err := os.WriteFile(path, []byte("doesn't matter"), 0o000); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	mgr := newManagerForTest(t, store, &fakeResurrectExec{}, dir, time.Now)
	err := mgr.Restore(context.Background(), "x")
	if !errors.Is(err, ErrSnapshotCorrupt) {
		t.Errorf("err = %v; want ErrSnapshotCorrupt for unreadable tarball", err)
	}
}

func TestRestoreSessionNotFoundInStore(t *testing.T) {
	dir := t.TempDir()

	path := SnapshotPath(dir, "ghost", time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC))
	if err := os.WriteFile(path, []byte("payload"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	store := newFakeSessionStore()
	mgr := newManagerForTest(t, store, &fakeResurrectExec{}, dir, time.Now)

	err := mgr.Restore(context.Background(), "ghost")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("err = %v; want ErrSessionNotFound", err)
	}
}

func TestRestoreResolveAliasStoreError(t *testing.T) {
	dir := t.TempDir()

	path := SnapshotPath(dir, "x", time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC))
	if err := os.WriteFile(path, []byte("payload"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	st := &failingStore{
		fakeSessionStore: newFakeSessionStore(),
		listErr:          errors.New("store down"),
	}
	mgr := newManagerForTest(t, st, &fakeResurrectExec{}, dir, time.Now)

	err := mgr.Restore(context.Background(), "x")
	if err == nil {
		t.Fatalf("Restore succeeded; expected store error")
	}
	if errors.Is(err, ErrSessionNotFound) {
		t.Errorf("non-sentinel store err mapped to ErrSessionNotFound: %v", err)
	}
	if !strings.Contains(err.Error(), "store down") {
		t.Errorf("err = %v; want wrap of 'store down'", err)
	}
}

func TestRestoreResurrectError(t *testing.T) {
	dir := t.TempDir()
	store := newFakeSessionStore()
	if err := store.UpsertSession(Session{
		Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive,
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	path := SnapshotPath(dir, "x", time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC))
	if err := os.WriteFile(path, []byte("payload"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	rex := &fakeResurrectExec{restoreErr: errors.New("plugin restore failed")}
	mgr := newManagerForTest(t, store, rex, dir, time.Now)

	err := mgr.Restore(context.Background(), "x")
	if err == nil {
		t.Fatalf("Restore succeeded; expected resurrect.restore error")
	}
	if !strings.Contains(err.Error(), "plugin restore failed") {
		t.Errorf("err = %v; want wrap of 'plugin restore failed'", err)
	}
}

func TestRestoreDefaultsSnapshotDirToHome(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	store := newFakeSessionStore()
	if err := store.UpsertSession(Session{
		Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive,
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	mgr := newManagerForTest(t, store, &fakeResurrectExec{}, "", time.Now)

	err := mgr.Restore(context.Background(), "x")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("err = %v; want ErrSessionNotFound (no snapshots in default dir)", err)
	}
	wantDir := filepath.Join(tempHome, ".config", "zen-swarm", "tmux-snapshots")
	if mgr.snapshotDir != wantDir {
		t.Errorf("m.snapshotDir = %q, want %q after default fallback", mgr.snapshotDir, wantDir)
	}
}

func TestRestoreDefaultSnapshotDirError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses 0o000 perms")
	}
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })
	t.Setenv("HOME", parent)

	store := newFakeSessionStore()
	mgr := newManagerForTest(t, store, &fakeResurrectExec{}, "", time.Now)

	err := mgr.Restore(context.Background(), "x")
	if err == nil {
		t.Fatalf("Restore succeeded; expected DefaultSnapshotDir error")
	}
	if !strings.Contains(err.Error(), "Manager.Restore") {
		t.Errorf("err = %v; want wrap of 'Manager.Restore'", err)
	}
}

func TestPruneOldSnapshotsKeepsLastK(t *testing.T) {
	dir := t.TempDir()

	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		path := SnapshotPath(dir, "internal-platform-x", base.Add(time.Duration(i)*time.Hour))
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	if err := PruneOldSnapshots(dir, 3); err != nil {
		t.Fatalf("PruneOldSnapshots err: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	tarballs := make([]string, 0)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tar.gz") {
			tarballs = append(tarballs, e.Name())
		}
	}
	if len(tarballs) != 3 {
		t.Errorf("kept %d tarballs, want 3: %v", len(tarballs), tarballs)
	}
	sort.Strings(tarballs)
	wantTail := []string{
		"internal-platform-x-20260501T070000Z.tar.gz",
		"internal-platform-x-20260501T080000Z.tar.gz",
		"internal-platform-x-20260501T090000Z.tar.gz",
	}
	for i, want := range wantTail {
		if tarballs[i] != want {
			t.Errorf("kept[%d] = %q, want %q", i, tarballs[i], want)
		}
	}
}

func TestPruneOldSnapshotsKeepLastZero(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		path := SnapshotPath(dir, "x", time.Date(2026, 5, 1, i, 0, 0, 0, time.UTC))
		if err := os.WriteFile(path, []byte("y"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	if err := PruneOldSnapshots(dir, 0); err != nil {
		t.Fatalf("err: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 5 {
		t.Errorf("Prune(keepLast=0) deleted files; expected no-op, got %d entries", len(entries))
	}
}

func TestPruneOldSnapshotsKeepLastNegative(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		path := SnapshotPath(dir, "x", time.Date(2026, 5, 1, i, 0, 0, 0, time.UTC))
		if err := os.WriteFile(path, []byte("z"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	if err := PruneOldSnapshots(dir, -5); err != nil {
		t.Fatalf("err: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 {
		t.Errorf("Prune(keepLast=-5) deleted files; expected no-op, got %d entries", len(entries))
	}
}

func TestPruneOldSnapshotsKeepMoreThanExist(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 2; i++ {
		path := SnapshotPath(dir, "x", time.Date(2026, 5, 1, i, 0, 0, 0, time.UTC))
		if err := os.WriteFile(path, []byte("z"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	if err := PruneOldSnapshots(dir, 50); err != nil {
		t.Fatalf("err: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("Prune(keepLast=50, n=2) deleted files; expected no-op, got %d entries", len(entries))
	}
}

func TestPruneOldSnapshotsIgnoresNonTarballs(t *testing.T) {
	dir := t.TempDir()
	stranger := filepath.Join(dir, "operator-notes.txt")
	if err := os.WriteFile(stranger, []byte("hi"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	for i := 0; i < 5; i++ {
		path := SnapshotPath(dir, "x", time.Date(2026, 5, 1, i, 0, 0, 0, time.UTC))
		_ = os.WriteFile(path, []byte("y"), 0o600)
	}
	if err := PruneOldSnapshots(dir, 2); err != nil {
		t.Fatalf("err: %v", err)
	}
	if _, err := os.Stat(stranger); err != nil {
		t.Errorf("stranger file deleted: %v", err)
	}
}

func TestPruneOldSnapshotsIgnoresSubdirectories(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "weird-named-dir.tar.gz")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	for i := 0; i < 5; i++ {
		path := SnapshotPath(dir, "x", time.Date(2026, 5, 1, i, 0, 0, 0, time.UTC))
		_ = os.WriteFile(path, []byte("y"), 0o600)
	}
	if err := PruneOldSnapshots(dir, 2); err != nil {
		t.Fatalf("err: %v", err)
	}
	if _, err := os.Stat(subdir); err != nil {
		t.Errorf("subdir deleted: %v", err)
	}
}

func TestPruneOldSnapshotsReadDirError(t *testing.T) {
	parent := t.TempDir()
	missing := filepath.Join(parent, "does-not-exist")
	err := PruneOldSnapshots(missing, 3)
	if err == nil {
		t.Fatalf("PruneOldSnapshots succeeded; expected ReadDir error")
	}
	if !strings.Contains(err.Error(), "ReadDir") {
		t.Errorf("err = %v; want 'ReadDir' in error", err)
	}
}

func TestPruneOldSnapshotsSurvivesPartialDeleteFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses 0o000 perms")
	}
	dir := t.TempDir()

	for i := 0; i < 5; i++ {
		path := SnapshotPath(dir, "x", time.Date(2026, 5, 1, i, 0, 0, 0, time.UTC))
		if err := os.WriteFile(path, []byte("z"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	oldestPath := SnapshotPath(dir, "x", time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	if err := os.Remove(oldestPath); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := os.MkdirAll(oldestPath, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if err := os.WriteFile(filepath.Join(oldestPath, "child"), []byte("nope"), 0o600); err != nil {
		t.Fatalf("WriteFile child: %v", err)
	}

	if err := PruneOldSnapshots(dir, 2); err != nil {
		t.Fatalf("PruneOldSnapshots: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	tarballs := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tar.gz") {
			tarballs++
		}
	}
	if tarballs != 3 {
		t.Errorf("entries with .tar.gz suffix = %d, want 3 (1 dir + 2 latest files)", tarballs)
	}
}

func TestStripScratchLines(t *testing.T) {
	in := []byte(strings.Join([]string{
		"window\tzen-x-12345678\t1\torch\tactive",
		"window\tzen-x-12345678\t2\tscratch\tactive",
		"window\tzen-x-12345678\t3\tlogs\tactive",
		"some other line with " + scratchSentinel + " in it",
		"final-line",
	}, "\n"))
	out := stripScratchLines(in)
	got := string(out)
	if strings.Contains(got, "scratch") {

		t.Errorf("output still contains scratch reference: %q", got)
	}
	if strings.Contains(got, scratchSentinel) {
		t.Errorf("output still contains scratchSentinel: %q", got)
	}
	if !strings.Contains(got, "orch") || !strings.Contains(got, "logs") || !strings.Contains(got, "final-line") {
		t.Errorf("output dropped non-scratch lines: %q", got)
	}
}

func TestUntarToRoundTrip(t *testing.T) {
	srcDir := t.TempDir()
	files := map[string]string{
		"file1.txt": "hello world\n",
		"file2.bin": "\x00\x01\x02\x03",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(srcDir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}
	payload, err := tarResurrectFiltered(srcDir, "irrelevant")
	if err != nil {
		t.Fatalf("tarResurrectFiltered: %v", err)
	}

	dstDir := t.TempDir()
	if err := untarTo(dstDir, payload); err != nil {
		t.Fatalf("untarTo: %v", err)
	}
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(dstDir, name))
		if err != nil {
			t.Errorf("ReadFile %s: %v", name, err)
			continue
		}

		gotS := string(got)
		if name == "file1.txt" {

			if !strings.HasPrefix(gotS, want) {
				t.Errorf("%s: got %q, want prefix %q", name, gotS, want)
			}
		} else {

			if len(gotS) == 0 {
				t.Errorf("%s: empty output, want non-empty", name)
			}
		}
	}
}

func TestUntarToCorruptGzip(t *testing.T) {
	dir := t.TempDir()
	err := untarTo(dir, []byte("not a gzip stream"))
	if err == nil {
		t.Fatalf("untarTo succeeded on corrupt input")
	}
	if !strings.Contains(err.Error(), "gunzip") {
		t.Errorf("err = %v; want 'gunzip' in error", err)
	}
}

func TestUntarToCorruptTar(t *testing.T) {
	dir := t.TempDir()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Write([]byte("definitely not tar headers, just garbage bytes"))
	_ = gz.Close()

	err := untarTo(dir, buf.Bytes())
	if err == nil {
		t.Fatalf("untarTo succeeded on corrupt tar")
	}
	if !strings.Contains(err.Error(), "tar.Next") {
		t.Errorf("err = %v; want 'tar.Next' in error", err)
	}
}

func TestUntarToCopyError(t *testing.T) {
	dir := t.TempDir()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{
		Name: "lying.txt",
		Mode: 0o644,
		Size: 100,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}

	_ = gz.Close()

	err := untarTo(dir, buf.Bytes())
	if err == nil {
		t.Fatalf("untarTo succeeded on lying-size tar")
	}

	if !strings.Contains(err.Error(), "Copy") && !strings.Contains(err.Error(), "tar.Next") {
		t.Errorf("err = %v; want 'Copy' or 'tar.Next' in error", err)
	}
}

func TestUntarToParentMkdirError(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "blocker"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile blocker: %v", err)
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := "child"
	hdr := &tar.Header{Name: "blocker/inner.txt", Mode: 0o644, Size: int64(len(body))}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write([]byte(body))
	_ = tw.Close()
	_ = gz.Close()

	err := untarTo(dir, buf.Bytes())
	if err == nil {
		t.Fatalf("untarTo succeeded; expected mkdir parent error")
	}
	if !strings.Contains(err.Error(), "mkdir parent") {
		t.Errorf("err = %v; want 'mkdir parent' in error", err)
	}
}

func TestUntarToTargetDirNotWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses 0o000 perms")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := "hi"
	hdr := &tar.Header{Name: "test.txt", Mode: 0o644, Size: int64(len(body))}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write([]byte(body))
	_ = tw.Close()
	_ = gz.Close()

	err := untarTo(dir, buf.Bytes())
	if err == nil {
		t.Fatalf("untarTo succeeded on read-only dir")
	}
	if !strings.Contains(err.Error(), "OpenFile") {
		t.Errorf("err = %v; want 'OpenFile' in error", err)
	}
}

func TestScratchInPayloadCorrupt(t *testing.T) {
	if !scratchInPayload([]byte("not a gzip stream")) {
		t.Errorf("scratchInPayload(corrupt) = false; want true (fail-closed)")
	}
}

func TestScratchInPayloadTruncatedTar(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Write([]byte("garbage"))
	_ = gz.Close()
	if !scratchInPayload(buf.Bytes()) {
		t.Errorf("scratchInPayload(truncated tar) = false; want true (fail-closed)")
	}
}

func TestScratchInPayloadReadAllError(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: "lying.txt", Mode: 0o644, Size: 100}
	_ = tw.WriteHeader(hdr)

	_, _ = tw.Write([]byte("short"))
	_ = gz.Close()
	if !scratchInPayload(buf.Bytes()) {
		t.Errorf("scratchInPayload(lying-size) = false; want true (fail-closed on read error)")
	}
}

func TestScratchInPayloadClean(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := "session-name: zen-x-12345678\nwindow: orch\n"
	hdr := &tar.Header{Name: "resurrect.txt", Mode: 0o644, Size: int64(len(body))}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write([]byte(body))
	_ = tw.Close()
	_ = gz.Close()
	if scratchInPayload(buf.Bytes()) {
		t.Errorf("scratchInPayload(clean) = true; want false")
	}
}

type failWriter struct {
	written int
	failAt  int
	failErr error
}

func (f *failWriter) Write(p []byte) (int, error) {
	if f.written+len(p) > f.failAt {

		accepted := f.failAt - f.written
		if accepted < 0 {
			accepted = 0
		}
		f.written += accepted
		return accepted, f.failErr
	}
	f.written += len(p)
	return len(p), nil
}

func TestWriteTarballToWriterWriteHeaderError(t *testing.T) {
	w := &failWriter{failAt: 0, failErr: errors.New("simulated write failure")}
	pairs := []tarPair{
		{name: "first.txt", body: []byte("hi")},
	}
	err := writeTarballToWriter(w, pairs, nil)
	if err == nil {
		t.Fatalf("writeTarballToWriter succeeded; expected failWriter to error")
	}

	if !strings.Contains(err.Error(), "WriteHeader") &&
		!strings.Contains(err.Error(), "gzip") &&
		!strings.Contains(err.Error(), "Write ") {
		t.Errorf("err = %v; want 'WriteHeader' or 'gzip' or 'Write' in error", err)
	}
}

func TestWriteTarballToWriterCloseError(t *testing.T) {

	w := &failWriter{failAt: 50, failErr: errors.New("simulated close failure")}
	pairs := []tarPair{
		{name: "first.txt", body: []byte("hi")},
	}
	err := writeTarballToWriter(w, pairs, nil)
	if err == nil {
		t.Fatalf("writeTarballToWriter succeeded; expected close failure")
	}
}

func TestWriteTarballToWriterMultipleEntries(t *testing.T) {

	w := &failWriter{failAt: 10, failErr: errors.New("partial flush failed")}
	pairs := []tarPair{
		{name: "a.txt", body: []byte("alpha")},
		{name: "b.txt", body: []byte("bravo")},
	}
	err := writeTarballToWriter(w, pairs, nil)
	if err == nil {
		t.Fatalf("writeTarballToWriter succeeded; expected partial-flush failure")
	}
}

func TestWriteTarballToWriterSizeMismatchTriggersWriteError(t *testing.T) {
	var buf bytes.Buffer
	pairs := []tarPair{
		{name: "lying.txt", body: []byte("this is way longer than declared")},
	}

	sizeOverride := func(_ tarPair) int64 { return 2 }
	err := writeTarballToWriter(&buf, pairs, sizeOverride)
	if err == nil {
		t.Fatalf("writeTarballToWriter succeeded; expected size-mismatch error")
	}
	if !strings.Contains(err.Error(), "Write ") {
		t.Errorf("err = %v; want 'Write' in error wrap", err)
	}
	if !strings.Contains(err.Error(), "lying.txt") {
		t.Errorf("err = %v; want filename 'lying.txt' in error wrap", err)
	}
}

func TestWriteTarballToWriterTarCloseError(t *testing.T) {
	var buf bytes.Buffer
	pairs := []tarPair{
		{name: "underweight.txt", body: []byte("only-5")},
	}

	sizeOverride := func(_ tarPair) int64 { return 100 }
	err := writeTarballToWriter(&buf, pairs, sizeOverride)
	if err == nil {
		t.Fatalf("writeTarballToWriter succeeded; expected tar Close error")
	}
	if !strings.Contains(err.Error(), "tar Close") {
		t.Errorf("err = %v; want 'tar Close' in error wrap", err)
	}
}

func TestTarResurrectFilteredReadDirError(t *testing.T) {
	parent := t.TempDir()
	missing := filepath.Join(parent, "does-not-exist")
	_, err := tarResurrectFiltered(missing, "x")
	if err == nil {
		t.Fatalf("tarResurrectFiltered succeeded on missing dir")
	}
	if !strings.Contains(err.Error(), "ReadDir") {
		t.Errorf("err = %v; want 'ReadDir' in error", err)
	}
}

func TestTarResurrectFilteredSkipsDirsAndUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses 0o000 perms")
	}
	dir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "ok.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile ok: %v", err)
	}

	unreadable := filepath.Join(dir, "nope.txt")
	if err := os.WriteFile(unreadable, []byte("secret"), 0o000); err != nil {
		t.Fatalf("WriteFile nope: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o644) })

	payload, err := tarResurrectFiltered(dir, "x")
	if err != nil {
		t.Fatalf("tarResurrectFiltered: %v", err)
	}

	gzr, _ := gzip.NewReader(bytes.NewReader(payload))
	tr := tar.NewReader(gzr)
	names := []string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar.Next: %v", err)
		}
		names = append(names, hdr.Name)
		_, _ = io.ReadAll(tr)
	}
	wantSubstr := "ok.txt"
	found := false
	for _, n := range names {
		if n == wantSubstr {
			found = true
		}
	}
	if !found {
		t.Errorf("ok.txt missing from tarball; names = %v", names)
	}
	for _, n := range names {
		if n == "nope.txt" || n == "subdir" {
			t.Errorf("excluded entry surfaced in tarball: %q", n)
		}
	}
}

func TestRealResurrectExecSavePluginNotInstalled(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	rex := realResurrectExec{}
	_, err := rex.save(context.Background(), "zen-x-12345678")
	if err == nil {
		t.Fatalf("save succeeded; expected plugin-not-installed error")
	}
	if !strings.Contains(err.Error(), "plugin not installed") {
		t.Errorf("err = %v; want 'plugin not installed' in error", err)
	}
}

func TestRealResurrectExecRestorePluginNotInstalled(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	rex := realResurrectExec{}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := "test"
	hdr := &tar.Header{Name: "test.txt", Mode: 0o644, Size: int64(len(body))}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write([]byte(body))
	_ = tw.Close()
	_ = gz.Close()

	err := rex.restore(context.Background(), "zen-x-12345678", buf.Bytes())
	if err == nil {
		t.Fatalf("restore succeeded; expected plugin-not-installed error")
	}
	if !strings.Contains(err.Error(), "plugin not installed") {
		t.Errorf("err = %v; want 'plugin not installed' in error", err)
	}
}

func TestRealResurrectExecRestoreUntarError(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	rex := realResurrectExec{}

	err := rex.restore(context.Background(), "zen-x-12345678", []byte("garbage"))
	if err == nil {
		t.Fatalf("restore succeeded on garbage payload")
	}
	if !strings.Contains(err.Error(), "untar") {
		t.Errorf("err = %v; want 'untar' in error", err)
	}
}

func TestRealResurrectExecSaveExecTmuxFailure(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	scriptDir := filepath.Join(tempHome, ".tmux", "plugins", "tmux-resurrect", "scripts")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptDir, "save.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile save.sh: %v", err)
	}

	rex := realResurrectExec{}
	_, err := rex.save(context.Background(), "zen-x-12345678")
	if err == nil {

		t.Skipf("save() returned nil; environment has tmux installed and the "+
			"happy path executed unexpectedly. tmux on PATH = %s", lookPathOrEmpty("tmux"))
	}

	if !strings.Contains(err.Error(), "realResurrectExec.save") {
		t.Errorf("err = %v; want wrap of 'realResurrectExec.save'", err)
	}
}

func TestRealResurrectExecRestoreExecTmuxFailure(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	scriptDir := filepath.Join(tempHome, ".tmux", "plugins", "tmux-resurrect", "scripts")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptDir, "restore.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile restore.sh: %v", err)
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := "test"
	hdr := &tar.Header{Name: "test.txt", Mode: 0o644, Size: int64(len(body))}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write([]byte(body))
	_ = tw.Close()
	_ = gz.Close()

	rex := realResurrectExec{}
	err := rex.restore(context.Background(), "zen-x-12345678", buf.Bytes())
	if err == nil {
		t.Skipf("restore() returned nil; environment has tmux installed and the "+
			"happy path executed. tmux on PATH = %s", lookPathOrEmpty("tmux"))
	}
	if !strings.Contains(err.Error(), "realResurrectExec.restore") {
		t.Errorf("err = %v; want wrap of 'realResurrectExec.restore'", err)
	}
}

func TestRealResurrectExecRestoreMkdirError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses 0o000 perms")
	}
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })
	t.Setenv("HOME", parent)

	rex := realResurrectExec{}
	err := rex.restore(context.Background(), "zen-x-12345678", []byte("garbage"))
	if err == nil {
		t.Fatalf("restore succeeded; expected mkdir error")
	}
	if !strings.Contains(err.Error(), "mkdir resurrectDir") {
		t.Errorf("err = %v; want 'mkdir resurrectDir' in error", err)
	}
}

func lookPathOrEmpty(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return "(not found)"
}

func TestRealResurrectExecSaveHomeError(t *testing.T) {
	if runtime.GOOS == "android" || runtime.GOOS == "ios" {
		t.Skip("UserHomeDir fallback yields fixed path on Android/iOS")
	}
	t.Setenv("HOME", "")
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		t.Skipf("HOME resolution still works (=%q); skipping HOME-error path", h)
	}
	rex := realResurrectExec{}
	_, err := rex.save(context.Background(), "x")
	if err == nil {
		t.Fatalf("save succeeded with HOME unset; expected error")
	}
}

func TestRealResurrectExecRestoreHomeError(t *testing.T) {
	if runtime.GOOS == "android" || runtime.GOOS == "ios" {
		t.Skip("UserHomeDir fallback yields fixed path on Android/iOS")
	}
	t.Setenv("HOME", "")
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		t.Skipf("HOME resolution still works (=%q); skipping HOME-error path", h)
	}
	rex := realResurrectExec{}
	err := rex.restore(context.Background(), "x", []byte("garbage"))
	if err == nil {
		t.Fatalf("restore succeeded with HOME unset; expected error")
	}
}

func TestDefaultSnapshotDirHomeUnsetError(t *testing.T) {
	if runtime.GOOS == "android" || runtime.GOOS == "ios" {
		t.Skip("UserHomeDir fallback yields fixed path on Android/iOS")
	}
	t.Setenv("HOME", "")
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		t.Skipf("HOME resolution still works (=%q); skipping HOME-error path", h)
	}
	_, err := DefaultSnapshotDir()
	if err == nil {
		t.Fatalf("DefaultSnapshotDir succeeded with HOME unset; expected error")
	}
	if !strings.Contains(err.Error(), "DefaultSnapshotDir") {
		t.Errorf("err = %v; want wrap of 'DefaultSnapshotDir'", err)
	}
}

func TestHomeOrEmptyReturnsHome(t *testing.T) {
	t.Setenv("HOME", "/some/home")
	got := homeOrEmpty()
	if got != "/some/home" {
		t.Errorf("homeOrEmpty = %q, want /some/home", got)
	}
}

func TestDefaultResurrectDir(t *testing.T) {
	t.Setenv("HOME", "/h")
	got, err := defaultResurrectDir()
	if err != nil {
		t.Fatalf("defaultResurrectDir: %v", err)
	}
	want := "/h/.local/share/tmux/resurrect"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
