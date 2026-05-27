// SPDX-License-Identifier: MIT
package checks

import (
	"context"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
)

type adrsValid struct{ d Deps }

func NewADRsValid(d Deps) autonomy.Check { return &adrsValid{d: d} }

func (c *adrsValid) Name() string { return autonomy.CheckADRsValid }

func (c *adrsValid) Run(ctx context.Context, _ autonomy.CheckEnv) (autonomy.CheckStatus, string, error) {
	if c.d.Exec == nil {
		return autonomy.CheckSkip, "adrs validator (Execer) not configured", nil
	}
	args := []string{"doctor", "adrs", "--strict"}
	if dir := strings.TrimSpace(c.d.Paths.ADRsDir); dir != "" {
		args = append(args, dir)
	}
	stdout, code, err := c.d.Exec.Run(ctx, "hades", args...)
	if err != nil {
		return autonomy.CheckFail, "adrs validator exec: " + err.Error(), nil
	}
	if code != 0 {
		reason := truncate(strings.TrimSpace(stdout), 200)
		if reason == "" {
			reason = "adrs validator exited with non-zero status"
		}
		return autonomy.CheckFail, reason, nil
	}
	return autonomy.CheckPass, "", nil
}
