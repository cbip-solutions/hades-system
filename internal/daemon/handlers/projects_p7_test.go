package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/projectctx"
)

type fakeProjectStore struct {
	mu       sync.Mutex
	byAlias  map[projectctx.Alias]*projectctx.Project
	history  map[projectctx.Alias][]projectctx.PathHistoryEntry
	archived map[projectctx.Alias]bool
	removed  map[projectctx.Alias]bool

	failGetByAlias bool
	failArchive    bool
	failRemove     bool
}

func newFakeProjectStore() *fakeProjectStore {
	return &fakeProjectStore{
		byAlias:  map[projectctx.Alias]*projectctx.Project{},
		history:  map[projectctx.Alias][]projectctx.PathHistoryEntry{},
		archived: map[projectctx.Alias]bool{},
		removed:  map[projectctx.Alias]bool{},
	}
}

func (f *fakeProjectStore) GetByAlias(ctx context.Context, alias projectctx.Alias) (*projectctx.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failGetByAlias {
		return nil, errFake
	}
	p, ok := f.byAlias[alias]
	if !ok {
		return nil, nil
	}
	return p, nil
}

func (f *fakeProjectStore) GetByID(ctx context.Context, id projectctx.ProjectID) (*projectctx.Project, error) {
	return nil, nil
}

func (f *fakeProjectStore) Insert(ctx context.Context, p *projectctx.Project) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byAlias[p.Alias] = p
	return nil
}

func (f *fakeProjectStore) UpdateLastSeen(ctx context.Context, alias projectctx.Alias, t time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.byAlias[alias]; ok {
		p.LastSeenAt = t
	}
	return nil
}

func (f *fakeProjectStore) Archive(ctx context.Context, alias projectctx.Alias) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failArchive {
		return errFake
	}
	f.archived[alias] = true
	return nil
}

func (f *fakeProjectStore) Remove(ctx context.Context, alias projectctx.Alias) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failRemove {
		return errFake
	}
	f.removed[alias] = true
	delete(f.byAlias, alias)
	return nil
}

func (f *fakeProjectStore) AppendPathHistory(ctx context.Context, e *projectctx.PathHistoryEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	alias := projectctx.Alias("")
	for a, p := range f.byAlias {
		if p.ID == e.ProjectID {
			alias = a
			break
		}
	}
	if alias == "" {
		return nil
	}
	f.history[alias] = append(f.history[alias], *e)
	return nil
}

func (f *fakeProjectStore) GetPathHistory(ctx context.Context, alias projectctx.Alias) ([]projectctx.PathHistoryEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.history[alias], nil
}

func (f *fakeProjectStore) List(ctx context.Context, includeArchived bool) ([]projectctx.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]projectctx.Project, 0, len(f.byAlias))
	for _, p := range f.byAlias {
		out = append(out, *p)
	}
	return out, nil
}

var errFake = &fakeError{msg: "fake store error"}

type fakeError struct{ msg string }

func (e *fakeError) Error() string { return e.msg }

type fakeAccessor struct {
	store projectctx.ProjectStore
}

func (f *fakeAccessor) ProjectStore() projectctx.ProjectStore { return f.store }

type nilAccessor struct{}

func (nilAccessor) ProjectStore() projectctx.ProjectStore { return nil }

func TestProjectDoctor503WhenNotConfigured(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/doctor", bytes.NewBufferString(`{"alias":"x"}`))
	ProjectDoctor(nilAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestProjectArchive503WhenNotConfigured(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/archive", bytes.NewBufferString(`{"alias":"x"}`))
	ProjectArchive(nilAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestProjectRm503WhenNotConfigured(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/rm", bytes.NewBufferString(`{"alias":"x"}`))
	ProjectRm(nilAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestProjectDoctorBadJSON(t *testing.T) {
	st := newFakeProjectStore()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/doctor", bytes.NewBufferString(`not-json`))
	ProjectDoctor(&fakeAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestProjectDoctorAliasOrCwdRequired(t *testing.T) {
	st := newFakeProjectStore()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/doctor", bytes.NewBufferString(`{}`))
	ProjectDoctor(&fakeAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestProjectDoctorAliasOnlyHealthy(t *testing.T) {
	st := newFakeProjectStore()
	now := time.Unix(1700000000, 0).UTC()
	st.byAlias["internal-platform-x"] = &projectctx.Project{
		ID:            "9f3a1c2d8b4e5f60111122223333444455556666777788889999aaaabbbbccccdd",
		Alias:         "internal-platform-x",
		CanonicalPath: "/path/to/projects/internal-platform-x",
		FirstSeenAt:   now,
		LastSeenAt:    now,
	}
	st.history["internal-platform-x"] = []projectctx.PathHistoryEntry{
		{
			ProjectID:   "9f3a1c2d8b4e5f60111122223333444455556666777788889999aaaabbbbccccdd",
			Path:        "/path/to/projects/internal-platform-x",
			FirstSeenAt: now,
			LastSeenAt:  now.Add(time.Minute),
		},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/doctor", bytes.NewBufferString(`{"alias":"internal-platform-x"}`))
	ProjectDoctor(&fakeAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["healthy"] != true {
		t.Errorf("healthy = %v, want true", resp["healthy"])
	}
	if resp["alias"] != "internal-platform-x" {
		t.Errorf("alias = %v, want internal-platform-x", resp["alias"])
	}
	if !strings.HasPrefix(resp["id_sha256"].(string), "9f3a1c2d") {
		t.Errorf("id_sha256 = %v, want 9f3a1c2d…", resp["id_sha256"])
	}
	if _, has := resp["mv_detected"]; has {
		t.Errorf("mv_detected should be omitted; got %v", resp["mv_detected"])
	}
}

func TestProjectDoctorAliasNotFound(t *testing.T) {
	st := newFakeProjectStore()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/doctor", bytes.NewBufferString(`{"alias":"missing"}`))
	ProjectDoctor(&fakeAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestProjectArchiveSuccess(t *testing.T) {
	st := newFakeProjectStore()
	st.byAlias["internal-platform-x"] = &projectctx.Project{
		ID:    "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Alias: "internal-platform-x",
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/archive", bytes.NewBufferString(`{"alias":"internal-platform-x"}`))
	ProjectArchive(&fakeAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !st.archived["internal-platform-x"] {
		t.Error("archived flag not set in fake store")
	}
}

func TestProjectArchiveAliasNotFound(t *testing.T) {
	st := newFakeProjectStore()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/archive", bytes.NewBufferString(`{"alias":"missing"}`))
	ProjectArchive(&fakeAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestProjectArchiveMissingAliasField(t *testing.T) {
	st := newFakeProjectStore()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/archive", bytes.NewBufferString(`{}`))
	ProjectArchive(&fakeAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestProjectArchiveBadJSON(t *testing.T) {
	st := newFakeProjectStore()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/archive", bytes.NewBufferString(`x`))
	ProjectArchive(&fakeAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestProjectArchiveStoreError(t *testing.T) {
	st := newFakeProjectStore()
	st.byAlias["x"] = &projectctx.Project{Alias: "x"}
	st.failArchive = true
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/archive", bytes.NewBufferString(`{"alias":"x"}`))
	ProjectArchive(&fakeAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestProjectArchiveGetByAliasError(t *testing.T) {
	st := newFakeProjectStore()
	st.failGetByAlias = true
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/archive", bytes.NewBufferString(`{"alias":"x"}`))
	ProjectArchive(&fakeAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestProjectRmSuccess(t *testing.T) {
	st := newFakeProjectStore()
	st.byAlias["internal-platform-x"] = &projectctx.Project{
		ID:    "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Alias: "internal-platform-x",
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/rm", bytes.NewBufferString(`{"alias":"internal-platform-x"}`))
	ProjectRm(&fakeAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !st.removed["internal-platform-x"] {
		t.Error("removed flag not set in fake store")
	}
}

func TestProjectRmAliasNotFound(t *testing.T) {
	st := newFakeProjectStore()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/rm", bytes.NewBufferString(`{"alias":"missing"}`))
	ProjectRm(&fakeAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestProjectRmMissingAlias(t *testing.T) {
	st := newFakeProjectStore()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/rm", bytes.NewBufferString(`{}`))
	ProjectRm(&fakeAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestProjectRmBadJSON(t *testing.T) {
	st := newFakeProjectStore()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/rm", bytes.NewBufferString(`!`))
	ProjectRm(&fakeAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestProjectRmStoreError(t *testing.T) {
	st := newFakeProjectStore()
	st.byAlias["x"] = &projectctx.Project{Alias: "x"}
	st.failRemove = true
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/rm", bytes.NewBufferString(`{"alias":"x"}`))
	ProjectRm(&fakeAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestProjectRmGetByAliasError(t *testing.T) {
	st := newFakeProjectStore()
	st.failGetByAlias = true
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/rm", bytes.NewBufferString(`{"alias":"x"}`))
	ProjectRm(&fakeAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestProjectDoctorGetByAliasError(t *testing.T) {
	st := newFakeProjectStore()
	st.failGetByAlias = true
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/doctor", bytes.NewBufferString(`{"alias":"x"}`))
	ProjectDoctor(&fakeAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestResolveProjectStoreNonAccessor(t *testing.T) {
	got := resolveProjectStore("not-an-accessor")
	if got != nil {
		t.Errorf("resolveProjectStore got %v, want nil", got)
	}
}

func TestProjectDoctorCwdPathHappy(t *testing.T) {
	st := newFakeProjectStore()

	dir := t.TempDir()
	canonDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(canonDir, "zenswarm.toml"), []byte("[project]\nid = \"smoke-test\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	body, _ := json.Marshal(map[string]any{"cwd": canonDir})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/doctor", bytes.NewReader(body))
	ProjectDoctor(&fakeAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["alias"] != "smoke-test" {
		t.Errorf("alias = %v, want smoke-test", resp["alias"])
	}
	if resp["healthy"] != true {
		t.Errorf("healthy = %v, want true", resp["healthy"])
	}
}

func TestProjectDoctorCwdPathMvDetected(t *testing.T) {
	st := newFakeProjectStore()
	dir := t.TempDir()
	canonDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(canonDir, "zenswarm.toml"), []byte("[project]\nid = \"smoke-test\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	prevPath := "/some/old/canonical/path-that-does-not-need-to-exist"
	prevID := projectctx.ProjectID("9f3a1c2d8b4e5f60111122223333444455556666777788889999aaaabbbbccccdd")
	now := time.Now().UTC()
	st.byAlias["smoke-test"] = &projectctx.Project{
		ID:            prevID,
		Alias:         "smoke-test",
		CanonicalPath: prevPath,
		FirstSeenAt:   now.Add(-time.Hour),
		LastSeenAt:    now.Add(-time.Minute),
	}
	st.history["smoke-test"] = []projectctx.PathHistoryEntry{
		{
			ProjectID:   prevID,
			Path:        prevPath,
			FirstSeenAt: now.Add(-time.Hour),
			LastSeenAt:  now.Add(-time.Minute),
		},
	}

	body, _ := json.Marshal(map[string]any{"cwd": canonDir})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/doctor", bytes.NewReader(body))
	ProjectDoctor(&fakeAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["healthy"] != false {
		t.Errorf("healthy = %v, want false", resp["healthy"])
	}
	mv, ok := resp["mv_detected"].(map[string]any)
	if !ok {
		t.Fatalf("mv_detected not a map; got %v", resp["mv_detected"])
	}
	if mv["old_path"] != prevPath {
		t.Errorf("old_path = %v, want %s", mv["old_path"], prevPath)
	}
	if mv["new_path"] != canonDir {
		t.Errorf("new_path = %v, want %s", mv["new_path"], canonDir)
	}
	oldIDShort, _ := mv["old_id_short"].(string)
	if oldIDShort == "" || !strings.HasPrefix(string(prevID), oldIDShort) {
		t.Errorf("old_id_short = %q; want prefix of %s", oldIDShort, prevID)
	}
	newIDShort, _ := mv["new_id_short"].(string)
	if newIDShort == "" {
		t.Error("new_id_short empty; want non-empty 8-hex prefix")
	}
	hint, _ := resp["hint"].(string)
	if hint == "" {
		t.Error("hint empty on mv-detected; want operator-actionable hint")
	}

	if !strings.Contains(hint, "rebind") {
		t.Errorf("hint missing 'rebind' segment: %q", hint)
	}
	if !strings.Contains(hint, "register") {
		t.Errorf("hint missing 'register as a new project' segment: %q", hint)
	}
}

func TestProjectDoctorCwdPathInvalid(t *testing.T) {
	st := newFakeProjectStore()
	body, _ := json.Marshal(map[string]any{"cwd": "/this/path/does/not/exist/anywhere"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/doctor", bytes.NewReader(body))
	ProjectDoctor(&fakeAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (canonicalise error)", rec.Code)
	}
}

func TestProjectDoctorCwdPathNoProjectRoot(t *testing.T) {
	st := newFakeProjectStore()

	dir := t.TempDir()
	canonDir, _ := filepath.EvalSymlinks(dir)
	body, _ := json.Marshal(map[string]any{"cwd": canonDir})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/doctor", bytes.NewReader(body))
	ProjectDoctor(&fakeAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (no project root)", rec.Code)
	}
}

func TestProjectDoctorMalformedTomlReturns422(t *testing.T) {
	st := newFakeProjectStore()
	dir := t.TempDir()
	canonDir, _ := filepath.EvalSymlinks(dir)

	if err := os.WriteFile(filepath.Join(canonDir, "zenswarm.toml"), []byte("[[[ not toml at all"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	body, _ := json.Marshal(map[string]any{"cwd": canonDir})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/doctor", bytes.NewReader(body))
	ProjectDoctor(&fakeAccessor{store: st}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422 (malformed TOML)", rec.Code)
	}
}

func TestIsAliasResolutionErrorTrueForSentinels(t *testing.T) {
	cases := []error{
		projectctx.ErrZenswarmTOMLMalformed,
		projectctx.ErrAliasInvalid,
		projectctx.ErrAliasEmpty,
		projectctx.ErrAliasInvalidChar,
		projectctx.ErrAliasReserved,
		projectctx.ErrAliasTooLong,
	}
	for _, e := range cases {
		if !isAliasResolutionError(e) {
			t.Errorf("isAliasResolutionError(%v) = false; want true", e)
		}
	}
	if isAliasResolutionError(errors.New("random")) {
		t.Error("isAliasResolutionError(random) = true; want false")
	}
	if isAliasResolutionError(nil) {
		t.Error("isAliasResolutionError(nil) = true; want false")
	}
}
