package inv_zen_165_gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/mcpgateway"
)

func TestInvZen165CompileAnchor(t *testing.T) {
	if !mcpgateway.AssertToolRegistryDedup() {
		t.Error("AssertToolRegistryDedup returned false")
	}
}

func TestInvZen165BoundaryAnchor(t *testing.T) {

	if !mcpgateway.AssertBoundaryPreserved() {
		t.Error("AssertBoundaryPreserved returned false")
	}
}

func TestInvZen165RegistryRejectsDuplicate(t *testing.T) {
	r := mcpgateway.NewToolRegistry()
	tn := mcpgateway.MustToolName("budget", "cap_status")
	noop := func(_ context.Context, _ mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
		return mcpgateway.CallResponse{}, nil
	}
	if err := r.Register(tn, noop, mcpgateway.ToolMeta{}); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := r.Register(tn, noop, mcpgateway.ToolMeta{})
	if err == nil {
		t.Fatal("inv-zen-165 violated: second Register returned nil err")
	}
	if !errors.Is(err, mcpgateway.ErrToolNameCollision) {
		t.Errorf("inv-zen-165: err = %v; expected wrap of ErrToolNameCollision", err)
	}
}

func TestInvZen165CrossSubsystemCollisionRejected(t *testing.T) {

	noop := func(_ context.Context, _ mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
		return mcpgateway.CallResponse{}, nil
	}
	collisionName := mcpgateway.MustToolName("budget", "cap_status")
	subA := &compSubsystem{name: "budget", tools: []mcpgateway.ToolEntry{{
		Name: collisionName, Handler: noop, Meta: mcpgateway.ToolMeta{},
	}}}
	subB := &compSubsystem{name: "budget-shadow", tools: []mcpgateway.ToolEntry{{
		Name: collisionName, Handler: noop, Meta: mcpgateway.ToolMeta{},
	}}}
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit: mcpgateway.NopAuditEmitter(),
	})
	if err := d.RegisterSubsystem(subA); err != nil {
		t.Fatalf("subA Register: %v", err)
	}
	err := d.RegisterSubsystem(subB)
	if err == nil {
		t.Fatal("inv-zen-165 violated: cross-subsystem collision accepted")
	}
	if !errors.Is(err, mcpgateway.ErrToolNameCollision) {
		t.Errorf("inv-zen-165: err = %v; expected wrap of ErrToolNameCollision", err)
	}
}

func TestInvZen165KnownSubsystemsClosedSet(t *testing.T) {

	known := mcpgateway.KnownSubsystems()
	want := map[string]bool{
		"research": true, "budget": true, "audit": true,
		"sshexec": true, "codegen": true, "caronte": true,
	}
	if len(known) != len(want) {
		t.Errorf("KnownSubsystems len = %d; want %d (Phase A frozen set)", len(known), len(want))
	}
	for _, k := range known {
		if !want[k] {
			t.Errorf("unexpected subsystem in KnownSubsystems: %q", k)
		}
	}
}

type compSubsystem struct {
	name  string
	tools []mcpgateway.ToolEntry
}

func (c *compSubsystem) Name() string                  { return c.name }
func (c *compSubsystem) Tools() []mcpgateway.ToolEntry { return c.tools }
