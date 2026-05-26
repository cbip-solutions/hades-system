package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeReindexEngine struct {
	indexFn func(ctx context.Context, projectID string) (CaronteReindexReport, error)
}

func (f *fakeReindexEngine) IndexProject(ctx context.Context, projectID string) (CaronteReindexReport, error) {
	if f.indexFn != nil {
		return f.indexFn(ctx, projectID)
	}

	return CaronteReindexReport{ProjectID: projectID, LanguageCounts: map[string]int{}, Completed: true}, nil
}

type fakeAliasResolverReindex struct {
	resolveFn func(ctx context.Context, idOrAlias string) (string, error)
}

func (f *fakeAliasResolverReindex) Resolve(ctx context.Context, idOrAlias string) (string, error) {
	if f.resolveFn != nil {
		return f.resolveFn(ctx, idOrAlias)
	}
	return idOrAlias, nil
}

type fakeReindexCtx struct {
	engine   CaronteEngineForReindex
	resolver ProjectsAliasResolverForReindex
}

func (f *fakeReindexCtx) CaronteEngineForReindex() CaronteEngineForReindex {
	return f.engine
}

func (f *fakeReindexCtx) AliasResolverForReindex() ProjectsAliasResolverForReindex {
	return f.resolver
}

func TestCaronteReindex_HappyPath(t *testing.T) {
	canonical := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	want := CaronteReindexReport{
		ProjectID:      canonical,
		NodesCreated:   42,
		FilesIndexed:   7,
		LanguageCounts: map[string]int{"go": 7},
		DurationMillis: 150,
		StartedAt:      time.Now(),
		Completed:      true,
	}
	ctx := &fakeReindexCtx{
		engine: &fakeReindexEngine{
			indexFn: func(_ context.Context, pid string) (CaronteReindexReport, error) {
				if pid != canonical {
					t.Errorf("engine received pid = %q; want canonical %q", pid, canonical)
				}
				return want, nil
			},
		},
		resolver: &fakeAliasResolverReindex{
			resolveFn: func(_ context.Context, alias string) (string, error) {
				if alias != canonical {
					t.Errorf("resolver received alias = %q; want canonical %q", alias, canonical)
				}
				return canonical, nil
			},
		},
	}
	h := CaronteReindex(ctx)
	req := httptest.NewRequest(http.MethodPost, "/v1/caronte/reindex", nil)
	req.Header.Set("X-Zen-Project-ID", canonical)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var got CaronteReindexReport
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ProjectID != canonical {
		t.Errorf("ProjectID = %q; want %q", got.ProjectID, canonical)
	}
	if got.NodesCreated != 42 {
		t.Errorf("NodesCreated = %d; want 42", got.NodesCreated)
	}
	if got.FilesIndexed != 7 {
		t.Errorf("FilesIndexed = %d; want 7", got.FilesIndexed)
	}
	if !got.Completed {
		t.Error("Completed = false; want true")
	}
}

func TestCaronteReindex_AliasResolution(t *testing.T) {
	const alias = "zen-swarm-3572a35b"
	const canonical = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	var observedEngineID string
	ctx := &fakeReindexCtx{
		engine: &fakeReindexEngine{
			indexFn: func(_ context.Context, pid string) (CaronteReindexReport, error) {
				observedEngineID = pid
				return CaronteReindexReport{ProjectID: pid, LanguageCounts: map[string]int{}, Completed: true}, nil
			},
		},
		resolver: &fakeAliasResolverReindex{
			resolveFn: func(_ context.Context, a string) (string, error) {
				if a == alias {
					return canonical, nil
				}
				return "", fmt.Errorf("unexpected alias %q", a)
			},
		},
	}
	h := CaronteReindex(ctx)
	req := httptest.NewRequest(http.MethodPost, "/v1/caronte/reindex", nil)
	req.Header.Set("X-Zen-Project-ID", alias)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if observedEngineID != canonical {
		t.Errorf("engine received id %q; want canonical %q (alias %q not resolved)",
			observedEngineID, canonical, alias)
	}
}

func TestCaronteReindex_MissingHeader(t *testing.T) {
	ctx := &fakeReindexCtx{
		engine:   &fakeReindexEngine{},
		resolver: &fakeAliasResolverReindex{},
	}
	h := CaronteReindex(ctx)
	req := httptest.NewRequest(http.MethodPost, "/v1/caronte/reindex", nil)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (missing header)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "X-Zen-Project-ID") {
		t.Errorf("body = %q; want substring 'X-Zen-Project-ID'", rec.Body.String())
	}
}

func TestCaronteReindex_AliasNotFound(t *testing.T) {
	ctx := &fakeReindexCtx{
		engine: &fakeReindexEngine{},
		resolver: &fakeAliasResolverReindex{
			resolveFn: func(_ context.Context, _ string) (string, error) {
				return "", ErrCaronteAliasNotFound
			},
		},
	}
	h := CaronteReindex(ctx)
	req := httptest.NewRequest(http.MethodPost, "/v1/caronte/reindex", nil)
	req.Header.Set("X-Zen-Project-ID", "nonexistent-alias")
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (alias not found)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not found") {
		t.Errorf("body = %q; want substring 'not found'", rec.Body.String())
	}
}

func TestCaronteReindex_ResolverError(t *testing.T) {
	ctx := &fakeReindexCtx{
		engine: &fakeReindexEngine{},
		resolver: &fakeAliasResolverReindex{
			resolveFn: func(_ context.Context, _ string) (string, error) {
				return "", errors.New("alias-table: connection refused")
			},
		},
	}
	h := CaronteReindex(ctx)
	req := httptest.NewRequest(http.MethodPost, "/v1/caronte/reindex", nil)
	req.Header.Set("X-Zen-Project-ID", "alias")
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (resolver error)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "alias-table") {
		t.Errorf("body = %q; want substring 'alias-table' (wrapped resolver error)", rec.Body.String())
	}
}

func TestCaronteReindex_EngineError(t *testing.T) {
	canonical := "1111111111111111111111111111111111111111111111111111111111111111"
	ctx := &fakeReindexCtx{
		engine: &fakeReindexEngine{
			indexFn: func(_ context.Context, _ string) (CaronteReindexReport, error) {
				return CaronteReindexReport{ProjectID: canonical}, errors.New("walk failed: EACCES")
			},
		},
		resolver: &fakeAliasResolverReindex{},
	}
	h := CaronteReindex(ctx)
	req := httptest.NewRequest(http.MethodPost, "/v1/caronte/reindex", nil)
	req.Header.Set("X-Zen-Project-ID", canonical)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (engine error)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "EACCES") {
		t.Errorf("body = %q; want substring 'EACCES' (wrapped engine error)", rec.Body.String())
	}
}

func TestCaronteReindex_MethodNotAllowed(t *testing.T) {
	ctx := &fakeReindexCtx{
		engine:   &fakeReindexEngine{},
		resolver: &fakeAliasResolverReindex{},
	}
	h := CaronteReindex(ctx)
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/v1/caronte/reindex", nil)
			req.Header.Set("X-Zen-Project-ID", "x")
			rec := httptest.NewRecorder()
			h(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s status = %d; want 405", method, rec.Code)
			}
		})
	}
}

func TestCaronteReindex_EngineUnwired(t *testing.T) {
	ctx := &fakeReindexCtx{
		engine:   nil,
		resolver: &fakeAliasResolverReindex{},
	}
	h := CaronteReindex(ctx)
	req := httptest.NewRequest(http.MethodPost, "/v1/caronte/reindex", nil)
	req.Header.Set("X-Zen-Project-ID", "x")
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (engine unwired)", rec.Code)
	}
}

func TestCaronteReindex_ResolverUnwired(t *testing.T) {
	ctx := &fakeReindexCtx{
		engine:   &fakeReindexEngine{},
		resolver: nil,
	}
	h := CaronteReindex(ctx)
	req := httptest.NewRequest(http.MethodPost, "/v1/caronte/reindex", nil)
	req.Header.Set("X-Zen-Project-ID", "x")
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (resolver unwired)", rec.Code)
	}
}

func TestCaronteReindex_JSONContentType(t *testing.T) {
	canonical := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ctx := &fakeReindexCtx{
		engine:   &fakeReindexEngine{},
		resolver: &fakeAliasResolverReindex{},
	}
	h := CaronteReindex(ctx)
	req := httptest.NewRequest(http.MethodPost, "/v1/caronte/reindex", nil)
	req.Header.Set("X-Zen-Project-ID", canonical)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("Content-Type = %q; want application/json", got)
	}
}
