package mcpgateway

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestHandleGetConsumersMissingEndpointArg(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	_, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "get_consumers"),
		Args:      map[string]any{"workspace": "ws-1"},
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err == nil || !strings.Contains(err.Error(), `missing "endpoint"`) {
		t.Errorf("err = %v; want missing-endpoint validation error", err)
	}
}

func TestHandleGetConsumersMissingWorkspaceArg(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	_, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "get_consumers"),
		Args:      map[string]any{"endpoint": "endpoint-1"},
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err == nil || !strings.Contains(err.Error(), `missing "workspace"`) {
		t.Errorf("err = %v; want missing-workspace validation error", err)
	}
}

func TestHandleGetConsumersEngineErrorEscalates(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{failAll: true}, NopAuditEmitter())
	for _, mode := range []Mode{ModeAutonomy, ModeAFK, ModeInteractive} {
		_, err := p.CallByTool(context.Background(), CallRequest{
			Tool:      MustToolName("caronte", "get_consumers"),
			Args:      map[string]any{"endpoint": "endpoint-1", "workspace": "ws-1"},
			ProjectID: "proj-1",
			Mode:      mode,
		})
		if !errors.Is(err, ErrCaronteUnreachable) {
			t.Errorf("mode=%s: err = %v; want ErrCaronteUnreachable", mode.String(), err)
		}
	}
}

func TestHandleGetConsumersHappyPath(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	resp, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "get_consumers"),
		Args:      map[string]any{"endpoint": "endpoint-1", "workspace": "ws-1"},
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err != nil {
		t.Fatalf("CallByTool: %v", err)
	}
	body := resp.Content[0].Text
	wantSubstrs := []string{`"endpoint_id":"endpoint-1"`, `"workspace_id":"ws-1"`, `"call_id":"call-1"`, `"confidence":"spec_artifact"`}
	for _, s := range wantSubstrs {
		if !strings.Contains(body, s) {
			t.Errorf("body %q missing substring %q", body, s)
		}
	}
}
