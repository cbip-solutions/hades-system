// SPDX-License-Identifier: MIT
package checks

import (
	"context"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
)

type researchMCPUp struct{ d Deps }

func NewResearchMCPUp(d Deps) autonomy.Check { return &researchMCPUp{d: d} }

func (c *researchMCPUp) Name() string { return autonomy.CheckResearchMCPUp }

func (c *researchMCPUp) Run(ctx context.Context, _ autonomy.CheckEnv) (autonomy.CheckStatus, string, error) {
	_, reason, skip := httpHealthProbe(ctx, "research_mcp_up", c.d.URLs.ResearchMCP, c.d)
	if skip {
		return autonomy.CheckSkip, reason, nil
	}
	if reason != "" {
		return autonomy.CheckFail, reason, nil
	}
	return autonomy.CheckPass, "", nil
}
