package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func newDoctorMergeTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/merge/cache/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"size":7,"hit_rate_pct":42.0,"last_rebuilt":"2026-05-05T00:00:00Z"}`))
	})

	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","version":"test","uptime_seconds":42}`))
	})
	mux.HandleFunc("/v1/bypass/doctor", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok","detail":"mock"}`))
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})
	return httptest.NewServer(mux)
}

// TestDoctor_AggregateIncludesMergeSection — `zen doctor --json` output
// MUST contain entries with Section="Merge" for all 4 checks.
// This is the load-bearing wiring assertion: pre-C-2 the section was
// absent because runMergeChecks was never called by doctorAggregateRunE.
func TestDoctor_AggregateIncludesMergeSection(t *testing.T) {
	srv := newDoctorMergeTestServer(t)
	defer srv.Close()

	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(uds string) *client.Client {
		return client.NewWithBaseURL(srv.URL)
	}
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	cmd := NewDoctorCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{"--json"})
	_ = cmd.Execute()

	out := stdout.String()
	var arr []map[string]any
	if err := json.Unmarshal([]byte(out), &arr); err != nil {
		t.Fatalf("--json output not valid JSON: %v\n%s", err, out)
	}
	mergeChecks := []map[string]any{}
	for _, m := range arr {
		if s, ok := m["section"].(string); ok && s == "Merge (Plan 6)" {
			mergeChecks = append(mergeChecks, m)
		}
	}
	if len(mergeChecks) != 4 {
		t.Fatalf("expected 4 Merge (Plan 6) checks; got %d in:\n%s", len(mergeChecks), out)
	}
	wantNames := map[string]bool{
		"merge.daemon_up":         false,
		"merge.git_version":       false,
		"merge.eventlog_writable": false,
		"merge.cache_health":      false,
	}
	for _, m := range mergeChecks {
		name, _ := m["name"].(string)
		if _, ok := wantNames[name]; ok {
			wantNames[name] = true
		}
	}
	for name, seen := range wantNames {
		if !seen {
			t.Errorf("Merge (Plan 6) section missing check %q in:\n%s", name, out)
		}
	}
}

func TestDoctor_HasMergeSubcommand(t *testing.T) {
	root := NewDoctorCmd()
	have := false
	for _, c := range root.Commands() {
		if c.Name() == "merge" {
			have = true
			break
		}
	}
	if !have {
		t.Error("missing subcommand: doctor merge")
	}
}

func TestDoctor_MergeSubcommand_Renders(t *testing.T) {
	srv := newDoctorMergeTestServer(t)
	defer srv.Close()

	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(uds string) *client.Client {
		return client.NewWithBaseURL(srv.URL)
	}
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	cmd := NewDoctorCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{"merge"})
	_ = cmd.Execute()

	out := stdout.String()
	for _, want := range []string{
		"merge.daemon_up",
		"merge.git_version",
		"merge.eventlog_writable",
		"merge.cache_health",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in `zen doctor merge` output:\n%s", want, out)
		}
	}
}
