// SPDX-License-Identifier: MIT
package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
)

type ciConsecutiveGreen struct{ d Deps }

func NewCIConsecutiveGreen(d Deps) autonomy.Check { return &ciConsecutiveGreen{d: d} }

func (c *ciConsecutiveGreen) Name() string { return autonomy.CheckCIConsecutiveGreen }

func (c *ciConsecutiveGreen) Run(_ context.Context, env autonomy.CheckEnv) (autonomy.CheckStatus, string, error) {
	if c.d.Read == nil || strings.TrimSpace(c.d.Paths.PlansStatusLog) == "" {
		return autonomy.CheckSkip, "plans status log not configured", nil
	}
	min := ciConsecutiveMin(env.Doctrine)
	raw, err := c.d.Read.ReadFile(c.d.Paths.PlansStatusLog)
	if err != nil {
		return autonomy.CheckFail, "ci status log read: " + err.Error(), nil
	}
	var f plansStatusFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return autonomy.CheckFail, "ci status log parse: " + err.Error(), nil
	}
	if f.CIConsecutiveGreen < min {
		return autonomy.CheckFail, fmt.Sprintf("ci consecutive green: have %d, need >= %d", f.CIConsecutiveGreen, min), nil
	}
	return autonomy.CheckPass, "", nil
}

func ciConsecutiveMin(doctrine string) int {
	switch strings.ToLower(strings.TrimSpace(doctrine)) {
	case "max-scope", "capa-firewall":
		return 30
	default:
		return 10
	}
}
