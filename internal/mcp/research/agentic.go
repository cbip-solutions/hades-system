// SPDX-License-Identifier: MIT
// agentic.go — Q4 C agentic_deep wrapper.
//
// The agentic_deep wrapper iterates the deterministic dispatch
// (Q4 C core) with LLM-driven gap detection between rounds:
//
//  1. Run dispatch(query) → raw findings + verified citations.
//  2. Pass findings to Synthesizer with profile=research-gap-detection
//     and ask "is there a gap? if yes, what followup_query?".
//  3. If gap detected and budget allows and max_iter not exceeded,
//     goto 1 with the followup query.
//  4. Otherwise terminate.
//
// Three terminate conditions (any one):
//   - Saturation: new_findings_ratio < 0.1 (less than 10% of results
//     in the new round are URLs we hadn't seen).
//   - Budget exhaustion: BudgetClient.PreCall denies.
//   - Max-iter exceeded.
//
// The wrapper builds on top of the deterministic Dispatcher; it does
// not call backends directly. This keeps the agentic refinement layer
// composable and testable without re-implementing the dispatch core.
package research

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var ErrBudgetExhausted = errors.New("agentic: budget exhausted")

type AgenticOptions struct {
	Dispatcher Dispatcher

	Synthesizer Synthesizer

	Budget BudgetClient

	MaxIter int

	SaturationThreshold float64
}

type Agentic struct {
	opts AgenticOptions
}

func NewAgentic(opts AgenticOptions) *Agentic {
	if opts.MaxIter == 0 {
		opts.MaxIter = 5
	}
	if opts.SaturationThreshold == 0 {
		opts.SaturationThreshold = 0.1
	}
	return &Agentic{opts: opts}
}

func (a *Agentic) Run(ctx context.Context, initialQuery string) (DispatchResult, error) {
	if a.opts.Dispatcher == nil {
		return DispatchResult{}, errors.New("agentic: nil Dispatcher")
	}
	if strings.TrimSpace(initialQuery) == "" {
		return DispatchResult{}, errors.New("agentic: empty initial query")
	}

	maxIter := a.opts.MaxIter
	if maxIter <= 0 {
		maxIter = 5
	}

	currentQuery := initialQuery
	mergedFindings := make([]SourceHit, 0)
	mergedCitations := make([]VerifiedCitation, 0)
	seenURLs := make(map[string]struct{})
	iterations := 0

	for i := 0; i < maxIter; i++ {

		if a.opts.Budget != nil {
			allowed, _, err := a.opts.Budget.PreCall(ctx, "stage", "research:agentic_deep", 0.50)
			if err != nil {
				return DispatchResult{
					Findings: mergedFindings, Citations: mergedCitations, Iterations: iterations,
				}, fmt.Errorf("agentic: budget pre-check: %w", err)
			}
			if !allowed {

				if iterations == 0 {
					return DispatchResult{}, ErrBudgetExhausted
				}
				break
			}
		}

		res, err := a.opts.Dispatcher.Dispatch(ctx, DispatchQuery{
			Query:         currentQuery,
			IsAgenticDeep: true,
		})
		if err != nil {
			if iterations == 0 {
				return DispatchResult{}, err
			}

			break
		}
		iterations++

		newCount := 0
		for _, f := range res.Findings {
			key := canonicalURL(f.URL)
			if key == "" {
				continue
			}
			if _, ok := seenURLs[key]; !ok {
				seenURLs[key] = struct{}{}
				newCount++
				mergedFindings = append(mergedFindings, f)
			}
		}
		mergedCitations = append(mergedCitations, res.Citations...)

		if len(res.Findings) > 0 {
			ratio := float64(newCount) / float64(len(res.Findings))
			if ratio < a.opts.SaturationThreshold && iterations > 1 {

				break
			}
		} else if iterations > 1 {

			break
		}

		if a.opts.Synthesizer == nil {
			break
		}
		followup, hasGap := a.detectGap(ctx, currentQuery, res.Findings)
		if !hasGap || followup == "" {
			break
		}
		currentQuery = followup
	}

	return DispatchResult{
		Findings:   mergedFindings,
		Citations:  mergedCitations,
		Iterations: iterations,
	}, nil
}

func (a *Agentic) detectGap(ctx context.Context, query string, findings []SourceHit) (string, bool) {
	if a.opts.Synthesizer == nil {
		return "", false
	}
	prompt := "You are a research gap detector. Given the original query and the current findings (JSON array), decide if there is a gap. Return a JSON envelope: " +
		"`{\"gap_detected\":true,\"followup_query\":\"...\"}` if you detect a gap; " +
		"`{\"gap_detected\":false}` otherwise. Original query: " + query

	rawFindings := make([]any, 0, len(findings))
	for _, f := range findings {
		rawFindings = append(rawFindings, f)
	}
	out, err := a.opts.Synthesizer.Synthesize(ctx, SynthesizeInput{
		Prompt:      prompt,
		RawFindings: rawFindings,
	})
	if err != nil {
		return "", false
	}

	var env struct {
		GapDetected   bool   `json:"gap_detected"`
		FollowupQuery string `json:"followup_query"`
	}
	if err := json.Unmarshal([]byte(out.Report), &env); err != nil {
		return "", false
	}
	return env.FollowupQuery, env.GapDetected
}
