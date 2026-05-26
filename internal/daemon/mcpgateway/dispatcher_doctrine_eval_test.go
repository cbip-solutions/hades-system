package mcpgateway_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/mcpgateway"
)

type fakeDoctrineEvaluator struct {
	decisionLabel string
	evidence      string
	err           error

	invocations []doctrineEvalInvocation
}

type doctrineEvalInvocation struct {
	mcpName  string
	toolName string
	params   any
}

func (f *fakeDoctrineEvaluator) EvaluateCall(_ context.Context, mcpName, toolName string, params any) (string, string, error) {
	f.invocations = append(f.invocations, doctrineEvalInvocation{
		mcpName:  mcpName,
		toolName: toolName,
		params:   params,
	})
	return f.decisionLabel, f.evidence, f.err
}

func TestDispatcher_DoctrineEval_AllowProceeds(t *testing.T) {
	called := atomic.Int32{}
	h := func(_ context.Context, _ mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
		called.Add(1)
		return mcpgateway.CallResponse{
			Content:   []mcpgateway.CallContentItem{{Type: "text", Text: "ok"}},
			Subsystem: "audit",
		}, nil
	}
	sub := &fakeSubsystem{name: "audit", tools: []mcpgateway.ToolEntry{
		entryFor(t, "audit", "emit", h),
	}}
	fakeEval := &fakeDoctrineEvaluator{decisionLabel: "allow", evidence: "test-allow"}
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit:     mcpgateway.NopAuditEmitter(),
		Evaluator: fakeEval,
	})
	if err := d.RegisterSubsystem(sub); err != nil {
		t.Fatalf("RegisterSubsystem: %v", err)
	}
	_, err := d.Dispatch(context.Background(), mcpgateway.CallRequest{
		Tool:     mcpgateway.MustToolName("audit", "emit"),
		Args:     map[string]any{"x": 1},
		Doctrine: mcpgateway.DoctrineDefault,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if called.Load() != 1 {
		t.Errorf("handler called = %d; want 1", called.Load())
	}
	if len(fakeEval.invocations) != 1 {
		t.Fatalf("evaluator invocations = %d; want 1", len(fakeEval.invocations))
	}
	inv := fakeEval.invocations[0]
	if inv.mcpName != "audit" || inv.toolName != "emit" {
		t.Errorf("evaluator called with (%q,%q); want (audit,emit)", inv.mcpName, inv.toolName)
	}

	m, ok := inv.params.(map[string]any)
	if !ok || m["x"] != 1 {
		t.Errorf("params = %#v; want map[x:1]", inv.params)
	}
}

func TestDispatcher_DoctrineEval_DenyRejectsCall(t *testing.T) {
	called := atomic.Int32{}
	h := func(_ context.Context, _ mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
		called.Add(1)
		return mcpgateway.CallResponse{}, nil
	}
	sub := &fakeSubsystem{name: "audit", tools: []mcpgateway.ToolEntry{
		entryFor(t, "audit", "emit", h),
	}}
	fakeEval := &fakeDoctrineEvaluator{decisionLabel: "deny", evidence: "test-deny"}
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit:     mcpgateway.NopAuditEmitter(),
		Evaluator: fakeEval,
	})
	if err := d.RegisterSubsystem(sub); err != nil {
		t.Fatalf("RegisterSubsystem: %v", err)
	}
	_, err := d.Dispatch(context.Background(), mcpgateway.CallRequest{
		Tool:     mcpgateway.MustToolName("audit", "emit"),
		Args:     map[string]any{},
		Doctrine: mcpgateway.DoctrineDefault,
	})
	if !errors.Is(err, mcpgateway.ErrDoctrineDeny) {
		t.Errorf("err = %v; want ErrDoctrineDeny", err)
	}
	if called.Load() != 0 {
		t.Errorf("handler called = %d; want 0 (deny should short-circuit)", called.Load())
	}
}

func TestDispatcher_DoctrineEval_ConfirmRequiredRejectsCall(t *testing.T) {
	called := atomic.Int32{}
	h := func(_ context.Context, _ mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
		called.Add(1)
		return mcpgateway.CallResponse{}, nil
	}
	sub := &fakeSubsystem{name: "audit", tools: []mcpgateway.ToolEntry{
		entryFor(t, "audit", "emit", h),
	}}
	fakeEval := &fakeDoctrineEvaluator{decisionLabel: "allow_with_confirm"}
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit:     mcpgateway.NopAuditEmitter(),
		Evaluator: fakeEval,
	})
	if err := d.RegisterSubsystem(sub); err != nil {
		t.Fatalf("RegisterSubsystem: %v", err)
	}
	_, err := d.Dispatch(context.Background(), mcpgateway.CallRequest{
		Tool:     mcpgateway.MustToolName("audit", "emit"),
		Args:     map[string]any{},
		Doctrine: mcpgateway.DoctrineDefault,
	})
	if !errors.Is(err, mcpgateway.ErrDoctrineConfirmRequired) {
		t.Errorf("err = %v; want ErrDoctrineConfirmRequired", err)
	}
	if called.Load() != 0 {
		t.Errorf("handler called = %d; want 0 (confirm required should short-circuit)", called.Load())
	}
}

func TestDispatcher_DoctrineEval_AllowWithAuditProceeds(t *testing.T) {
	called := atomic.Int32{}
	h := func(_ context.Context, _ mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
		called.Add(1)
		return mcpgateway.CallResponse{
			Content:   []mcpgateway.CallContentItem{{Type: "text", Text: "ok"}},
			Subsystem: "audit",
		}, nil
	}
	sub := &fakeSubsystem{name: "audit", tools: []mcpgateway.ToolEntry{
		entryFor(t, "audit", "emit", h),
	}}
	fakeEval := &fakeDoctrineEvaluator{decisionLabel: "allow_with_audit"}
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit:     mcpgateway.NopAuditEmitter(),
		Evaluator: fakeEval,
	})
	if err := d.RegisterSubsystem(sub); err != nil {
		t.Fatalf("RegisterSubsystem: %v", err)
	}
	_, err := d.Dispatch(context.Background(), mcpgateway.CallRequest{
		Tool:     mcpgateway.MustToolName("audit", "emit"),
		Args:     map[string]any{},
		Doctrine: mcpgateway.DoctrineDefault,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if called.Load() != 1 {
		t.Errorf("handler called = %d; want 1", called.Load())
	}
}

func TestDispatcher_DoctrineEval_NilEvaluatorPreservesPlan11Baseline(t *testing.T) {
	called := atomic.Int32{}
	h := func(_ context.Context, _ mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
		called.Add(1)
		return mcpgateway.CallResponse{
			Content:   []mcpgateway.CallContentItem{{Type: "text", Text: "ok"}},
			Subsystem: "audit",
		}, nil
	}
	sub := &fakeSubsystem{name: "audit", tools: []mcpgateway.ToolEntry{
		entryFor(t, "audit", "emit", h),
	}}
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit: mcpgateway.NopAuditEmitter(),
	})
	if err := d.RegisterSubsystem(sub); err != nil {
		t.Fatalf("RegisterSubsystem: %v", err)
	}
	_, err := d.Dispatch(context.Background(), mcpgateway.CallRequest{
		Tool:     mcpgateway.MustToolName("audit", "emit"),
		Args:     map[string]any{},
		Doctrine: mcpgateway.DoctrineDefault,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if called.Load() != 1 {
		t.Errorf("handler called = %d; want 1", called.Load())
	}
}
