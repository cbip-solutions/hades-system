package intent

import (
	"context"
	"hash/fnv"
)

type fakeEmbedder struct {
	dim int

	hook func(text string)
}

func (f fakeEmbedder) Dimensions() int { return f.dim }

func (f fakeEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.hook != nil {
		f.hook(text)
	}
	v := make([]float32, f.dim)

	h := fnv.New32a()
	_, _ = h.Write([]byte(text))
	seed := h.Sum32()
	v[seed%uint32(f.dim)] = 1.0
	v[(seed/7)%uint32(f.dim)] = 0.5
	return v, nil
}

type fakeReranker struct{}

func (fakeReranker) Rerank(ctx context.Context, query string, passages []SemanticPassage) ([]SemanticPassage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]SemanticPassage, len(passages))
	for i, p := range passages {
		out[len(passages)-1-i] = p
	}
	return out, nil
}

type fakeGitProber struct {
	touched map[string]int64
}

func (f fakeGitProber) LastTouchedUnix(_ context.Context, repoRel string) (int64, bool) {
	if f.touched == nil {
		return 0, false
	}
	ts, ok := f.touched[repoRel]
	return ts, ok
}
