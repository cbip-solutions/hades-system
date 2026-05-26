package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/client"
)

func newFakeDoctorBackend(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	probeOK := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
			"detail": "fake-ok",
		})
	}

	mux.HandleFunc("/v1/hermes/probe", probeOK)
	mux.HandleFunc("/v1/augment/probe", probeOK)
	mux.HandleFunc("/v1/citation/probe", probeOK)
	mux.HandleFunc("/v1/coordination/probe", probeOK)
	mux.HandleFunc("/v1/bypass/doctor", probeOK)
	mux.HandleFunc("/v1/mcpgateway", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		nowFresh := time.Now().Add(-1 * time.Hour).Unix()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"ProjectID":    "fake-doctor-aa11",
				"NodeCount":    100,
				"EdgeCount":    300,
				"PackageCount": 12,
				"Languages":    []string{"go"},
				"Degraded":     false,
				"ResolveMode":  "vta",
				"LastIndexed":  nowFresh,
			},
		})
	})

	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version": "test", "uptime_seconds": 1,
		})
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[]}`))
	})

	return httptest.NewServer(mux)
}

func TestDoctorPlan11FullOutputContainsAllSections(t *testing.T) {
	srv := newFakeDoctorBackend(t)
	defer srv.Close()

	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(_ string) *client.Client { return client.NewWithBaseURL(srv.URL) }
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	root := NewRootCmd()
	root.SetArgs([]string{"doctor"})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = root.ExecuteContext(ctx)

	out := buf.String()

	wantSections := []string{
		"Hermes integration (Plan 11)",
		"Augmentation (Plan 11)",
		"Citation system (Plan 11)",
		"Coordination (Plan 11)",
		"Caronte",
	}
	for _, s := range wantSections {
		if !strings.Contains(out, s) {
			t.Errorf("expected section header %q not in output\nfull output:\n%s", s, out)
		}
	}

	wantChecks := []string{

		"hermes.installed",
		"hermes.plugin-zen-swarm-loaded",
		"hermes.config.mcp_servers.zen-swarm-reachable",
		"hermes.curator.last-run",

		"augment.endpoint-reachable",
		"augment.budget.headroom",
		"augment.cache.hit-rate",
		"augment.latency.p50-p99",
		"augment.5-lane-rrf.healthy",
		"augment.privacy-filter.tested",

		"citation.envelope.serialize-roundtrip",
		"citation.renderers",
		"citation.audit-chain.zen://audit-handler-functional",

		"plan-9-d.aggregator.db-substrate-available",

		"caronte.engine.healthy",
		"caronte.index.freshness",
		"caronte.language.coverage",
		"caronte.project-db.status",
	}
	for _, c := range wantChecks {
		if !strings.Contains(out, c) {
			t.Errorf("expected check %q not in output", c)
		}
	}
}

func TestDoctorPlan11JSONFormatStructured(t *testing.T) {
	srv := newFakeDoctorBackend(t)
	defer srv.Close()

	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(_ string) *client.Client { return client.NewWithBaseURL(srv.URL) }
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	root := NewRootCmd()
	root.SetArgs([]string{"doctor", "--format", "json"})
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = root.ExecuteContext(ctx)

	var rows []CheckResult
	if err := json.Unmarshal(stdout.Bytes(), &rows); err != nil {
		t.Fatalf("doctor --format json must emit valid JSON []CheckResult: %v\nfull stdout:\n%s\nstderr:\n%s",
			err, stdout.String(), stderr.String())
	}
	wantSections := map[string]bool{
		"Hermes integration (Plan 11)": true,
		"Augmentation (Plan 11)":       true,
		"Citation system (Plan 11)":    true,
		"Coordination (Plan 11)":       true,
	}
	gotSections := map[string]bool{}
	for _, r := range rows {
		gotSections[r.Section] = true
	}
	for s := range wantSections {
		if !gotSections[s] {
			t.Errorf("JSON output missing section %q", s)
		}
	}
}

func TestRootCmdRegistersPlan11Commands(t *testing.T) {
	t.Parallel()
	root := NewRootCmd()
	wantTopLevel := []string{"codegraph", "impact", "context", "wiki"}
	for _, sub := range wantTopLevel {
		_, _, err := root.Find([]string{sub})
		if err != nil {
			t.Errorf("zen %s: not registered: %v", sub, err)
		}
	}

	if _, _, err := root.Find([]string{"daemon", "restart-mcp"}); err != nil {
		t.Errorf("zen daemon restart-mcp: not registered: %v", err)
	}

	if _, _, err := root.Find([]string{"audit", "event"}); err != nil {
		t.Errorf("zen audit event: not registered: %v", err)
	}

	for _, sub := range []string{"promote", "sync", "restore"} {
		if _, _, err := root.Find([]string{"knowledge", sub}); err != nil {
			t.Errorf("zen knowledge %s: not registered: %v", sub, err)
		}
	}
}

var _ = cobra.Command{}
