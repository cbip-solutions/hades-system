// SPDX-License-Identifier: MIT
// Package trace renders feature execution timelines (`hades trace <feature>`).
// Reads from SQLite events, llm_calls, decisions, doc_versions tables.
package trace

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type Timeline struct {
	Project string
	Feature string
	Items   []Item
}

type Item struct {
	TS      int64
	Type    string
	Summary string
	CostUSD float64
}

func Build(project, feature string) (*Timeline, error) {
	return nil, zerrors.ErrNotImplementedPlan9
}

func (t *Timeline) RenderText() string {
	return ""
}
