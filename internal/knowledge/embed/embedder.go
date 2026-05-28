// SPDX-License-Identifier: MIT
// Package embed provides Embedder implementations for hades-system knowledge
// search. Three implementations are shipped:
//
// - MPSEmbedder: Mac M-series GPU path via Python sentence-transformers
// subprocess (JSON stdin/stdout; MPS inference; ~10-20ms warm latency).
// - CPUEmbedder: pure-Go deterministic-from-hash placeholder; functional
// contract complete (L2-normalized 384-dim); FTS5 + wikilink graph
// compensate quality gap via RRF. upgrade hook for real
// Model2Vec when a stable pure-Go port ships.
// - MockEmbedder: deterministic from sha256+sin; for unit tests only.
//
// Factory (NewEmbedder) auto-detects backend per Config.Backend:
// "auto" → MPS on darwin if python3+script available, else CPU.
//
// invariant: NO net/http imports in this package — all embed operations
// are local-only. Compliance grep in tests/compliance enforces this.
package embed

import (
	"math"

	"github.com/cbip-solutions/hades-system/internal/knowledge/aggregator"
)

type Embedder = aggregator.Embedder

func NormalizeL2(v []float32) []float32 {
	if len(v) == 0 {
		return v
	}
	var sum float64
	for _, x := range v {
		sum += float64(x * x)
	}
	mag := math.Sqrt(sum)
	out := make([]float32, len(v))
	if mag == 0 {
		return out
	}
	inv := 1.0 / mag
	for i, x := range v {
		out[i] = float32(float64(x) * inv)
	}
	return out
}

func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, magA, magB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		magA += float64(a[i]) * float64(a[i])
		magB += float64(b[i]) * float64(b[i])
	}
	if magA == 0 || magB == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(magA) * math.Sqrt(magB)))
}
