package mcpgateway

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestHandleFederationHealthEngineErrorEscalates(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{failAll: true}, NopAuditEmitter())
	for _, mode := range []Mode{ModeAutonomy, ModeAFK, ModeInteractive} {
		_, err := p.CallByTool(context.Background(), CallRequest{
			Tool:      MustToolName("caronte", "federation_health"),
			Args:      map[string]any{"workspace": "ws-1"},
			ProjectID: "proj-1",
			Mode:      mode,
		})
		if !errors.Is(err, ErrCaronteUnreachable) {
			t.Errorf("mode=%s: err = %v; want ErrCaronteUnreachable", mode.String(), err)
		}
	}
}

func TestHandleFederationHealthHappyPathWithWorkspace(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	resp, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "federation_health"),
		Args:      map[string]any{"workspace": "ws-1"},
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err != nil {
		t.Fatalf("CallByTool: %v", err)
	}
	body := resp.Content[0].Text
	wantSubstrs := []string{`"workspace_id":"ws-1"`, `"reachable":true`, `"gate_latency_p95_ms":1.2`}
	for _, s := range wantSubstrs {
		if !strings.Contains(body, s) {
			t.Errorf("body %q missing substring %q", body, s)
		}
	}
}

func TestHandleFederationHealthHappyPathDaemonWide(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	resp, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "federation_health"),
		Args:      map[string]any{},
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err != nil {
		t.Fatalf("CallByTool: %v", err)
	}
	body := resp.Content[0].Text

	if !strings.Contains(body, `"reachable":true`) {
		t.Errorf("body %q missing reachable=true", body)
	}
	if !strings.Contains(body, `"workspace_id":""`) {
		t.Errorf("body %q missing workspace_id=\"\" (daemon-wide marker)", body)
	}
}
