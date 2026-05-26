//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package semantic

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
)

// resolveMultiLangTail sends the bounded set of unresolved interfaces (no
// SCIP/heuristic implementation found) to the LLM via the C-2 single-egress
// seam — the SAME CaronteDispatcher Phase C declares (Profile=local-code →
// Ollama, §13, inv-zen-088/236). It records high-confidence disambiguations as
// ConfLLMHint implements edges. Returns the count written.
//
// REUSE (not re-declaration): CaronteDispatcher, DefaultLLMProfile,
// ErrNoDispatcher, unresolvedSite, llmTailRequest, tailResolution, and
// parseTailResolutions are all Phase C's (same package). This function is the
// multi-language analogue of Phase C's resolveTail (Go); it shares the seam +
// envelope shape, differing only in the prompt + that it emits implements edges
// (interface→concrete) rather than call edges.
//
// A nil dispatcher ⇒ ErrNoDispatcher (caller treats the tail as skipped,
// inv-zen-234). A dispatcher error (Ollama down) is returned; the caller
// swallows it (degrade, do not block). NEVER dials a backend directly.
func (r *MultiLangResolver) resolveMultiLangTail(ctx context.Context, language string, unresolved []unresolvedSite) (int, error) {
	if r.dispatcher == nil {
		return 0, ErrNoDispatcher
	}
	if len(unresolved) == 0 {
		return 0, nil
	}
	body, err := json.Marshal(llmTailRequest{
		Task:  fmt.Sprintf("For each %s interface/trait with no statically-resolved implementation, name the concrete type(s) that implement it. High-confidence resolutions only.", language),
		Sites: unresolved,
	})
	if err != nil {
		return 0, fmt.Errorf("caronte/semantic: resolveMultiLangTail marshal: %w", err)
	}
	call := orchestrator.Call{
		Profile: DefaultLLMProfile,
		Method:  "POST",
		Path:    "/v1/messages",
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    body,
	}
	resp, err := r.dispatcher.Forward(ctx, call)
	if err != nil {
		return 0, fmt.Errorf("caronte/semantic: resolveMultiLangTail dispatch: %w", err)
	}
	resolutions := parseTailResolutions(resp.Body)
	written := 0
	for _, res := range resolutions {
		if res.FromID == "" || res.ToID == "" {
			continue
		}
		edge := store.Edge{
			SourceID:   res.FromID,
			TargetID:   res.ToID,
			Kind:       string(store.EdgeImplements),
			Confidence: store.ConfLLMHint,
			SiteFile:   res.SiteFile,
			SiteLine:   res.SiteLine,
		}
		if err := r.store.UpsertEdge(ctx, edge); err != nil {
			return written, fmt.Errorf("caronte/semantic: resolveMultiLangTail write %s→%s: %w", edge.SourceID, edge.TargetID, err)
		}
		written++
	}
	return written, nil
}
