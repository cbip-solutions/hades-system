// SPDX-License-Identifier: MIT
// Package augment — cache_split.go divides post-summarization output into
// the static (Anthropic-cacheable) + volatile (NEVER cached) portions.
//
// Anthropic prompt cache (per Anthropic Engineering blog 2024):
// - cache_control: {"type": "ephemeral"} on a system-prompt block triggers
// 5-minute cache TTL.
// - Static portion = community summaries + project meta (stable across queries).
// - Volatile portion = per-query fused results + callers/callees (DIFFERENT
// per query; never cacheable).
//
// takes callers + callees as explicit per-call parameters. Pre-fix the
// CacheSplit struct held callers/callees on shared state mutated by an
// (unused) WithCallersCallees builder method — a concurrent-Run race
// hazard for any future caller that adopted the builder. The fix is
// idempotent + lock-free + makes the per-call lifecycle explicit.
package augment

func NewCacheSplit() *CacheSplit {
	return &CacheSplit{}
}

func (cs *CacheSplit) Split(summaries []CommunitySummary, fused []RRFFusedResult, meta ProjectMeta, callers, callees []string) (StaticContext, VolatileContext) {
	staticCtx := StaticContext{
		ProjectMeta:        meta,
		CommunitySummaries: append([]CommunitySummary(nil), summaries...),
	}
	staticCtx.EstimatedTokens = estimateStaticTokens(meta, summaries)

	volatileCtx := VolatileContext{
		FusedResults: append([]RRFFusedResult(nil), fused...),
	}
	if len(callers) > 0 {
		volatileCtx.Callers = append([]string(nil), callers...)
	}
	if len(callees) > 0 {
		volatileCtx.Callees = append([]string(nil), callees...)
	}
	volatileCtx.EstimatedTokens = estimateVolatileTokens(fused, callers, callees)

	return staticCtx, volatileCtx
}

func estimateStaticTokens(meta ProjectMeta, summaries []CommunitySummary) int {
	chars := 30
	chars += len(meta.ProjectID) + len(meta.Doctrine) + len(meta.Stage)
	for _, s := range summaries {
		chars += s.TokenCount * 4
		chars += 20
	}
	return chars / 4
}

func estimateVolatileTokens(fused []RRFFusedResult, callers, callees []string) int {
	chars := 0
	for _, f := range fused {
		chars += len(f.Title) + len(f.Snippet) + 30
	}
	for _, c := range callers {
		chars += len(c) + 5
	}
	for _, c := range callees {
		chars += len(c) + 5
	}
	return chars / 4
}
