// SPDX-License-Identifier: MIT
// internal/mcp/sshexec/emit.go
//
// Task L-8 — sshexec audit emitter.
//
// Wraps the *client.EmitClient with sshexec-specific event types
// and structured payload schema. Implements the AuditEmitter interface
// consumed by exec.Run.
//
// invariant (no-loss): every emit attempt has two paths inside the
// underlying EmitClient:
//
// 1. POST <daemon>/v1/audit/emit (3 retries built into client.Do)
// 2. on 5xx / dial failure: append JSONL line to the buffer file
// <bufDir>/hades-mcp-ssh-exec-emit-buffer-<pid>.jsonl
//
// EmitClient does NOT return an error when the daemon path
// fails — it falls back to the buffer file silently (so callers in the
// MCP tool path never block). When BOTH paths fail, the EmitClient
// writes a stderr log line and returns nil — events are NEVER silently
// dropped (invariant satisfied; the stderr line is the audit trail of
// the trail).
//
// Emitter is a thin typed wrapper: build the JSON payload for
// each ssh_exec.* event type, hand it to EmitClient.Emit, surface
// nothing else.
//
// Boundary (invariant): imports only stdlib + internal/mcp/client.

package sshexec

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/cbip-solutions/hades-system/internal/mcp/client"
)

type Emitter struct {
	ec        *client.EmitClient
	projectID string
}

func NewEmitter(ec *client.EmitClient, projectID string) *Emitter {
	return &Emitter{ec: ec, projectID: projectID}
}

func (e *Emitter) EmitStarted(req ExecRequest) error {
	return e.emit("ssh_exec.started", map[string]any{
		"host":        req.Host,
		"cmd_preview": preview(req.Command, 80),
		"project":     req.Project,
		"timeout_ms":  req.Timeout.Milliseconds(),
	})
}

func (e *Emitter) EmitCompleted(req ExecRequest, res ExecResult) error {
	return e.emit("ssh_exec.completed", map[string]any{
		"host":             req.Host,
		"cmd_preview":      preview(req.Command, 80),
		"project":          req.Project,
		"exit_code":        res.ExitCode,
		"exit_reason":      string(res.ExitReason),
		"duration_ms":      res.Duration.Milliseconds(),
		"stdout_bytes":     res.StdoutBytes,
		"stderr_bytes":     res.StderrBytes,
		"stdout_truncated": res.StdoutTruncated,
		"stderr_truncated": res.StderrTruncated,
	})
}

func (e *Emitter) EmitDenied(req ExecRequest, reason string) error {
	return e.emit("ssh_exec.denied", map[string]any{
		"host":        req.Host,
		"cmd_preview": preview(req.Command, 80),
		"project":     req.Project,
		"reason":      reason,
	})
}

func (e *Emitter) EmitInteractiveBlocked(req ExecRequest, snippet []byte) error {
	return e.emit("ssh_exec.interactive_blocked", map[string]any{
		"host":                    req.Host,
		"cmd_preview":             preview(req.Command, 80),
		"project":                 req.Project,
		"interactive_snippet_b64": base64.StdEncoding.EncodeToString(snippet),
	})
}

func (e *Emitter) DrainBuffer(ctx context.Context) (int, error) {
	if e.ec == nil {
		return 0, nil
	}
	return e.ec.DrainBuffer(ctx)
}

func (e *Emitter) emit(eventType string, payload map[string]any) error {
	if e.ec == nil {
		return nil
	}
	body, err := json.Marshal(payload)
	if err != nil {

		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return e.ec.Emit(ctx, client.AuditEvent{
		ProjectID: e.projectID,
		Type:      eventType,
		Payload:   string(body),
	})
}

func preview(cmd string, n int) string {
	if len(cmd) <= n {
		return cmd
	}
	return cmd[:n] + "…"
}
