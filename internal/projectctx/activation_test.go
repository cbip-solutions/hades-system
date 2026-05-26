package projectctx

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestActivateFirstActivationInserts(t *testing.T) {
	store := newFakeProjectStore()
	ctx := context.Background()
	dir := t.TempDir()
	id, err := ResolveProjectID(dir)
	if err != nil {
		t.Fatalf("ResolveProjectID: %v", err)
	}
	alias := Alias("first-test")
	cp, _ := CanonicalPath(dir)
	res, err := Activate(ctx, store, cp, alias)
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if !res.IsFirstActivation {
		t.Errorf("IsFirstActivation = false, want true")
	}
	if res.MvDetected != nil {
		t.Errorf("MvDetected non-nil on first activation: %+v", res.MvDetected)
	}
	if res.Project == nil {
		t.Fatal("Project nil")
	}
	if res.Project.ID != id {
		t.Errorf("Project.ID = %s, want %s", res.Project.ID, id)
	}
	if res.Project.Alias != alias {
		t.Errorf("Project.Alias = %s, want %s", res.Project.Alias, alias)
	}

	got, err := store.GetByAlias(ctx, alias)
	if err != nil {
		t.Fatalf("GetByAlias post-Activate: %v", err)
	}
	if got == nil {
		t.Fatal("Project not inserted into store")
	}
	hist, err := store.GetPathHistory(ctx, alias)
	if err != nil {
		t.Fatalf("GetPathHistory: %v", err)
	}
	if len(hist) != 1 {
		t.Errorf("len(history) = %d, want 1", len(hist))
	}
}

func TestActivateSecondActivationUpdatesLastSeen(t *testing.T) {
	store := newFakeProjectStore()
	ctx := context.Background()
	dir := t.TempDir()
	cp, _ := CanonicalPath(dir)
	alias := Alias("second-test")
	res1, err := Activate(ctx, store, cp, alias)
	if err != nil {
		t.Fatalf("first Activate: %v", err)
	}
	t1 := res1.Project.LastSeenAt
	time.Sleep(2 * time.Millisecond)
	res2, err := Activate(ctx, store, cp, alias)
	if err != nil {
		t.Fatalf("second Activate: %v", err)
	}
	if res2.IsFirstActivation {
		t.Error("IsFirstActivation = true on second activation")
	}
	if res2.MvDetected != nil {
		t.Errorf("MvDetected non-nil on same-path re-activation: %+v", res2.MvDetected)
	}
	if !res2.Project.LastSeenAt.After(t1) {
		t.Errorf("LastSeenAt not updated: t1=%v t2=%v", t1, res2.Project.LastSeenAt)
	}
}

func TestActivateMvDetected(t *testing.T) {
	store := newFakeProjectStore()
	ctx := context.Background()

	alias := Alias("moved-project")
	oldID := mkID("old")
	oldProject := &Project{
		ID:            oldID,
		Alias:         alias,
		CanonicalPath: "/old/canonical/path",
		FirstSeenAt:   time.Now().Add(-1 * time.Hour),
		LastSeenAt:    time.Now().Add(-30 * time.Minute),
	}
	if err := store.Insert(ctx, oldProject); err != nil {
		t.Fatalf("seed Insert: %v", err)
	}
	if err := store.AppendPathHistory(ctx, &PathHistoryEntry{
		ProjectID:   oldID,
		Path:        "/old/canonical/path",
		FirstSeenAt: time.Now().Add(-1 * time.Hour),
		LastSeenAt:  time.Now().Add(-30 * time.Minute),
	}); err != nil {
		t.Fatalf("seed AppendPathHistory: %v", err)
	}
	dir := t.TempDir()
	cp, _ := CanonicalPath(dir)
	res, err := Activate(ctx, store, cp, alias)
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if res.MvDetected == nil {
		t.Fatal("expected MvDetected, got nil")
	}
	if res.MvDetected.OldPath != "/old/canonical/path" {
		t.Errorf("OldPath = %q", res.MvDetected.OldPath)
	}
	if res.MvDetected.NewPath != cp {
		t.Errorf("NewPath = %q, want %q", res.MvDetected.NewPath, cp)
	}
	// Mv-detection MUST NOT mutate store (operator agency: rebind requires explicit doctor command).
	got, _ := store.GetByAlias(ctx, alias)
	if got.CanonicalPath != "/old/canonical/path" {
		t.Errorf("store mutated on mv-detection: CanonicalPath = %q", got.CanonicalPath)
	}
	if got.ID != oldID {
		t.Errorf("store ID changed: got %s want %s", got.ID, oldID)
	}
	if res.IsFirstActivation {
		t.Error("IsFirstActivation = true on mv-detected, want false")
	}
}

func TestActivateEmptyAliasRejected(t *testing.T) {
	store := newFakeProjectStore()
	ctx := context.Background()
	dir := t.TempDir()
	cp, _ := CanonicalPath(dir)
	_, err := Activate(ctx, store, cp, Alias(""))
	if err == nil {
		t.Error("expected error for empty alias")
	}

	if !errors.Is(err, ErrAliasInvalid) {
		t.Errorf("err = %v, want errors.Is(err, ErrAliasInvalid)", err)
	}
	if !errors.Is(err, ErrAliasEmpty) {
		t.Errorf("err = %v, want errors.Is(err, ErrAliasEmpty)", err)
	}
}

func TestActivateInvalidAliasRejected(t *testing.T) {
	store := newFakeProjectStore()
	ctx := context.Background()
	dir := t.TempDir()
	cp, _ := CanonicalPath(dir)
	_, err := Activate(ctx, store, cp, Alias("invalid alias with spaces"))
	if err == nil {
		t.Error("expected error for invalid alias")
	}

	if !errors.Is(err, ErrAliasInvalid) {
		t.Errorf("err = %v, want errors.Is(err, ErrAliasInvalid)", err)
	}
	if !errors.Is(err, ErrAliasInvalidChar) {
		t.Errorf("err = %v, want errors.Is(err, ErrAliasInvalidChar)", err)
	}
}

func TestActivateNonexistentPathErrs(t *testing.T) {
	store := newFakeProjectStore()
	ctx := context.Background()
	_, err := Activate(ctx, store, "/nonexistent/zen-swarm-test/never-exists", Alias("test"))
	if err == nil {
		t.Error("expected error for nonexistent path")
	}
}

func TestActivateEmptyPathReturnsSentinel(t *testing.T) {

	store := newFakeProjectStore()
	ctx := context.Background()
	_, err := Activate(ctx, store, "", Alias("ok-alias"))
	if err == nil {
		t.Fatal("expected error for empty canonicalPath")
	}
	if !errors.Is(err, ErrEmptyPath) {
		t.Errorf("err = %v, want errors.Is(err, ErrEmptyPath)", err)
	}
}

func TestActivateNilStoreRejected(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cp, _ := CanonicalPath(dir)
	_, err := Activate(ctx, nil, cp, Alias("ok-alias"))
	if err == nil {
		t.Fatal("expected error for nil store")
	}
	if !strings.Contains(err.Error(), "store is nil") {
		t.Errorf("err = %v, want contains \"store is nil\"", err)
	}
}

func TestActivateAliasCollision(t *testing.T) {

	store := newFakeProjectStore()
	ctx := context.Background()
	d1 := t.TempDir()
	d2 := t.TempDir()
	cp1, _ := CanonicalPath(d1)
	cp2, _ := CanonicalPath(d2)
	if _, err := Activate(ctx, store, cp1, Alias("collide")); err != nil {
		t.Fatalf("first Activate: %v", err)
	}
	res, err := Activate(ctx, store, cp2, Alias("collide"))

	if err != nil {

		if !strings.Contains(err.Error(), "collision") && !strings.Contains(err.Error(), "alias") {
			t.Errorf("unexpected error: %v", err)
		}
		return
	}

	if res == nil || res.MvDetected == nil {
		t.Fatal("expected MvDetected on different-path same-alias collision; got nil")
	}
	if res.MvDetected.NewPath != cp2 {
		t.Errorf("MvDetected.NewPath = %q, want %q", res.MvDetected.NewPath, cp2)
	}
}

func TestActivationResultProjectIsCopy(t *testing.T) {
	store := newFakeProjectStore()
	ctx := context.Background()
	dir := t.TempDir()
	cp, _ := CanonicalPath(dir)
	res, err := Activate(ctx, store, cp, Alias("copy-test"))
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}

	originalLastSeen := res.Project.LastSeenAt
	res.Project.LastSeenAt = time.Time{}
	got, _ := store.GetByAlias(ctx, Alias("copy-test"))
	if got.LastSeenAt.Equal(time.Time{}) {
		t.Error("store mutated by mutation of ActivationResult.Project")
	}
	if !got.LastSeenAt.Equal(originalLastSeen) {
		t.Errorf("store last_seen drifted: got %v want %v", got.LastSeenAt, originalLastSeen)
	}
}

type faultyStore struct {
	getErr            error
	insertErr         error
	updateErr         error
	appendErr         error
	getHistoryErr     error
	getReturnsNilPost bool
	knownByAlias      *Project
	historyByAlias    map[Alias][]PathHistoryEntry
}

func (s *faultyStore) GetByAlias(ctx context.Context, alias Alias) (*Project, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.getReturnsNilPost {

		ret := s.knownByAlias
		s.knownByAlias = nil
		return ret, nil
	}
	return s.knownByAlias, nil
}
func (s *faultyStore) GetByID(ctx context.Context, id ProjectID) (*Project, error) { return nil, nil }
func (s *faultyStore) Insert(ctx context.Context, p *Project) error                { return s.insertErr }
func (s *faultyStore) UpdateLastSeen(ctx context.Context, alias Alias, t time.Time) error {
	return s.updateErr
}
func (s *faultyStore) Archive(ctx context.Context, alias Alias) error { return nil }
func (s *faultyStore) Remove(ctx context.Context, alias Alias) error  { return nil }
func (s *faultyStore) AppendPathHistory(ctx context.Context, e *PathHistoryEntry) error {
	return s.appendErr
}
func (s *faultyStore) GetPathHistory(ctx context.Context, alias Alias) ([]PathHistoryEntry, error) {
	if s.getHistoryErr != nil {
		return nil, s.getHistoryErr
	}
	return s.historyByAlias[alias], nil
}
func (s *faultyStore) List(ctx context.Context, includeArchived bool) ([]Project, error) {
	return nil, nil
}

var _ ProjectStore = (*faultyStore)(nil)

func TestActivateGetByAliasErrorWrapped(t *testing.T) {
	dir := t.TempDir()
	cp, _ := CanonicalPath(dir)
	want := errors.New("simulated GetByAlias failure")
	store := &faultyStore{getErr: want}
	_, err := Activate(context.Background(), store, cp, Alias("ok"))
	if err == nil {
		t.Fatal("expected error from GetByAlias")
	}
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want errors.Is(err, %v)", err, want)
	}
	if !strings.Contains(err.Error(), "GetByAlias") {
		t.Errorf("err message must mention GetByAlias context; got %q", err.Error())
	}
}

func TestActivateInsertErrorWrapped(t *testing.T) {
	dir := t.TempDir()
	cp, _ := CanonicalPath(dir)
	want := errors.New("simulated Insert failure")
	store := &faultyStore{insertErr: want}
	_, err := Activate(context.Background(), store, cp, Alias("ok"))
	if err == nil {
		t.Fatal("expected Insert error")
	}
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want errors.Is(err, %v)", err, want)
	}
	if !strings.Contains(err.Error(), "Insert") {
		t.Errorf("err must mention Insert; got %q", err.Error())
	}
}

func TestActivateAppendPathHistoryErrorAfterInsert(t *testing.T) {
	dir := t.TempDir()
	cp, _ := CanonicalPath(dir)
	want := errors.New("simulated AppendPathHistory failure")
	store := &faultyStore{appendErr: want}
	_, err := Activate(context.Background(), store, cp, Alias("ok"))
	if err == nil {
		t.Fatal("expected AppendPathHistory error")
	}
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want errors.Is(err, %v)", err, want)
	}
	if !strings.Contains(err.Error(), "AppendPathHistory") {
		t.Errorf("err must mention AppendPathHistory; got %q", err.Error())
	}
}

func TestActivateInsertReReadNilFallback(t *testing.T) {
	dir := t.TempDir()
	cp, _ := CanonicalPath(dir)
	store := &faultyStore{getReturnsNilPost: true}
	res, err := Activate(context.Background(), store, cp, Alias("ok"))
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if res.Project == nil {
		t.Fatal("Project nil — fallback to *p failed")
	}
	if res.Project.Alias != Alias("ok") {
		t.Errorf("Project.Alias = %q, want \"ok\"", res.Project.Alias)
	}
	if !res.IsFirstActivation {
		t.Error("IsFirstActivation = false, want true")
	}
}

func TestActivateUpdateLastSeenErrorWrapped(t *testing.T) {
	dir := t.TempDir()
	cp, _ := CanonicalPath(dir)
	id, _ := ResolveProjectID(dir)
	known := &Project{
		ID:            id,
		Alias:         Alias("ok"),
		CanonicalPath: cp,
		FirstSeenAt:   time.Now().Add(-time.Hour),
		LastSeenAt:    time.Now().Add(-time.Hour),
	}
	want := errors.New("simulated UpdateLastSeen failure")
	store := &faultyStore{knownByAlias: known, updateErr: want}
	_, err := Activate(context.Background(), store, cp, Alias("ok"))
	if err == nil {
		t.Fatal("expected UpdateLastSeen error")
	}
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want errors.Is(err, %v)", err, want)
	}
	if !strings.Contains(err.Error(), "UpdateLastSeen") {
		t.Errorf("err must mention UpdateLastSeen; got %q", err.Error())
	}
}

func TestActivateAppendPathHistoryErrorAfterUpdate(t *testing.T) {
	dir := t.TempDir()
	cp, _ := CanonicalPath(dir)
	id, _ := ResolveProjectID(dir)
	known := &Project{
		ID:            id,
		Alias:         Alias("ok"),
		CanonicalPath: cp,
		FirstSeenAt:   time.Now().Add(-time.Hour),
		LastSeenAt:    time.Now().Add(-time.Hour),
	}
	want := errors.New("simulated AppendPathHistory after UpdateLastSeen failure")
	store := &faultyStore{knownByAlias: known, appendErr: want}
	_, err := Activate(context.Background(), store, cp, Alias("ok"))
	if err == nil {
		t.Fatal("expected AppendPathHistory error")
	}
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want errors.Is(err, %v)", err, want)
	}
	if !strings.Contains(err.Error(), "AppendPathHistory") {
		t.Errorf("err must mention AppendPathHistory; got %q", err.Error())
	}
}

func TestActivateUpdateReReadNilFallback(t *testing.T) {
	dir := t.TempDir()
	cp, _ := CanonicalPath(dir)
	id, _ := ResolveProjectID(dir)
	known := &Project{
		ID:            id,
		Alias:         Alias("ok"),
		CanonicalPath: cp,
		FirstSeenAt:   time.Now().Add(-time.Hour),
		LastSeenAt:    time.Now().Add(-time.Hour),
	}
	store := &faultyStore{knownByAlias: known, getReturnsNilPost: true}
	res, err := Activate(context.Background(), store, cp, Alias("ok"))
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if res.Project == nil {
		t.Fatal("Project nil — fallback to *known failed")
	}
	if res.Project.Alias != Alias("ok") {
		t.Errorf("Project.Alias = %q, want \"ok\"", res.Project.Alias)
	}
	if res.IsFirstActivation {
		t.Error("IsFirstActivation = true on subsequent activation; want false")
	}
}

func TestActivateGetPathHistoryErrorWrapped(t *testing.T) {
	dir := t.TempDir()
	cp, _ := CanonicalPath(dir)

	known := &Project{
		ID:            mkID("different-id"),
		Alias:         Alias("ok"),
		CanonicalPath: "/old/path",
		FirstSeenAt:   time.Now().Add(-time.Hour),
		LastSeenAt:    time.Now().Add(-time.Hour),
	}
	want := errors.New("simulated GetPathHistory failure")
	store := &faultyStore{knownByAlias: known, getHistoryErr: want}
	_, err := Activate(context.Background(), store, cp, Alias("ok"))
	if err == nil {
		t.Fatal("expected GetPathHistory error")
	}
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want errors.Is(err, %v)", err, want)
	}
	if !strings.Contains(err.Error(), "GetPathHistory") {
		t.Errorf("err must mention GetPathHistory; got %q", err.Error())
	}
}
