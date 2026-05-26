package mcpgateway

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestHandleContractDiffMissingEndpointArg(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	_, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "contract_diff"),
		Args:      map[string]any{"since": float64(1700000000)},
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err == nil || !strings.Contains(err.Error(), `missing "endpoint"`) {
		t.Errorf("err = %v; want missing-endpoint validation error", err)
	}
}

func TestHandleContractDiffMissingSinceArg(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	_, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "contract_diff"),
		Args:      map[string]any{"endpoint": "endpoint-1"},
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err == nil || !strings.Contains(err.Error(), `missing "since"`) {
		t.Errorf("err = %v; want missing-since validation error (since REQUIRED for contract_diff)", err)
	}
}

func TestHandleContractDiffEngineErrorEscalates(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{failAll: true}, NopAuditEmitter())
	for _, mode := range []Mode{ModeAutonomy, ModeAFK, ModeInteractive} {
		_, err := p.CallByTool(context.Background(), CallRequest{
			Tool:      MustToolName("caronte", "contract_diff"),
			Args:      map[string]any{"endpoint": "endpoint-1", "since": float64(1700000000)},
			ProjectID: "proj-1",
			Mode:      mode,
		})
		if !errors.Is(err, ErrCaronteUnreachable) {
			t.Errorf("mode=%s: err = %v; want ErrCaronteUnreachable", mode.String(), err)
		}
	}
}

func TestHandleContractDiffHappyPath(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	resp, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "contract_diff"),
		Args:      map[string]any{"endpoint": "endpoint-1", "since": float64(1700000000)},
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err != nil {
		t.Fatalf("CallByTool: %v", err)
	}
	body := resp.Content[0].Text
	wantSubstrs := []string{`"endpoint_id":"endpoint-1"`, `"severity":"BREAKING"`, `"kind":"param_added_required"`}
	for _, s := range wantSubstrs {
		if !strings.Contains(body, s) {
			t.Errorf("body %q missing substring %q", body, s)
		}
	}
}
