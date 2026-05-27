// go:build realworld
//go:build realworld
// +build realworld

package realworld

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/mcp/research"
)

type recordingAudit struct {
	mu     sync.Mutex
	events []string
}

func (r *recordingAudit) Emit(_ context.Context, t string, _ []byte) error {
	r.mu.Lock()
	r.events = append(r.events, t)
	r.mu.Unlock()
	return nil
}

func (r *recordingAudit) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

type failBackend struct{ msg string }

func (f *failBackend) Search(_ context.Context, _ string, _ int) ([]research.SourceHit, error) {
	return nil, errors.New(f.msg)
}

func TestRealArxivLive(t *testing.T) {
	a := research.NewArxiv(research.ArxivOptions{
		RequestTimeout: 30 * time.Second,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	hits, err := a.Search(ctx, "transformer attention", 3, "relevance")
	if err != nil {
		t.Fatalf("ArXiv live: %v", err)
	}
	if len(hits) == 0 {
		t.Errorf("expected hits from real ArXiv")
	}
	for _, h := range hits {
		if !strings.Contains(h.URL, "arxiv.org") {
			t.Errorf("URL not on arxiv.org: %s", h.URL)
		}
	}
}

func TestRealGitHubSearchLive(t *testing.T) {
	g := research.NewGitHubSearch(research.GitHubSearchOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	hits, err := g.Search(ctx, "model context protocol", "Go", 100)
	if err != nil {
		t.Fatalf("GitHub live: %v", err)
	}
	if len(hits) == 0 {
		t.Errorf("expected hits from real GitHub")
	}
}

func TestDispatchAuditEvent_AllFailRealworld(t *testing.T) {
	rec := &recordingAudit{}
	d := research.NewDispatcher(research.DispatcherOptions{
		WebSearch:   &failBackend{msg: "ddg unreachable"},
		Cite:        research.NewCiteVerifier(research.CiteVerifierOptions{}),
		AuditClient: rec,
	})
	_, err := d.Dispatch(context.Background(), research.DispatchQuery{Query: "x"})
	if err == nil {
		t.Fatal("expected error when all backends fail")
	}
	events := rec.snapshot()
	foundNoSource := false
	for _, e := range events {
		if e == "dispatch-no-source" {
			foundNoSource = true
		}
	}
	if !foundNoSource {
		t.Errorf("expected dispatch-no-source audit event; got events=%v", events)
	}
}

func TestRealCiteVerifierLive(t *testing.T) {
	v := research.NewCiteVerifier(research.CiteVerifierOptions{
		Timeout: 10 * time.Second,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := v.Verify(ctx, []research.RawCitation{
		{SourceID: "real", URL: "https://www.google.com/", Title: "Google"},
		{SourceID: "fake", URL: "https://nonexistent-zenswarm-realworld.invalid/", Title: "Nope"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(out) != 1 {
		t.Errorf("expected exactly 1 verified, got %d", len(out))
	}
	if len(out) > 0 && !strings.Contains(out[0].URL, "google.com") {
		t.Errorf("verified the wrong one: %v", out[0])
	}
}
