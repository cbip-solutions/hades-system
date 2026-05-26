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

func invokeStateCmd(t *testing.T, args []string, baseURL string, stdin string) (string, string, error) {
	t.Helper()
	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(uds string) *client.Client { return client.NewWithBaseURL(baseURL) }
	t.Cleanup(func() { TestOnlyClientFactory = prev })
	cmd := NewStateCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	if stdin != "" {
		cmd.SetIn(strings.NewReader(stdin))
	}
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func mockStateServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/state/show", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.StateManifest{
			LastRegenerateUnix: 1762000000,
			ManualFieldCount:   1,
			MissingSourceCount: 0,
			TomlContent: `[meta]
schema_version = "23"

[fields]
substrate_min_version = "0.7.1"
`,
		})
	})

	mux.HandleFunc("/v1/state/regenerate", func(w http.ResponseWriter, r *http.Request) {
		var req client.StateRegenerateReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.DryRun {
			_ = json.NewEncoder(w).Encode(client.StateRegenerateResp{
				DryRun:        true,
				ChangedFields: []string{"schema_version"},
				Diff:          "+ schema_version: 24\n- schema_version: 23",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(client.StateRegenerateResp{
			DryRun:        false,
			ChangedFields: []string{},
		})
	})

	mux.HandleFunc("/v1/state/verify", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.StateDiff{Match: true})
	})

	mux.HandleFunc("/v1/state/pin", func(w http.ResponseWriter, r *http.Request) {
		var req client.StatePinReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/v1/state/history", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.StateChange{
				{
					Field:    "substrate_min_version",
					OldValue: "0.7.0",
					NewValue: "0.7.1",
					Reason:   "CVE-2026-X",
					At:       1762000000,
				},
			},
			"count": 1,
		})
	})

	return httptest.NewServer(mux)
}

func TestState_RegistersAllSubcommands(t *testing.T) {
	cmd := NewStateCmd()
	want := []string{"show", "regenerate", "verify", "pin", "history"}
	have := map[string]bool{}
	for _, c := range cmd.Commands() {
		have[c.Name()] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("missing subcommand %q", w)
		}
	}
}

func TestStateShow_HappyPath(t *testing.T) {
	srv := mockStateServer(t)
	defer srv.Close()
	stdout, _, err := invokeStateCmd(t, []string{"show"}, srv.URL, "")
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	for _, want := range []string{"schema_version", "23", "substrate_min_version", "0.7.1"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in output: %s", want, stdout)
		}
	}
}

func TestStateRegenerate_DryRun(t *testing.T) {
	srv := mockStateServer(t)
	defer srv.Close()
	stdout, _, err := invokeStateCmd(t, []string{"regenerate", "--dry-run"}, srv.URL, "")
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "+ schema_version") || !strings.Contains(stdout, "- schema_version") {
		t.Errorf("expected diff output: %s", stdout)
	}
}

func TestStateRegenerate_HappyPath(t *testing.T) {
	srv := mockStateServer(t)
	defer srv.Close()
	_, _, err := invokeStateCmd(t, []string{"regenerate"}, srv.URL, "")
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
}

func TestStateVerify_NoDrift(t *testing.T) {
	srv := mockStateServer(t)
	defer srv.Close()
	stdout, _, err := invokeStateCmd(t, []string{"verify"}, srv.URL, "")
	if err != nil {
		t.Fatalf("expected no-drift exit 0; got %v", err)
	}
	if !strings.Contains(strings.ToLower(stdout), "no drift") && !strings.Contains(strings.ToLower(stdout), "ok") {
		t.Errorf("expected no-drift message: %s", stdout)
	}
}

func TestStateVerify_DriftReturnsError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/state/verify", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.StateDiff{
			Match: false,
			Diff:  "+ x: 1\n- x: 0",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(uds string) *client.Client { return client.NewWithBaseURL(srv.URL) }
	t.Cleanup(func() { TestOnlyClientFactory = prev })
	cmd := NewStateCmd()
	cmd.SetArgs([]string{"verify"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected drift to surface as exit non-zero (CI gate)")
	}
}

func TestStatePin_RequiresReason(t *testing.T) {
	srv := mockStateServer(t)
	defer srv.Close()
	_, _, err := invokeStateCmd(t, []string{"pin", "substrate_min_version", "0.7.1"}, srv.URL, "")
	if err == nil {
		t.Fatal("expected --reason required (inv-zen-146)")
	}
}

func TestStatePin_HappyPath(t *testing.T) {
	srv := mockStateServer(t)
	defer srv.Close()
	stdout, _, err := invokeStateCmd(t,
		[]string{"pin", "substrate_min_version", "0.7.1", "--reason", "OpenClaude 0.7.0 has CVE-2026-X"},
		srv.URL, "y\n")
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "substrate_min_version") {
		t.Errorf("expected field name in output: %s", stdout)
	}
}

func TestStateHistory_HappyPath(t *testing.T) {
	srv := mockStateServer(t)
	defer srv.Close()
	stdout, _, err := invokeStateCmd(t, []string{"history"}, srv.URL, "")
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	for _, want := range []string{"substrate_min_version", "0.7.0", "0.7.1", "CVE-2026-X"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in output: %s", want, stdout)
		}
	}
}
