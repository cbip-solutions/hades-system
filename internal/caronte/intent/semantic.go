// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package intent

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type SemanticIndexer struct {
	store    *store.Store
	embedder CodeEmbedder
	reranker Reranker
	params   IntentParams

	mu     sync.RWMutex
	corpus []corpusChunk
}

type corpusChunk struct {
	sourceID   string
	sourceKind string
	text       string
	embedding  []float32
}

func NewSemanticIndexer(s *store.Store, embedder CodeEmbedder, reranker Reranker, params IntentParams) (*SemanticIndexer, error) {
	if s == nil {
		return nil, ErrEmptyStore
	}
	if embedder == nil {
		return nil, ErrNoEmbedder
	}
	if embedder.Dimensions() != storeVecDim {
		return nil, fmt.Errorf("caronte/intent: embedder dim %d != store vec dim %d", embedder.Dimensions(), storeVecDim)
	}
	return &SemanticIndexer{
		store:    s,
		embedder: embedder,
		reranker: reranker,
		params:   DefaultIntentParams(params),
	}, nil
}

const storeVecDim = 1536

func nodeEmbedText(n store.Node) string {
	parts := make([]string, 0, 3)
	if n.Name != "" {
		parts = append(parts, n.Name)
	}
	if n.Signature != "" {
		parts = append(parts, n.Signature)
	}
	if n.Doc != "" {
		parts = append(parts, n.Doc)
	}
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " "
		}
		out += p
	}
	return out
}

func (si *SemanticIndexer) IndexNodes(ctx context.Context) error {
	rows, err := si.store.DB().QueryContext(ctx, `SELECT node_id, name, signature, doc FROM graph_nodes`)
	if err != nil {
		return fmt.Errorf("caronte/intent: IndexNodes list: %w", err)
	}
	type nrec struct{ id, name, sig, doc string }
	var recs []nrec
	for rows.Next() {
		var r nrec
		if err := rows.Scan(&r.id, &r.name, &r.sig, &r.doc); err != nil {
			rows.Close()
			return fmt.Errorf("caronte/intent: IndexNodes scan: %w", err)
		}
		recs = append(recs, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("caronte/intent: IndexNodes rows: %w", err)
	}
	rows.Close()

	for _, r := range recs {
		text := nodeEmbedText(store.Node{Name: r.name, Signature: r.sig, Doc: r.doc})
		if text == "" {
			continue
		}
		vec, err := si.embedder.Embed(ctx, text)
		if err != nil {
			return fmt.Errorf("caronte/intent: embed node %s: %w", r.id, err)
		}
		if err := si.store.UpsertNodeVector(ctx, r.id, vec); err != nil {
			return fmt.Errorf("caronte/intent: persist node vector %s: %w", r.id, err)
		}
	}
	return nil
}

func (si *SemanticIndexer) AddCorpusChunk(ctx context.Context, sourceID, sourceKind, text string) error {
	if text == "" {
		return nil
	}
	vec, err := si.embedder.Embed(ctx, text)
	if err != nil {
		return fmt.Errorf("caronte/intent: embed corpus chunk %s: %w", sourceID, err)
	}
	si.mu.Lock()
	si.corpus = append(si.corpus, corpusChunk{sourceID: sourceID, sourceKind: sourceKind, text: text, embedding: vec})
	si.mu.Unlock()
	return nil
}

func (si *SemanticIndexer) ChunkBody(body string) []string {
	runes := []rune(body)
	size := si.params.ChunkRunes
	var out []string
	for start := 0; start < len(runes); start += size {
		end := start + size
		if end > len(runes) {
			end = len(runes)
		}
		chunk := string(runes[start:end])
		out = append(out, chunk)
	}
	return out
}

func (si *SemanticIndexer) RetrieveForSymbol(ctx context.Context, symbol string) ([]SemanticPassage, error) {
	queryText := symbol
	if node, err := si.store.GetNode(ctx, symbol); err == nil {
		if t := nodeEmbedText(node); t != "" {
			queryText = t
		}
	}
	qvec, err := si.embedder.Embed(ctx, queryText)
	if err != nil {
		return nil, fmt.Errorf("caronte/intent: embed query %q: %w", symbol, err)
	}

	var passages []SemanticPassage

	knn, err := si.store.KNNNodeIDs(ctx, qvec, si.params.KNNFanout)
	if err != nil {
		return nil, fmt.Errorf("caronte/intent: KNN for %q: %w", symbol, err)
	}
	for _, nd := range knn {
		node, gerr := si.store.GetNode(ctx, nd.NodeID)
		text := nd.NodeID
		if gerr == nil {
			text = nodeEmbedText(node)
		}
		passages = append(passages, SemanticPassage{
			SourceID:   nd.NodeID,
			SourceKind: "code",
			Text:       text,
			Score:      distanceToSimilarity(nd.Distance),
		})
	}

	si.mu.RLock()
	for _, ch := range si.corpus {
		passages = append(passages, SemanticPassage{
			SourceID:   ch.sourceID,
			SourceKind: ch.sourceKind,
			Text:       ch.text,
			Score:      cosine(qvec, ch.embedding),
		})
	}
	si.mu.RUnlock()

	if si.reranker != nil {
		reranked, rerr := si.reranker.Rerank(ctx, queryText, passages)
		if rerr != nil {
			return nil, fmt.Errorf("caronte/intent: rerank for %q: %w", symbol, rerr)
		}
		passages = reranked
	} else {
		sort.SliceStable(passages, func(i, j int) bool {
			if passages[i].Score != passages[j].Score {
				return passages[i].Score > passages[j].Score
			}
			return passages[i].SourceID < passages[j].SourceID
		})
	}

	out := make([]SemanticPassage, 0, si.params.SemanticTopK)
	for _, p := range passages {
		if p.Score < si.params.SemanticThreshold {
			continue
		}
		out = append(out, p)
		if len(out) >= si.params.SemanticTopK {
			break
		}
	}
	return out, nil
}

func distanceToSimilarity(distance float64) float64 {
	if distance < 0 {
		distance = 0
	}
	return 1.0 / (1.0 + distance)
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	sim := dot / (math.Sqrt(na) * math.Sqrt(nb))
	if sim < 0 {
		return 0
	}
	if sim > 1 {
		return 1
	}
	return sim
}
