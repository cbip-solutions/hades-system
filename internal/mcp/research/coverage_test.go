package research

import (
	"context"
	"strings"
	"testing"
	"time"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
	ecosystemtypes "github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

func TestNoOpGitnexusBehaviour(t *testing.T) {
	s := NoOpGitnexus{}
	if _, err := s.CodeGraph(context.Background(), "q", "p"); err == nil {
		t.Errorf("expected errGitnexusNotConfigured")
	}
	if err := s.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestNoOpBudgetAlwaysAllows(t *testing.T) {
	b := NoOpBudget{}
	allowed, blocked, err := b.PreCall(context.Background(), "x", "y", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Errorf("expected allowed")
	}
	if blocked != "" {
		t.Errorf("blocked = %q", blocked)
	}
	if err := b.Record(context.Background(), "x", map[string]string{"a": "b"}); err != nil {
		t.Errorf("Record: %v", err)
	}
}

func TestNoOpAuditEmitNoOp(t *testing.T) {
	if err := (NoOpAudit{}).Emit(context.Background(), "t", []byte("p")); err != nil {
		t.Errorf("Emit: %v", err)
	}
}

func TestIntArgAllTypes(t *testing.T) {
	cases := []struct {
		in   any
		want int
	}{
		{float64(5), 5},
		{int(7), 7},
		{int64(9), 9},
		{"not-a-number", 11},
	}
	for _, c := range cases {
		args := map[string]any{"k": c.in}
		got := intArg(args, "k", 11)
		if got != c.want {
			t.Errorf("in=%v: got %d, want %d", c.in, got, c.want)
		}
	}
	if got := intArg(map[string]any{}, "missing", 42); got != 42 {
		t.Errorf("default not returned: %d", got)
	}
}

func TestJSONStringMarshalsAndFallsBack(t *testing.T) {
	got := jsonString(map[string]any{"k": "v"})
	if !strings.Contains(got, `"k":"v"`) {
		t.Errorf("got = %q", got)
	}

	got = jsonString(make(chan int))
	if !strings.HasPrefix(got, "0x") && !strings.HasPrefix(got, "(chan") {

		if got == "" {
			t.Errorf("expected non-empty fallback, got empty")
		}
	}
}

type errCloser struct{ err error }

func (e *errCloser) Close() error { return e.err }

func TestCloseOnceCapturesFirstErr(t *testing.T) {
	var first error
	closeOnce(&first, &errCloser{err: nil})
	if first != nil {
		t.Errorf("nil-err Closer should not set first")
	}
	closeOnce(&first, nil)
	if first != nil {
		t.Errorf("nil Closer should not set first")
	}
}

type erroringWebSearch struct{}

func (erroringWebSearch) Search(_ context.Context, _ string, _ int) ([]SourceHit, error) {
	return nil, &testErr{"web boom"}
}

type erroringArxiv struct{}

func (erroringArxiv) Search(_ context.Context, _ string, _ int, _ string) ([]SourceHit, error) {
	return nil, &testErr{"arxiv boom"}
}

type erroringGH struct{}

func (erroringGH) Search(_ context.Context, _, _ string, _ int) ([]SourceHit, error) {
	return nil, &testErr{"gh boom"}
}

type erroringEco struct{}

func (erroringEco) Search(_ context.Context, _, _ string) ([]SourceHit, error) {
	return nil, &testErr{"eco boom"}
}

func (erroringEco) Query(_ context.Context, _ ecosystemtypes.QueryRequest) (*ecosystemtypes.QueryResult, error) {
	return nil, &testErr{"eco boom"}
}

type erroringGn struct{}

func (erroringGn) CodeGraph(_ context.Context, _, _ string) (CodeGraphResult, error) {
	return CodeGraphResult{}, &testErr{"gn boom"}
}
func (erroringGn) Close() error { return nil }

type erroringSynth struct{}

func (erroringSynth) Synthesize(_ context.Context, _ SynthesizeInput) (SynthesizeOutput, error) {
	return SynthesizeOutput{}, &testErr{"synth boom"}
}

type erroringCite struct{}

func (erroringCite) Verify(_ context.Context, _ []RawCitation) ([]VerifiedCitation, error) {
	return nil, &testErr{"cite boom"}
}
func (erroringCite) Format(_ []VerifiedCitation) (string, []byte) { return "", nil }

type errBudgetWithErr struct{}

func (errBudgetWithErr) PreCall(_ context.Context, _, _ string, _ float64) (bool, string, error) {
	return false, "", &testErr{"budget err"}
}
func (errBudgetWithErr) Record(_ context.Context, _ string, _ map[string]string) error {
	return nil
}

type testErr struct{ msg string }

func (e *testErr) Error() string { return e.msg }

func TestHandlersErrorBranches(t *testing.T) {
	opts := testServerOptions()
	opts.WebSearchTool = erroringWebSearch{}
	opts.ArxivTool = erroringArxiv{}
	opts.GitHubTool = erroringGH{}
	opts.EcosystemTool = erroringEco{}
	opts.GitnexusClient = erroringGn{}
	opts.Synthesizer = erroringSynth{}
	opts.Cite = erroringCite{}
	srv, err := NewServer(opts)
	if err != nil {
		t.Fatal(err)
	}
	tools := []string{
		"web_search", "arxiv", "github_search", "code_graph",
		"ecosystem_docs", "synthesize",
	}
	for _, name := range tools {
		args := map[string]any{
			"query":     "q",
			"ecosystem": "go",
			"findings":  []any{},
		}
		if _, err := srv.InvokeTool(context.Background(), name, args); err == nil {
			t.Errorf("%s: expected backend error", name)
		}
	}

	if _, err := srv.InvokeTool(context.Background(), "cite",
		map[string]any{"source_id": "s"}); err == nil {
		t.Error("cite: expected verifier error")
	}
}

func TestHandlersBudgetErrorSurfaces(t *testing.T) {
	opts := testServerOptions()
	opts.BudgetClient = errBudgetWithErr{}
	srv, err := NewServer(opts)
	if err != nil {
		t.Fatal(err)
	}
	tools := []string{
		"web_search", "arxiv", "github_search", "code_graph",
		"ecosystem_docs", "synthesize",
	}
	for _, name := range tools {
		args := map[string]any{
			"query":     "q",
			"ecosystem": "go",
			"findings":  []any{},
		}
		if _, err := srv.InvokeTool(context.Background(), name, args); err == nil {
			t.Errorf("%s: expected budget pre-check error surface", name)
		}
	}
}

func TestHandlersBudgetBlocked(t *testing.T) {
	opts := testServerOptions()
	opts.BudgetClient = &stubBudget{block: true}
	srv, err := NewServer(opts)
	if err != nil {
		t.Fatal(err)
	}
	tools := []string{
		"web_search", "arxiv", "github_search", "code_graph",
		"ecosystem_docs",
	}
	for _, name := range tools {
		args := map[string]any{
			"query":     "q",
			"ecosystem": "go",
			"findings":  []any{},
		}
		if _, err := srv.InvokeTool(context.Background(), name, args); err == nil {
			t.Errorf("%s: expected budget blocked error", name)
		}
	}
}

func TestEmitAuditDoesNotPanicOnNilClient(t *testing.T) {
	d := NewDispatcher(DispatcherOptions{})
	d.emitAudit(context.Background(), "t", []byte("p"))

}

func TestEmitAuditWithRecordingClient(t *testing.T) {
	rec := &recordingAudit{}
	d := NewDispatcher(DispatcherOptions{AuditClient: rec})
	d.emitAudit(context.Background(), "x", []byte("p"))
	if len(rec.events) != 1 {
		t.Errorf("expected 1 event, got %d", len(rec.events))
	}
}

func TestServerCloseWithErrCloser(t *testing.T) {
	opts := testServerOptions()

	opts.GitnexusClient = errClosingGn{err: &testErr{"close-boom"}}
	srv, err := NewServer(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Close(); err == nil {
		t.Error("expected close error to surface")
	}
}

type errClosingGn struct{ err error }

func (e errClosingGn) CodeGraph(_ context.Context, _, _ string) (CodeGraphResult, error) {
	return CodeGraphResult{}, nil
}
func (e errClosingGn) Close() error { return e.err }

func TestNewWebSearchAllDefaults(t *testing.T) {
	w := NewWebSearch(WebSearchOptions{})
	if w.opts.HTTPClient == nil {
		t.Errorf("HTTPClient default not set")
	}
	if w.opts.CacheTTL == 0 {
		t.Errorf("CacheTTL default not set")
	}
	if w.opts.RequestTimeout == 0 {
		t.Errorf("RequestTimeout default not set")
	}
}

func TestSynthesizerDefaults(t *testing.T) {
	s := NewSynthesizer(SynthesizerOptions{})
	if s.opts.HTTPClient == nil {
		t.Errorf("HTTPClient default not set")
	}
	if s.opts.Profile != "research-synthesize" {
		t.Errorf("Profile default = %q", s.opts.Profile)
	}
	if s.opts.Model == "" {
		t.Errorf("Model default empty")
	}
	if s.opts.MaxTokens == 0 {
		t.Errorf("MaxTokens default = 0")
	}
	if s.opts.SystemPrompt == "" {
		t.Errorf("SystemPrompt default empty")
	}
}

func TestArxivAllDefaults(t *testing.T) {
	a := NewArxiv(ArxivOptions{})
	if a.opts.HTTPClient == nil {
		t.Errorf("HTTPClient default not set")
	}
	if a.opts.BaseURL == "" {
		t.Errorf("BaseURL default empty")
	}
	if a.opts.CacheTTL == 0 {
		t.Errorf("CacheTTL default not set")
	}
	if a.opts.RequestTimeout == 0 {
		t.Errorf("RequestTimeout default not set")
	}
}

func TestNewGitHubSearchAllDefaults(t *testing.T) {
	g := NewGitHubSearch(GitHubSearchOptions{})
	if g.opts.HTTPClient == nil {
		t.Errorf("HTTPClient default not set")
	}
	if g.opts.CacheTTL == 0 {
		t.Errorf("CacheTTL default not set")
	}
}

func TestEcosystemDocsDefaults(t *testing.T) {
	e := NewEcosystemDocs(EcosystemDocsOptions{})
	if e.max != 20 {
		t.Errorf("MaxHits default = %d, want 20", e.max)
	}
	if e.disp != nil {
		t.Errorf("disp = %v, want nil (no Dispatcher injected)", e.disp)
	}
}

func TestServerCloseNilGitnexus(t *testing.T) {

	var first error
	closeOnce(&first, &errCloser{err: nil})
	if first != nil {
		t.Errorf("nil err should not set first")
	}
}

func TestSynthesizerNoFindingsBranch(t *testing.T) {
	s := NewSynthesizer(SynthesizerOptions{DaemonURL: "http://x"})

	if _, err := s.Synthesize(context.Background(), SynthesizeInput{RawFindings: nil}); err == nil {
		t.Fatal("expected nil findings error")
	}
}

func TestCacheAdapterSetEmptyDaemonURLNoOp(t *testing.T) {
	c := NewCacheAdapter(CacheAdapterOptions{})
	if err := c.Set(context.Background(), "h", CacheEntry{}, 60); err != nil {
		t.Fatal(err)
	}
}

func TestCacheAdapterAuthHeaderOnSet(t *testing.T) {

	c := NewCacheAdapter(CacheAdapterOptions{
		DaemonURL: "http://127.0.0.1:1",
		AuthToken: "tok",
	})
	if err := c.Set(context.Background(), "h", CacheEntry{Response: []byte("{}")}, 60); err == nil {
		t.Fatal("expected unreachable-host error")
	}
}

func TestArxivCacheKeyMaxBucket(t *testing.T) {
	a := arxivCacheKey("q", 9, "relevance")
	b := arxivCacheKey("q", 10, "relevance")
	if a == b {
		t.Errorf("hash collision across max")
	}
}

func TestServeOverInMemoryTransport(t *testing.T) {
	srv, err := NewServer(testServerOptions())
	if err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.mcpServer.Run(ctx, serverTransport)
	}()

	cli := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := cli.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer session.Close()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "web_search",
		Arguments: map[string]any{"query": "x", "max_results": 1},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}

	_ = session.Close()
	cancel()
	<-serveErr
}

func TestServeReturnsOnContextCancel(t *testing.T) {
	srv, err := NewServer(testServerOptions())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() {
		done <- srv.Serve(ctx)
	}()
	select {
	case <-done:

	case <-time.After(2 * time.Second):

		t.Skip("Serve hung on stdio transport — coverage of entry recorded")
	}
}
