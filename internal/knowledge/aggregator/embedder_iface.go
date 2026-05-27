// SPDX-License-Identifier: MIT
package aggregator

import "context"

// Embedder is the embedding model abstraction. Implementations live en
// internal/knowledge/embed/ (Mac MPS / daemon CPU / mock for tests).
//
// Boundary implementations MUST NOT make web calls.
// The contract is enforced by the no-net/http compliance test on the
// internal/knowledge/embed package (D-7's home).
//
// Pre ctx valid; text any string (including empty).
// Post returned []float32 has length == Dimensions(); L2-normalized.
//
// Failure mode #9 (spec §4.1): if model unavailable (Mac MPS GPU fault,
// daemon CPU OOM), returns a wrapped error; caller skips embedding for
// that note and indexes FTS5 + wikilinks only.
type Embedder interface {
	Dimensions() int

	Embed(ctx context.Context, text string) ([]float32, error)
}
