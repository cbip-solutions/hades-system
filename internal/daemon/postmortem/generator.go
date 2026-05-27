// SPDX-License-Identifier: MIT
// Package postmortem generates structured postmortems for failed swarms
// (spec §9.4). implements; declares types.
package postmortem

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type Postmortem struct {
	Project     string
	SwarmID     string
	Outcome     string
	Timeline    []Event
	RootCause   string
	Suggestions []string
}

type Event struct {
	TS      int64
	Phase   string
	Message string
}

func Generate(swarmID string) (*Postmortem, error) {
	return nil, zerrors.ErrNotImplementedPlan11
}

func (p *Postmortem) RenderMarkdown() string {
	return ""
}

func (p *Postmortem) SaveTo(repoPath string) (string, error) {
	return "", zerrors.ErrNotImplementedPlan11
}
