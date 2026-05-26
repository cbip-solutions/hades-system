package mcpgateway

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestHandleGetContractMissingEndpointArg(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	_, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "get_contract"),
		Args:      map[string]any{},
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err == nil {
		t.Fatal("CallByTool(get_contract, no endpoint) returned nil error; want validation error")
	}
	if !strings.Contains(err.Error(), `missing "endpoint"`) {
		t.Errorf("err = %v; want missing-endpoint validation error", err)
	}
}

func TestHandleGetContractEmptyEndpointArg(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	_, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "get_contract"),
		Args:      map[string]any{"endpoint": ""},
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err == nil || !strings.Contains(err.Error(), `"endpoint" empty`) {
		t.Errorf("err = %v; want empty-endpoint validation error", err)
	}
}

func TestHandleGetContractEngineErrorEscalates(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{failAll: true}, NopAuditEmitter())
	for _, mode := range []Mode{ModeAutonomy, ModeAFK, ModeInteractive} {
		_, err := p.CallByTool(context.Background(), CallRequest{
			Tool:      MustToolName("caronte", "get_contract"),
			Args:      map[string]any{"endpoint": "endpoint-1"},
			ProjectID: "proj-1",
			Mode:      mode,
		})
		if !errors.Is(err, ErrCaronteUnreachable) {
			t.Errorf("mode=%s: err = %v; want ErrCaronteUnreachable", mode.String(), err)
		}
	}
}

func TestHandleGetContractHappyPath(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	resp, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "get_contract"),
		Args:      map[string]any{"endpoint": "endpoint-42"},
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err != nil {
		t.Fatalf("CallByTool: %v", err)
	}
	body := resp.Content[0].Text
	wantSubstrs := []string{`"endpoint_id":"endpoint-42"`, `"method":"GET"`, `"path_template":"/users/{id}"`}
	for _, s := range wantSubstrs {
		if !strings.Contains(body, s) {
			t.Errorf("body %q missing substring %q", body, s)
		}
	}
	if resp.Subsystem != "caronte" {
		t.Errorf("Subsystem = %q; want caronte", resp.Subsystem)
	}
}
