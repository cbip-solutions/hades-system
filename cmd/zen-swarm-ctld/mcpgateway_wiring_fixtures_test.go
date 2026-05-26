package main

import (
	"context"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	mcpaudit "github.com/cbip-solutions/hades-system/internal/mcp/audit"
	mcpbudget "github.com/cbip-solutions/hades-system/internal/mcp/budget"
	mcpsshexec "github.com/cbip-solutions/hades-system/internal/mcp/sshexec"
)

type fakeDaemonAuditServer struct {
	received []handlers.AuditEventIn
}

func (f *fakeDaemonAuditServer) Emit(event handlers.AuditEventIn) error {
	f.received = append(f.received, event)
	return nil
}

func (f *fakeDaemonAuditServer) AuditEmit(event handlers.AuditEventIn) error {
	return f.Emit(event)
}

func newBudgetSubsystemForTest(t *testing.T) *internalMCPSubsystem {
	t.Helper()
	s := mcpbudget.NewServer(nil)
	return newBudgetSubsystem(s)
}

func newAuditSubsystemForTest(t *testing.T) *internalMCPSubsystem {
	t.Helper()
	s, err := mcpaudit.NewServer(mcpaudit.ServerConfig{
		ReviewerFamilyPool: []string{"anthropic", "google", "openai"},
		MinPoolSize:        2,
	})
	if err != nil {
		t.Fatalf("mcpaudit.NewServer: %v", err)
	}
	return newAuditSubsystem(s)
}

func newSSHExecSubsystemForTest(t *testing.T) *internalMCPSubsystem {
	t.Helper()
	resolver := func(_ string) (*mcpsshexec.Allowlist, error) {
		return &mcpsshexec.Allowlist{Project: "test"}, nil
	}
	s := mcpsshexec.NewServer(mcpsshexec.ServerConfig{
		Component:         "ssh-exec",
		AllowlistResolver: resolver,
		Emitter:           mcpsshexec.NopAuditEmitter{},
	})
	return newSSHExecSubsystem(s)
}

type fakeResearchServer struct {
	names    []string
	invokeFn func(ctx context.Context, name string, args map[string]any) (any, error)
}

func (f fakeResearchServer) ToolNames() []string {
	return append([]string(nil), f.names...)
}

func (f fakeResearchServer) InvokeTool(ctx context.Context, name string, args map[string]any) (any, error) {
	if f.invokeFn == nil {
		return nil, nil
	}
	return f.invokeFn(ctx, name, args)
}
