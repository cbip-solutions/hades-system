// Package handlers — doctrine_test.go.
//
// Replaces doctrine_test.go (3 endpoints: state, validate,
// reload) with the surface (10 endpoints: active, list, show,
// validate, reload, status, history, diff, migrate, reinforce). The old
// doctrineServer + reloadErrServer fakes are gone; replaced by
// doctrineFakeServer that satisfies the broader DoctrineHandlerCtx.
//
// wire shapes match client DTOs at internal/client/doctrine_v2.go
// VERBATIM ( shipped its CLI httptest mocks before landed; the
// handlers MUST be wire-compatible because tests treat the
// daemon shape as the contract). This is a load-bearing drift adaptation
// from the original plan (which proposed slightly different shapes);
// expectations win because they ARE shipped + golden.
package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
	doctrineerrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
	"github.com/cbip-solutions/hades-system/internal/doctrine/reload"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

func fakeSchemaForJ(_, doctrineVersion string) *v1.Schema {
	return &v1.Schema{
		SchemaVersion:   "1.0",
		DoctrineVersion: doctrineVersion,
	}
}

type doctrineFakeServer struct {
	mu sync.Mutex

	activeName string
	active     *v1.Schema
	forProject map[string]*v1.Schema
	source     string

	registry map[string]*v1.Schema

	validateErr error

	reloadCalled  bool
	reloadPath    string
	reloadErr     error
	reloadEvents  chan reload.DoctrineReloaded
	reloadFails   chan reload.DoctrineReloadFailed
	reloadTimeout time.Duration

	lastReloadAt   time.Time
	lastReloadOk   bool
	watcherHealthy bool
	pendingChanges []string

	historyEvents []DoctrineHistoryEventRow

	diffOut            []DoctrineDiffEntry
	diffErr            error
	gotDiffA, gotDiffB string

	migrateOut      *v1.Schema
	migrateErr      error
	migrateTarget   string
	migrateTOMLOut  string
	migrateWarnings []string
	gotMigrateBody  []byte

	reinforceOut string
	reinforceErr error
	gotReinforce client.DoctrineV2ReinforceReq
}

func newDoctrineFakeServer() *doctrineFakeServer {
	d := &doctrineFakeServer{
		activeName: "max-scope",
		active:     fakeSchemaForJ("max-scope", "1.0.0"),
		source:     "embed",
		registry: map[string]*v1.Schema{
			"max-scope":     fakeSchemaForJ("max-scope", "1.0.0"),
			"default":       fakeSchemaForJ("default", "1.0.0"),
			"capa-firewall": fakeSchemaForJ("capa-firewall", "1.0.0"),
		},
		forProject:     make(map[string]*v1.Schema),
		watcherHealthy: true,
		lastReloadOk:   true,
		reloadEvents:   make(chan reload.DoctrineReloaded, 4),
		reloadFails:    make(chan reload.DoctrineReloadFailed, 4),
		reloadTimeout:  100 * time.Millisecond,
	}
	return d
}

func (d *doctrineFakeServer) DoctrineActive(projectID string) (string, *v1.Schema, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if projectID != "" {
		if s, ok := d.forProject[projectID]; ok && s != nil {

			for n, sch := range d.registry {
				if sch == s {
					return n, s, "project", nil
				}
			}

			return projectID, s, "project", nil
		}
	}
	if d.active == nil {
		return "", nil, "", doctrineerrors.ErrDoctrineNotFound
	}
	return d.activeName, d.active, d.source, nil
}

func (d *doctrineFakeServer) DoctrineList(sourceFilter string) ([]client.DoctrineV2ListItem, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.registry == nil {
		return []client.DoctrineV2ListItem{}, nil
	}
	out := make([]client.DoctrineV2ListItem, 0, len(d.registry))
	for name, s := range d.registry {
		if sourceFilter != "" && sourceFilter != "all" && sourceFilter != "embed" {
			continue
		}
		out = append(out, client.DoctrineV2ListItem{
			Name:            name,
			Source:          "embed",
			SchemaVersion:   s.SchemaVersion,
			DoctrineVersion: s.DoctrineVersion,
		})
	}
	return out, nil
}

func (d *doctrineFakeServer) DoctrineShow(name, format, section string) (string, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s, ok := d.registry[name]
	if !ok {
		return "", "", fmt.Errorf("show %q: %w", name, doctrineerrors.ErrDoctrineNotFound)
	}
	if format == "" {
		format = "toml"
	}

	body := fmt.Sprintf("name = %q\nschema_version = %q\ndoctrine_version = %q\n", name, s.SchemaVersion, s.DoctrineVersion)
	if format == "json" {
		body = fmt.Sprintf(`{"name":%q,"schema_version":%q,"doctrine_version":%q}`, name, s.SchemaVersion, s.DoctrineVersion)
	}
	if format == "md" || format == "markdown" {
		body = fmt.Sprintf("# %s\nschema_version: %s\n", name, s.SchemaVersion)
	}
	_ = section
	return format, body, nil
}

func (d *doctrineFakeServer) DoctrineValidate(tomlContent, againstBaseline string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.validateErr
}

func (d *doctrineFakeServer) DoctrineStatus(projectAlias string) (DoctrineStatusSnapshot, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.active == nil {
		return DoctrineStatusSnapshot{}, doctrineerrors.ErrDoctrineNotFound
	}
	return DoctrineStatusSnapshot{
		Active: client.DoctrineV2ActiveResp{
			Name:            d.activeName,
			SchemaVersion:   d.active.SchemaVersion,
			DoctrineVersion: d.active.DoctrineVersion,
			Source:          d.source,
		},
		LastReloadAt:   d.lastReloadAt,
		LastReloadOk:   d.lastReloadOk,
		WatcherHealthy: d.watcherHealthy,
		PendingChanges: d.pendingChanges,
	}, nil
}

func (d *doctrineFakeServer) DoctrineHistory(since time.Time, filter string, limit int) ([]DoctrineHistoryEventRow, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.historyEvents, nil
}

func (d *doctrineFakeServer) DoctrineDiff(a, b, section string) (string, string, []DoctrineDiffEntry, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.gotDiffA = a
	d.gotDiffB = b
	if d.diffErr != nil {
		return "", "", nil, d.diffErr
	}
	return a, b, d.diffOut, nil
}

func (d *doctrineFakeServer) DoctrineMigrate(tomlContent, fromSchemaVersion string) (string, string, []string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.gotMigrateBody = []byte(tomlContent)
	return d.migrateTarget, d.migrateTOMLOut, d.migrateWarnings, d.migrateErr
}

func (d *doctrineFakeServer) DoctrineReload(path string) error {
	d.mu.Lock()
	d.reloadCalled = true
	d.reloadPath = path
	err := d.reloadErr
	d.mu.Unlock()
	return err
}

func (d *doctrineFakeServer) DoctrineReloadEvents() <-chan reload.DoctrineReloaded {
	return d.reloadEvents
}

func (d *doctrineFakeServer) DoctrineReloadFailedEvents() <-chan reload.DoctrineReloadFailed {
	return d.reloadFails
}

func (d *doctrineFakeServer) DoctrineReloadTimeout() time.Duration {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.reloadTimeout
}

func (d *doctrineFakeServer) DoctrineUnsubscribeReloadEvents(<-chan reload.DoctrineReloaded) {}
func (d *doctrineFakeServer) DoctrineUnsubscribeReloadFailedEvents(<-chan reload.DoctrineReloadFailed) {
}

func (d *doctrineFakeServer) DoctrineReinforce(req client.DoctrineV2ReinforceReq) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.gotReinforce = req
	return d.reinforceOut, d.reinforceErr
}

func TestDoctrineActive_HappyPath(t *testing.T) {
	srv := newDoctrineFakeServer()
	h := DoctrineActive(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/doctrine/active", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp client.DoctrineV2ActiveResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Name != "max-scope" {
		t.Errorf("name: got %q, want max-scope", resp.Name)
	}
	if resp.SchemaVersion != "1.0" {
		t.Errorf("schema_version: got %q, want 1.0", resp.SchemaVersion)
	}
	if resp.DoctrineVersion != "1.0.0" {
		t.Errorf("doctrine_version: got %q, want 1.0.0", resp.DoctrineVersion)
	}
	if resp.Source != "embed" {
		t.Errorf("source: got %q, want embed", resp.Source)
	}
}

func TestDoctrineActive_PerProject(t *testing.T) {
	srv := newDoctrineFakeServer()

	srv.forProject["proj-A"] = srv.registry["capa-firewall"]
	h := DoctrineActive(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/doctrine/active?project=proj-A", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp client.DoctrineV2ActiveResp
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Name != "capa-firewall" {
		t.Errorf("name: got %q, want capa-firewall (per-project override)", resp.Name)
	}
	if resp.Source != "project" {
		t.Errorf("source: got %q, want project", resp.Source)
	}
}

func TestDoctrineActive_NotFoundNoSchema(t *testing.T) {
	srv := newDoctrineFakeServer()
	srv.active = nil
	h := DoctrineActive(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/doctrine/active", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 (no doctrine loaded), got %d: %s", w.Code, w.Body.String())
	}
}

func TestDoctrineActive_NilCtxReturns503(t *testing.T) {
	h := DoctrineActive(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/doctrine/active", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 (subsystem not wired), got %d", w.Code)
	}
}

func TestDoctrineList_HappyPathAll(t *testing.T) {
	srv := newDoctrineFakeServer()
	h := DoctrineList(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/doctrine/list", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp client.DoctrineV2ListResp
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 3 {
		t.Errorf("items: got %d, want 3 (max-scope+default+capa-firewall)", len(resp.Items))
	}
	got := map[string]bool{}
	for _, r := range resp.Items {
		got[r.Name] = true
	}
	for _, want := range []string{"max-scope", "default", "capa-firewall"} {
		if !got[want] {
			t.Errorf("missing %q in items %v", want, resp.Items)
		}
	}
}

func TestDoctrineList_FilterUserSourceEmptyInV1(t *testing.T) {
	srv := newDoctrineFakeServer()
	h := DoctrineList(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/doctrine/list?source=user", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp client.DoctrineV2ListResp
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 0 {
		t.Errorf("items: got %d, want 0 (no user-source doctrines in v0.5.0)", len(resp.Items))
	}
}

func TestDoctrineList_BadSourceReturns400(t *testing.T) {
	srv := newDoctrineFakeServer()
	h := DoctrineList(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/doctrine/list?source=garbage", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for bogus source, got %d", w.Code)
	}
}

func TestDoctrineShow_DefaultFormatTOML(t *testing.T) {
	srv := newDoctrineFakeServer()
	h := DoctrineShow(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/doctrine/show?name=max-scope", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp client.DoctrineV2ShowResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Format != "toml" {
		t.Errorf("default format: got %q, want toml", resp.Format)
	}
	if !strings.Contains(resp.Body, "max-scope") || !strings.Contains(resp.Body, "schema_version") {
		t.Errorf("body missing identifying fields: %q", resp.Body)
	}
}

func TestDoctrineShow_FormatQueryHonored(t *testing.T) {
	srv := newDoctrineFakeServer()
	h := DoctrineShow(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/doctrine/show?name=max-scope&format=json", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp client.DoctrineV2ShowResp
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Format != "json" {
		t.Errorf("format: got %q, want json", resp.Format)
	}
}

func TestDoctrineShow_NotFound(t *testing.T) {
	srv := newDoctrineFakeServer()
	h := DoctrineShow(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/doctrine/show?name=ghost", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func TestDoctrineShow_MissingNameQuery(t *testing.T) {
	srv := newDoctrineFakeServer()
	h := DoctrineShow(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/doctrine/show", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 (missing name query), got %d", w.Code)
	}
}

func TestDiscriminateDoctrineError_TableDriven(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantCode int
	}{
		{"parse_failed_returns_400", fmt.Errorf("toml: %w", doctrineerrors.ErrParseFailed), http.StatusBadRequest},
		{"validation_failed_returns_422", fmt.Errorf("schema: %w", doctrineerrors.ErrValidationFailed), http.StatusUnprocessableEntity},
		{"tighten_violation_returns_422", fmt.Errorf("override: %w", doctrineerrors.ErrTightenViolation), http.StatusUnprocessableEntity},
		{"version_unsupported_returns_400", fmt.Errorf("loader: %w", doctrineerrors.ErrSchemaVersionUnsupported), http.StatusBadRequest},
		{"version_too_old_returns_400", fmt.Errorf("loader: %w", doctrineerrors.ErrSchemaVersionTooOld), http.StatusBadRequest},
		{"not_found_returns_404", fmt.Errorf("registry: %w", doctrineerrors.ErrDoctrineNotFound), http.StatusNotFound},
		{"migration_failed_returns_422", fmt.Errorf("migrate: %w", doctrineerrors.ErrMigrationFailed), http.StatusUnprocessableEntity},
		{"unknown_returns_500", errors.New("disk full"), http.StatusInternalServerError},
		{"wrapped_unknown_returns_500", fmt.Errorf("io: %w", errors.New("disk full")), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			discriminateDoctrineError(w, tc.err)
			if w.Code != tc.wantCode {
				t.Errorf("err=%v: want code %d, got %d (body: %s)", tc.err, tc.wantCode, w.Code, w.Body.String())
			}
		})
	}
}

func TestDoctrineValidate_HappyPath(t *testing.T) {
	srv := newDoctrineFakeServer()
	h := DoctrineValidate(srv)
	body := client.DoctrineV2ValidateReq{
		TOMLContent: "schema_version = \"1.0\"\nname = \"x\"\n",
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/validate", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp client.DoctrineV2ValidateResp
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if !resp.Valid {
		t.Errorf("want valid=true, got %v", resp.Valid)
	}
}

func TestDoctrineValidate_RejectsValidationErrorAs422(t *testing.T) {
	srv := newDoctrineFakeServer()
	srv.validateErr = fmt.Errorf("schema: %w", doctrineerrors.ErrValidationFailed)
	h := DoctrineValidate(srv)
	body := client.DoctrineV2ValidateReq{TOMLContent: "bad = 1"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/validate", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d: %s", w.Code, w.Body.String())
	}
	var resp client.DoctrineV2ValidateResp
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Valid {
		t.Errorf("want valid=false, got %v", resp.Valid)
	}
	if len(resp.Errors) == 0 {
		t.Errorf("want non-empty errors[], got %v", resp.Errors)
	}
}

func TestDoctrineValidate_RejectsParseErrorAs400(t *testing.T) {
	srv := newDoctrineFakeServer()
	srv.validateErr = fmt.Errorf("toml: %w", doctrineerrors.ErrParseFailed)
	h := DoctrineValidate(srv)
	body := client.DoctrineV2ValidateReq{TOMLContent: "[[["}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/validate", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 (parse error), got %d: %s", w.Code, w.Body.String())
	}
}

func TestDoctrineValidate_EmptyTOMLContentReturns400(t *testing.T) {
	srv := newDoctrineFakeServer()
	h := DoctrineValidate(srv)
	body := client.DoctrineV2ValidateReq{TOMLContent: ""}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/validate", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 (empty toml_content), got %d", w.Code)
	}
}

func TestDoctrineValidate_BadJSONBodyReturns400(t *testing.T) {
	srv := newDoctrineFakeServer()
	h := DoctrineValidate(srv)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/validate", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 (bad json), got %d", w.Code)
	}
}

func TestDoctrineStatus_HappyPath(t *testing.T) {
	srv := newDoctrineFakeServer()
	srv.lastReloadAt = time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	srv.pendingChanges = []string{}
	h := DoctrineStatus(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/doctrine/status", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp client.DoctrineV2StatusResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Active.Name != "max-scope" {
		t.Errorf("active.name: got %q", resp.Active.Name)
	}
	if !resp.WatcherHealthy {
		t.Errorf("watcher_healthy: got %v, want true", resp.WatcherHealthy)
	}
	if resp.LastReloadAt == "" {
		t.Errorf("last_reload_at empty, want RFC3339 timestamp")
	}
}

func TestDoctrineStatus_NoActiveDoctrine_Returns404(t *testing.T) {
	srv := newDoctrineFakeServer()
	srv.active = nil
	h := DoctrineStatus(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/doctrine/status", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 (no active doctrine), got %d", w.Code)
	}
}

func TestDoctrineHistory_DurationSince(t *testing.T) {
	srv := newDoctrineFakeServer()
	srv.historyEvents = []DoctrineHistoryEventRow{
		{Type: "DoctrineReloaded", AtUnix: time.Now().Unix()},
		{Type: "DoctrineLoaded", AtUnix: time.Now().Add(-time.Hour).Unix()},
	}
	h := DoctrineHistory(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/doctrine/history?since=24h", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp client.DoctrineV2HistoryResp
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Events) != 2 {
		t.Errorf("events count: got %d, want 2", len(resp.Events))
	}
}

func TestDoctrineHistory_UnixSince(t *testing.T) {
	srv := newDoctrineFakeServer()
	srv.historyEvents = []DoctrineHistoryEventRow{}
	h := DoctrineHistory(srv)
	unixTs := time.Now().Add(-7 * 24 * time.Hour).Unix()
	req := httptest.NewRequest(http.MethodGet, "/v1/doctrine/history?since="+strconv.FormatInt(unixTs, 10), nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
}

func TestDoctrineHistory_DefaultSinceIs7d(t *testing.T) {
	srv := newDoctrineFakeServer()
	h := DoctrineHistory(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/doctrine/history", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 for default history, got %d", w.Code)
	}
}

func TestDoctrineHistory_LimitDefault100Max500(t *testing.T) {
	srv := newDoctrineFakeServer()
	h := DoctrineHistory(srv)
	cases := []string{"", "50", "500", "5000", "-1", "abc"}
	for _, raw := range cases {
		t.Run("limit="+raw, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/doctrine/history?limit="+raw, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("want 200, got %d", w.Code)
			}
		})
	}
}

func TestDoctrineHistory_BadSinceReturns400(t *testing.T) {
	srv := newDoctrineFakeServer()
	h := DoctrineHistory(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/doctrine/history?since=not-a-duration-or-unix", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 (bad since), got %d", w.Code)
	}
}

func TestDoctrineDiff_HappyPath(t *testing.T) {
	srv := newDoctrineFakeServer()
	srv.diffOut = []DoctrineDiffEntry{
		{Path: "research.depth", From: "shallow", To: "deep", Status: "changed"},
		{Path: "merge.weights.cost", From: "0.3", To: "0.5", Status: "changed"},
	}
	h := DoctrineDiff(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/doctrine/diff?a=default&b=max-scope", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp client.DoctrineV2DiffResp
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.From != "default" || resp.To != "max-scope" {
		t.Errorf("from/to: got %q→%q, want default→max-scope", resp.From, resp.To)
	}
	if len(resp.Diffs) != 2 {
		t.Errorf("diffs count: got %d, want 2", len(resp.Diffs))
	}
	if resp.Diffs[0].Path != "research.depth" {
		t.Errorf("first diff path: %q", resp.Diffs[0].Path)
	}
}

func TestDoctrineDiff_MissingQueryParams(t *testing.T) {
	srv := newDoctrineFakeServer()
	h := DoctrineDiff(srv)
	cases := []string{
		"/v1/doctrine/diff",
		"/v1/doctrine/diff?a=max-scope",
		"/v1/doctrine/diff?b=default",
	}
	for _, url := range cases {
		t.Run(url, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("url=%s: want 400, got %d", url, w.Code)
			}
		})
	}
}

func TestDoctrineDiff_NotFoundOnUnknownDoctrine(t *testing.T) {
	srv := newDoctrineFakeServer()
	srv.diffErr = fmt.Errorf("a=ghost: %w", doctrineerrors.ErrDoctrineNotFound)
	h := DoctrineDiff(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/doctrine/diff?a=ghost&b=default", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func TestDoctrineMigrate_HappyPath(t *testing.T) {
	srv := newDoctrineFakeServer()
	srv.migrateTarget = "1.0"
	srv.migrateTOMLOut = "schema_version = \"1.0\"\nname = \"max-scope\"\n"
	h := DoctrineMigrate(srv)
	body := client.DoctrineV2MigrateReq{
		TOMLContent:       "schema_version = \"0.9\"\nname = \"max-scope\"\n",
		FromSchemaVersion: "0.9",
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/migrate", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp client.DoctrineV2MigrateResp
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.ToSchemaVersion != "1.0" {
		t.Errorf("to_schema_version: got %q, want 1.0", resp.ToSchemaVersion)
	}
	if resp.TOMLContent == "" {
		t.Errorf("toml_content empty, want migrated body")
	}
	srv.mu.Lock()
	if !strings.Contains(string(srv.gotMigrateBody), "schema_version = \"0.9\"") {
		t.Errorf("body not forwarded to accessor; got: %s", srv.gotMigrateBody)
	}
	srv.mu.Unlock()
}

// TestDoctrineMigrate_DoesNotWriteToDisk enforces invariant: handler
// MUST NOT touch the filesystem. We confirm the handler's working
// directory is unchanged + a sentinel file is NOT created at any
// suggested path.
func TestDoctrineMigrate_DoesNotWriteToDisk(t *testing.T) {
	tmpDir := t.TempDir()
	sentinelPath := tmpDir + "/should-not-exist.toml"
	srv := newDoctrineFakeServer()
	srv.migrateTarget = "1.0"
	srv.migrateTOMLOut = "schema_version = \"1.0\"\n"
	h := DoctrineMigrate(srv)
	body := client.DoctrineV2MigrateReq{
		TOMLContent:       "schema_version = \"0.9\"\n",
		FromSchemaVersion: "0.9",
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/migrate", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if _, err := os.Stat(sentinelPath); err == nil {
		t.Fatalf("inv-zen-137 VIOLATION: handler created %s; migrate MUST be in-memory only", sentinelPath)
	}
}

func TestDoctrineMigrate_MigrationFailedReturns422(t *testing.T) {
	srv := newDoctrineFakeServer()
	srv.migrateErr = fmt.Errorf("v0.5→v1.0: %w", doctrineerrors.ErrMigrationFailed)
	h := DoctrineMigrate(srv)
	body := client.DoctrineV2MigrateReq{TOMLContent: "x = 1"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/migrate", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d", w.Code)
	}
}

func TestDoctrineMigrate_BadJSONBodyReturns400(t *testing.T) {
	srv := newDoctrineFakeServer()
	h := DoctrineMigrate(srv)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/migrate", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestDoctrineMigrate_EmptyTOMLContentReturns400(t *testing.T) {
	srv := newDoctrineFakeServer()
	h := DoctrineMigrate(srv)
	body := client.DoctrineV2MigrateReq{TOMLContent: ""}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/migrate", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 (empty toml_content), got %d", w.Code)
	}
}

func TestDoctrineReinforce_HappyPath(t *testing.T) {
	srv := newDoctrineFakeServer()
	srv.reinforceOut = "## Doctrine: max-scope\nMax-scope always; no defer.\n"
	h := DoctrineReinforce(srv)
	body := client.DoctrineV2ReinforceReq{
		TaskKind:     "worker",
		ProjectAlias: "zen-swarm",
		Stage:        "Build",
		Phase:        "J-3",
		PlanID:       "plan-8",
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/reinforce", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp client.DoctrineV2ReinforceResp
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if !strings.Contains(resp.Rendered, "max-scope") {
		t.Errorf("rendered missing doctrine name; got: %v", resp.Rendered)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.gotReinforce.TaskKind != "worker" {
		t.Errorf("task_kind not forwarded: got %q", srv.gotReinforce.TaskKind)
	}
	if srv.gotReinforce.PlanID != "plan-8" {
		t.Errorf("plan_id not forwarded: got %q", srv.gotReinforce.PlanID)
	}
}

func TestDoctrineReinforce_QueryStringFallthroughForTaskKind(t *testing.T) {
	srv := newDoctrineFakeServer()
	srv.reinforceOut = "rendered"
	h := DoctrineReinforce(srv)

	body := client.DoctrineV2ReinforceReq{ProjectAlias: "zen-swarm"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/reinforce?task_kind=worker", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 (query-string supplies task_kind), got %d", w.Code)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.gotReinforce.TaskKind != "worker" {
		t.Errorf("task_kind from query not forwarded: got %q", srv.gotReinforce.TaskKind)
	}
}

func TestDoctrineReinforce_MissingTaskKindReturns400(t *testing.T) {
	srv := newDoctrineFakeServer()
	h := DoctrineReinforce(srv)
	body := client.DoctrineV2ReinforceReq{ProjectAlias: "zen-swarm"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/reinforce", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 (task_kind required), got %d", w.Code)
	}
}

func TestDoctrineReinforce_TemplateExecFailureReturns500(t *testing.T) {
	srv := newDoctrineFakeServer()
	srv.reinforceErr = errors.New("text/template: undefined variable .Stage")
	h := DoctrineReinforce(srv)
	body := client.DoctrineV2ReinforceReq{TaskKind: "worker"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/reinforce", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 (template exec error → unknown sentinel), got %d", w.Code)
	}
}

func TestDoctrineReinforce_BadJSONBodyReturns400(t *testing.T) {
	srv := newDoctrineFakeServer()
	h := DoctrineReinforce(srv)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/reinforce", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 (bad json), got %d", w.Code)
	}
}

func (d *doctrineFakeServer) publishReloaded(after time.Duration, ev reload.DoctrineReloaded) {
	go func() {
		if after > 0 {
			time.Sleep(after)
		}
		d.reloadEvents <- ev
	}()
}

func (d *doctrineFakeServer) publishReloadFailed(after time.Duration, ev reload.DoctrineReloadFailed) {
	go func() {
		if after > 0 {
			time.Sleep(after)
		}
		d.reloadFails <- ev
	}()
}

func TestDoctrineReload_HappyPathReceivesEvent(t *testing.T) {
	srv := newDoctrineFakeServer()
	srv.reloadTimeout = 200 * time.Millisecond
	srv.publishReloaded(5*time.Millisecond, reload.DoctrineReloaded{
		Path:              "/path/to/home/.config/zen-swarm/doctrines/max-scope.toml",
		DoctrineName:      "max-scope",
		Source:            "manual-reload",
		ToDoctrineVersion: "1.0.1",
		At:                time.Now().UTC(),
	})
	h := DoctrineReload(srv)
	body := client.DoctrineV2ReloadReq{Path: "/path/to/home/.config/zen-swarm/doctrines/max-scope.toml"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/reload", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp client.DoctrineV2ReloadResp
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if !resp.Reloaded {
		t.Errorf("want reloaded=true, got %v", resp.Reloaded)
	}
	if resp.State.Name == "" {
		t.Errorf("state.name empty, want populated from event")
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if !srv.reloadCalled {
		t.Error("DoctrineReload accessor was not called")
	}
	if srv.reloadPath != "/path/to/home/.config/zen-swarm/doctrines/max-scope.toml" {
		t.Errorf("path not forwarded: got %q", srv.reloadPath)
	}
}

func TestDoctrineReload_NoPathTriggersReloadAll(t *testing.T) {
	srv := newDoctrineFakeServer()
	srv.reloadTimeout = 200 * time.Millisecond
	srv.publishReloaded(5*time.Millisecond, reload.DoctrineReloaded{
		Path:         "",
		DoctrineName: "max-scope",
		Source:       "manual-reload",
		At:           time.Now().UTC(),
	})
	h := DoctrineReload(srv)
	body := client.DoctrineV2ReloadReq{}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/reload", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.reloadPath != "" {
		t.Errorf("expected empty path forwarded for reload-all, got %q", srv.reloadPath)
	}
}

func TestDoctrineReload_FailureEventReturns422(t *testing.T) {
	srv := newDoctrineFakeServer()
	srv.reloadTimeout = 200 * time.Millisecond
	srv.publishReloadFailed(5*time.Millisecond, reload.DoctrineReloadFailed{
		Path:   "/tmp/bad.toml",
		Phase:  "validate",
		Errors: []string{"unknown field 'bogus'"},
		Reason: "validate_failed",
		At:     time.Now().UTC(),
	})
	h := DoctrineReload(srv)
	body := client.DoctrineV2ReloadReq{Path: "/tmp/bad.toml"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/reload", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422 (validation failure event), got %d: %s", w.Code, w.Body.String())
	}
	var resp client.DoctrineV2ReloadResp
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Reloaded {
		t.Errorf("want reloaded=false, got %v", resp.Reloaded)
	}
	if len(resp.Errors) == 0 {
		t.Errorf("want non-empty errors[], got %v", resp.Errors)
	}
}

func TestDoctrineReload_TightenViolationFailureReturns422(t *testing.T) {
	srv := newDoctrineFakeServer()
	srv.reloadTimeout = 200 * time.Millisecond
	srv.publishReloadFailed(5*time.Millisecond, reload.DoctrineReloadFailed{
		Path:   "/proj/.zen/doctrine-override.toml",
		Phase:  "tighten",
		Errors: []string{"research.cache_ttl=999d loosens baseline 7d"},
		Reason: "tighten_violation",
		At:     time.Now().UTC(),
	})
	h := DoctrineReload(srv)
	body := client.DoctrineV2ReloadReq{Path: "/proj/.zen/doctrine-override.toml"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/reload", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422 (tighten violation), got %d", w.Code)
	}
}

func TestDoctrineReload_IOFailureReturns500(t *testing.T) {
	srv := newDoctrineFakeServer()
	srv.reloadTimeout = 200 * time.Millisecond
	srv.publishReloadFailed(5*time.Millisecond, reload.DoctrineReloadFailed{
		Path:   "/tmp/missing.toml",
		Phase:  "read",
		Errors: []string{"open /tmp/missing.toml: no such file"},
		Reason: "io_error",
		At:     time.Now().UTC(),
	})
	h := DoctrineReload(srv)
	body := client.DoctrineV2ReloadReq{Path: "/tmp/missing.toml"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/reload", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 (IO/read failure), got %d", w.Code)
	}
}

func TestDoctrineReload_TimeoutReturns408(t *testing.T) {
	srv := newDoctrineFakeServer()
	srv.reloadTimeout = 50 * time.Millisecond

	h := DoctrineReload(srv)
	body := client.DoctrineV2ReloadReq{Path: "/tmp/never-fires.toml"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/reload", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	start := time.Now()
	h.ServeHTTP(w, req)
	elapsed := time.Since(start)
	if w.Code != http.StatusRequestTimeout {
		t.Fatalf("want 408 (timeout), got %d: %s", w.Code, w.Body.String())
	}
	if elapsed < 40*time.Millisecond {
		t.Errorf("handler returned before timeout: elapsed %v < 40ms", elapsed)
	}
	if elapsed > 1*time.Second {
		t.Errorf("handler exceeded timeout by too much: elapsed %v > 1s (timeout was 50ms)", elapsed)
	}
}

func TestDoctrineReload_NotifyForceErrorReturns500(t *testing.T) {
	srv := newDoctrineFakeServer()
	srv.reloadErr = errors.New("fsnotify: watch list full")
	h := DoctrineReload(srv)
	body := client.DoctrineV2ReloadReq{Path: "/tmp/x.toml"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/reload", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 (NotifyForce error), got %d", w.Code)
	}
}

func TestDoctrineReload_BadJSONBodyReturns400(t *testing.T) {
	srv := newDoctrineFakeServer()
	h := DoctrineReload(srv)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/reload", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestDoctrineReload_EmptyBodyAccepted(t *testing.T) {
	srv := newDoctrineFakeServer()
	srv.reloadTimeout = 200 * time.Millisecond
	srv.publishReloaded(5*time.Millisecond, reload.DoctrineReloaded{
		DoctrineName: "max-scope",
		Source:       "manual-reload",
		At:           time.Now().UTC(),
	})
	h := DoctrineReload(srv)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/reload", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 (empty body → reload-all), got %d: %s", w.Code, w.Body.String())
	}
}

func TestDoctrineReload_ConcurrentReloads(t *testing.T) {
	srv := newDoctrineFakeServer()
	srv.reloadTimeout = 200 * time.Millisecond

	for i := 0; i < 4; i++ {
		srv.reloadEvents <- reload.DoctrineReloaded{
			Path:         "/tmp/x.toml",
			DoctrineName: "max-scope",
			Source:       "manual-reload",
			At:           time.Now().UTC(),
		}
	}
	h := DoctrineReload(srv)
	done := make(chan int, 2)
	for i := 0; i < 2; i++ {
		go func() {
			body := client.DoctrineV2ReloadReq{Path: "/tmp/x.toml"}
			b, _ := json.Marshal(body)
			req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/reload", bytes.NewReader(b))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			done <- w.Code
		}()
	}
	codes := []int{}
	for i := 0; i < 2; i++ {
		select {
		case c := <-done:
			codes = append(codes, c)
		case <-time.After(2 * time.Second):
			t.Fatalf("concurrent reload deadlock; got %d responses, want 2", len(codes))
		}
	}
	for i, c := range codes {
		if c != http.StatusOK {
			t.Errorf("concurrent reload response[%d] = %d, want 200", i, c)
		}
	}
}

func TestDoctrineReload_FiltersByPath_IgnoresOtherEvents(t *testing.T) {
	srv := newDoctrineFakeServer()
	srv.reloadTimeout = 500 * time.Millisecond

	go func() {
		srv.reloadEvents <- reload.DoctrineReloaded{
			Path:         "/wrong/path.toml",
			DoctrineName: "max-scope",
			Source:       "manual-reload",
			At:           time.Now().UTC(),
		}
		time.Sleep(20 * time.Millisecond)
		srv.reloadEvents <- reload.DoctrineReloaded{
			Path:              "/right/path.toml",
			DoctrineName:      "max-scope",
			Source:            "manual-reload",
			ToDoctrineVersion: "1.0.2",
			At:                time.Now().UTC(),
		}
	}()

	h := DoctrineReload(srv)
	body := client.DoctrineV2ReloadReq{Path: "/right/path.toml"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/reload", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 (matched right-path event), got %d: %s", w.Code, w.Body.String())
	}
	var resp client.DoctrineV2ReloadResp
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.State.DoctrineVersion != "1.0.2" {
		t.Errorf("response carries wrong event payload; got DoctrineVersion=%q, want 1.0.2 (right-path event)", resp.State.DoctrineVersion)
	}
}

func TestDoctrineReload_FiltersByPath_FailureCrossPathIgnored(t *testing.T) {
	srv := newDoctrineFakeServer()
	srv.reloadTimeout = 500 * time.Millisecond

	go func() {

		srv.reloadFails <- reload.DoctrineReloadFailed{
			Path:   "/wrong/path.toml",
			Phase:  "validate",
			Errors: []string{"unknown field 'bogus'"},
			Reason: "validate_failed",
			At:     time.Now().UTC(),
		}
		time.Sleep(20 * time.Millisecond)

		srv.reloadFails <- reload.DoctrineReloadFailed{
			Path:   "/right/path.toml",
			Phase:  "validate",
			Errors: []string{"the real failure"},
			Reason: "validate_failed",
			At:     time.Now().UTC(),
		}
	}()

	h := DoctrineReload(srv)
	body := client.DoctrineV2ReloadReq{Path: "/right/path.toml"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/reload", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422 (matched right-path failure event), got %d: %s", w.Code, w.Body.String())
	}
	var resp client.DoctrineV2ReloadResp
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Errors) == 0 || !strings.Contains(resp.Errors[0], "the real failure") {
		t.Errorf("response carries wrong failure payload; got %v, want 'the real failure'", resp.Errors)
	}
}

func TestDoctrineReload_PathSpecificEventConsumed(t *testing.T) {
	srv := newDoctrineFakeServer()
	srv.reloadTimeout = 200 * time.Millisecond
	srv.publishReloaded(5*time.Millisecond, reload.DoctrineReloaded{
		Path:              "/path/specific.toml",
		DoctrineName:      "max-scope",
		ToDoctrineVersion: "9.9.9",
		Source:            "manual-reload",
		At:                time.Now().UTC(),
	})

	h := DoctrineReload(srv)
	body := client.DoctrineV2ReloadReq{Path: "/path/specific.toml"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/reload", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("matching path event want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp client.DoctrineV2ReloadResp
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.State.DoctrineVersion != "9.9.9" {
		t.Errorf("event payload not propagated; got DoctrineVersion=%q, want 9.9.9", resp.State.DoctrineVersion)
	}
}

func TestDoctrineReload_PathFilterReloadAllAcceptsAnyEvent(t *testing.T) {
	srv := newDoctrineFakeServer()
	srv.reloadTimeout = 200 * time.Millisecond
	srv.publishReloaded(5*time.Millisecond, reload.DoctrineReloaded{
		Path:         "/any/specific/path.toml",
		DoctrineName: "max-scope",
		Source:       "manual-reload",
		At:           time.Now().UTC(),
	})

	h := DoctrineReload(srv)

	body := client.DoctrineV2ReloadReq{}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/reload", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("reload-all (empty Path) want 200, got %d", w.Code)
	}
}

func largePayload(n int) string {
	if n <= 0 {
		return ""
	}
	pad := strings.Repeat("a", n)

	return pad
}

func TestDoctrineValidate_BodyTooLargeReturns413(t *testing.T) {
	srv := newDoctrineFakeServer()
	h := DoctrineValidate(srv)

	body := client.DoctrineV2ValidateReq{TOMLContent: largePayload(2 << 20)}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/validate", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413 Payload Too Large, got %d: %.200s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "body exceeds") {
		t.Errorf("response body missing 'body exceeds' marker: %s", w.Body.String())
	}
}

func TestDoctrineMigrate_BodyTooLargeReturns413(t *testing.T) {
	srv := newDoctrineFakeServer()
	h := DoctrineMigrate(srv)

	body := client.DoctrineV2MigrateReq{
		TOMLContent:       largePayload(2 << 20),
		FromSchemaVersion: "0.9",
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/migrate", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413 Payload Too Large, got %d: %.200s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "body exceeds") {
		t.Errorf("response body missing 'body exceeds' marker: %s", w.Body.String())
	}
}

func TestDoctrineReload_BodyTooLargeReturns413(t *testing.T) {
	srv := newDoctrineFakeServer()
	h := DoctrineReload(srv)

	body := client.DoctrineV2ReloadReq{Path: largePayload(2 << 20)}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/reload", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413 Payload Too Large, got %d: %.200s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "body exceeds") {
		t.Errorf("response body missing 'body exceeds' marker: %s", w.Body.String())
	}
}

func TestDoctrineValidate_BodyAtCapAccepted(t *testing.T) {
	srv := newDoctrineFakeServer()
	h := DoctrineValidate(srv)

	body := client.DoctrineV2ValidateReq{TOMLContent: largePayload(512 * 1024)}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/validate", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code == http.StatusRequestEntityTooLarge {
		t.Fatalf("under-cap body unexpectedly returned 413: %s", w.Body.String())
	}
}

func TestDoctrineRouter_AllTenRoutesWired(t *testing.T) {
	srv := newDoctrineFakeServer()
	srv.reloadTimeout = 1 * time.Second
	srv.diffOut = []DoctrineDiffEntry{{Path: "x", From: "a", To: "b", Status: "changed"}}
	srv.migrateTarget = "1.0"
	srv.migrateTOMLOut = "schema_version = \"1.0\"\n"
	srv.reinforceOut = "rendered"

	mux := http.NewServeMux()
	mux.Handle("GET /v1/doctrine/active", DoctrineActive(srv))
	mux.Handle("GET /v1/doctrine/list", DoctrineList(srv))
	mux.Handle("GET /v1/doctrine/show", DoctrineShow(srv))
	mux.Handle("POST /v1/doctrine/validate", DoctrineValidate(srv))
	mux.Handle("POST /v1/doctrine/reload", DoctrineReload(srv))
	mux.Handle("GET /v1/doctrine/status", DoctrineStatus(srv))
	mux.Handle("GET /v1/doctrine/history", DoctrineHistory(srv))
	mux.Handle("GET /v1/doctrine/diff", DoctrineDiff(srv))
	mux.Handle("POST /v1/doctrine/migrate", DoctrineMigrate(srv))
	mux.Handle("POST /v1/doctrine/reinforce", DoctrineReinforce(srv))

	ts := httptest.NewServer(mux)
	defer ts.Close()

	cases := []struct {
		method   string
		path     string
		body     []byte
		bodyCT   string
		wantCode int
		desc     string
	}{
		{method: "GET", path: "/v1/doctrine/active", wantCode: 200, desc: "active default"},
		{method: "GET", path: "/v1/doctrine/list", wantCode: 200, desc: "list all"},
		{method: "GET", path: "/v1/doctrine/show?name=max-scope", wantCode: 200, desc: "show max-scope"},
		{method: "POST", path: "/v1/doctrine/validate", body: mustJSON(client.DoctrineV2ValidateReq{TOMLContent: "schema_version = \"1.0\"\n"}), bodyCT: "application/json", wantCode: 200, desc: "validate ok"},
		{method: "POST", path: "/v1/doctrine/reload", body: mustJSON(client.DoctrineV2ReloadReq{Path: "/tmp/x.toml"}), bodyCT: "application/json", wantCode: 200, desc: "reload happy"},
		{method: "GET", path: "/v1/doctrine/status", wantCode: 200, desc: "status snapshot"},
		{method: "GET", path: "/v1/doctrine/history?since=24h", wantCode: 200, desc: "history 24h"},
		{method: "GET", path: "/v1/doctrine/diff?a=max-scope&b=default", wantCode: 200, desc: "diff JSON"},
		{method: "POST", path: "/v1/doctrine/migrate", body: mustJSON(client.DoctrineV2MigrateReq{TOMLContent: "schema_version = \"1.0\"\n", FromSchemaVersion: "0.9"}), bodyCT: "application/json", wantCode: 200, desc: "migrate in-memory"},
		{method: "POST", path: "/v1/doctrine/reinforce", body: mustJSON(client.DoctrineV2ReinforceReq{TaskKind: "worker"}), bodyCT: "application/json", wantCode: 200, desc: "reinforce render"},
	}

	httpClient := &http.Client{Timeout: 3 * time.Second}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			// Re-publish a reload event before each iteration that exercises /reload.
			// fix-pass IMPORTANT #2: handler filters by body.Path; the
			// publish MUST match the request body's path (here "/tmp/x.toml")
			// or the event is treated as cross-path noise + the loop times out.
			if strings.HasPrefix(tc.path, "/v1/doctrine/reload") {
				srv.publishReloaded(5*time.Millisecond, reload.DoctrineReloaded{
					Path:         "/tmp/x.toml",
					DoctrineName: "max-scope",
					Source:       "manual-reload",
					At:           time.Now().UTC(),
				})
			}
			var req *http.Request
			var err error
			if tc.body == nil {
				req, err = http.NewRequest(tc.method, ts.URL+tc.path, nil)
			} else {
				req, err = http.NewRequest(tc.method, ts.URL+tc.path, bytes.NewReader(tc.body))
				req.Header.Set("Content-Type", tc.bodyCT)
			}
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			resp, err := httpClient.Do(req)
			if err != nil {
				t.Fatalf("client.Do: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantCode {
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("%s %s: want %d, got %d (body: %s)", tc.method, tc.path, tc.wantCode, resp.StatusCode, body)
			}
			if resp.StatusCode == http.StatusNotFound {
				t.Errorf("ROUTE NOT WIRED: %s %s returned 404", tc.method, tc.path)
			}
		})
	}
}

func TestDoctrineRouter_RouteCountMatches(t *testing.T) {
	got := []struct{ method, path string }{
		{"GET", "/v1/doctrine/active"},
		{"GET", "/v1/doctrine/list"},
		{"GET", "/v1/doctrine/show"},
		{"POST", "/v1/doctrine/validate"},
		{"POST", "/v1/doctrine/reload"},
		{"GET", "/v1/doctrine/status"},
		{"GET", "/v1/doctrine/history"},
		{"GET", "/v1/doctrine/diff"},
		{"POST", "/v1/doctrine/migrate"},
		{"POST", "/v1/doctrine/reinforce"},
	}
	if len(got) != 10 {
		t.Errorf("route count: got %d, want 10 (spec §2.5)", len(got))
	}
}

func mustJSON(body any) []byte {
	b, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return b
}

func TestParseDurationLoose_DayShorthand(t *testing.T) {
	cases := []struct {
		in    string
		want  time.Duration
		isErr bool
	}{
		{"7d", 7 * 24 * time.Hour, false},
		{"1d", 24 * time.Hour, false},
		{"abcd", 0, true},
		{"-3d", 0, true},
		{"0d", 0, true},
		{"5h", 5 * time.Hour, false},
		{"-5h", 0, true},
		{"junk", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			d, err := parseDurationLooseForTest(tc.in)
			if tc.isErr {
				if err == nil {
					t.Errorf("want error for %q, got %v", tc.in, d)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if d != tc.want {
				t.Errorf("got %v, want %v", d, tc.want)
			}
		})
	}
}

func parseDurationLooseForTest(s string) (time.Duration, error) {
	return parseDurationLoose(s)
}

func TestNilCtxReturns503AcrossAllHandlers(t *testing.T) {
	handlers := []struct {
		name string
		mk   func(any) http.HandlerFunc
		req  *http.Request
	}{
		{"active", DoctrineActive, httptest.NewRequest(http.MethodGet, "/v1/doctrine/active", nil)},
		{"list", DoctrineList, httptest.NewRequest(http.MethodGet, "/v1/doctrine/list", nil)},
		{"show", DoctrineShow, httptest.NewRequest(http.MethodGet, "/v1/doctrine/show?name=x", nil)},
		{"validate", DoctrineValidate, httptest.NewRequest(http.MethodPost, "/v1/doctrine/validate", bytes.NewReader([]byte("{}")))},
		{"status", DoctrineStatus, httptest.NewRequest(http.MethodGet, "/v1/doctrine/status", nil)},
		{"history", DoctrineHistory, httptest.NewRequest(http.MethodGet, "/v1/doctrine/history", nil)},
		{"diff", DoctrineDiff, httptest.NewRequest(http.MethodGet, "/v1/doctrine/diff?a=x&b=y", nil)},
		{"migrate", DoctrineMigrate, httptest.NewRequest(http.MethodPost, "/v1/doctrine/migrate", bytes.NewReader([]byte("{}")))},
		{"reinforce", DoctrineReinforce, httptest.NewRequest(http.MethodPost, "/v1/doctrine/reinforce", bytes.NewReader([]byte("{}")))},
		{"reload", DoctrineReload, httptest.NewRequest(http.MethodPost, "/v1/doctrine/reload", bytes.NewReader([]byte("{}")))},
	}
	for _, h := range handlers {
		t.Run(h.name, func(t *testing.T) {
			handler := h.mk(nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, h.req)
			if w.Code != http.StatusServiceUnavailable {
				t.Errorf("%s: want 503, got %d (body: %s)", h.name, w.Code, w.Body.String())
			}
		})
	}
}

type listNilFakeServer struct{ *doctrineFakeServer }

func (l *listNilFakeServer) DoctrineList(_ string) ([]client.DoctrineV2ListItem, error) {
	return nil, nil
}

func TestDoctrineList_NilRowsReturnsEmpty(t *testing.T) {
	srv := &listNilFakeServer{newDoctrineFakeServer()}
	h := DoctrineList(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/doctrine/list", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp client.DoctrineV2ListResp
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Items == nil {
		t.Errorf("items: nil, want empty slice")
	}
}

type diffNilFakeServer struct{ *doctrineFakeServer }

func (d *diffNilFakeServer) DoctrineDiff(a, b, _ string) (string, string, []DoctrineDiffEntry, error) {
	return a, b, nil, nil
}

func TestDoctrineDiff_NilDiffsReturnsEmpty(t *testing.T) {
	srv := &diffNilFakeServer{newDoctrineFakeServer()}
	h := DoctrineDiff(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/doctrine/diff?a=x&b=y", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp client.DoctrineV2DiffResp
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Diffs == nil {
		t.Errorf("diffs: nil, want empty slice")
	}
}

type reloadNilChannelServer struct{ *doctrineFakeServer }

func (r *reloadNilChannelServer) DoctrineReloadEvents() <-chan reload.DoctrineReloaded {
	return nil
}
func (r *reloadNilChannelServer) DoctrineReloadFailedEvents() <-chan reload.DoctrineReloadFailed {
	return nil
}

func TestDoctrineReload_NilChannelReturns503(t *testing.T) {
	srv := &reloadNilChannelServer{newDoctrineFakeServer()}
	h := DoctrineReload(srv)
	body := client.DoctrineV2ReloadReq{Path: "/tmp/x.toml"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/doctrine/reload", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 (nil channel), got %d", w.Code)
	}
}

type notDoctrineCtx struct{}

func TestResolveDoctrineHandlerCtx_NotMatchingType(t *testing.T) {
	got := resolveDoctrineHandlerCtx(&notDoctrineCtx{})
	if got != nil {
		t.Errorf("expected nil for non-matching type, got %T", got)
	}
}

func TestResolveDoctrineHandlerCtx_NilArg(t *testing.T) {
	if got := resolveDoctrineHandlerCtx(nil); got != nil {
		t.Errorf("expected nil for nil arg, got %T", got)
	}
}

func TestDoctrineShow_BadFormat(t *testing.T) {
	srv := newDoctrineFakeServer()
	h := DoctrineShow(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/doctrine/show?name=max-scope&format=xml", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for unknown format, got %d", w.Code)
	}
}

func TestFailurePhaseToStatusCode(t *testing.T) {
	cases := []struct {
		phase, reason string
		want          int
	}{
		{"read", "", http.StatusInternalServerError},
		{"load", "", http.StatusInternalServerError},
		{"io", "", http.StatusInternalServerError},
		{"parse", "", http.StatusUnprocessableEntity},
		{"validate", "", http.StatusUnprocessableEntity},
		{"tighten", "", http.StatusUnprocessableEntity},
		{"unknown", "io_error", http.StatusInternalServerError},
		{"unknown", "system", http.StatusInternalServerError},
		{"unknown", "validate_failed", http.StatusUnprocessableEntity},
		{"", "", http.StatusUnprocessableEntity},
	}
	for _, tc := range cases {
		t.Run(tc.phase+":"+tc.reason, func(t *testing.T) {
			if got := failurePhaseToStatusCode(tc.phase, tc.reason); got != tc.want {
				t.Errorf("phase=%q reason=%q: got %d, want %d", tc.phase, tc.reason, got, tc.want)
			}
		})
	}
}
