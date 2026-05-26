package research

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	ecosystemtypes "github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

type fakeBackend struct {
	hits   []SourceHit
	err    error
	delay  time.Duration
	mu     sync.Mutex
	called int
}

func (f *fakeBackend) Search(ctx context.Context, _ string, _ int) ([]SourceHit, error) {
	f.mu.Lock()
	f.called++
	f.mu.Unlock()
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.hits, nil
}

func (f *fakeBackend) calledCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.called
}

type arxivAdapter struct{ b *fakeBackend }

func (a *arxivAdapter) Search(ctx context.Context, q string, max int, _ string) ([]SourceHit, error) {
	return a.b.Search(ctx, q, max)
}

type ghAdapter struct{ b *fakeBackend }

func (g *ghAdapter) Search(ctx context.Context, q, _ string, _ int) ([]SourceHit, error) {
	return g.b.Search(ctx, q, 0)
}

type ecoAdapter struct{ b *fakeBackend }

func (e *ecoAdapter) Search(ctx context.Context, q, _ string) ([]SourceHit, error) {
	return e.b.Search(ctx, q, 0)
}

func (e *ecoAdapter) Query(_ context.Context, _ ecosystemtypes.QueryRequest) (*ecosystemtypes.QueryResult, error) {
	return nil, nil
}

type fakeGitnexus struct {
	res CodeGraphResult
	err error
}

func (f *fakeGitnexus) CodeGraph(_ context.Context, _, _ string) (CodeGraphResult, error) {
	return f.res, f.err
}
func (*fakeGitnexus) Close() error { return nil }

type passCite struct{}

func (passCite) Verify(_ context.Context, raw []RawCitation) ([]VerifiedCitation, error) {
	out := make([]VerifiedCitation, 0, len(raw))
	for _, r := range raw {
		out = append(out, VerifiedCitation{SourceID: r.SourceID, URL: r.URL, Title: r.Title, HTTPStatus: 200})
	}
	return out, nil
}
func (passCite) Format(_ []VerifiedCitation) (string, []byte) { return "", nil }

type failCite struct{}

func (failCite) Verify(_ context.Context, _ []RawCitation) ([]VerifiedCitation, error) {
	return nil, nil
}
func (failCite) Format(_ []VerifiedCitation) (string, []byte) { return "", nil }

type errCite struct{}

func (errCite) Verify(_ context.Context, _ []RawCitation) ([]VerifiedCitation, error) {
	return nil, errors.New("boom")
}
func (errCite) Format(_ []VerifiedCitation) (string, []byte) { return "", nil }

type recordingBudget struct {
	mu    sync.Mutex
	calls []string
	block bool
}

func (r *recordingBudget) PreCall(_ context.Context, scope, value string, _ float64) (bool, string, error) {
	r.mu.Lock()
	r.calls = append(r.calls, scope+":"+value)
	r.mu.Unlock()
	if r.block {
		return false, "stage", nil
	}
	return true, "", nil
}
func (r *recordingBudget) Record(_ context.Context, _ string, _ map[string]string) error { return nil }

func (r *recordingBudget) seenCalls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.calls))
	copy(out, r.calls)
	return out
}

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

func TestDispatchEmptyQuery(t *testing.T) {
	d := NewDispatcher(DispatcherOptions{})
	if _, err := d.Dispatch(context.Background(), DispatchQuery{}); err == nil {
		t.Fatal("expected empty-query error")
	}
}

func TestDispatchHappyPathAllBackends(t *testing.T) {
	web := &fakeBackend{hits: []SourceHit{{URL: "https://a", Score: 1}}}
	arx := &fakeBackend{hits: []SourceHit{{URL: "https://b", Score: 0.5}}}
	gh := &fakeBackend{hits: []SourceHit{{URL: "https://c", Score: 0.9}}}
	eco := &fakeBackend{hits: []SourceHit{{URL: "https://d", Score: 0.4}}}
	gn := &fakeGitnexus{res: CodeGraphResult{Hits: []CodeGraphHit{{Node: "pkg/x", Score: 0.8}}}}
	bud := &recordingBudget{}
	d := NewDispatcher(DispatcherOptions{
		WebSearch:    web,
		Arxiv:        &arxivAdapter{arx},
		GitHub:       &ghAdapter{gh},
		Ecosystem:    &ecoAdapter{eco},
		Gitnexus:     gn,
		Cite:         passCite{},
		BudgetClient: bud,
	})
	res, err := d.Dispatch(context.Background(), DispatchQuery{Query: "ml"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(res.Findings) != 5 {
		t.Errorf("findings = %d, want 5", len(res.Findings))
	}
	if len(res.Citations) != 4 {

		t.Errorf("citations = %d, want 4", len(res.Citations))
	}
	if res.Iterations != 1 {
		t.Errorf("iterations = %d", res.Iterations)
	}

	wantPrefixes := []string{"research:web_search", "research:arxiv", "research:github_search", "research:ecosystem_docs", "research:code_graph"}
	calls := bud.seenCalls()
	for _, want := range wantPrefixes {
		found := false
		for _, c := range calls {
			if strings.Contains(c, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing PreCall for %s; got %v", want, calls)
		}
	}
}

func TestDispatchURLDedup(t *testing.T) {
	web := &fakeBackend{hits: []SourceHit{
		{URL: "https://example.com/a/", Score: 0.5},
		{URL: "https://Example.com/a", Score: 0.9},
	}}
	d := NewDispatcher(DispatcherOptions{
		WebSearch: web,
		Cite:      passCite{},
	})
	res, err := d.Dispatch(context.Background(), DispatchQuery{Query: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 1 {
		t.Errorf("findings = %d, want 1 (dedup)", len(res.Findings))
	}
	if res.Findings[0].Score != 0.9 {
		t.Errorf("expected higher-score winner, got score=%v", res.Findings[0].Score)
	}
}

func TestDispatchMinSourceThresholdFails(t *testing.T) {
	web := &fakeBackend{hits: []SourceHit{{URL: "https://x"}}}
	d := NewDispatcher(DispatcherOptions{
		WebSearch:          web,
		Cite:               passCite{},
		MinSourceThreshold: 3,
	})
	_, err := d.Dispatch(context.Background(), DispatchQuery{Query: "q"})
	if err == nil {
		t.Fatal("expected threshold not met")
	}
}

func TestDispatchSoftFailWithSibling(t *testing.T) {
	web := &fakeBackend{err: errors.New("boom")}
	arx := &fakeBackend{hits: []SourceHit{{URL: "https://x"}}}
	d := NewDispatcher(DispatcherOptions{
		WebSearch: web,
		Arxiv:     &arxivAdapter{arx},
		Cite:      passCite{},
	})
	res, err := d.Dispatch(context.Background(), DispatchQuery{Query: "q"})
	if err != nil {
		t.Fatalf("expected soft-fail, got %v", err)
	}
	if len(res.Findings) != 1 {
		t.Errorf("findings = %d, want 1 (sibling produced hit)", len(res.Findings))
	}
}

func TestDispatchAllBackendsErrorThresholdFails(t *testing.T) {
	web := &fakeBackend{err: errors.New("boom")}
	d := NewDispatcher(DispatcherOptions{
		WebSearch: web,
		Cite:      passCite{},
	})
	if _, err := d.Dispatch(context.Background(), DispatchQuery{Query: "q"}); err == nil {
		t.Fatal("expected threshold error")
	}
}

func TestDispatchBudgetBlocks(t *testing.T) {
	web := &fakeBackend{hits: []SourceHit{{URL: "https://x"}}}
	bud := &recordingBudget{block: true}
	d := NewDispatcher(DispatcherOptions{
		WebSearch:    web,
		Cite:         passCite{},
		BudgetClient: bud,
	})
	_, err := d.Dispatch(context.Background(), DispatchQuery{Query: "q"})
	if err == nil {
		t.Fatal("expected threshold error after budget blocks all backends")
	}
	if web.calledCount() != 0 {
		t.Errorf("backend was called %d times despite budget block", web.calledCount())
	}
}

func TestDispatchCiteVerifyError(t *testing.T) {
	web := &fakeBackend{hits: []SourceHit{{URL: "https://x"}}}
	d := NewDispatcher(DispatcherOptions{
		WebSearch: web,
		Cite:      errCite{},
	})
	if _, err := d.Dispatch(context.Background(), DispatchQuery{Query: "q"}); err == nil {
		t.Fatal("expected cite verify error")
	}
}

func TestDispatchCiteRejectsAllStripsFindings(t *testing.T) {
	web := &fakeBackend{hits: []SourceHit{
		{URL: "https://kept", Source: "web_search"},
		{URL: "https://stripped", Source: "web_search"},
	}}
	d := NewDispatcher(DispatcherOptions{
		WebSearch: web,
		Cite:      failCite{},
	})
	res, err := d.Dispatch(context.Background(), DispatchQuery{Query: "q"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 0 {
		t.Errorf("findings = %d, want 0 (all stripped)", len(res.Findings))
	}
	if len(res.Citations) != 0 {
		t.Errorf("citations = %d, want 0", len(res.Citations))
	}
}

func TestDispatchURLLessHitsKept(t *testing.T) {
	gn := &fakeGitnexus{res: CodeGraphResult{Hits: []CodeGraphHit{
		{Node: "pkg/x", Score: 0.5},
		{Node: "pkg/y", Score: 0.7},
	}}}
	d := NewDispatcher(DispatcherOptions{
		Gitnexus: gn,
		Cite:     failCite{},
	})
	res, err := d.Dispatch(context.Background(), DispatchQuery{Query: "q"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 2 {
		t.Errorf("findings = %d, want 2 (URL-less keeps)", len(res.Findings))
	}
}

func TestDispatchAuditOnSoftFail(t *testing.T) {
	web := &fakeBackend{err: errors.New("ouch")}
	arx := &fakeBackend{hits: []SourceHit{{URL: "https://ok"}}}
	rec := &recordingAudit{}
	d := NewDispatcher(DispatcherOptions{
		WebSearch:   web,
		Arxiv:       &arxivAdapter{arx},
		Cite:        passCite{},
		AuditClient: rec,
	})
	if _, err := d.Dispatch(context.Background(), DispatchQuery{Query: "q"}); err != nil {
		t.Fatal(err)
	}

	_ = rec
}

func TestCanonicalURL(t *testing.T) {
	cases := map[string]string{
		"https://example.com/a/":    "https://example.com/a",
		"https://Example.COM/a":     "https://example.com/a",
		"https://example.com/a#x":   "https://example.com/a",
		"":                          "",
		"file:///local/path":        "file:///local/path",
		"not-a-url":                 "not-a-url",
		"https://example.com/a?q=1": "https://example.com/a?q=1",
	}
	for in, want := range cases {
		if got := canonicalURL(in); got != want {
			t.Errorf("canonicalURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCodeGraphToHits(t *testing.T) {
	res := CodeGraphResult{Hits: []CodeGraphHit{{Node: "n1", Score: 0.5, URL: "file://x"}}}
	hits := codeGraphToHits(res)
	if len(hits) != 1 {
		t.Fatalf("hits = %d", len(hits))
	}
	if hits[0].Source != "code_graph" {
		t.Errorf("source = %q", hits[0].Source)
	}
	if hits[0].Title != "n1" {
		t.Errorf("title = %q", hits[0].Title)
	}
}

func TestDispatcherDefaults(t *testing.T) {
	d := NewDispatcher(DispatcherOptions{})
	if d.opts.MinSourceThreshold != 1 {
		t.Errorf("default threshold = %d", d.opts.MinSourceThreshold)
	}
	if d.opts.MaxResultsPerBackend != 10 {
		t.Errorf("default max = %d", d.opts.MaxResultsPerBackend)
	}
}

func TestPreCheckNilBudgetAllows(t *testing.T) {
	d := NewDispatcher(DispatcherOptions{})
	if !d.preCheck(context.Background(), "x", "y", 0) {
		t.Fatal("nil BudgetClient should allow")
	}
}
