// SPDX-License-Identifier: MIT
package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
)

type plans49Green struct{ d Deps }

func NewPlans49Green(d Deps) autonomy.Check { return &plans49Green{d: d} }

func (c *plans49Green) Name() string { return autonomy.CheckPlans49Green }

func (c *plans49Green) Run(_ context.Context, _ autonomy.CheckEnv) (autonomy.CheckStatus, string, error) {
	if c.d.Read == nil || strings.TrimSpace(c.d.Paths.PlansStatusLog) == "" {
		return autonomy.CheckSkip, "plans status log not configured", nil
	}
	status, reason, err := readPlansStatus(c.d)
	if err != "" {
		return autonomy.CheckFail, err, nil
	}
	_ = status
	if reason != "" {
		return autonomy.CheckFail, reason, nil
	}
	return autonomy.CheckPass, "", nil
}

type plansStatusFile struct {
	Plans []struct {
		Plan   int    `json:"plan"`
		Status string `json:"status"`
	} `json:"plans"`
	CIConsecutiveGreen int `json:"ci_consecutive_green"`
}

func readPlansStatus(d Deps) (plansStatusFile, string, string) {
	raw, err := d.Read.ReadFile(d.Paths.PlansStatusLog)
	if err != nil {
		return plansStatusFile{}, "", "plans status log read: " + err.Error()
	}
	var f plansStatusFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return plansStatusFile{}, "", "plans status log parse: " + err.Error()
	}
	seen := make(map[int]string, len(f.Plans))
	for _, p := range f.Plans {
		seen[p.Plan] = p.Status
	}
	var missing, notGreen []int
	for n := 4; n <= 9; n++ {
		s, ok := seen[n]
		if !ok {
			missing = append(missing, n)
			continue
		}
		if s != "green" {
			notGreen = append(notGreen, n)
		}
	}
	switch {
	case len(missing) > 0:
		return f, fmt.Sprintf("plans missing from status log: %v", missing), ""
	case len(notGreen) > 0:
		return f, fmt.Sprintf("plans not green: %v", notGreen), ""
	}
	return f, "", ""
}
