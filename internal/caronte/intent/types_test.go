package intent

import (
	"context"
	"errors"
	"testing"
)

func TestSentinelsAreDistinct(t *testing.T) {
	for i, e := range []error{ErrCGODisabled, ErrEmptyStore, ErrNoEmbedder} {
		if e == nil {
			t.Fatalf("sentinel[%d] is nil", i)
		}
	}
	if errors.Is(ErrCGODisabled, ErrEmptyStore) || errors.Is(ErrEmptyStore, ErrNoEmbedder) || errors.Is(ErrCGODisabled, ErrNoEmbedder) {
		t.Error("sentinels must be distinct error values")
	}
}

func TestWhyAnswerFieldSet(t *testing.T) {
	a := WhyAnswer{
		Subject: "internal/caronte/intent.GetWhy",
		LinkedADRs: []LinkedADR{{
			ADRID: "docs/decisions/0100-caronte.md", ADRTitle: "Caronte architecture",
			LinkKind: "explicit_ref", Confidence: 1.0, Stale: true,
		}},
		SemanticPassages: []SemanticPassage{{
			SourceID: "docs/decisions/0100-caronte.md#chunk-3", SourceKind: "adr",
			Text: "Caronte is the sovereign code-graph engine.", Score: 0.81,
		}},
		LoreTrailers: []LoreEntry{{
			CommitSHA: "abc123", TrailerKind: "constraint",
			Body: "no net/http in the embed path", AuthoredAt: 1700000000,
		}},
		Degraded: false,
	}
	if a.Subject == "" || len(a.LinkedADRs) != 1 || len(a.SemanticPassages) != 1 || len(a.LoreTrailers) != 1 {
		t.Fatal("WhyAnswer field set incomplete")
	}
}

func TestCodeEmbedderSeamShape(t *testing.T) {
	var _ CodeEmbedder = fakeEmbedder{dim: 1536}
	e := fakeEmbedder{dim: 1536}
	if e.Dimensions() != 1536 {
		t.Errorf("Dimensions() = %d; want 1536", e.Dimensions())
	}
	v, err := e.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(v) != 1536 {
		t.Errorf("Embed len = %d; want 1536", len(v))
	}
}

func TestRerankerAndGitProberSeamShape(t *testing.T) {
	var _ Reranker = fakeReranker{}
	var _ GitProber = fakeGitProber{}
}
