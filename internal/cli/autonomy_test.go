package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

type autonomyRec struct {
	modeReq              client.AutonomyModeRequest
	lockedByCapaFirewall bool
}

func newFakeAutonomyDaemon(t *testing.T, show client.AutonomyShow, check client.AutonomyCheckResult) (*httptest.Server, *autonomyRec) {
	t.Helper()
	rec := &autonomyRec{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/autonomy/show", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONP5(w, show)
	})
	mux.HandleFunc("/v1/autonomy/check", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONP5(w, check)
	})
	mux.HandleFunc("/v1/autonomy/mode", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&rec.modeReq)
		if rec.lockedByCapaFirewall {
			http.Error(w, "capa-firewall: mode override forbidden", http.StatusForbidden)
			return
		}
		writeJSONP5(w, map[string]string{"status": "ok"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, rec
}

func runAutonomySubcommand(t *testing.T, srvURL string, args ...string) (string, error) {
	t.Helper()
	root := NewAutonomyCmd()
	if err := root.PersistentFlags().Set(plan5DaemonURLFlag, srvURL); err != nil {
		t.Fatalf("set %s: %v", plan5DaemonURLFlag, err)
	}
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return buf.String(), err
}

func TestAutonomyShow_PrintsEffectiveMode(t *testing.T) {
	srv, _ := newFakeAutonomyDaemon(t, client.AutonomyShow{
		EffectiveMode:    "semi",
		ResolvedFrom:     "doctrine",
		DoctrineMode:     "max-scope",
		ZenswarmTOMLMode: "semi",
		FlagMode:         "",
		CapaFirewallLock: false,
		CostDegradation: client.CostTierStatus{
			CurrentTier: "none", BudgetPct: 32.0,
		},
	}, client.AutonomyCheckResult{})

	out, err := runAutonomySubcommand(t, srv.URL, "show")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"semi", "doctrine", "max-scope", "32.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestAutonomyCheck_HardFailExitsNonZero(t *testing.T) {
	srv, _ := newFakeAutonomyDaemon(t, client.AutonomyShow{}, client.AutonomyCheckResult{
		OverallPass: false, HardFailed: 1,
		Rows: []client.AutonomyCheckRow{
			{Name: "research_mcp_up", Tier: "hard", Pass: false, Detail: "MCP unreachable"},
		},
	})
	_, err := runAutonomySubcommand(t, srv.URL, "--check")
	if err == nil {
		t.Fatal("expected non-nil error on hard fail")
	}
	if !strings.Contains(err.Error(), "hard tier") {
		t.Errorf("error should mention hard tier: %v", err)
	}
}

func TestAutonomyCheck_SoftFailWithAllow(t *testing.T) {
	srv, _ := newFakeAutonomyDaemon(t, client.AutonomyShow{}, client.AutonomyCheckResult{
		OverallPass: false, SoftFailed: 1,
		Rows: []client.AutonomyCheckRow{
			{Name: "caronte_index_currency", Tier: "soft", Pass: false},
		},
	})
	_, err := runAutonomySubcommand(t, srv.URL, "--check", "--allow-soft-warnings")
	if err != nil {
		t.Fatalf("with --allow-soft-warnings should not error on soft-only fail: %v", err)
	}
}

func TestAutonomyCheck_SoftFailWithoutAllow(t *testing.T) {
	srv, _ := newFakeAutonomyDaemon(t, client.AutonomyShow{}, client.AutonomyCheckResult{
		OverallPass: false, SoftFailed: 1,
		Rows: []client.AutonomyCheckRow{
			{Name: "caronte_index_currency", Tier: "soft", Pass: false},
		},
	})
	_, err := runAutonomySubcommand(t, srv.URL, "--check")
	if err == nil {
		t.Fatal("expected error when soft tier fails without --allow-soft-warnings")
	}
	if !strings.Contains(err.Error(), "soft tier") {
		t.Errorf("error should mention soft tier: %v", err)
	}
}

func TestAutonomyCheck_AllPass(t *testing.T) {
	srv, _ := newFakeAutonomyDaemon(t, client.AutonomyShow{}, client.AutonomyCheckResult{
		OverallPass: true,
		Rows: []client.AutonomyCheckRow{
			{Name: "research_mcp_up", Tier: "hard", Pass: true},
		},
	})
	out, err := runAutonomySubcommand(t, srv.URL, "--check", "--verbose")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "PASS") || !strings.Contains(out, "research_mcp_up") {
		t.Errorf("verbose output should show passing rows: %s", out)
	}
}

func TestAutonomyMode_Happy(t *testing.T) {
	srv, rec := newFakeAutonomyDaemon(t, client.AutonomyShow{}, client.AutonomyCheckResult{})
	_, err := runAutonomySubcommand(t, srv.URL, "mode", "semi")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rec.modeReq.Mode != "semi" {
		t.Errorf("mode not propagated: %+v", rec.modeReq)
	}
}

func TestAutonomyMode_Reset(t *testing.T) {
	srv, rec := newFakeAutonomyDaemon(t, client.AutonomyShow{}, client.AutonomyCheckResult{})
	_, err := runAutonomySubcommand(t, srv.URL, "mode", "--reset")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !rec.modeReq.Reset {
		t.Errorf("--reset not propagated: %+v", rec.modeReq)
	}
}

func TestAutonomyMode_ResetAndArgConflict(t *testing.T) {
	srv, _ := newFakeAutonomyDaemon(t, client.AutonomyShow{}, client.AutonomyCheckResult{})
	_, err := runAutonomySubcommand(t, srv.URL, "mode", "--reset", "semi")
	if err == nil {
		t.Fatal("expected error: --reset and mode arg are mutually exclusive")
	}
}

func TestAutonomyMode_CapaFirewallRejected(t *testing.T) {
	srv, rec := newFakeAutonomyDaemon(t, client.AutonomyShow{}, client.AutonomyCheckResult{})
	rec.lockedByCapaFirewall = true
	_, err := runAutonomySubcommand(t, srv.URL, "mode", "full")
	if err == nil {
		t.Fatal("expected error: capa-firewall forbids mode override")
	}
	if !strings.Contains(err.Error(), "capa-firewall") {
		t.Errorf("error should mention capa-firewall: %v", err)
	}
}

func TestAutonomyMode_RejectsInvalidMode(t *testing.T) {
	srv, _ := newFakeAutonomyDaemon(t, client.AutonomyShow{}, client.AutonomyCheckResult{})
	_, err := runAutonomySubcommand(t, srv.URL, "mode", "yolo")
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
	if !strings.Contains(err.Error(), "invalid mode") {
		t.Errorf("error should mention invalid mode: %v", err)
	}
}

func TestAutonomyMode_RequiresArg(t *testing.T) {
	srv, _ := newFakeAutonomyDaemon(t, client.AutonomyShow{}, client.AutonomyCheckResult{})
	_, err := runAutonomySubcommand(t, srv.URL, "mode")
	if err == nil {
		t.Fatal("expected error: mode requires argument")
	}
}

func TestAutonomy_NoFlagsPrintsHelp(t *testing.T) {
	srv, _ := newFakeAutonomyDaemon(t, client.AutonomyShow{}, client.AutonomyCheckResult{})
	out, err := runAutonomySubcommand(t, srv.URL)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(out, "autonomy") {
		t.Errorf("expected help output, got %q", out)
	}
}
