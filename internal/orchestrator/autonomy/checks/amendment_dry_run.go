// SPDX-License-Identifier: MIT
package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
)

type amendmentDryRunApproved struct{ d Deps }

func NewAmendmentDryRunApproved(d Deps) autonomy.Check { return &amendmentDryRunApproved{d: d} }

func (c *amendmentDryRunApproved) Name() string { return autonomy.CheckAmendmentDryRunApproved }

func (c *amendmentDryRunApproved) Run(_ context.Context, env autonomy.CheckEnv) (autonomy.CheckStatus, string, error) {
	if c.d.Read == nil || strings.TrimSpace(c.d.Paths.AmendmentDryRunLog) == "" {
		return autonomy.CheckSkip, "amendment dry-run log not configured", nil
	}
	min := amendmentDryRunMin(env.Doctrine)
	raw, err := c.d.Read.ReadFile(c.d.Paths.AmendmentDryRunLog)
	if err != nil {
		return autonomy.CheckFail, "amendment dry-run log read: " + err.Error(), nil
	}
	var entries []struct {
		Approved bool `json:"approved"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return autonomy.CheckFail, "amendment dry-run log parse: " + err.Error(), nil
	}
	approved := 0
	for _, e := range entries {
		if e.Approved {
			approved++
		}
	}
	if approved < min {
		return autonomy.CheckFail, fmt.Sprintf("amendment dry-runs approved: have %d, need >= %d", approved, min), nil
	}
	return autonomy.CheckPass, "", nil
}

func amendmentDryRunMin(doctrine string) int {
	switch strings.ToLower(strings.TrimSpace(doctrine)) {
	case "max-scope":
		return 1
	case "capa-firewall":
		return 3
	default:
		return 0
	}
}
