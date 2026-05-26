package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type fakeEcosystemHandler struct {
	mu sync.Mutex

	pinErr          error
	prunePreview    EcosystemPrunePreviewResult
	prunePreviewErr error
	pruneErr        error
	ingestErr       error
	sweepFPErr      error
	sweepCNErr      error
	sweepRSIErr     error
	casGCErr        error
	newVersions     []string
	newVersionsErr  error

	pinCalls          []pinCall
	prunePreviewCalls []ecoVerCall
	pruneCalls        []ecoVerCall
	ingestCalls       []string
	sweepFPCalls      []string
	sweepCNCalls      []string
	sweepRSICalls     []string
	casGCCalls        int
	detectCalls       []string
}

type pinCall struct {
	Ecosystem, Version string
}

type ecoVerCall struct {
	Ecosystem, Version string
}

func (f *fakeEcosystemHandler) Pin(_ context.Context, eco, ver string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pinCalls = append(f.pinCalls, pinCall{eco, ver})
	return f.pinErr
}

func (f *fakeEcosystemHandler) PrunePreview(_ context.Context, eco, ver string) (EcosystemPrunePreviewResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prunePreviewCalls = append(f.prunePreviewCalls, ecoVerCall{eco, ver})
	if f.prunePreviewErr != nil {
		return EcosystemPrunePreviewResult{}, f.prunePreviewErr
	}
	return f.prunePreview, nil
}

func (f *fakeEcosystemHandler) Prune(_ context.Context, eco, ver string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pruneCalls = append(f.pruneCalls, ecoVerCall{eco, ver})
	return f.pruneErr
}

func (f *fakeEcosystemHandler) IngestDelta(_ context.Context, eco string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ingestCalls = append(f.ingestCalls, eco)
	return f.ingestErr
}

func (f *fakeEcosystemHandler) SweepChunkFingerprints(_ context.Context, eco string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sweepFPCalls = append(f.sweepFPCalls, eco)
	return f.sweepFPErr
}

func (f *fakeEcosystemHandler) SweepChangeNodes(_ context.Context, eco string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sweepCNCalls = append(f.sweepCNCalls, eco)
	return f.sweepCNErr
}

func (f *fakeEcosystemHandler) RebuildSymbolIndex(_ context.Context, eco string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sweepRSICalls = append(f.sweepRSICalls, eco)
	return f.sweepRSIErr
}

func (f *fakeEcosystemHandler) CASGarbageCollect(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.casGCCalls++
	return f.casGCErr
}

func (f *fakeEcosystemHandler) DetectNewVersions(_ context.Context, eco string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.detectCalls = append(f.detectCalls, eco)
	if f.newVersionsErr != nil {
		return nil, f.newVersionsErr
	}
	return f.newVersions, nil
}

type fakeEcosystemAccessor struct{ h EcosystemHandler }

func (f *fakeEcosystemAccessor) EcosystemHandler() EcosystemHandler { return f.h }

type nilEcosystemAccessor struct{}

func (nilEcosystemAccessor) EcosystemHandler() EcosystemHandler { return nil }

func TestEcosystemPin503WhenUnwired(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/pin", strings.NewReader(`{}`))
	EcosystemPin(nilEcosystemAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestEcosystemPrunePreview503WhenUnwired(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/ecosystem/prune-preview", nil)
	EcosystemPrunePreview(nilEcosystemAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestEcosystemVersionDelete503WhenUnwired(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/v1/ecosystem/version", strings.NewReader(`{}`))
	EcosystemVersionDelete(nilEcosystemAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestEcosystemIngestDelta503WhenUnwired(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/ingest-delta", strings.NewReader(`{}`))
	EcosystemIngestDelta(nilEcosystemAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestEcosystemSweepFingerprints503WhenUnwired(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/sweep/fingerprints", strings.NewReader(`{}`))
	EcosystemSweepFingerprints(nilEcosystemAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestEcosystemSweepChangeNodes503WhenUnwired(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/sweep/change-nodes", strings.NewReader(`{}`))
	EcosystemSweepChangeNodes(nilEcosystemAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestEcosystemSweepRebuildSymbolIndex503WhenUnwired(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/sweep/rebuild-symbol-index", strings.NewReader(`{}`))
	EcosystemSweepRebuildSymbolIndex(nilEcosystemAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestEcosystemSweepCASGC503WhenUnwired(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/sweep/cas-gc", nil)
	EcosystemSweepCASGC(nilEcosystemAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestEcosystemNewVersions503WhenUnwired(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/ecosystem/new-versions/go", nil)
	req.SetPathValue("eco", "go")
	EcosystemNewVersions(nilEcosystemAccessor{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestEcosystemResolveNonAccessor(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/pin", strings.NewReader(`{}`))

	EcosystemPin("not-an-accessor").ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (non-accessor s)", rec.Code)
	}
}

func TestEcosystemPinBadJSON(t *testing.T) {
	h := &fakeEcosystemHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/pin", strings.NewReader(`not json`))
	EcosystemPin(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEcosystemPinMissingEcosystem(t *testing.T) {
	h := &fakeEcosystemHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/pin",
		strings.NewReader(`{"version": "1.0.0"}`))
	EcosystemPin(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEcosystemPinMissingVersion(t *testing.T) {
	h := &fakeEcosystemHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/pin",
		strings.NewReader(`{"ecosystem": "go"}`))
	EcosystemPin(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEcosystemPinUnknownEcosystem(t *testing.T) {
	h := &fakeEcosystemHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/pin",
		strings.NewReader(`{"ecosystem": "fortran", "version": "1.0.0"}`))
	EcosystemPin(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEcosystemPrunePreviewMissingEcosystemParam(t *testing.T) {
	h := &fakeEcosystemHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/ecosystem/prune-preview?version=1.0.0", nil)
	EcosystemPrunePreview(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEcosystemPrunePreviewMissingVersionParam(t *testing.T) {
	h := &fakeEcosystemHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/ecosystem/prune-preview?ecosystem=go", nil)
	EcosystemPrunePreview(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEcosystemPrunePreviewUnknownEcosystemParam(t *testing.T) {
	h := &fakeEcosystemHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/v1/ecosystem/prune-preview?ecosystem=fortran&version=1.0.0", nil)
	EcosystemPrunePreview(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEcosystemVersionDeleteBadJSON(t *testing.T) {
	h := &fakeEcosystemHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/v1/ecosystem/version", strings.NewReader(`not json`))
	EcosystemVersionDelete(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEcosystemIngestDeltaBadJSON(t *testing.T) {
	h := &fakeEcosystemHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/ingest-delta", strings.NewReader(`not json`))
	EcosystemIngestDelta(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEcosystemIngestDeltaMissingEcosystem(t *testing.T) {
	h := &fakeEcosystemHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/ingest-delta",
		strings.NewReader(`{}`))
	EcosystemIngestDelta(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEcosystemIngestDeltaUnknownEcosystem(t *testing.T) {
	h := &fakeEcosystemHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/ingest-delta",
		strings.NewReader(`{"ecosystem": "fortran"}`))
	EcosystemIngestDelta(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEcosystemSweepFingerprintsBadJSON(t *testing.T) {
	h := &fakeEcosystemHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/sweep/fingerprints",
		strings.NewReader(`not json`))
	EcosystemSweepFingerprints(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEcosystemNewVersionsMissingEco(t *testing.T) {
	h := &fakeEcosystemHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/ecosystem/new-versions/", nil)

	EcosystemNewVersions(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEcosystemNewVersionsUnknownEco(t *testing.T) {
	h := &fakeEcosystemHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/ecosystem/new-versions/fortran", nil)
	req.SetPathValue("eco", "fortran")
	EcosystemNewVersions(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEcosystemPin404WhenVersionNotFound(t *testing.T) {
	h := &fakeEcosystemHandler{pinErr: ErrEcosystemVersionNotFound}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/pin",
		strings.NewReader(`{"ecosystem": "go", "version": "9.9.9"}`))
	EcosystemPin(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEcosystemPin409WhenAlreadyPinned(t *testing.T) {
	h := &fakeEcosystemHandler{pinErr: ErrEcosystemPinAlreadyPinned}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/pin",
		strings.NewReader(`{"ecosystem": "go", "version": "1.0.0"}`))
	EcosystemPin(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEcosystemPrunePreview404WhenVersionNotFound(t *testing.T) {
	h := &fakeEcosystemHandler{prunePreviewErr: ErrEcosystemVersionNotFound}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/v1/ecosystem/prune-preview?ecosystem=go&version=9.9.9", nil)
	EcosystemPrunePreview(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEcosystemVersionDelete409WhenPinned(t *testing.T) {
	h := &fakeEcosystemHandler{pruneErr: ErrEcosystemVersionPinned}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/v1/ecosystem/version",
		strings.NewReader(`{"ecosystem": "go", "version": "1.0.0"}`))
	EcosystemVersionDelete(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEcosystemVersionDelete404WhenVersionNotFound(t *testing.T) {
	h := &fakeEcosystemHandler{pruneErr: ErrEcosystemVersionNotFound}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/v1/ecosystem/version",
		strings.NewReader(`{"ecosystem": "go", "version": "9.9.9"}`))
	EcosystemVersionDelete(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEcosystemPin500WhenSeamFails(t *testing.T) {
	h := &fakeEcosystemHandler{pinErr: errors.New("sqlite: disk I/O error")}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/pin",
		strings.NewReader(`{"ecosystem": "go", "version": "1.0.0"}`))
	EcosystemPin(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEcosystemPrunePreview500WhenSeamFails(t *testing.T) {
	h := &fakeEcosystemHandler{prunePreviewErr: errors.New("sqlite: corrupt header")}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/v1/ecosystem/prune-preview?ecosystem=go&version=1.0.0", nil)
	EcosystemPrunePreview(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEcosystemIngestDelta500WhenSeamFails(t *testing.T) {
	h := &fakeEcosystemHandler{ingestErr: errors.New("ingest: pkg.go.dev returned 503")}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/ingest-delta",
		strings.NewReader(`{"ecosystem": "go"}`))
	EcosystemIngestDelta(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEcosystemSweepFingerprints500WhenSeamFails(t *testing.T) {
	h := &fakeEcosystemHandler{sweepFPErr: errors.New("verifier: chunk hash mismatch in row 42")}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/sweep/fingerprints",
		strings.NewReader(`{"ecosystem": "go"}`))
	EcosystemSweepFingerprints(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEcosystemSweepCASGC500WhenSeamFails(t *testing.T) {
	h := &fakeEcosystemHandler{casGCErr: errors.New("cas: missing chunk file")}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/sweep/cas-gc", nil)
	EcosystemSweepCASGC(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEcosystemNewVersions500WhenSeamFails(t *testing.T) {
	h := &fakeEcosystemHandler{newVersionsErr: errors.New("upstream: 504 gateway timeout")}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/ecosystem/new-versions/go", nil)
	req.SetPathValue("eco", "go")
	EcosystemNewVersions(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEcosystemPinHappyPath(t *testing.T) {
	h := &fakeEcosystemHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/pin",
		strings.NewReader(`{"ecosystem": "go", "version": "1.22.0"}`))
	EcosystemPin(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	if len(h.pinCalls) != 1 {
		t.Fatalf("pinCalls = %d, want 1", len(h.pinCalls))
	}
	if h.pinCalls[0].Ecosystem != "go" || h.pinCalls[0].Version != "1.22.0" {
		t.Errorf("pinCalls[0] = %+v, want {go, 1.22.0}", h.pinCalls[0])
	}
}

func TestEcosystemPrunePreviewHappyPath(t *testing.T) {
	h := &fakeEcosystemHandler{
		prunePreview: EcosystemPrunePreviewResult{
			Ecosystem:      "go",
			Version:        "1.18.0",
			ChunkCount:     150,
			ChunkFP32Count: 150,
			SymbolCount:    300,
			ChangeCount:    25,
			FTS5Count:      150,
			Pinned:         false,
		},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/v1/ecosystem/prune-preview?ecosystem=go&version=1.18.0", nil)
	EcosystemPrunePreview(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var got EcosystemPrunePreviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Ecosystem != "go" || got.Version != "1.18.0" {
		t.Errorf("got eco/ver = %q/%q, want go/1.18.0", got.Ecosystem, got.Version)
	}
	if got.ChunkCount != 150 || got.SymbolCount != 300 || got.ChangeCount != 25 {
		t.Errorf("unexpected counts: %+v", got)
	}
	if got.Pinned {
		t.Errorf("Pinned = true, want false")
	}
	if len(h.prunePreviewCalls) != 1 ||
		h.prunePreviewCalls[0].Ecosystem != "go" ||
		h.prunePreviewCalls[0].Version != "1.18.0" {
		t.Errorf("prunePreviewCalls = %+v", h.prunePreviewCalls)
	}
}

func TestEcosystemPrunePreviewPinnedFlag(t *testing.T) {
	h := &fakeEcosystemHandler{
		prunePreview: EcosystemPrunePreviewResult{
			Ecosystem: "go", Version: "1.20.0", Pinned: true,
		},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/v1/ecosystem/prune-preview?ecosystem=go&version=1.20.0", nil)
	EcosystemPrunePreview(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got EcosystemPrunePreviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Pinned {
		t.Errorf("Pinned = false, want true")
	}
}

func TestEcosystemVersionDeleteHappyPath(t *testing.T) {
	h := &fakeEcosystemHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/v1/ecosystem/version",
		strings.NewReader(`{"ecosystem": "python", "version": "3.10.0"}`))
	EcosystemVersionDelete(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	if len(h.pruneCalls) != 1 {
		t.Fatalf("pruneCalls = %d, want 1", len(h.pruneCalls))
	}
	if h.pruneCalls[0].Ecosystem != "python" || h.pruneCalls[0].Version != "3.10.0" {
		t.Errorf("pruneCalls[0] = %+v, want {python, 3.10.0}", h.pruneCalls[0])
	}
}

func TestEcosystemIngestDeltaHappyPath(t *testing.T) {
	h := &fakeEcosystemHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/ingest-delta",
		strings.NewReader(`{"ecosystem": "rust"}`))
	EcosystemIngestDelta(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	if len(h.ingestCalls) != 1 || h.ingestCalls[0] != "rust" {
		t.Errorf("ingestCalls = %+v, want [rust]", h.ingestCalls)
	}
}

func TestEcosystemSweepFingerprintsHappyPath(t *testing.T) {
	h := &fakeEcosystemHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/sweep/fingerprints",
		strings.NewReader(`{"ecosystem": "typescript"}`))
	EcosystemSweepFingerprints(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	if len(h.sweepFPCalls) != 1 || h.sweepFPCalls[0] != "typescript" {
		t.Errorf("sweepFPCalls = %+v, want [typescript]", h.sweepFPCalls)
	}
}

func TestEcosystemSweepChangeNodesHappyPath(t *testing.T) {
	h := &fakeEcosystemHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/sweep/change-nodes",
		strings.NewReader(`{"ecosystem": "python"}`))
	EcosystemSweepChangeNodes(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	if len(h.sweepCNCalls) != 1 || h.sweepCNCalls[0] != "python" {
		t.Errorf("sweepCNCalls = %+v, want [python]", h.sweepCNCalls)
	}
}

func TestEcosystemSweepRebuildSymbolIndexHappyPath(t *testing.T) {
	h := &fakeEcosystemHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/sweep/rebuild-symbol-index",
		strings.NewReader(`{"ecosystem": "go"}`))
	EcosystemSweepRebuildSymbolIndex(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	if len(h.sweepRSICalls) != 1 || h.sweepRSICalls[0] != "go" {
		t.Errorf("sweepRSICalls = %+v, want [go]", h.sweepRSICalls)
	}
}

func TestEcosystemSweepCASGCHappyPath(t *testing.T) {
	h := &fakeEcosystemHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/sweep/cas-gc", nil)
	EcosystemSweepCASGC(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	if h.casGCCalls != 1 {
		t.Errorf("casGCCalls = %d, want 1", h.casGCCalls)
	}
}

func TestEcosystemNewVersionsHappyPath(t *testing.T) {
	h := &fakeEcosystemHandler{
		newVersions: []string{"1.23.0", "1.24.0"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/ecosystem/new-versions/go", nil)
	req.SetPathValue("eco", "go")
	EcosystemNewVersions(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var got EcosystemNewVersionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Versions) != 2 || got.Versions[0] != "1.23.0" || got.Versions[1] != "1.24.0" {
		t.Errorf("Versions = %+v, want [1.23.0, 1.24.0]", got.Versions)
	}
	if len(h.detectCalls) != 1 || h.detectCalls[0] != "go" {
		t.Errorf("detectCalls = %+v, want [go]", h.detectCalls)
	}
}

func TestEcosystemNewVersionsEmptyListNotNull(t *testing.T) {
	h := &fakeEcosystemHandler{newVersions: nil}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/ecosystem/new-versions/go", nil)
	req.SetPathValue("eco", "go")
	EcosystemNewVersions(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	body := strings.TrimSpace(rec.Body.String())
	if !strings.Contains(body, `"versions":[]`) {
		t.Errorf("body = %q, want versions:[] non-null", body)
	}
}

func TestEcosystemPinAllValidEcosystems(t *testing.T) {
	for _, eco := range []string{"go", "python", "typescript", "rust"} {
		t.Run(eco, func(t *testing.T) {
			h := &fakeEcosystemHandler{}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/pin",
				strings.NewReader(`{"ecosystem": "`+eco+`", "version": "1.0.0"}`))
			EcosystemPin(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
			if rec.Code != http.StatusNoContent {
				t.Errorf("eco=%s: status = %d, want 204; body=%s",
					eco, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestEcosystemNewVersionsAllValidEcosystems(t *testing.T) {
	for _, eco := range []string{"go", "python", "typescript", "rust"} {
		t.Run(eco, func(t *testing.T) {
			h := &fakeEcosystemHandler{newVersions: []string{}}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/v1/ecosystem/new-versions/"+eco, nil)
			req.SetPathValue("eco", eco)
			EcosystemNewVersions(&fakeEcosystemAccessor{h: h}).ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("eco=%s: status = %d, want 200; body=%s",
					eco, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestIsValidEcosystem(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"go", true},
		{"python", true},
		{"typescript", true},
		{"rust", true},
		{"java", false},
		{"GO", false},
		{"  go", false},
		{"go ", false},
	}
	for _, c := range cases {
		if got := isValidEcosystem(c.in); got != c.want {
			t.Errorf("isValidEcosystem(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestEcosystemPinRoundTripCapturesIndefiniteRetain(t *testing.T) {

	seam := &fakeEcosystemHandler{}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ecosystem/pin",
		strings.NewReader(`{"ecosystem": "go", "version": "1.21.0"}`))
	EcosystemPin(&fakeEcosystemAccessor{h: seam}).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	if len(seam.pinCalls) != 1 {
		t.Fatalf("Pin not called exactly once: %d", len(seam.pinCalls))
	}
	got := seam.pinCalls[0]
	if got.Ecosystem != "go" || got.Version != "1.21.0" {
		t.Errorf("seam.Pin called with %+v, want {go, 1.21.0}", got)
	}

}
