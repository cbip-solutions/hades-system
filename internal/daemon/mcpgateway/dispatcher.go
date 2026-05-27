// SPDX-License-Identifier: MIT
// internal/daemon/mcpgateway/dispatcher.go
//
// Dispatcher — multiplexes the 5 internal Go MCPs + the in-process caronte
// code-graph engine. The single entry point Hermes uses for tool dispatch (via
// server.go A-6 HTTP wrapper). Built on top of ToolRegistry (A-2) +
// RBAC (A-3); subsystem-agnostic via the Subsystem interface.
//
// Routing flow per CallRequest:
// 1. RBAC.Check (doctrine filter → ACL → concurrency gate)
// 2. ToolRegistry.Lookup (handler resolution)
// 3. recover-wrapped Handler call
// 4. Audit emit (ToolDispatched / HandlerPanic)
// 5. Release concurrency slot via deferred RBAC release()
//
// Subsystem.Name() identifies the downstream for observability + audit
// payloads; the canonical tool name's subsystem segment is the source of
// truth for routing (a Subsystem is free to expose tools across subsystems
// in principle, though in practice each Subsystem owns one segment).
//
// wires the in-process MCPs as Subsystem instances via thin
// adapters lifted from each MCP package's existing Server.InvokeTool
// method (lifted at the boundary in main.go A-7). The caronte engine is
// wired the same way via CaronteProxy ( ; the engine's Close
// is deferred at the daemon composition root, not in the Dispatcher).
package mcpgateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"time"
)

type Subsystem interface {
	Name() string

	Tools() []ToolEntry
}

type DispatcherConfig struct {
	Audit     AuditEmitter
	RBACCfg   RBACConfig
	Evaluator DoctrineEvaluator
}

type DoctrineEvaluator interface {
	EvaluateCall(ctx context.Context, mcpName, toolName string, params any) (decisionLabel string, evidence string, err error)
}

type Dispatcher struct {
	registry  *ToolRegistry
	rbac      *RBAC
	audit     AuditEmitter
	evaluator DoctrineEvaluator
}

func NewDispatcher(cfg DispatcherConfig) *Dispatcher {
	if cfg.Audit == nil {
		cfg.Audit = NopAuditEmitter()
	}
	reg := NewToolRegistry()
	return &Dispatcher{
		registry:  reg,
		rbac:      NewRBAC(reg, cfg.RBACCfg),
		audit:     cfg.Audit,
		evaluator: cfg.Evaluator,
	}
}

func (d *Dispatcher) RegisterSubsystem(s Subsystem) error {
	if s == nil {
		return errors.New("mcpgateway: RegisterSubsystem nil")
	}
	tools := s.Tools()
	for _, t := range tools {
		if t.Handler == nil {
			return fmt.Errorf("%w: subsystem %q tool %q has nil Handler",
				ErrToolNameInvalid, s.Name(), t.Name.String())
		}
		if err := d.registry.Register(t.Name, t.Handler, t.Meta); err != nil {
			return fmt.Errorf("subsystem %q: %w", s.Name(), err)
		}
	}
	return nil
}

func (d *Dispatcher) Dispatch(ctx context.Context, req CallRequest) (CallResponse, error) {

	if !d.registry.Has(req.Tool) {
		return CallResponse{}, fmt.Errorf("%w: %s", ErrToolNotRegistered, req.Tool.String())
	}

	if d.evaluator != nil {
		decisionLabel, evidence, evalErr := d.evaluator.EvaluateCall(
			ctx,
			req.Tool.Subsystem(),
			req.Tool.Tool(),
			anyOrEmptyMap(req.Args),
		)
		_ = evalErr // emit-failure is best-effort; do not block call
		switch decisionLabel {
		case "deny":
			return CallResponse{}, fmt.Errorf("%w: %s; evidence: %s",
				ErrDoctrineDeny, req.Tool.String(), evidence)
		case "allow_with_confirm":
			return CallResponse{}, fmt.Errorf("%w: %s; evidence: %s",
				ErrDoctrineConfirmRequired, req.Tool.String(), evidence)
		case "allow", "allow_with_audit":

		default:

		}
	}

	release, err := d.rbac.Check(ctx, req)
	if err != nil {
		return CallResponse{}, err
	}
	defer release()

	entry, _ := d.registry.Lookup(req.Tool)

	resp, callErr := d.callHandler(ctx, entry, req)
	if callErr != nil {
		return resp, callErr
	}

	d.emitToolDispatched(req, resp)
	return resp, nil
}

func anyOrEmptyMap(args map[string]any) any {
	if args == nil {
		return nil
	}
	return args
}

func (d *Dispatcher) callHandler(ctx context.Context, entry ToolEntry, req CallRequest) (resp CallResponse, err error) {
	started := time.Now()
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			d.emitHandlerPanic(req, r, stack)
			err = fmt.Errorf("mcpgateway: handler panic for %s: %v",
				req.Tool.String(), r)
		}
	}()
	resp, err = entry.Handler(ctx, req)
	if resp.Latency == 0 {
		resp.Latency = time.Since(started)
	}
	return resp, err
}

func (d *Dispatcher) ListTools() []ToolEntry {
	return d.registry.List()
}

func (d *Dispatcher) Stat() (current, queued int) {
	return d.rbac.Stat()
}

func (d *Dispatcher) Close() error {
	return nil
}

func (d *Dispatcher) emitToolDispatched(req CallRequest, resp CallResponse) {
	payload, _ := json.Marshal(map[string]any{
		"tool":       req.Tool.String(),
		"subsystem":  req.Tool.Subsystem(),
		"doctrine":   string(req.Doctrine.Resolved()),
		"mode":       req.Mode.String(),
		"session_id": req.SessionID,
		"project_id": req.ProjectID,
		"latency_ms": resp.Latency.Milliseconds(),
		"is_error":   resp.IsError,
	})
	d.audit.Emit("ToolDispatched", payload)
}

func (d *Dispatcher) emitHandlerPanic(req CallRequest, recovered any, stack []byte) {
	const stackCap = 4 * 1024
	if len(stack) > stackCap {
		stack = stack[:stackCap]
	}
	payload, _ := json.Marshal(map[string]any{
		"tool":       req.Tool.String(),
		"recovered":  fmt.Sprintf("%v", recovered),
		"stack":      string(stack),
		"session_id": req.SessionID,
		"project_id": req.ProjectID,
	})
	d.audit.Emit("HandlerPanic", payload)
}
