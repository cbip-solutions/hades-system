// SPDX-License-Identifier: MIT
// Package opencode wraps the OpenCode CLI for daemon-side spawning of
// subagents. Verified R1: `opencode run` and `opencode serve --port=N`
// are the supported entry points. Plan 5 implements; Plan 1 declares
// types + signatures.
package opencode

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type SpawnConfig struct {
	WorkdirPath  string
	AgentProfile string
	TaskPrompt   string
	Port         int
	AuthPassword string
}

type Subagent struct {
	PID       int
	Port      int
	Config    SpawnConfig
	StartedAt int64
}

func Spawn(cfg SpawnConfig) (*Subagent, error) {
	return nil, zerrors.ErrNotImplementedPlan5
}

func (s *Subagent) Stop() error {
	return zerrors.ErrNotImplementedPlan5
}

func (s *Subagent) Kill() error {
	return zerrors.ErrNotImplementedPlan5
}
