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

func invokeBudgetCmd(t *testing.T, args []string, baseURL string) (string, string, error) {
	t.Helper()
	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(uds string) *client.Client {
		return client.NewWithBaseURL(baseURL)
	}
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	cmd := NewBudgetCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func mockBudgetServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/budget/cap_status", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.BudgetCapStatus{RemainingUSD: 24.5, Blocked: false})
	})
	mux.HandleFunc("/v1/budget/record", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]bool{"recorded": true})
	})
	mux.HandleFunc("/v1/budget/axes", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]client.BudgetAxisTag{
			{AxisName: "project", AxisValue: "internal-platform-x"},
			{AxisName: "stage", AxisValue: "design"},
		})
	})
	mux.HandleFunc("/v1/budget/anomaly", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.BudgetAnomaly{ZScore: 5.7, Mean: 1.0, StdDev: 0.5, Samples: 100})
	})
	mux.HandleFunc("/v1/budget/events", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"events": []client.BudgetEvent{
				{ID: "e1", Scope: "stage", Value: "design", EventType: "cap_set", AmountUSD: 5.0, OccurredAt: 1759320000},
				{ID: "e2", Scope: "project", Value: "internal-platform-x", EventType: "cost", AmountUSD: 2.5, OccurredAt: 1759320500},
			},
			"count": 2,
		})
	})
	mux.HandleFunc("/v1/budget/pause", func(w http.ResponseWriter, r *http.Request) {
		var body client.BudgetPauseReq
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(client.BudgetPauseResp{State: "paused", Scope: body.Scope, Value: body.Value})
	})
	mux.HandleFunc("/v1/budget/resume", func(w http.ResponseWriter, r *http.Request) {
		var body client.BudgetResumeReq
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(client.BudgetPauseResp{State: "running", Scope: body.Scope, Value: body.Value})
	})
	return httptest.NewServer(mux)
}

func TestBudgetCapStatus_RequiresFlags(t *testing.T) {
	srv := mockBudgetServer(t)
	defer srv.Close()
	_, _, err := invokeBudgetCmd(t, []string{"cap-status"}, srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBudgetCapStatus_HappyPath(t *testing.T) {
	srv := mockBudgetServer(t)
	defer srv.Close()
	stdout, _, err := invokeBudgetCmd(t, []string{"cap-status", "--axis=stage", "--value=design"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "RemainingUSD") || !strings.Contains(stdout, "24.5") {
		t.Errorf("got %s", stdout)
	}
}

func TestBudgetRecord(t *testing.T) {
	srv := mockBudgetServer(t)
	defer srv.Close()
	stdout, _, err := invokeBudgetCmd(t, []string{"record",
		"--cost-id=c1", "--amount-usd=0.5", "--tag=project=internal-platform-x", "--tag=stage=design",
	}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "recorded") || !strings.Contains(stdout, "internal-platform-x") {
		t.Errorf("got %s", stdout)
	}
}

func TestBudgetRecord_RequiresCostID(t *testing.T) {
	srv := mockBudgetServer(t)
	defer srv.Close()
	_, _, err := invokeBudgetCmd(t, []string{"record"}, srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBudgetRecord_BadTag(t *testing.T) {
	srv := mockBudgetServer(t)
	defer srv.Close()
	_, _, err := invokeBudgetCmd(t, []string{"record", "--cost-id=c1", "--tag=bogus"}, srv.URL)
	if err == nil {
		t.Fatal("expected tag format error")
	}
}

func TestBudgetAxes(t *testing.T) {
	srv := mockBudgetServer(t)
	defer srv.Close()
	stdout, _, err := invokeBudgetCmd(t, []string{"axes", "--cost-id=c1"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "internal-platform-x") || !strings.Contains(stdout, "design") {
		t.Errorf("got %s", stdout)
	}
}

func TestBudgetAxes_RequiresCostID(t *testing.T) {
	srv := mockBudgetServer(t)
	defer srv.Close()
	_, _, err := invokeBudgetCmd(t, []string{"axes"}, srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBudgetAnomaly(t *testing.T) {
	srv := mockBudgetServer(t)
	defer srv.Close()
	stdout, _, err := invokeBudgetCmd(t, []string{"anomaly", "--scope=project", "--value=internal-platform-x", "--window=1h"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "5.7") {
		t.Errorf("got %s", stdout)
	}
}

func TestBudgetAnomaly_BadWindow(t *testing.T) {
	srv := mockBudgetServer(t)
	defer srv.Close()
	_, _, err := invokeBudgetCmd(t, []string{"anomaly", "--scope=p", "--value=v", "--window=xyz"}, srv.URL)
	if err == nil {
		t.Fatal("expected window error")
	}
}

func TestBudgetEvents(t *testing.T) {
	srv := mockBudgetServer(t)
	defer srv.Close()
	stdout, _, err := invokeBudgetCmd(t, []string{"events", "--limit=10"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "cap_set") || !strings.Contains(stdout, "design") {
		t.Errorf("got %s", stdout)
	}
}

func TestBudgetPause_RequiresFlags(t *testing.T) {
	srv := mockBudgetServer(t)
	defer srv.Close()
	_, _, err := invokeBudgetCmd(t, []string{"pause"}, srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBudgetPause_RequiresYes(t *testing.T) {
	srv := mockBudgetServer(t)
	defer srv.Close()
	_, _, err := invokeBudgetCmd(t, []string{"pause", "--scope=stage", "--value=design"}, srv.URL)
	if err == nil {
		t.Fatal("expected --yes error")
	}
}

func TestBudgetPause_HappyPath(t *testing.T) {
	srv := mockBudgetServer(t)
	defer srv.Close()
	stdout, _, err := invokeBudgetCmd(t, []string{"pause", "--scope=stage", "--value=design", "--reason=test", "--yes"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "paused") {
		t.Errorf("got %s", stdout)
	}
}

func TestBudgetResume_HappyPath(t *testing.T) {
	srv := mockBudgetServer(t)
	defer srv.Close()
	stdout, _, err := invokeBudgetCmd(t, []string{"resume", "--scope=stage", "--value=design", "--yes"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "running") {
		t.Errorf("got %s", stdout)
	}
}

func TestBudgetPauseModes(t *testing.T) {
	srv := mockBudgetServer(t)
	defer srv.Close()
	stdout, _, err := invokeBudgetCmd(t, []string{"pause-modes"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}

	for _, want := range []string{"descriptive", "quiet", "fail_loud"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing canonical pause-mode %q in %s", want, stdout)
		}
	}

	for _, bad := range []string{"paused_descriptive", "paused_quiet", "paused_after_apply"} {
		if strings.Contains(stdout, bad) {
			t.Errorf("legacy pause-mode %q must not appear in CLI output: %s", bad, stdout)
		}
	}
}

func TestBudgetRollup(t *testing.T) {
	srv := mockBudgetServer(t)
	defer srv.Close()
	stdout, _, err := invokeBudgetCmd(t, []string{"rollup"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "Total:") || !strings.Contains(stdout, "7.5") {
		t.Errorf("got %s", stdout)
	}
}

func TestBudgetCapStatus_JSON(t *testing.T) {
	srv := mockBudgetServer(t)
	defer srv.Close()
	stdout, _, err := invokeBudgetCmd(t, []string{
		"cap-status", "--axis=stage", "--value=design", "--json",
	}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(stdout), &arr); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
}

func TestBudgetAnomaly_JSON(t *testing.T) {
	srv := mockBudgetServer(t)
	defer srv.Close()
	stdout, _, err := invokeBudgetCmd(t, []string{
		"anomaly", "--scope=p", "--value=v", "--window=1h", "--json",
	}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "5.7") {
		t.Errorf("got %s", stdout)
	}
}

func TestBudgetEvents_BadSince(t *testing.T) {
	srv := mockBudgetServer(t)
	defer srv.Close()
	_, _, err := invokeBudgetCmd(t, []string{"events", "--since=not-a-duration"}, srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBudgetEvents_ExclusiveFlags(t *testing.T) {
	srv := mockBudgetServer(t)
	defer srv.Close()
	_, _, err := invokeBudgetCmd(t, []string{"events", "--quiet", "--verbose"}, srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBudgetEvents_WithSince(t *testing.T) {
	srv := mockBudgetServer(t)
	defer srv.Close()
	_, _, err := invokeBudgetCmd(t, []string{"events", "--since=24h", "--limit=10"}, srv.URL)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
}

func TestBudgetResume_RequiresYes(t *testing.T) {
	srv := mockBudgetServer(t)
	defer srv.Close()
	_, _, err := invokeBudgetCmd(t, []string{"resume", "--scope=stage", "--value=design"}, srv.URL)
	if err == nil {
		t.Fatal("expected --yes error")
	}
}

func TestBudgetResume_RequiresFlags(t *testing.T) {
	srv := mockBudgetServer(t)
	defer srv.Close()
	_, _, err := invokeBudgetCmd(t, []string{"resume", "--yes"}, srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestBudgetPauseModes_ShortClarifiesNotAFlag covers review M-6: the
// pause-modes Short text MUST clarify that pause_mode is set in the
// doctrine TOML, not as a flag on `zen budget pause`. Pre-fix the
// Short read "Doctrine-resolved pause-mode catalog" which operators
// misread as flag values.
func TestBudgetPauseModes_ShortClarifiesNotAFlag(t *testing.T) {
	root := NewBudgetCmd()
	for _, c := range root.Commands() {
		if c.Name() == "pause-modes" {
			if !strings.Contains(c.Short, "zenswarm.toml") {
				t.Errorf("Short text should mention zenswarm.toml: %q", c.Short)
			}
			if !strings.Contains(c.Long, "NOT command-line flags") &&
				!strings.Contains(c.Long, "not a flag") {
				t.Errorf("Long text should clarify not-a-flag: %q", c.Long)
			}
			return
		}
	}
	t.Fatal("pause-modes subcommand not found")
}

func TestBudgetPauseModes_ExclusiveFlags(t *testing.T) {
	srv := mockBudgetServer(t)
	defer srv.Close()
	_, _, err := invokeBudgetCmd(t, []string{"pause-modes", "--quiet", "--verbose"}, srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBudgetRollup_BadSince(t *testing.T) {
	srv := mockBudgetServer(t)
	defer srv.Close()
	_, _, err := invokeBudgetCmd(t, []string{"rollup", "--since=not-a-duration"}, srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBudgetRollup_JSON(t *testing.T) {
	srv := mockBudgetServer(t)
	defer srv.Close()
	stdout, _, err := invokeBudgetCmd(t, []string{"rollup", "--json"}, srv.URL)
	if err != nil {
		t.Fatalf("rollup: %v", err)
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(stdout), &arr); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
}

func TestBudgetSubcommandsRegistered(t *testing.T) {
	root := NewBudgetCmd()
	want := []string{"cap-status", "record", "axes", "anomaly", "events", "pause", "resume", "pause-modes", "rollup"}
	have := map[string]bool{}
	for _, c := range root.Commands() {
		have[c.Name()] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("missing subcommand: budget %s", w)
		}
	}
	if len(want) < 9 {
		t.Errorf("expected ≥9 leaves, got %d", len(want))
	}
}
