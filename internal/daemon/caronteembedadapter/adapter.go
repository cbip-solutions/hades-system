// SPDX-License-Identifier: MIT
// Package caronteembedadapter bridges the caronte intent layer's narrow
// CodeEmbedder + Reranker seams to the real release ecosystem implementations
// (JinaCodeEmbeddings + BGEReRankerV2M3). It lives in internal/daemon (the
// composition-root layer) because internal/caronte must NOT import
// internal/research/ecosystem — inv-hades-129 (the embed path is a local
// subprocess, no net/http) is preserved structurally by keeping caronte's
// dependency on a pure interface. The daemon wires these adapters into
// caronte.Deps at main.go.
//
// Boundary this package is the ONLY sanctioned bridge that imports BOTH
// internal/caronte/intent (the narrow interface seams) AND
// internal/research/ecosystem (the Jina/BGE implementations). The daemon
// orchestrator, dispatcher, and mcpgateway MUST NOT import either side
// directly — they receive the wired intent.CodeEmbedder / intent.Reranker
// values from the composition root (inv-hades-031 + inv-hades-129).
package caronteembedadapter

import (
	"context"

	"github.com/cbip-solutions/hades-system/internal/caronte/intent"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

type embedFP32 interface {
	EmbedFP32_1536d(ctx context.Context, text string) ([]float32, error)
}

type embedderBridge struct {
	jina embedFP32
}

var _ intent.CodeEmbedder = (*embedderBridge)(nil)

func NewEmbedder(jina embedFP32) intent.CodeEmbedder {
	return embedderBridge{jina: jina}
}

func (b embedderBridge) Embed(ctx context.Context, text string) ([]float32, error) {
	return b.jina.EmbedFP32_1536d(ctx, text)
}

func (embedderBridge) Dimensions() int { return 1536 }

type rerankFn interface {
	Rerank(ctx context.Context, query string, candidates []ecosystem.Candidate, topK int) ([]ecosystem.RankedResult, error)
}

type rerankerBridge struct {
	bge rerankFn
}

var _ intent.Reranker = (*rerankerBridge)(nil)

func NewReranker(bge rerankFn) intent.Reranker {
	return rerankerBridge{bge: bge}
}

// Rerank maps []SemanticPassage → []Candidate, calls the BGE reranker, and
// maps []RankedResult → []SemanticPassage in the reranker's order.
//
// Passing topK=0 tells BGEReRankerV2M3 to return all candidates (its
// contract: "topK ≤ 0 or topK > len(candidates) → defaults to len"). The
// intent layer's SemanticTopK cap is applied upstream by the semantic stage
// after reranking, not here — this bridge is transparent to that cap.
//
// Invariant MUST preserve the input set (no add/drop) — only reorder.
// The BGE reranker guarantees this when topK=0; if the backend returns fewer
// results (e.g., topK=1), the output is shorter — the caller is responsible
// for setting topK correctly.
func (b rerankerBridge) Rerank(ctx context.Context, query string, passages []intent.SemanticPassage) ([]intent.SemanticPassage, error) {
	if len(passages) == 0 {
		return nil, nil
	}

	candidates := make([]ecosystem.Candidate, len(passages))
	for i, p := range passages {
		candidates[i] = ecosystem.Candidate{
			ChunkID:         int64(i),
			ContentText:     p.Text,
			SymbolPath:      p.SourceID,
			SimilarityScore: p.Score,
		}
	}

	ranked, err := b.bge.Rerank(ctx, query, candidates, 0)
	if err != nil {
		return nil, err
	}

	out := make([]intent.SemanticPassage, 0, len(ranked))
	for _, r := range ranked {
		idx := int(r.Candidate.ChunkID)

		var sourceID, sourceKind, text string
		if idx >= 0 && idx < len(passages) {

			sourceID = passages[idx].SourceID
			sourceKind = passages[idx].SourceKind
			text = passages[idx].Text
		} else {

			sourceID = r.Candidate.SymbolPath
			sourceKind = ""
			text = r.Candidate.ContentText
		}

		out = append(out, intent.SemanticPassage{
			SourceID:   sourceID,
			SourceKind: sourceKind,
			Text:       text,
			Score:      r.RerankerScore,
		})
	}
	return out, nil
}
