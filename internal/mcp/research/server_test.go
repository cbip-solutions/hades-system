package research

import (
	"context"
	"strings"
	"testing"

	ecosystemtypes "github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

func TestNewServerNilOptions(t *testing.T) {
	_, err := NewServer(nil)
	if err == nil {
		t.Fatal("NewServer(nil) returned nil error")
	}
}

func TestNewServerMissingDispatcher(t *testing.T) {
	opts := &ServerOptions{}
	_, err := NewServer(opts)
	if err == nil || !strings.Contains(err.Error(), "Dispatcher") {
		t.Fatalf("err = %v, want mention of Dispatcher", err)
	}
}

func TestNewServerMissingFields(t *testing.T) {
	wants := []string{
		"WebSearchTool",
		"ArxivTool",
		"GitHubTool",
		"EcosystemTool",
		"GitnexusClient",
		"Synthesizer",
		"Cache",
		"BudgetClient",
		"AuditClient",
		"Cite",
	}
	for _, missing := range wants {
		opts := testServerOptions()
		switch missing {
		case "WebSearchTool":
			opts.WebSearchTool = nil
		case "ArxivTool":
			opts.ArxivTool = nil
		case "GitHubTool":
			opts.GitHubTool = nil
		case "EcosystemTool":
			opts.EcosystemTool = nil
		case "GitnexusClient":
			opts.GitnexusClient = nil
		case "Synthesizer":
			opts.Synthesizer = nil
		case "Cache":
			opts.Cache = nil
		case "BudgetClient":
			opts.BudgetClient = nil
		case "AuditClient":
			opts.AuditClient = nil
		case "Cite":
			opts.Cite = nil
		}
		_, err := NewServer(opts)
		if err == nil || !strings.Contains(err.Error(), missing) {
			t.Errorf("missing %s: err = %v, want mention of %s", missing, err, missing)
		}
	}
}

func TestServerToolsRegistered(t *testing.T) {
	srv, err := NewServer(testServerOptions())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	got := srv.RegisteredToolNames()
	want := map[string]bool{
		"web_search":     true,
		"arxiv":          true,
		"github_search":  true,
		"code_graph":     true,
		"ecosystem_docs": true,
		"synthesize":     true,
		"cite":           true,
		"agentic_deep":   true,
	}
	for _, name := range got {
		if !want[name] {
			t.Errorf("unexpected tool registered: %s", name)
		}
		delete(want, name)
	}
	if len(want) > 0 {
		t.Errorf("tools missing: %v", want)
	}
}

func TestServerInvokeAllTools(t *testing.T) {
	srv, err := NewServer(testServerOptions())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, name := range srv.RegisteredToolNames() {
		args := map[string]any{
			"query":         "test",
			"project_id":    "p",
			"ecosystem":     "go",
			"source_id":     "src-1",
			"initial_query": "q",

			"findings": []any{map[string]any{"url": "https://placeholder.test/"}},
		}
		_, err := srv.InvokeTool(ctx, name, args)
		if err != nil {
			t.Errorf("InvokeTool(%s): %v", name, err)
		}
	}
}

func TestServerInvokeUnknownTool(t *testing.T) {
	srv, err := NewServer(testServerOptions())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.InvokeTool(context.Background(), "no-such-tool", nil); err == nil {
		t.Fatal("expected unknown tool error")
	}
}

func TestServerCloseNoChild(t *testing.T) {
	srv, err := NewServer(testServerOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestServerSynthesizeBudgetBlocked(t *testing.T) {
	opts := testServerOptions()
	opts.BudgetClient = &stubBudget{block: true}
	srv, err := NewServer(opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.InvokeTool(context.Background(), "synthesize",
		map[string]any{"findings": []any{}}); err == nil {
		t.Fatal("expected budget block error")
	}
}

func TestServerCiteRequiresSourceID(t *testing.T) {
	srv, err := NewServer(testServerOptions())
	if err != nil {
		t.Fatal(err)
	}
	_, err = srv.InvokeTool(context.Background(), "cite", map[string]any{})
	if err == nil {
		t.Fatal("expected source_id required")
	}
}

func TestServerAgenticDeepRequiresInitialQuery(t *testing.T) {
	srv, err := NewServer(testServerOptions())
	if err != nil {
		t.Fatal(err)
	}
	_, err = srv.InvokeTool(context.Background(), "agentic_deep",
		map[string]any{})
	if err == nil {
		t.Fatal("expected initial_query required")
	}
}

func testServerOptions() *ServerOptions {
	return &ServerOptions{
		Dispatcher:     &stubDispatcher{},
		GitnexusClient: &stubGitnexus{},
		Synthesizer:    &stubSynthesizer{},
		Cache:          &stubCache{},
		Cite:           &stubCite{},
		BudgetClient:   &stubBudget{},
		AuditClient:    &stubAudit{},
		WebSearchTool:  &stubWebSearch{},
		ArxivTool:      &stubArxiv{},
		GitHubTool:     &stubGitHub{},
		EcosystemTool:  &stubEcosystem{},
	}
}

type stubDispatcher struct{}

func (*stubDispatcher) Dispatch(_ context.Context, _ DispatchQuery) (DispatchResult, error) {
	return DispatchResult{}, nil
}

type stubGitnexus struct{}

func (*stubGitnexus) CodeGraph(_ context.Context, _, _ string) (CodeGraphResult, error) {
	return CodeGraphResult{}, nil
}
func (*stubGitnexus) Close() error { return nil }

type stubSynthesizer struct{}

func (*stubSynthesizer) Synthesize(_ context.Context, _ SynthesizeInput) (SynthesizeOutput, error) {
	return SynthesizeOutput{}, nil
}

type stubCache struct{}

func (*stubCache) Get(_ context.Context, _ string) (CacheEntry, bool, error) {
	return CacheEntry{}, false, nil
}
func (*stubCache) Set(_ context.Context, _ string, _ CacheEntry, _ int64) error {
	return nil
}

type stubCite struct{}

func (*stubCite) Verify(_ context.Context, raw []RawCitation) ([]VerifiedCitation, error) {
	out := make([]VerifiedCitation, 0, len(raw))
	for _, r := range raw {
		out = append(out, VerifiedCitation{SourceID: r.SourceID, URL: r.URL, Title: r.Title})
	}
	return out, nil
}
func (*stubCite) Format(_ []VerifiedCitation) (string, []byte) { return "", nil }

type stubBudget struct {
	block bool
}

func (b *stubBudget) PreCall(_ context.Context, _, _ string, _ float64) (bool, string, error) {
	if b.block {
		return false, "stage", nil
	}
	return true, "", nil
}
func (*stubBudget) Record(_ context.Context, _ string, _ map[string]string) error { return nil }

type stubAudit struct{}

func (*stubAudit) Emit(_ context.Context, _ string, _ []byte) error { return nil }

type stubWebSearch struct{}

func (*stubWebSearch) Search(_ context.Context, _ string, _ int) ([]SourceHit, error) {
	return nil, nil
}

type stubArxiv struct{}

func (*stubArxiv) Search(_ context.Context, _ string, _ int, _ string) ([]SourceHit, error) {
	return nil, nil
}

type stubGitHub struct{}

func (*stubGitHub) Search(_ context.Context, _, _ string, _ int) ([]SourceHit, error) {
	return nil, nil
}

type stubEcosystem struct{}

func (*stubEcosystem) Search(_ context.Context, _, _ string) ([]SourceHit, error) {
	return nil, nil
}

func (*stubEcosystem) Query(_ context.Context, _ ecosystemtypes.QueryRequest) (*ecosystemtypes.QueryResult, error) {
	return nil, nil
}
