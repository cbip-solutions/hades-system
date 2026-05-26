// SPDX-License-Identifier: MIT
package walkers

import (
	"context"
	"encoding/json"
	"os"
	"time"
)

type AutonomyResult struct {
	PrerequisitesMet bool

	LastCheck      time.Time
	MissingSources []string
}

type AutonomyWalker struct {
	stampPath string
}

func NewAutonomyWalker(stampPath string) *AutonomyWalker {
	return &AutonomyWalker{stampPath: stampPath}
}

type autonomyStamp struct {
	PrerequisitesMet bool   `json:"prerequisites_met"`
	LastCheckAt      string `json:"last_check_at"`
}

func (w *AutonomyWalker) Walk(_ context.Context) (AutonomyResult, error) {
	res := AutonomyResult{}
	body, err := os.ReadFile(w.stampPath)
	if err != nil {
		res.MissingSources = append(res.MissingSources, "autonomy-stamp")
		return res, nil
	}
	var s autonomyStamp
	if err := json.Unmarshal(body, &s); err != nil {
		res.MissingSources = append(res.MissingSources, "autonomy-stamp")
		return res, nil
	}
	res.PrerequisitesMet = s.PrerequisitesMet
	if s.LastCheckAt != "" {
		t, err := time.Parse(time.RFC3339, s.LastCheckAt)
		if err == nil {
			res.LastCheck = t.UTC()
		}
	}
	return res, nil
}
