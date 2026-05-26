// SPDX-License-Identifier: MIT
package intent

import "context"

type CodeEmbedder interface {
	// Embed returns the 1536-d FP32 embedding of text. MUST be safe for
	// concurrent calls (the indexer may embed package chunks in parallel).
	Embed(ctx context.Context, text string) ([]float32, error)

	Dimensions() int
}

type Reranker interface {
	// Rerank returns passages re-ordered most-relevant-first for the query.
	// MUST preserve the input set (no add/drop) — only reorder. Returns
	// ctx.Err() on cancellation.
	Rerank(ctx context.Context, query string, passages []SemanticPassage) ([]SemanticPassage, error)
}

type GitProber interface {
	LastTouchedUnix(ctx context.Context, repoRel string) (int64, bool)
}

type ADRRef struct {
	RepoRel    string
	ID         string
	Title      string
	CitedPaths []string
	Body       string
}

type LinkedADR struct {
	ADRID      string
	ADRTitle   string
	LinkKind   string
	Confidence float64
	Stale      bool
}

type SemanticPassage struct {
	SourceID   string
	SourceKind string
	Text       string
	Score      float64
}

type LoreEntry struct {
	CommitSHA   string
	TrailerKind string
	Body        string
	AuthoredAt  int64
}

type WhyAnswer struct {
	Subject          string
	LinkedADRs       []LinkedADR
	SemanticPassages []SemanticPassage
	LoreTrailers     []LoreEntry
	Degraded         bool
}

type IntentParams struct {
	SemanticThreshold float64

	SemanticTopK int

	KNNFanout int

	ChunkRunes int
}

func DefaultIntentParams(p IntentParams) IntentParams {
	if p.SemanticThreshold <= 0 {
		p.SemanticThreshold = 0.30
	}
	if p.SemanticTopK <= 0 {
		p.SemanticTopK = 5
	}
	if p.KNNFanout <= 0 {
		p.KNNFanout = 20
	}
	if p.ChunkRunes <= 0 {
		p.ChunkRunes = 1200
	}
	return p
}
