// SPDX-License-Identifier: MIT
package walkers

import (
	"context"
	"encoding/json"
	"os"
)

type ADRResult struct {
	Count          int
	Location       string
	MissingSources []string
}

type ADRWalker struct {
	indexPath string
}

func NewADRWalker(indexPath string) *ADRWalker { return &ADRWalker{indexPath: indexPath} }

type adrIndexEnvelope struct {
	ADRs []struct {
		ID string `json:"id"`
	} `json:"adrs"`

	Entries []struct {
		ID string `json:"id"`
	} `json:"entries"`

	SchemaVersion int    `json:"schema_version"`
	GeneratedAt   string `json:"generated_at"`
}

func (w *ADRWalker) Walk(_ context.Context) (ADRResult, error) {
	res := ADRResult{Location: "docs/decisions/"}

	body, err := os.ReadFile(w.indexPath)
	if err != nil {
		res.MissingSources = append(res.MissingSources, "_index.json")
		return res, nil
	}
	var env adrIndexEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		res.MissingSources = append(res.MissingSources, "_index.json")
		return res, nil
	}

	switch {
	case len(env.Entries) > 0:
		res.Count = len(env.Entries)
	case len(env.ADRs) > 0:
		res.Count = len(env.ADRs)
	default:

		res.Count = 0
	}
	return res, nil
}
