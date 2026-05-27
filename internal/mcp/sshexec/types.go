// SPDX-License-Identifier: MIT
// internal/mcp/sshexec/types.go
//
// Task L-3 — package-level value types. Pure values; no I/O,
// no mutation of global state, no goroutines.
//
// Boundary anchor: this file imports only stdlib.

package sshexec

import "time"

type StreamLabel string

const (
	StreamStdout StreamLabel = "stdout"

	StreamStderr StreamLabel = "stderr"
)

type ExecRequest struct {
	Host string `json:"host"`

	Command string `json:"cmd"`

	Cwd string `json:"cwd,omitempty"`

	Timeout time.Duration `json:"timeout"`

	Project string `json:"project"`

	MaxStdout int64 `json:"max_stdout,omitempty"`

	MaxStderr int64 `json:"max_stderr,omitempty"`
}

const (
	FloorTimeout = 60 * time.Second

	FloorMaxStdout int64 = 10 * 1024 * 1024

	FloorMaxStderr int64 = 1024 * 1024
)

func (r *ExecRequest) ApplyDefaults() {
	r.ApplyDefaultsFrom(nil)
}

func (r *ExecRequest) ApplyDefaultsFrom(d *Defaults) {
	if r.Timeout <= 0 {
		switch {
		case d != nil && d.Timeout > 0:
			r.Timeout = d.Timeout
		default:
			r.Timeout = FloorTimeout
		}
	}
	if r.MaxStdout <= 0 {
		switch {
		case d != nil && d.MaxStdout > 0:
			r.MaxStdout = d.MaxStdout
		default:
			r.MaxStdout = FloorMaxStdout
		}
	}
	if r.MaxStderr <= 0 {
		switch {
		case d != nil && d.MaxStderr > 0:
			r.MaxStderr = d.MaxStderr
		default:
			r.MaxStderr = FloorMaxStderr
		}
	}
}

type StreamChunk struct {
	Ordinal int64 `json:"ordinal"`

	Stream StreamLabel `json:"stream"`

	Data []byte `json:"data"`
}

type ExitReason string

const (
	ExitReasonNormal ExitReason = "normal"

	ExitReasonTimeout ExitReason = "timeout"

	ExitReasonTransport ExitReason = "transport_error"

	ExitReasonInteractiveBlocked ExitReason = "interactive_blocked"
)

type ExecResult struct {
	ExitCode int `json:"exit_code"`

	ExitReason ExitReason `json:"exit_reason"`

	StdoutBytes int64 `json:"stdout_bytes"`

	StderrBytes int64 `json:"stderr_bytes"`

	StdoutTruncated bool `json:"stdout_truncated"`

	StderrTruncated bool `json:"stderr_truncated"`

	Duration time.Duration `json:"duration"`

	InteractiveBlocked bool `json:"interactive_blocked"`

	BlockedReason string `json:"blocked_reason,omitempty"`
}

type ListAllowedResult struct {
	Project string `json:"project"`

	Patterns []string `json:"patterns"`

	Hosts []string `json:"hosts"`

	Source string `json:"source"`
}
