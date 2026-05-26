//go:build adversarial
// +build adversarial

package adversarial

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/mcp/research"
)

func TestAdvCitationFloodHandled(t *testing.T) {
	v := research.NewCiteVerifier(research.CiteVerifierOptions{
		MaxConcurrent: 16,
		Timeout:       2 * time.Second,
	})
	raw := make([]research.RawCitation, 1000)
	for i := range raw {
		raw[i] = research.RawCitation{
			SourceID: "flood",
			URL:      "https://nonexistent-zen-attack-" + strings.Repeat("x", i%5+1) + ".invalid/",
		}
	}
	start := time.Now()
	out, err := v.Verify(context.Background(), raw)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("expected 0 verified (all .invalid), got %d", len(out))
	}

	if elapsed > 60*time.Second {
		t.Errorf("verifier elapsed %v — concurrency may have failed", elapsed)
	}
}

type advBackend struct {
	hits []research.SourceHit
}

func (a *advBackend) Search(_ context.Context, _ string, _ int) ([]research.SourceHit, error) {
	return a.hits, nil
}

type advArxiv struct{ b *advBackend }

func (a *advArxiv) Search(_ context.Context, _ string, _ int, _ string) ([]research.SourceHit, error) {
	return a.b.hits, nil
}

type passCite struct{}

func (passCite) Verify(_ context.Context, raw []research.RawCitation) ([]research.VerifiedCitation, error) {
	out := make([]research.VerifiedCitation, 0, len(raw))
	for _, r := range raw {
		out = append(out, research.VerifiedCitation{SourceID: r.SourceID, URL: r.URL, Title: r.Title, HTTPStatus: 200})
	}
	return out, nil
}
func (passCite) Format(_ []research.VerifiedCitation) (string, []byte) { return "", nil }

func TestAdvCrossSourceDedupNormalisesURLs(t *testing.T) {
	web := &advBackend{hits: []research.SourceHit{
		{URL: "https://example.com/a/", Source: "web_search"},
		{URL: "https://Example.COM/a", Source: "web_search"},
	}}
	arx := &advArxiv{b: &advBackend{hits: []research.SourceHit{
		{URL: "https://example.com/a#section", Source: "arxiv"},
	}}}
	d := research.NewDispatcher(research.DispatcherOptions{
		WebSearch: web,
		Arxiv:     arx,
		Cite:      passCite{},
	})
	res, err := d.Dispatch(context.Background(), research.DispatchQuery{Query: "q"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 1 {
		t.Errorf("dedup failed: got %d findings, want 1", len(res.Findings))
	}
}

// TestAdvHallucinatedURLStripped: synthesizer-emitted URL pointing at
// a non-existent host MUST be removed by the cite verifier before any
// downstream consumer sees it.
func TestAdvHallucinatedURLStripped(t *testing.T) {
	v := research.NewCiteVerifier(research.CiteVerifierOptions{
		Timeout: 3 * time.Second,
	})
	raw := []research.RawCitation{
		{
			SourceID: "synthesizer-hallucination",
			URL:      "https://this-host-does-not-exist-zenswarm-adversarial.invalid/",
			Title:    "Fake Paper",
		},
	}
	out, err := v.Verify(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("hallucinated URL not stripped: %v", out)
	}
}

// TestAdvCarontePoisonedURLStrippedByCite: a CodeGraph hit carrying
// an attacker-controlled exfiltration URL (e.g. evil.example.com)
// MUST be stripped by the cite verifier — the dispatcher's cite gate
// is the last line of defence. Uses the research.GitnexusClient drop-in
// interface (DECISION L-3; identifier retained as stable contract name).
func TestAdvCarontePoisonedURLStripped(t *testing.T) {
	gn := &poisonedCodeGraph{}
	d := research.NewDispatcher(research.DispatcherOptions{
		Gitnexus: gn,
		Cite:     research.NewCiteVerifier(research.CiteVerifierOptions{}),
	})
	res, err := d.Dispatch(context.Background(), research.DispatchQuery{Query: "q"})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range res.Findings {
		if strings.Contains(h.URL, "evil.example.com") {
			t.Errorf("poisoned URL leaked into findings: %s", h.URL)
		}
	}
}

type poisonedCodeGraph struct{}

func (poisonedCodeGraph) CodeGraph(_ context.Context, _, _ string) (research.CodeGraphResult, error) {
	return research.CodeGraphResult{
		Hits: []research.CodeGraphHit{
			{Node: "pkg/x", Score: 1.0, URL: "https://evil.example.com/exfil?leak=secret"},
		},
	}, nil
}
func (poisonedCodeGraph) Close() error { return nil }
