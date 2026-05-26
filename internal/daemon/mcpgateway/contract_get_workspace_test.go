package mcpgateway

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestHandleGetWorkspaceMissingWorkspaceArg(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	_, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "get_workspace"),
		Args:      map[string]any{},
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err == nil || !strings.Contains(err.Error(), `missing "workspace"`) {
		t.Errorf("err = %v; want missing-workspace validation error", err)
	}
}

func TestHandleGetWorkspaceEngineErrorEscalates(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{failAll: true}, NopAuditEmitter())
	for _, mode := range []Mode{ModeAutonomy, ModeAFK, ModeInteractive} {
		_, err := p.CallByTool(context.Background(), CallRequest{
			Tool:      MustToolName("caronte", "get_workspace"),
			Args:      map[string]any{"workspace": "ws-1"},
			ProjectID: "proj-1",
			Mode:      mode,
		})
		if !errors.Is(err, ErrCaronteUnreachable) {
			t.Errorf("mode=%s: err = %v; want ErrCaronteUnreachable", mode.String(), err)
		}
	}
}

func TestHandleGetWorkspaceHappyPath(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	resp, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "get_workspace"),
		Args:      map[string]any{"workspace": "ws-1"},
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err != nil {
		t.Fatalf("CallByTool: %v", err)
	}
	body := resp.Content[0].Text
	wantSubstrs := []string{`"workspace_id":"ws-1"`, `"owning_project":"proj-a"`, `"members":["proj-a","proj-b"]`, `"policy_locked":false`}
	for _, s := range wantSubstrs {
		if !strings.Contains(body, s) {
			t.Errorf("body %q missing substring %q", body, s)
		}
	}
}
