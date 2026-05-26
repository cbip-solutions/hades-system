package projectctx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func chdirRestore(t *testing.T, dir string) {
	t.Helper()
	prevWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir %q: %v", dir, err)
	}
	t.Cleanup(func() {
		if cerr := os.Chdir(prevWd); cerr != nil {
			t.Logf("cleanup Chdir restore failed: %v", cerr)
		}
	})
}

func TestResolveProjectIDIsSha256Hex(t *testing.T) {
	dir := t.TempDir()
	id, err := ResolveProjectID(dir)
	if err != nil {
		t.Fatalf("ResolveProjectID: %v", err)
	}
	if len(id) != 64 {
		t.Errorf("ProjectID len = %d, want 64 (sha256 hex)", len(id))
	}
	for _, r := range string(id) {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Errorf("ProjectID contains non-hex rune %q", r)
			break
		}
	}
}

func TestResolveProjectIDDeterministic(t *testing.T) {
	dir := t.TempDir()
	id1, err := ResolveProjectID(dir)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	id2, err := ResolveProjectID(dir)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if id1 != id2 {
		t.Errorf("ProjectID nondeterministic: %s vs %s", id1, id2)
	}
}

func TestResolveProjectIDFollowsSymlinks(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatalf("mkdir real: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("os.Symlink unsupported: %v", err)
	}
	idReal, err := ResolveProjectID(real)
	if err != nil {
		t.Fatalf("real: %v", err)
	}
	idLink, err := ResolveProjectID(link)
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if idReal != idLink {
		t.Errorf("symlink not resolved: real=%s link=%s", idReal, idLink)
	}
}

func TestResolveProjectIDDistinctPaths(t *testing.T) {
	d1 := t.TempDir()
	d2 := t.TempDir()
	id1, _ := ResolveProjectID(d1)
	id2, _ := ResolveProjectID(d2)
	if id1 == id2 {
		t.Errorf("distinct paths gave identical IDs: %s == %s", id1, id2)
	}
}

func TestResolveProjectIDNonexistentPathErrs(t *testing.T) {
	_, err := ResolveProjectID("/nonexistent/path/that/will/never/exist/zen-swarm-test")
	if err == nil {
		t.Error("expected error for nonexistent path; got nil")
	}
}

func TestResolveProjectIDEmptyPathErrs(t *testing.T) {
	_, err := ResolveProjectID("")
	if err == nil {
		t.Error("expected error for empty path; got nil")
	}
}

func TestResolveProjectIDRelativePathConvertedToAbs(t *testing.T) {
	dir := t.TempDir()

	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	idAbs, err := ResolveProjectID(abs)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}

	parent := filepath.Dir(abs)
	rel, err := filepath.Rel(parent, abs)
	if err != nil {
		t.Fatalf("Rel: %v", err)
	}
	chdirRestore(t, parent)
	idRel, err := ResolveProjectID(rel)
	if err != nil {
		t.Fatalf("rel: %v", err)
	}
	if idAbs != idRel {
		t.Errorf("abs/rel mismatch: abs=%s rel=%s", idAbs, idRel)
	}
}

func TestProjectIDShort(t *testing.T) {
	id := ProjectID("abcdef0123456789" + strings.Repeat("0", 48))
	if got := id.Short(); got != "abcdef01" {
		t.Errorf("Short = %q, want abcdef01", got)
	}
}

func TestProjectIDShortHandlesShorterValues(t *testing.T) {

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Short panicked on short ID: %v", r)
		}
	}()
	id := ProjectID("abc")
	if got := id.Short(); got != "abc" {
		t.Errorf("Short = %q, want abc (full short string)", got)
	}
}

func TestProjectIDShortEmpty(t *testing.T) {
	id := ProjectID("")
	if got := id.Short(); got != "" {
		t.Errorf("Short(empty) = %q, want empty", got)
	}
}

func TestProjectIDStringMatchesValue(t *testing.T) {
	id := ProjectID("xyz")
	if id.String() != "xyz" {
		t.Errorf("String() = %q, want xyz", id.String())
	}
}

func TestSha256OfPathMatchesManualComputation(t *testing.T) {

	dir := t.TempDir()
	got, err := ResolveProjectID(dir)
	if err != nil {
		t.Fatalf("ResolveProjectID: %v", err)
	}
	abs, _ := filepath.Abs(dir)
	resolved, _ := filepath.EvalSymlinks(abs)
	cleaned := filepath.Clean(resolved)
	sum := sha256.Sum256([]byte(cleaned))
	want := hex.EncodeToString(sum[:])
	if string(got) != want {
		t.Errorf("ID mismatch: got %s want %s", got, want)
	}
}

func TestCanonicalPathReturnsCleanedAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	cp, err := CanonicalPath(dir)
	if err != nil {
		t.Fatalf("CanonicalPath: %v", err)
	}
	if !filepath.IsAbs(cp) {
		t.Errorf("CanonicalPath = %q, want absolute", cp)
	}

	abs, _ := filepath.Abs(dir)
	resolved, _ := filepath.EvalSymlinks(abs)
	want := filepath.Clean(resolved)
	if cp != want {
		t.Errorf("CanonicalPath = %q, want %q", cp, want)
	}
}

func TestCanonicalPathFollowsSymlinks(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatalf("mkdir real: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("os.Symlink unsupported: %v", err)
	}
	cpReal, err := CanonicalPath(real)
	if err != nil {
		t.Fatalf("real: %v", err)
	}
	cpLink, err := CanonicalPath(link)
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if cpReal != cpLink {
		t.Errorf("symlink not resolved: real=%s link=%s", cpReal, cpLink)
	}
}

func TestCanonicalPathEmptyPathErrs(t *testing.T) {
	_, err := CanonicalPath("")
	if err == nil {
		t.Fatal("expected error for empty path; got nil")
	}
	if !errors.Is(err, ErrEmptyPath) {
		t.Errorf("err = %v, want errors.Is(err, ErrEmptyPath)", err)
	}
}

func TestCanonicalPathNonexistentPathErrs(t *testing.T) {
	_, err := CanonicalPath("/nonexistent/path/that/will/never/exist/zen-swarm-test-canonical")
	if err == nil {
		t.Error("expected error for nonexistent path; got nil")
	}
}

func TestCanonicalPathRelativeMatchesAbsolute(t *testing.T) {
	dir := t.TempDir()
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	parent := filepath.Dir(abs)
	rel, err := filepath.Rel(parent, abs)
	if err != nil {
		t.Fatalf("Rel: %v", err)
	}
	chdirRestore(t, parent)
	cpRel, err := CanonicalPath(rel)
	if err != nil {
		t.Fatalf("rel: %v", err)
	}
	cpAbs, err := CanonicalPath(abs)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if cpRel != cpAbs {
		t.Errorf("rel/abs mismatch: rel=%q abs=%q", cpRel, cpAbs)
	}
}

func TestResolveProjectIDEmptyPathReturnsSentinel(t *testing.T) {
	_, err := ResolveProjectID("")
	if err == nil {
		t.Fatal("expected error for empty path; got nil")
	}
	if !errors.Is(err, ErrEmptyPath) {
		t.Errorf("err = %v, want errors.Is(err, ErrEmptyPath)", err)
	}
}

func TestResolveProjectIDAbsErrorPath(t *testing.T) {
	dir := t.TempDir()
	transient := filepath.Join(dir, "transient")
	if err := os.Mkdir(transient, 0o755); err != nil {
		t.Fatalf("mkdir transient: %v", err)
	}
	prevWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd before: %v", err)
	}
	if err := os.Chdir(transient); err != nil {
		t.Fatalf("Chdir transient: %v", err)
	}
	t.Cleanup(func() { os.Chdir(prevWd) })
	if err := os.Remove(transient); err != nil {

		t.Skipf("cannot remove CWD on this platform: %v", err)
	}
	if _, gwErr := os.Getwd(); gwErr == nil {

		t.Skipf("platform tolerates deleted CWD; Abs error branch unreachable")
	}
	_, rerr := ResolveProjectID("relative-path-into-deleted-cwd")
	if rerr == nil {
		t.Fatal("expected error from ResolveProjectID with deleted CWD; got nil")
	}
	if !strings.Contains(rerr.Error(), "Abs(") {

		t.Logf("error did not mention Abs (likely EvalSymlinks branch); err=%v", rerr)
	}
}

func TestCanonicalPathAbsErrorPath(t *testing.T) {
	dir := t.TempDir()
	transient := filepath.Join(dir, "transient")
	if err := os.Mkdir(transient, 0o755); err != nil {
		t.Fatalf("mkdir transient: %v", err)
	}
	prevWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd before: %v", err)
	}
	if err := os.Chdir(transient); err != nil {
		t.Fatalf("Chdir transient: %v", err)
	}
	t.Cleanup(func() { os.Chdir(prevWd) })
	if err := os.Remove(transient); err != nil {
		t.Skipf("cannot remove CWD on this platform: %v", err)
	}
	if _, gwErr := os.Getwd(); gwErr == nil {
		t.Skipf("platform tolerates deleted CWD; Abs error branch unreachable")
	}
	_, rerr := CanonicalPath("relative-path-into-deleted-cwd")
	if rerr == nil {
		t.Fatal("expected error from CanonicalPath with deleted CWD; got nil")
	}
	if !strings.Contains(rerr.Error(), "Abs(") {
		t.Logf("error did not mention Abs (likely EvalSymlinks branch); err=%v", rerr)
	}
}

func TestProjectIsArchivedNil(t *testing.T) {
	p := &Project{ID: ProjectID("abc"), Alias: Alias("alpha")}
	if p.IsArchived() {
		t.Errorf("IsArchived() = true for nil ArchivedAt, want false")
	}
}

func TestProjectIsArchivedNonNil(t *testing.T) {
	now := time.Now()
	p := &Project{ID: ProjectID("abc"), Alias: Alias("alpha"), ArchivedAt: &now}
	if !p.IsArchived() {
		t.Errorf("IsArchived() = false for non-nil ArchivedAt, want true")
	}
}

func TestProjectJSONOmitEmptyArchivedAt(t *testing.T) {
	p := Project{ID: "abc", Alias: "test", CanonicalPath: "/tmp/x"}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(data), "archived_at") {
		t.Errorf("active project should omit archived_at; got %s", data)
	}
}

func TestProjectJSONIncludesArchivedAt(t *testing.T) {
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	p := Project{ID: "abc", Alias: "test", CanonicalPath: "/tmp/x", ArchivedAt: &now}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(data), "archived_at") {
		t.Errorf("archived project should include archived_at; got %s", data)
	}
}

type fakeProjectStore struct {
	projects map[Alias]*Project
	history  map[ProjectID][]PathHistoryEntry
	byID     map[ProjectID]*Project
}

func newFakeProjectStore() *fakeProjectStore {
	return &fakeProjectStore{
		projects: make(map[Alias]*Project),
		history:  make(map[ProjectID][]PathHistoryEntry),
		byID:     make(map[ProjectID]*Project),
	}
}

func (f *fakeProjectStore) GetByAlias(ctx context.Context, alias Alias) (*Project, error) {
	p, ok := f.projects[alias]
	if !ok {
		return nil, nil
	}
	return p, nil
}

func (f *fakeProjectStore) GetByID(ctx context.Context, id ProjectID) (*Project, error) {
	p, ok := f.byID[id]
	if !ok {
		return nil, nil
	}
	return p, nil
}

func (f *fakeProjectStore) Insert(ctx context.Context, p *Project) error {
	if _, exists := f.projects[p.Alias]; exists {
		return errors.New("fake: alias collision")
	}
	if _, exists := f.byID[p.ID]; exists {
		return errors.New("fake: id collision")
	}
	pp := *p
	f.projects[p.Alias] = &pp
	f.byID[p.ID] = &pp
	return nil
}

func (f *fakeProjectStore) UpdateLastSeen(ctx context.Context, alias Alias, lastSeenAt time.Time) error {
	p, ok := f.projects[alias]
	if !ok {
		return errors.New("fake: alias not found")
	}
	p.LastSeenAt = lastSeenAt
	return nil
}

func (f *fakeProjectStore) Archive(ctx context.Context, alias Alias) error {
	p, ok := f.projects[alias]
	if !ok {
		return errors.New("fake: alias not found")
	}
	now := time.Now()
	p.ArchivedAt = &now
	return nil
}

func (f *fakeProjectStore) Remove(ctx context.Context, alias Alias) error {
	p, ok := f.projects[alias]
	if !ok {
		return errors.New("fake: alias not found")
	}
	delete(f.projects, alias)
	delete(f.byID, p.ID)
	delete(f.history, p.ID)
	return nil
}

func (f *fakeProjectStore) AppendPathHistory(ctx context.Context, entry *PathHistoryEntry) error {
	for i := range f.history[entry.ProjectID] {
		if f.history[entry.ProjectID][i].Path == entry.Path {
			f.history[entry.ProjectID][i].LastSeenAt = entry.LastSeenAt
			return nil
		}
	}
	f.history[entry.ProjectID] = append(f.history[entry.ProjectID], *entry)
	return nil
}

func (f *fakeProjectStore) GetPathHistory(ctx context.Context, alias Alias) ([]PathHistoryEntry, error) {
	p, ok := f.projects[alias]
	if !ok {
		return nil, nil
	}
	return f.history[p.ID], nil
}

func (f *fakeProjectStore) List(ctx context.Context, includeArchived bool) ([]Project, error) {
	out := make([]Project, 0, len(f.projects))
	for _, p := range f.projects {
		if !includeArchived && p.IsArchived() {
			continue
		}
		out = append(out, *p)
	}
	return out, nil
}

var _ ProjectStore = (*fakeProjectStore)(nil)

func TestFakeProjectStoreSatisfiesInterface(t *testing.T) {

	store := newFakeProjectStore()
	ctx := context.Background()
	got, err := store.GetByAlias(ctx, Alias("none"))
	if err != nil {
		t.Errorf("GetByAlias on empty: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestFakeProjectStoreInsertAndGet(t *testing.T) {
	store := newFakeProjectStore()
	ctx := context.Background()
	p := &Project{
		ID:            mkID("test"),
		Alias:         Alias("test-alias"),
		CanonicalPath: "/tmp/test",
		FirstSeenAt:   time.Now(),
		LastSeenAt:    time.Now(),
	}
	if err := store.Insert(ctx, p); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := store.GetByAlias(ctx, Alias("test-alias"))
	if err != nil {
		t.Fatalf("GetByAlias: %v", err)
	}
	if got == nil {
		t.Fatal("got nil after Insert")
	}
	if got.Alias != p.Alias || got.ID != p.ID {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", got, p)
	}
}

func TestFakeProjectStoreInsertCollisionRejected(t *testing.T) {
	store := newFakeProjectStore()
	ctx := context.Background()
	p1 := &Project{
		ID:            mkID("test1"),
		Alias:         Alias("same-alias"),
		CanonicalPath: "/tmp/1",
		FirstSeenAt:   time.Now(),
		LastSeenAt:    time.Now(),
	}
	if err := store.Insert(ctx, p1); err != nil {
		t.Fatalf("first Insert: %v", err)
	}
	p2 := &Project{
		ID:            mkID("test2"),
		Alias:         Alias("same-alias"),
		CanonicalPath: "/tmp/2",
		FirstSeenAt:   time.Now(),
		LastSeenAt:    time.Now(),
	}
	if err := store.Insert(ctx, p2); err == nil {
		t.Error("expected collision error on duplicate alias")
	}
}

func TestFakeProjectStoreAppendPathHistoryAndGet(t *testing.T) {
	store := newFakeProjectStore()
	ctx := context.Background()
	id := mkID("history")
	p := &Project{
		ID:            id,
		Alias:         Alias("hist"),
		CanonicalPath: "/tmp/hist",
		FirstSeenAt:   time.Now(),
		LastSeenAt:    time.Now(),
	}
	if err := store.Insert(ctx, p); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	entry := &PathHistoryEntry{
		ProjectID:   id,
		Path:        "/tmp/hist",
		FirstSeenAt: time.Now(),
		LastSeenAt:  time.Now(),
	}
	if err := store.AppendPathHistory(ctx, entry); err != nil {
		t.Fatalf("AppendPathHistory: %v", err)
	}
	hist, err := store.GetPathHistory(ctx, Alias("hist"))
	if err != nil {
		t.Fatalf("GetPathHistory: %v", err)
	}
	if len(hist) != 1 {
		t.Errorf("len(hist) = %d, want 1", len(hist))
	}
}
