package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

type fakeCitationProber struct {
	resps map[string]*client.CitationProbeResp
	err   error
}

func (f *fakeCitationProber) CitationProbe(_ context.Context, check string) (*client.CitationProbeResp, error) {
	if f.err != nil {
		return nil, f.err
	}
	if r, ok := f.resps[check]; ok {
		return r, nil
	}
	return &client.CitationProbeResp{Status: "ok", Detail: "stub-ok"}, nil
}

func TestRunCitationChecksReturnsThree(t *testing.T) {
	t.Parallel()
	prober := &fakeCitationProber{
		resps: map[string]*client.CitationProbeResp{
			"envelope-serialize-roundtrip": {Status: "ok", Detail: "roundtrip passed"},
			"renderers":                    {Status: "warn", Detail: "1/7 renderers (markdown_fallback only)"},
			"audit-handler-functional":     {Status: "ok", Detail: "/v1/audit/event/* reachable"},
		},
	}
	got := runCitationChecksWith(context.Background(), prober)
	if len(got) != 3 {
		t.Fatalf("len=%d, want 3", len(got))
	}
	wantOrder := []string{
		"citation.envelope.serialize-roundtrip",
		"citation.renderers",
		"citation.audit-chain.zen://audit-handler-functional",
	}
	for i, name := range wantOrder {
		if got[i].Name != name {
			t.Errorf("results[%d].Name=%q, want %q", i, got[i].Name, name)
		}
	}
}

func TestRunCitationChecksDaemonError(t *testing.T) {
	t.Parallel()
	prober := &fakeCitationProber{err: errors.New("daemon dead")}
	got := runCitationChecksWith(context.Background(), prober)
	if len(got) != 3 {
		t.Fatalf("len=%d, want 3", len(got))
	}
	for _, r := range got {
		if r.Status != "fail" {
			t.Errorf("check %q status=%q, want fail", r.Name, r.Status)
		}
	}
}

func TestDoctorCitationCmdRegistered(t *testing.T) {
	t.Parallel()
	cmd := NewDoctorCitationCmd()
	if cmd.Use != "citation" {
		t.Fatalf("Use=%q, want %q", cmd.Use, "citation")
	}
}
