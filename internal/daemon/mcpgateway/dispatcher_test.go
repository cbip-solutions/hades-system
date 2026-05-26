package mcpgateway_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/mcpgateway"
)

type recordingAudit struct {
	events []string
}

func (r *recordingAudit) Emit(t string, _ []byte) { r.events = append(r.events, t) }

type fakeSubsystem struct {
	name  string
	tools []mcpgateway.ToolEntry
}

func (f *fakeSubsystem) Name() string                  { return f.name }
func (f *fakeSubsystem) Tools() []mcpgateway.ToolEntry { return f.tools }

func entryFor(t *testing.T, subsystem, tool string, h mcpgateway.Handler) mcpgateway.ToolEntry {
	t.Helper()
	tn, err := mcpgateway.NewToolName(subsystem, tool)
	if err != nil {
		t.Fatalf("NewToolName: %v", err)
	}
	return mcpgateway.ToolEntry{Name: tn, Handler: h, Meta: mcpgateway.ToolMeta{
		Description: "test", InputSchema: map[string]any{"type": "object"},
	}}
}

func TestDispatcherDispatchSuccess(t *testing.T) {
	called := atomic.Int32{}
	h := func(_ context.Context, req mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
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
	resp, err := d.Dispatch(context.Background(), mcpgateway.CallRequest{
		Tool:     mcpgateway.MustToolName("audit", "emit"),
		Args:     map[string]any{"x": 1},
		Doctrine: mcpgateway.DoctrineDefault,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if resp.Subsystem != "audit" {
		t.Errorf("Subsystem = %q want %q", resp.Subsystem, "audit")
	}
	if called.Load() != 1 {
		t.Errorf("handler called = %d, want 1", called.Load())
	}
}

func TestDispatcherDispatchUnknownTool(t *testing.T) {
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit: mcpgateway.NopAuditEmitter(),
	})
	_, err := d.Dispatch(context.Background(), mcpgateway.CallRequest{
		Tool:     mcpgateway.MustToolName("audit", "ghost"),
		Args:     map[string]any{},
		Doctrine: mcpgateway.DoctrineDefault,
	})
	if err == nil {
		t.Fatal("nil err on unknown tool")
	}
	if !errors.Is(err, mcpgateway.ErrToolNotRegistered) {
		t.Errorf("err = %v; expected ErrToolNotRegistered (I-4: registry-miss != rbac-deny)", err)
	}
	if errors.Is(err, mcpgateway.ErrRBACDenied) {
		t.Errorf("err = %v; must NOT alias ErrRBACDenied (I-4 separates the wires)", err)
	}
}

func TestDispatcherDispatchHandlerPanicRecovered(t *testing.T) {
	h := func(_ context.Context, _ mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
		panic("boom")
	}
	sub := &fakeSubsystem{name: "audit", tools: []mcpgateway.ToolEntry{
		entryFor(t, "audit", "emit", h),
	}}
	audit := &recordingAudit{}
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{Audit: audit})
	if err := d.RegisterSubsystem(sub); err != nil {
		t.Fatalf("RegisterSubsystem: %v", err)
	}
	_, err := d.Dispatch(context.Background(), mcpgateway.CallRequest{
		Tool:     mcpgateway.MustToolName("audit", "emit"),
		Args:     map[string]any{},
		Doctrine: mcpgateway.DoctrineDefault,
	})
	if err == nil {
		t.Fatal("Dispatch returned nil err on panic")
	}
	if !strings.Contains(err.Error(), "panic") {
		t.Errorf("err = %v; expected to mention panic", err)
	}

	found := false
	for _, e := range audit.events {
		if e == "HandlerPanic" {
			found = true
		}
	}
	if !found {
		t.Errorf("audit events = %v; missing HandlerPanic", audit.events)
	}
}

func TestDispatcherDispatchEmitsToolDispatched(t *testing.T) {
	h := func(_ context.Context, _ mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
		return mcpgateway.CallResponse{Subsystem: "audit"}, nil
	}
	sub := &fakeSubsystem{name: "audit", tools: []mcpgateway.ToolEntry{
		entryFor(t, "audit", "emit", h),
	}}
	audit := &recordingAudit{}
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{Audit: audit})
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
	found := false
	for _, e := range audit.events {
		if e == "ToolDispatched" {
			found = true
		}
	}
	if !found {
		t.Errorf("audit events = %v; missing ToolDispatched", audit.events)
	}
}

func TestDispatcherRegisterSubsystemRejectsCollision(t *testing.T) {
	h := func(_ context.Context, _ mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
		return mcpgateway.CallResponse{}, nil
	}
	subA := &fakeSubsystem{name: "audit", tools: []mcpgateway.ToolEntry{
		entryFor(t, "audit", "emit", h),
	}}

	subB := &fakeSubsystem{name: "audit", tools: []mcpgateway.ToolEntry{
		entryFor(t, "audit", "emit", h),
	}}
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit: mcpgateway.NopAuditEmitter(),
	})
	if err := d.RegisterSubsystem(subA); err != nil {
		t.Fatalf("RegisterSubsystem subA: %v", err)
	}
	err := d.RegisterSubsystem(subB)
	if err == nil {
		t.Fatal("RegisterSubsystem subB: nil err; expected ErrToolNameCollision")
	}
	if !errors.Is(err, mcpgateway.ErrToolNameCollision) {
		t.Errorf("err = %v; expected ErrToolNameCollision", err)
	}
}

func TestDispatcherRegisterSubsystemRejectsNil(t *testing.T) {
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit: mcpgateway.NopAuditEmitter(),
	})
	if err := d.RegisterSubsystem(nil); err == nil {
		t.Fatal("RegisterSubsystem(nil) returned nil err")
	}
}

func TestDispatcherRegisterSubsystemRejectsNilHandler(t *testing.T) {

	tn, _ := mcpgateway.NewToolName("audit", "broken")
	sub := &fakeSubsystem{name: "audit", tools: []mcpgateway.ToolEntry{{
		Name: tn, Handler: nil, Meta: mcpgateway.ToolMeta{},
	}}}
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit: mcpgateway.NopAuditEmitter(),
	})
	err := d.RegisterSubsystem(sub)
	if err == nil {
		t.Fatal("RegisterSubsystem with nil handler: nil err")
	}
	if !errors.Is(err, mcpgateway.ErrToolNameInvalid) {
		t.Errorf("err = %v; expected wrap of ErrToolNameInvalid", err)
	}
}

func TestDispatcherListAllTools(t *testing.T) {
	h := func(_ context.Context, _ mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
		return mcpgateway.CallResponse{}, nil
	}
	subA := &fakeSubsystem{name: "audit", tools: []mcpgateway.ToolEntry{
		entryFor(t, "audit", "emit", h),
		entryFor(t, "audit", "query", h),
	}}
	subB := &fakeSubsystem{name: "budget", tools: []mcpgateway.ToolEntry{
		entryFor(t, "budget", "cap_status", h),
	}}
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit: mcpgateway.NopAuditEmitter(),
	})
	if err := d.RegisterSubsystem(subA); err != nil {
		t.Fatalf("RegisterSubsystem: %v", err)
	}
	if err := d.RegisterSubsystem(subB); err != nil {
		t.Fatalf("RegisterSubsystem: %v", err)
	}
	all := d.ListTools()
	if len(all) != 3 {
		t.Errorf("ListTools len = %d; want 3", len(all))
	}
}

func TestDispatcherCloseEmpty(t *testing.T) {
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit: mcpgateway.NopAuditEmitter(),
	})
	if err := d.Close(); err != nil {
		t.Errorf("Close empty: %v", err)
	}
}

func TestDispatcherContextCancelledMidDispatch(t *testing.T) {

	h := func(ctx context.Context, _ mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
		<-ctx.Done()
		return mcpgateway.CallResponse{}, ctx.Err()
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
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := d.Dispatch(ctx, mcpgateway.CallRequest{
		Tool:     mcpgateway.MustToolName("audit", "emit"),
		Args:     map[string]any{},
		Doctrine: mcpgateway.DoctrineDefault,
	})
	if err == nil {
		t.Fatal("nil err with cancelled ctx")
	}
}

func TestDispatcherStat(t *testing.T) {
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit: mcpgateway.NopAuditEmitter(),
	})
	cur, q := d.Stat()
	if cur != 0 || q != 0 {
		t.Errorf("empty Stat cur=%d q=%d; want 0,0", cur, q)
	}
}

func TestDispatcherNilAuditNormalises(t *testing.T) {

	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{Audit: nil})
	if d == nil {
		t.Fatal("nil Dispatcher")
	}
}

func TestDispatcherDispatchHandlerDeepStackPanicTruncated(t *testing.T) {

	var deepPanic func(int)
	deepPanic = func(n int) {
		if n <= 0 {
			panic("deep")
		}
		deepPanic(n - 1)
	}
	h := func(_ context.Context, _ mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
		deepPanic(200)
		return mcpgateway.CallResponse{}, nil
	}
	sub := &fakeSubsystem{name: "audit", tools: []mcpgateway.ToolEntry{
		entryFor(t, "audit", "emit", h),
	}}
	audit := &recordingAudit{}
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{Audit: audit})
	if err := d.RegisterSubsystem(sub); err != nil {
		t.Fatalf("RegisterSubsystem: %v", err)
	}
	_, err := d.Dispatch(context.Background(), mcpgateway.CallRequest{
		Tool:     mcpgateway.MustToolName("audit", "emit"),
		Args:     map[string]any{},
		Doctrine: mcpgateway.DoctrineDefault,
	})
	if err == nil {
		t.Fatal("nil err on deep panic")
	}
	foundPanic := false
	for _, e := range audit.events {
		if e == "HandlerPanic" {
			foundPanic = true
		}
	}
	if !foundPanic {
		t.Errorf("audit events = %v; missing HandlerPanic", audit.events)
	}
}

func TestDispatcherHandlerReturnsError(t *testing.T) {

	h := func(_ context.Context, _ mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
		return mcpgateway.CallResponse{}, errors.New("handler boom")
	}
	sub := &fakeSubsystem{name: "audit", tools: []mcpgateway.ToolEntry{
		entryFor(t, "audit", "emit", h),
	}}
	audit := &recordingAudit{}
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{Audit: audit})
	if err := d.RegisterSubsystem(sub); err != nil {
		t.Fatalf("RegisterSubsystem: %v", err)
	}
	_, err := d.Dispatch(context.Background(), mcpgateway.CallRequest{
		Tool:     mcpgateway.MustToolName("audit", "emit"),
		Args:     map[string]any{},
		Doctrine: mcpgateway.DoctrineDefault,
	})
	if err == nil {
		t.Fatal("Dispatch on handler error: nil err")
	}

	for _, e := range audit.events {
		if e == "HandlerPanic" {
			t.Errorf("unexpected HandlerPanic audit on non-panic error")
		}
	}
}
