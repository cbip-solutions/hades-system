// SPDX-License-Identifier: MIT
// Package openapi — release OpenAPI breaking-change detector wrapping
// oasdiff/oasdiff (Go SDK; Apache-2.0). The sole library used here per
// invariant; the imports scan asserts no other diff library is imported
// in this file.
//
// AS-BUILT divergence from the plan: the upstream Go module path
// for oasdiff is github.com/oasdiff/oasdiff (renamed from tufin/oasdiff;
// see chore(go) commit body). The plan cited the tufin path; everything
// else is unchanged.
//
// # Pipeline
//
// 1. Size-gate: oldSpec + newSpec ≤ Params.MaxSpecBytes (default 5 MiB);
// exceed → ErrSpecTooLarge BEFORE any parse (defense-in-depth).
// 2. Parse: openapi3.NewLoader().LoadFromData(spec) → *openapi3.T;
// failure → ErrInvalidSpec wrapped (errors.Is gate at call sites).
// 3. Diff: diff.Get(diff.NewConfig(), oldDoc, newDoc) → *diff.Diff.
// 4. Classify: checker.CheckBackwardCompatibility(...) → checker.Changes;
// map checker.Level → bcdetect.Severity per the four-value enum:
// ERR (3) → SevBreaking
// WARN (2) → SevDangerous
// INFO (1) → SevNonBreaking
// NONE / INVALID → skipped (defensive — should not appear from
// CheckBackwardCompatibility but the guard protects against upstream
// drift adding new Level values without surfacing as compile errors)
// 5. For each Change, emit one DiffResult with the change's Id (the Kind
// anchor used to identify the canonical rule, e.g.,
// "request-parameter-added-required") + the canonicalized Detail
// bytes (JSON of {id, operation, path, source, attributes}).
//
// Severity mapping rationale: oasdiff has a comprehensive 450+ rule
// classification; INSUFFICIENT (SevInsufficient) is never produced here
// (the Go path classifies every change), so the Node fallback gate
// (invariant) never fires for OpenAPI — only for GraphQL when the
// Go SDL diff hits a rule outside the canonical six.
//
// Concurrency Detect is safe to call concurrently from multiple goroutines
// on the same *OpenAPIDetector instance — the underlying openapi3.Loader
// + diff.Get + checker.CheckBackwardCompatibility allocate fresh state per
// call.
package openapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/oasdiff/oasdiff/checker"
	"github.com/oasdiff/oasdiff/diff"

	br "github.com/cbip-solutions/hades-system/internal/caronte/contract/bcdetect"
)

type OpenAPIDetector struct {
	params br.Params
}

func NewOpenAPIDetector(p br.Params) *OpenAPIDetector {
	return &OpenAPIDetector{params: p}
}

func (d *OpenAPIDetector) DetectorID() string { return "oasdiff" }

func (d *OpenAPIDetector) Detect(ctx context.Context, oldSpec, newSpec []byte) ([]br.DiffResult, error) {
	if len(oldSpec) > d.params.MaxSpecBytes || len(newSpec) > d.params.MaxSpecBytes {
		return nil, br.ErrSpecTooLarge
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	oldDoc, err := openapi3.NewLoader().LoadFromData(oldSpec)
	if err != nil {
		return nil, fmt.Errorf("%w: load old: %v", br.ErrInvalidSpec, err)
	}
	newDoc, err := openapi3.NewLoader().LoadFromData(newSpec)
	if err != nil {
		return nil, fmt.Errorf("%w: load new: %v", br.ErrInvalidSpec, err)
	}

	cfg := diff.NewConfig()
	diffReport, err := diff.Get(cfg, oldDoc, newDoc)
	if err != nil {

		return nil, fmt.Errorf("oasdiff diff.Get: %w", err)
	}

	if diffReport == nil || diffReport.Empty() {
		return nil, nil
	}

	emptySources := diff.OperationsSourcesMap{}
	checkerCfg := checker.NewConfig(checker.GetAllChecks())
	changes := checker.CheckBackwardCompatibility(checkerCfg, diffReport, &emptySources)

	return translateChanges(changes), nil
}

func translateChanges(changes checker.Changes) []br.DiffResult {
	out := make([]br.DiffResult, 0, len(changes))
	for _, c := range changes {
		sev := translateLevel(c.GetLevel())
		if sev == "" {

			continue
		}
		detail, _ := json.Marshal(map[string]any{
			"id":         c.GetId(),
			"operation":  c.GetOperation(),
			"path":       c.GetPath(),
			"section":    c.GetSection(),
			"attributes": c.GetAttributes(),
		})
		out = append(out, br.DiffResult{
			DetectorID: "oasdiff",
			Kind:       c.GetId(),
			Severity:   sev,
			Detail:     detail,
		})
	}
	return out
}

func translateLevel(l checker.Level) br.Severity {
	switch l {
	case checker.ERR:
		return br.SevBreaking
	case checker.WARN:
		return br.SevDangerous
	case checker.INFO:
		return br.SevNonBreaking
	default:
		return ""
	}
}

var _ br.Detector = (*OpenAPIDetector)(nil)
