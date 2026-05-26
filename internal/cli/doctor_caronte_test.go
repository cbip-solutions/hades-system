package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
)

// startFakeCaronteProbeServer returns an httptest.Server that answers
// the v0.20.0 Phase E `mcp_zen-swarm_caronte_get_health` JSON-RPC
// tools/call envelope with a non-degraded HealthReport (inv-zen-275).
// All synthesized probe rows derived from this report MUST report
// "ok" status (engine non-degraded, languages populated, recent
// LastIndexed, NodeCount > 0).
func startFakeCaronteProbeServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcpgateway" {
			http.Error(w, "fake server expects /v1/mcpgateway; got "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		nowFresh := time.Now().Add(-1 * time.Hour).Unix()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"ProjectID":    "fake-project-aa11",
				"NodeCount":    1234,
				"EdgeCount":    5678,
				"PackageCount": 42,
				"CyclicSCCs":   0,
				"Languages":    []string{"go", "ts"},
				"Degraded":     false,
				"ResolveMode":  "vta",
				"LastIndexed":  nowFresh,
			},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newFakeCaronteProbeClient(t *testing.T) *client.Client {
	t.Helper()
	srv := startFakeCaronteProbeServer(t)
	return client.NewWithBaseURL(srv.URL)
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestRunCaronteChecks_AutoResolvesProjectFromCwd asserts inv-zen-281
// (v0.20.1 fix #1): when neither --project flag nor ZEN_PROJECT_ID env
// is set, runCaronteChecks calls /v1/projects/doctor with the current
// cwd and uses the resolved alias as the X-Zen-Project-ID header on
// every subsequent /v1/mcpgateway probe call.
//
// Why before v0.20.1 each `zen doctor caronte` probe sent an empty
// header against a registered project, causing `project_id required`
// daemon responses → spurious aggregate failures. The fix is to
// resolve cwd→alias once at the start of the section. Failure of the
// resolve is silently absorbed (fallback to empty header / daemon-
// default-project) so the no-cwd-project case stays graceful.
//
// Sister-test bite check: stub `resolveCaronteAliasViaCwd` to return
// "" unconditionally; this test MUST fail (the gateway then sees no
// header).
func TestRunCaronteChecks_AutoResolvesProjectFromCwd(t *testing.T) {

	var gatewayHeaders []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/projects/doctor":

			_ = json.NewEncoder(w).Encode(map[string]any{
				"healthy":        true,
				"alias":          "zen-cwd-resolved-aa11",
				"id_sha256":      "aa11" + strings.Repeat("0", 60),
				"canonical_path": "/tmp/zen-test-cwd",
				"path_history":   []any{},
			})
		case "/v1/mcpgateway":
			gatewayHeaders = append(gatewayHeaders, r.Header.Get("X-Zen-Project-ID"))
			nowFresh := time.Now().Add(-1 * time.Hour).Unix()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result": map[string]any{
					"ProjectID":    "zen-cwd-resolved-aa11",
					"NodeCount":    100,
					"EdgeCount":    50,
					"PackageCount": 5,
					"CyclicSCCs":   0,
					"Languages":    []string{"go"},
					"Degraded":     false,
					"ResolveMode":  "vta",
					"LastIndexed":  nowFresh,
				},
			})
		default:
			http.Error(w, "unknown path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c := client.NewWithBaseURL(srv.URL)

	t.Setenv("ZEN_PROJECT_ID", "")

	results := runCaronteChecks(context.Background(), c)
	if len(results) == 0 {
		t.Fatal("runCaronteChecks returned no rows")
	}

	if len(gatewayHeaders) != 5 {
		t.Errorf("expected 5 gateway probe calls, saw %d", len(gatewayHeaders))
	}
	for i, h := range gatewayHeaders {
		if h != "zen-cwd-resolved-aa11" {
			t.Errorf("gateway call #%d had X-Zen-Project-ID = %q, want %q (auto-resolve from cwd)",
				i, h, "zen-cwd-resolved-aa11")
		}
	}
}

// TestRunCaronteChecks_GracefulFallbackOnCwdResolveFailure asserts the
// graceful-degrade half of inv-zen-281: when /v1/projects/doctor errors
// (project not registered for this cwd; daemon transport failure), the
// auto-resolve falls back to empty alias and the probe section still
// runs (against daemon-default-project) instead of bubbling the resolve
// failure up to the operator.
//
// Sister-test bite check: change the fallback to "return error" instead
// of "return empty"; this test MUST fail because the section would no
// longer emit probe rows.
func TestRunCaronteChecks_GracefulFallbackOnCwdResolveFailure(t *testing.T) {
	var gatewayHeaders []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/projects/doctor":

			http.Error(w, "project not found for cwd", http.StatusNotFound)
		case "/v1/mcpgateway":
			gatewayHeaders = append(gatewayHeaders, r.Header.Get("X-Zen-Project-ID"))
			nowFresh := time.Now().Add(-1 * time.Hour).Unix()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result": map[string]any{
					"ProjectID":    "daemon-default",
					"NodeCount":    1,
					"PackageCount": 1,
					"Languages":    []string{"go"},
					"Degraded":     false,
					"ResolveMode":  "vta",
					"LastIndexed":  nowFresh,
				},
			})
		default:
			http.Error(w, "unknown path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c := client.NewWithBaseURL(srv.URL)

	t.Setenv("ZEN_PROJECT_ID", "")

	results := runCaronteChecks(context.Background(), c)
	if len(results) != 5 {
		t.Errorf("expected 5 probe rows even on cwd-resolve failure, got %d", len(results))
	}
	// Every gateway call MUST carry an empty X-Zen-Project-ID header
	// (graceful fallback to daemon-default-project).
	for i, h := range gatewayHeaders {
		if h != "" {
			t.Errorf("gateway call #%d had X-Zen-Project-ID = %q on cwd-resolve failure, want empty (graceful fallback)",
				i, h)
		}
	}
}

func TestCaronteChecksNamesAndNoLicenseProbe(t *testing.T) {
	c := newFakeCaronteProbeClient(t)
	results := runCaronteChecks(context.Background(), c)
	names := map[string]bool{}
	for _, r := range results {
		names[r.Name] = true
		if r.Name == "gitnexus.license.commercial-use-detected" || r.Name == "caronte.license.commercial-use-detected" {
			t.Errorf("doctor still has a license-disclosure probe %q; Plan 19 sovereignty drops it", r.Name)
		}
	}
	for _, want := range []string{
		"caronte.engine.healthy",
		"caronte.index.freshness",
		"caronte.language.coverage",
		"caronte.project-db.status",
		"caronte.rerank.available",
	} {
		if !names[want] {
			t.Errorf("runCaronteChecks missing %q; got %v", want, keys(names))
		}
	}
}

// TestRunCaronteChecks_IncludesRerankAvailable asserts the Phase F
// addition: the probes slice MUST include a "rerank.available" entry
// whose resultName is "caronte.rerank.available" and whose hint points
// at scripts/download-bge-model.sh. inv-zen-278.
func TestRunCaronteChecks_IncludesRerankAvailable(t *testing.T) {
	c := newFakeCaronteProbeClient(t)
	results := runCaronteChecks(context.Background(), c)
	for _, r := range results {
		if r.Name == "caronte.rerank.available" {

			return
		}
	}
	t.Fatalf("runCaronteChecks did not emit a caronte.rerank.available entry; got %d results", len(results))
}

// TestCaronteResultFrom_ErrorPath asserts the err != nil branch of
// caronteResultFrom: when the probe call fails with a transport error,
// the result MUST have Status=="fail", Detail==err.Error(), and Hint
// set to the supplied hint string. Coverage for the err-branch closes
// a gap surfaced when Phase F's new probe entries exercised the
// happy-path branches.
func TestCaronteResultFrom_ErrorPath(t *testing.T) {
	someErr := errors.New("connection refused")
	got := caronteResultFrom("caronte.engine.healthy", nil, someErr, "test hint")
	if got.Status != "fail" {
		t.Errorf("status = %q, want fail", got.Status)
	}
	if got.Detail != "connection refused" {
		t.Errorf("detail = %q, want %q", got.Detail, "connection refused")
	}
	if got.Hint != "test hint" {
		t.Errorf("hint = %q, want %q", got.Hint, "test hint")
	}
	if got.Name != "caronte.engine.healthy" {
		t.Errorf("name = %q, want %q", got.Name, "caronte.engine.healthy")
	}
}

// TestRerankAvailableProbe_HintReferencesDownloadScript asserts the
// downgrade-path detail: when the synthesized rerank.available row
// reports non-ok status, the hint MUST reference
// scripts/download-bge-model.sh so operators know the install path
// (inv-zen-278 + inv-zen-275 v0.20.0 Phase E migration).
//
// Under the v0.20.0 synthesis, rerank.available derives from
// HealthReport.Degraded: a degraded engine → "warn" rerank row (the
// finer empirical BGE-presence probe lives at the daemon side per
// internal/daemon/handlers/caronte_probe.go).
func TestRerankAvailableProbe_HintReferencesDownloadScript(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcpgateway" {
			http.Error(w, "wrong path", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")

		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"ProjectID":   "fake-project-aa11",
				"Degraded":    true,
				"ResolveMode": "stale_snapshot",
			},
		})
	}))
	t.Cleanup(srv.Close)
	c := client.NewWithBaseURL(srv.URL)
	results := runCaronteChecks(context.Background(), c)
	found := false
	for _, r := range results {
		if r.Name != "caronte.rerank.available" {
			continue
		}
		found = true
		if r.Status != "warn" {
			t.Errorf("rerank.available status = %q, want %q", r.Status, "warn")
		}
		if !strings.Contains(r.Hint, "scripts/download-bge-model.sh") {
			t.Errorf("rerank.available hint = %q, want it to reference scripts/download-bge-model.sh", r.Hint)
		}
	}
	if !found {
		t.Fatal("runCaronteChecks did not emit caronte.rerank.available under warn status")
	}
}
