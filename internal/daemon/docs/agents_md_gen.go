// SPDX-License-Identifier: MIT
// Package docs implements the documentation system: generating project instructions
// from project instructions, scaffolding llms.txt, OpenSpec templates, drift checks.
package docs

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type AgentsMDInput struct {
	ProjectName string
	Doctrine    string
	CLAUDEMD    string
	MemoryDir   string
}

func GenerateAgentsMD(in AgentsMDInput) (string, error) {
	return "", zerrors.ErrNotImplementedPlan14
}
