//go:build integration

package augment_integration_test

import (
	"context"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/augment"
	"github.com/cbip-solutions/hades-system/internal/daemon/mcpgateway"
)

func TestAugmentToolNames_MatchGatewayRegistry(t *testing.T) {
	wantLane1 := mcpgateway.MustToolName("caronte", "query").String()
	if augment.ToolCaronteQuery != wantLane1 {
		t.Errorf("augment.ToolCaronteQuery=%q; want %q (registry mismatch — Lane 1 dispatches to a non-existent tool)",
			augment.ToolCaronteQuery, wantLane1)
	}

	wantLane3 := mcpgateway.MustToolName("caronte", "context").String()
	if augment.ToolCaronteContext != wantLane3 {
		t.Errorf("augment.ToolCaronteContext=%q; want %q (registry mismatch — Lane 3 dispatches to a non-existent tool)",
			augment.ToolCaronteContext, wantLane3)
	}

	const prefix = "mcp_zen-swarm_"
	for _, name := range []string{augment.ToolCaronteQuery, augment.ToolCaronteContext} {
		if len(name) < len(prefix) || name[:len(prefix)] != prefix {
			t.Errorf("tool name %q missing required prefix %q", name, prefix)
		}
	}
}

type fakeCaronteSubsystem struct{}

func (fakeCaronteSubsystem) Name() string { return "caronte" }

func (fakeCaronteSubsystem) Tools() []mcpgateway.ToolEntry {
	nopHandler := func(_ context.Context, _ mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
		return mcpgateway.CallResponse{}, nil
	}
	return []mcpgateway.ToolEntry{
		{
			Name:    mcpgateway.MustToolName("caronte", "query"),
			Handler: nopHandler,
			Meta:    mcpgateway.ToolMeta{Description: "caronte query"},
		},
		{
			Name:    mcpgateway.MustToolName("caronte", "context"),
			Handler: nopHandler,
			Meta:    mcpgateway.ToolMeta{Description: "caronte context"},
		},
		{
			Name:    mcpgateway.MustToolName("caronte", "impact"),
			Handler: nopHandler,
			Meta:    mcpgateway.ToolMeta{Description: "caronte impact"},
		},
	}
}

func TestAugmentToolNames_DispatcherKnowsThem(t *testing.T) {
	disp := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit:   mcpgateway.NopAuditEmitter(),
		RBACCfg: mcpgateway.RBACConfig{},
	})
	t.Cleanup(func() { _ = disp.Close() })

	if err := disp.RegisterSubsystem(fakeCaronteSubsystem{}); err != nil {
		t.Fatalf("RegisterSubsystem caronte: %v", err)
	}

	tools := disp.ListTools()
	want := map[string]bool{
		augment.ToolCaronteQuery:   false,
		augment.ToolCaronteContext: false,
	}
	for _, te := range tools {
		if _, ok := want[te.Name.String()]; ok {
			want[te.Name.String()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("dispatcher missing tool %q (augment Lane would 0-row in production)", name)
		}
	}

	for _, name := range []string{augment.ToolCaronteQuery, augment.ToolCaronteContext} {
		tn, err := mcpgateway.ParseToolName(name)
		if err != nil {
			t.Fatalf("ParseToolName(%q): %v", name, err)
		}
		req := mcpgateway.CallRequest{
			Tool:      tn,
			ProjectID: "test-proj",
			Doctrine:  mcpgateway.DoctrineDefault,
			Args:      map[string]any{"query": "test"},
		}
		if _, err := disp.Dispatch(context.Background(), req); err != nil {

			t.Errorf("Dispatch(%q) returned unexpected error: %v (must not be ErrToolNotRegistered)", name, err)
		}
	}
}
