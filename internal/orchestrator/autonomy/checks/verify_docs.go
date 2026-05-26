// SPDX-License-Identifier: MIT
package checks

import (
	"context"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
)

type verifyDocs struct{ d Deps }

func NewVerifyDocs(d Deps) autonomy.Check { return &verifyDocs{d: d} }

func (c *verifyDocs) Name() string { return autonomy.CheckVerifyDocs }

func (c *verifyDocs) Run(ctx context.Context, _ autonomy.CheckEnv) (autonomy.CheckStatus, string, error) {
	if c.d.Exec == nil {
		return autonomy.CheckSkip, "verify-docs (Execer) not configured", nil
	}
	stdout, code, err := c.d.Exec.Run(ctx, "zen", "doctor", "verify-docs")
	if err != nil {
		return autonomy.CheckFail, "verify-docs exec: " + err.Error(), nil
	}
	if code != 0 {
		reason := truncate(strings.TrimSpace(stdout), 200)
		if reason == "" {
			reason = "verify-docs exited with non-zero status"
		}
		return autonomy.CheckFail, reason, nil
	}
	return autonomy.CheckPass, "", nil
}
