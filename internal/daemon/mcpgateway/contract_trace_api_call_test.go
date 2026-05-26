package mcpgateway

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestHandleTraceAPICallMissingCallArg(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	_, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "trace_api_call"),
		Args:      map[string]any{"workspace": "ws-1"},
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err == nil || !strings.Contains(err.Error(), `missing "call"`) {
		t.Errorf("err = %v; want missing-call validation error", err)
	}
}

func TestHandleTraceAPICallMissingWorkspaceArg(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	_, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "trace_api_call"),
		Args:      map[string]any{"call": "call-1"},
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err == nil || !strings.Contains(err.Error(), `missing "workspace"`) {
		t.Errorf("err = %v; want missing-workspace validation error", err)
	}
}

func TestHandleTraceAPICallEngineErrorEscalates(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{failAll: true}, NopAuditEmitter())
	for _, mode := range []Mode{ModeAutonomy, ModeAFK, ModeInteractive} {
		_, err := p.CallByTool(context.Background(), CallRequest{
			Tool:      MustToolName("caronte", "trace_api_call"),
			Args:      map[string]any{"call": "call-1", "workspace": "ws-1"},
			ProjectID: "proj-1",
			Mode:      mode,
		})
		if !errors.Is(err, ErrCaronteUnreachable) {
			t.Errorf("mode=%s: err = %v; want ErrCaronteUnreachable", mode.String(), err)
		}
	}
}

func TestHandleTraceAPICallHappyPath(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	resp, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "trace_api_call"),
		Args:      map[string]any{"call": "call-1", "workspace": "ws-1"},
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err != nil {
		t.Fatalf("CallByTool: %v", err)
	}
	body := resp.Content[0].Text
	wantSubstrs := []string{`"call_id":"call-1"`, `"workspace_id":"ws-1"`, `"endpoint_id":"endpoint-1"`, `"unresolved":false`}
	for _, s := range wantSubstrs {
		if !strings.Contains(body, s) {
			t.Errorf("body %q missing substring %q", body, s)
		}
	}
}
