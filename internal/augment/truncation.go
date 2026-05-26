// SPDX-License-Identifier: MIT
// Package augment — truncation.go ships the graceful_truncate guard.
//
// Q8=C on_timeout = "graceful_truncate" (constant; not doctrine-tunable).
// When static + volatile token estimate exceeds doctrine.augmentation.max_kg_tokens,
// the guard returns a partial summary rather than fail-loud.
//
// Truncation strategy (load-bearing ordering):
//   1. Drop volatile FusedResults from tail (lowest RRF score first).
//   2. Trim CommunitySummaries' Symbols + Files lists to top-N per cluster.
//   3. Drop CommunitySummaries from tail (lowest aggregate score first).
//   4. ALWAYS keep ProjectMeta (minimum viable static portion).

package augment

import "context"

const MaxSymbolsPerSummary = 5

func (g *Truncation) Apply(_ context.Context, staticCtx StaticContext, volatileCtx VolatileContext, maxTokens int) (StaticContext, VolatileContext, bool) {
	current := staticCtx.EstimatedTokens + volatileCtx.EstimatedTokens
	if current <= maxTokens && maxTokens > 0 {
		return staticCtx, volatileCtx, false
	}

	truncated := false

	newStatic := StaticContext{
		ProjectMeta:        staticCtx.ProjectMeta,
		CommunitySummaries: append([]CommunitySummary(nil), staticCtx.CommunitySummaries...),
		EstimatedTokens:    staticCtx.EstimatedTokens,
	}
	newVolatile := VolatileContext{
		FusedResults:    append([]RRFFusedResult(nil), volatileCtx.FusedResults...),
		Callers:         append([]string(nil), volatileCtx.Callers...),
		Callees:         append([]string(nil), volatileCtx.Callees...),
		EstimatedTokens: volatileCtx.EstimatedTokens,
	}

	if maxTokens <= 0 {
		if len(newStatic.CommunitySummaries) > 0 {
			newStatic.CommunitySummaries = nil
			truncated = true
		}
		if len(newVolatile.FusedResults) > 0 || len(newVolatile.Callers) > 0 || len(newVolatile.Callees) > 0 {
			newVolatile.FusedResults = nil
			newVolatile.Callers = nil
			newVolatile.Callees = nil
			truncated = true
		}
		newStatic.EstimatedTokens = estimateStaticTokens(newStatic.ProjectMeta, nil)
		newVolatile.EstimatedTokens = 0
		return newStatic, newVolatile, truncated
	}

	for newStatic.EstimatedTokens+newVolatile.EstimatedTokens > maxTokens && len(newVolatile.FusedResults) > 0 {
		newVolatile.FusedResults = newVolatile.FusedResults[:len(newVolatile.FusedResults)-1]
		newVolatile.EstimatedTokens = estimateVolatileTokens(newVolatile.FusedResults, newVolatile.Callers, newVolatile.Callees)
		truncated = true
	}

	if len(newVolatile.FusedResults) == 0 && newStatic.EstimatedTokens > maxTokens {
		if len(newVolatile.Callers) > 0 {
			newVolatile.Callers = nil
			truncated = true
		}
		if len(newVolatile.Callees) > 0 {
			newVolatile.Callees = nil
			truncated = true
		}
		newVolatile.EstimatedTokens = 0
	}

	if newStatic.EstimatedTokens+newVolatile.EstimatedTokens > maxTokens {
		didTrim := false
		for i := range newStatic.CommunitySummaries {
			s := &newStatic.CommunitySummaries[i]
			if len(s.Symbols) > MaxSymbolsPerSummary {
				s.Symbols = s.Symbols[:MaxSymbolsPerSummary]
				truncated = true
				didTrim = true
			}
			if len(s.Files) > MaxSymbolsPerSummary {
				s.Files = s.Files[:MaxSymbolsPerSummary]
				truncated = true
				didTrim = true
			}
			if didTrim {
				s.TokenCount = estimateSummaryTokens(*s)
			}
		}
		if didTrim {
			newStatic.EstimatedTokens = estimateStaticTokens(newStatic.ProjectMeta, newStatic.CommunitySummaries)
		}
	}

	for newStatic.EstimatedTokens+newVolatile.EstimatedTokens > maxTokens && len(newStatic.CommunitySummaries) > 0 {
		newStatic.CommunitySummaries = newStatic.CommunitySummaries[:len(newStatic.CommunitySummaries)-1]
		newStatic.EstimatedTokens = estimateStaticTokens(newStatic.ProjectMeta, newStatic.CommunitySummaries)
		truncated = true
	}

	return newStatic, newVolatile, truncated
}

func estimateSummaryTokens(s CommunitySummary) int {
	chars := len(s.ClusterID) + len(s.Topic)
	for _, f := range s.Files {
		chars += len(f) + 1
	}
	for _, sym := range s.Symbols {
		chars += len(sym) + 1
	}
	tokens := chars / 4
	if tokens == 0 {
		return 1
	}
	return tokens
}
