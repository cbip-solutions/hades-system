package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

type fakeHermesProber struct {
	resps map[string]*client.HermesProbeResp
	err   error
}

func (f *fakeHermesProber) HermesProbe(_ context.Context, check string) (*client.HermesProbeResp, error) {
	if f.err != nil {
		return nil, f.err
	}
	if r, ok := f.resps[check]; ok {
		return r, nil
	}
	return &client.HermesProbeResp{Status: "ok", Detail: "stub-ok"}, nil
}

func TestRunHermesChecksReturnsFour(t *testing.T) {
	t.Parallel()
	prober := &fakeHermesProber{
		resps: map[string]*client.HermesProbeResp{
			"installed":               {Status: "ok", Detail: "v0.13.2"},
			"plugin-zen-swarm-loaded": {Status: "ok", Detail: "plugin/zen-swarm/plugin.yaml"},
			"config-mcp-reachable":    {Status: "ok", Detail: "http://localhost:9181/v1/mcpgateway"},
			"curator-last-run":        {Status: "ok", Detail: "2026-05-08T11:00:00Z"},
		},
	}
	got := runHermesChecksWith(context.Background(), prober)
	if len(got) != 4 {
		t.Fatalf("len=%d, want 4", len(got))
	}
	wantOrder := []string{
		"hermes.installed",
		"hermes.plugin-zen-swarm-loaded",
		"hermes.config.mcp_servers.zen-swarm-reachable",
		"hermes.curator.last-run",
	}
	for i, name := range wantOrder {
		if got[i].Name != name {
			t.Errorf("results[%d].Name=%q, want %q", i, got[i].Name, name)
		}
	}
}

func TestRunHermesChecksDaemonError(t *testing.T) {
	t.Parallel()
	prober := &fakeHermesProber{err: errors.New("daemon dead")}
	got := runHermesChecksWith(context.Background(), prober)
	for _, r := range got {
		if r.Status != "fail" {
			t.Errorf("check %q status=%q, want fail", r.Name, r.Status)
		}
		if !strings.Contains(r.Detail, "daemon dead") {
			t.Errorf("check %q detail=%q, want includes 'daemon dead'", r.Name, r.Detail)
		}
	}
}

func TestHermesInstalledFailHasUpgradeHint(t *testing.T) {
	t.Parallel()
	prober := &fakeHermesProber{
		resps: map[string]*client.HermesProbeResp{
			"installed": {Status: "fail", Detail: "binary not found"},
		},
	}
	got := runHermesChecksWith(context.Background(), prober)
	if got[0].Status != "fail" {
		t.Fatalf("status=%q, want fail", got[0].Status)
	}
	if !strings.Contains(got[0].Hint, "brew install hermes-agent") {
		t.Errorf("hint=%q, want includes brew install hermes-agent", got[0].Hint)
	}
}

func TestDoctorHermesCmdRegistered(t *testing.T) {
	t.Parallel()
	cmd := NewDoctorHermesCmd()
	if cmd.Use != "hermes" {
		t.Fatalf("Use=%q, want %q", cmd.Use, "hermes")
	}
}
