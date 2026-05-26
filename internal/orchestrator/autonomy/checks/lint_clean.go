// SPDX-License-Identifier: MIT
package checks

import (
	"context"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
)

type lintClean struct{ d Deps }

func NewLintClean(d Deps) autonomy.Check { return &lintClean{d: d} }

func (c *lintClean) Name() string { return autonomy.CheckLintClean }

func (c *lintClean) Run(ctx context.Context, _ autonomy.CheckEnv) (autonomy.CheckStatus, string, error) {
	if c.d.Exec == nil {
		return autonomy.CheckSkip, "lint runner (Execer) not configured", nil
	}
	stdout, code, err := c.d.Exec.Run(ctx, "make", "lint", "-q")
	if err != nil {
		return autonomy.CheckFail, "make lint exec: " + err.Error(), nil
	}
	if code != 0 {
		reason := truncate(strings.TrimSpace(stdout), 200)
		if reason == "" {
			reason = "make lint exited with non-zero status"
		}
		return autonomy.CheckFail, reason, nil
	}
	return autonomy.CheckPass, "", nil
}
