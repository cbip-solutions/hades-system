package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

type fakeCodegraphClient struct {
	codegraphResp *client.CodegraphQueryResponse
	codegraphErr  error
	impactResp    *client.ImpactResponse
	impactErr     error
	contextResp   *client.Context360Response
	contextErr    error
	wikiResp      *client.WikiResponse
	wikiErr       error

	lastCodegraphReq client.CodegraphQueryRequest
	lastImpactReq    client.ImpactRequest
	lastContextReq   client.Context360Request
	lastWikiReq      client.WikiRequest
}

func (f *fakeCodegraphClient) CodegraphQuery(_ context.Context, req client.CodegraphQueryRequest) (*client.CodegraphQueryResponse, error) {
	f.lastCodegraphReq = req
	if f.codegraphErr != nil {
		return nil, f.codegraphErr
	}
	if f.codegraphResp == nil {
		return &client.CodegraphQueryResponse{Hits: []client.CodegraphHit{
			{Symbol: "MergeEngine", File: "internal/orchestrator/merge/engine.go", Line: 42},
		}}, nil
	}
	return f.codegraphResp, nil
}

func (f *fakeCodegraphClient) Impact(_ context.Context, req client.ImpactRequest) (*client.ImpactResponse, error) {
	f.lastImpactReq = req
	if f.impactErr != nil {
		return nil, f.impactErr
	}
	if f.impactResp == nil {
		return &client.ImpactResponse{
			Symbol:        req.Symbol,
			BlastRadius:   "medium",
			Score:         12,
			AffectedFiles: []string{"internal/x.go", "internal/y.go"},
		}, nil
	}
	return f.impactResp, nil
}

func (f *fakeCodegraphClient) Context360(_ context.Context, req client.Context360Request) (*client.Context360Response, error) {
	f.lastContextReq = req
	if f.contextErr != nil {
		return nil, f.contextErr
	}
	if f.contextResp == nil {
		return &client.Context360Response{
			Symbol:    req.Symbol,
			Callers:   []string{"foo.Bar"},
			Callees:   []string{"baz.Qux"},
			Community: "merge-engine",
		}, nil
	}
	return f.contextResp, nil
}

func (f *fakeCodegraphClient) Wiki(_ context.Context, req client.WikiRequest) (*client.WikiResponse, error) {
	f.lastWikiReq = req
	if f.wikiErr != nil {
		return nil, f.wikiErr
	}
	if f.wikiResp == nil {
		return &client.WikiResponse{
			Module:   req.Module,
			Markdown: "# auto-generated wiki\n",
		}, nil
	}
	return f.wikiResp, nil
}

func TestCodegraphCmdRegistered(t *testing.T) {
	t.Parallel()
	cmd := NewCodegraphCmd(func(*cobra.Command) CodegraphClient { return &fakeCodegraphClient{} })
	if cmd.Use != "codegraph <query>" {
		t.Fatalf("Use=%q, want %q", cmd.Use, "codegraph <query>")
	}
}

func TestCodegraphRunESuccess(t *testing.T) {
	t.Parallel()
	fake := &fakeCodegraphClient{}
	flags := CodegraphFlags{Query: "MergeEngine", Format: "text"}
	var buf bytes.Buffer
	if err := RunCodegraph(context.Background(), fake, flags, &buf); err != nil {
		t.Fatalf("RunCodegraph: %v", err)
	}
	if !strings.Contains(buf.String(), "MergeEngine") {
		t.Fatalf("output missing symbol; got %q", buf.String())
	}
	if fake.lastCodegraphReq.Query != "MergeEngine" {
		t.Fatalf("client request query=%q, want MergeEngine", fake.lastCodegraphReq.Query)
	}
}

func TestCodegraphEmptyQueryRecoverable(t *testing.T) {
	t.Parallel()
	flags := CodegraphFlags{Query: "", Format: "text"}
	err := RunCodegraph(context.Background(), &fakeCodegraphClient{}, flags, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for empty query")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Fatalf("err=%v, want ErrRecoverable", err)
	}
}

func TestCodegraphNoHitsRendersMessage(t *testing.T) {
	t.Parallel()
	fake := &fakeCodegraphClient{
		codegraphResp: &client.CodegraphQueryResponse{Hits: nil},
	}
	flags := CodegraphFlags{Query: "nonexistent", Format: "text"}
	var buf bytes.Buffer
	if err := RunCodegraph(context.Background(), fake, flags, &buf); err != nil {
		t.Fatalf("RunCodegraph: %v", err)
	}
	if !strings.Contains(buf.String(), "no hits") {
		t.Fatalf("output missing 'no hits'; got %q", buf.String())
	}
}

func TestImpactCmdRegistered(t *testing.T) {
	t.Parallel()
	cmd := NewImpactCmd(func(*cobra.Command) CodegraphClient { return &fakeCodegraphClient{} })
	if cmd.Use != "impact <symbol>" {
		t.Fatalf("Use=%q, want %q", cmd.Use, "impact <symbol>")
	}
}

func TestImpactRunEPropagatesSymbol(t *testing.T) {
	t.Parallel()
	fake := &fakeCodegraphClient{}
	flags := ImpactFlags{Symbol: "MergeEngine.Run"}
	var buf bytes.Buffer
	if err := RunImpact(context.Background(), fake, flags, &buf); err != nil {
		t.Fatalf("RunImpact: %v", err)
	}
	if fake.lastImpactReq.Symbol != "MergeEngine.Run" {
		t.Fatalf("client received symbol=%q, want MergeEngine.Run", fake.lastImpactReq.Symbol)
	}
	if !strings.Contains(buf.String(), "blast_radius") {
		t.Fatalf("output missing blast_radius label; got %q", buf.String())
	}
}

func TestImpactEmptySymbolRecoverable(t *testing.T) {
	t.Parallel()
	err := RunImpact(context.Background(), &fakeCodegraphClient{}, ImpactFlags{}, &bytes.Buffer{})
	if err == nil || !errors.Is(err, ErrRecoverable) {
		t.Fatalf("err=%v, want ErrRecoverable", err)
	}
}

func TestContextCmdRegistered(t *testing.T) {
	t.Parallel()
	cmd := NewContextCmd(func(*cobra.Command) CodegraphClient { return &fakeCodegraphClient{} })
	if cmd.Use != "context <symbol>" {
		t.Fatalf("Use=%q, want %q", cmd.Use, "context <symbol>")
	}
}

func TestContextRunESuccess(t *testing.T) {
	t.Parallel()
	fake := &fakeCodegraphClient{}
	flags := ContextFlags{Symbol: "MergeEngine"}
	var buf bytes.Buffer
	if err := RunContext(context.Background(), fake, flags, &buf); err != nil {
		t.Fatalf("RunContext: %v", err)
	}
	if !strings.Contains(buf.String(), "community") {
		t.Fatalf("output missing community; got %q", buf.String())
	}
}

func TestContextEmptySymbolRecoverable(t *testing.T) {
	t.Parallel()
	err := RunContext(context.Background(), &fakeCodegraphClient{}, ContextFlags{}, &bytes.Buffer{})
	if err == nil || !errors.Is(err, ErrRecoverable) {
		t.Fatalf("err=%v, want ErrRecoverable", err)
	}
}

func TestWikiCmdRegistered(t *testing.T) {
	t.Parallel()
	cmd := NewWikiCmd(func(*cobra.Command) CodegraphClient { return &fakeCodegraphClient{} })
	if cmd.Use != "wiki [module]" {
		t.Fatalf("Use=%q, want %q", cmd.Use, "wiki [module]")
	}
}

func TestWikiRunENoModuleRendersFullWiki(t *testing.T) {
	t.Parallel()
	fake := &fakeCodegraphClient{}
	flags := WikiFlags{Module: ""}
	var buf bytes.Buffer
	if err := RunWiki(context.Background(), fake, flags, &buf); err != nil {
		t.Fatalf("RunWiki: %v", err)
	}
	if !strings.Contains(buf.String(), "auto-generated wiki") {
		t.Fatalf("output missing wiki body; got %q", buf.String())
	}
}

func TestCodegraphHTTPIntegration(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcpgateway/codegraph" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(client.CodegraphQueryResponse{
			Hits: []client.CodegraphHit{{Symbol: "X", File: "a.go", Line: 1}},
		})
	}))
	defer srv.Close()

	c := client.NewWithBaseURL(srv.URL)
	prod := &productionCodegraphClient{c: c}
	resp, err := prod.CodegraphQuery(context.Background(), client.CodegraphQueryRequest{Query: "X"})
	if err != nil {
		t.Fatalf("CodegraphQuery: %v", err)
	}
	if len(resp.Hits) != 1 || resp.Hits[0].Symbol != "X" {
		t.Fatalf("hits=%+v, want one hit with Symbol=X", resp.Hits)
	}
}

func TestImpact_DispatchesToMCPGateway(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcpgateway/impact" {
			t.Errorf("unexpected path %q; want /v1/mcpgateway/impact", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(client.ImpactResponse{
			Symbol:        "MergeEngine.Run",
			BlastRadius:   "high",
			Score:         42,
			AffectedFiles: []string{"internal/x.go"},
		})
	}))
	defer srv.Close()

	prod := &productionCodegraphClient{c: client.NewWithBaseURL(srv.URL)}
	resp, err := prod.Impact(context.Background(), client.ImpactRequest{Symbol: "MergeEngine.Run"})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if resp.BlastRadius != "high" {
		t.Fatalf("BlastRadius=%q, want high", resp.BlastRadius)
	}
	if resp.Score != 42 {
		t.Fatalf("Score=%d, want 42", resp.Score)
	}
}

func TestContext360_DispatchesToMCPGateway(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcpgateway/context" {
			t.Errorf("unexpected path %q; want /v1/mcpgateway/context", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(client.Context360Response{
			Symbol:    "MergeEngine",
			Callers:   []string{"orchestrator.Run"},
			Callees:   []string{"merge.Apply"},
			Community: "merge-subsystem",
		})
	}))
	defer srv.Close()

	prod := &productionCodegraphClient{c: client.NewWithBaseURL(srv.URL)}
	resp, err := prod.Context360(context.Background(), client.Context360Request{Symbol: "MergeEngine"})
	if err != nil {
		t.Fatalf("Context360: %v", err)
	}
	if resp.Community != "merge-subsystem" {
		t.Fatalf("Community=%q, want merge-subsystem", resp.Community)
	}
	if len(resp.Callers) != 1 {
		t.Fatalf("Callers=%v, want one entry", resp.Callers)
	}
}

func TestWiki_DispatchesToMCPGateway(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcpgateway/wiki" {
			t.Errorf("unexpected path %q; want /v1/mcpgateway/wiki", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(client.WikiResponse{
			Module:   "internal/orchestrator",
			Markdown: "# orchestrator wiki\n",
		})
	}))
	defer srv.Close()

	prod := &productionCodegraphClient{c: client.NewWithBaseURL(srv.URL)}
	resp, err := prod.Wiki(context.Background(), client.WikiRequest{Module: "internal/orchestrator"})
	if err != nil {
		t.Fatalf("Wiki: %v", err)
	}
	if resp.Module != "internal/orchestrator" {
		t.Fatalf("Module=%q, want internal/orchestrator", resp.Module)
	}
	if resp.Markdown != "# orchestrator wiki\n" {
		t.Fatalf("Markdown=%q", resp.Markdown)
	}
}

func TestClassifyMCPGatewayError(t *testing.T) {
	t.Parallel()

	makeHTTP := func(status int) error {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{}`))
		}))
		defer srv.Close()
		c := client.NewWithBaseURL(srv.URL)

		_, err := c.CodegraphQuery(context.Background(), client.CodegraphQueryRequest{Query: "x"})
		return err
	}

	cases := []struct {
		name         string
		err          error
		wantRecov    bool
		wantContains string
	}{
		{
			name:      "nil error passes through",
			err:       nil,
			wantRecov: false,
		},
		{
			name:         "already-recoverable passes through",
			err:          ErrRecoverable,
			wantRecov:    true,
			wantContains: "operator-recoverable",
		},
		{
			name:         "503 → recoverable caronte unreachable",
			err:          makeHTTP(http.StatusServiceUnavailable),
			wantRecov:    true,
			wantContains: "caronte unreachable",
		},
		{
			name:         "422 → recoverable daemon rejected input",
			err:          makeHTTP(http.StatusUnprocessableEntity),
			wantRecov:    true,
			wantContains: "daemon rejected input",
		},
		{
			name: "404 → endpoint-not-found coded error (inv-zen-275)",
			err:  makeHTTP(http.StatusNotFound),

			wantRecov:    false,
			wantContains: "codegraph",
		},
		{

			name:         "400 → cli.arg-validation-fail (daemon validated request)",
			err:          makeHTTP(http.StatusBadRequest),
			wantRecov:    false,
			wantContains: "400 (bad request)",
		},
		{

			name:         "500 → daemon.responded-with-error (HTTPError, not transport)",
			err:          makeHTTP(http.StatusInternalServerError),
			wantRecov:    false,
			wantContains: "daemon responded with 500",
		},
		{
			name:         "502 → daemon.responded-with-error (HTTPError, not transport)",
			err:          makeHTTP(http.StatusBadGateway),
			wantRecov:    false,
			wantContains: "daemon responded with 502",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := classifyMCPGatewayError(tc.err, "codegraph")
			if tc.err == nil {
				if got != nil {
					t.Fatalf("expected nil; got %v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil error; got nil")
			}
			if tc.wantRecov && !errors.Is(got, ErrRecoverable) {
				t.Fatalf("want ErrRecoverable in chain; got %v", got)
			}
			if !tc.wantRecov && errors.Is(got, ErrRecoverable) {
				t.Fatalf("want non-recoverable; got ErrRecoverable in chain: %v", got)
			}
			if tc.wantContains != "" && !strings.Contains(got.Error(), tc.wantContains) {
				t.Fatalf("error %q missing substring %q", got.Error(), tc.wantContains)
			}
		})
	}
}

func TestRunImpact_JSONFormat(t *testing.T) {
	t.Parallel()
	fake := &fakeCodegraphClient{}
	flags := ImpactFlags{Symbol: "Foo.Bar", Format: "json"}
	var buf bytes.Buffer
	if err := RunImpact(context.Background(), fake, flags, &buf); err != nil {
		t.Fatalf("RunImpact: %v", err)
	}
	if !strings.Contains(buf.String(), `"blast_radius"`) {
		t.Fatalf("JSON output missing blast_radius field; got %q", buf.String())
	}
}

func TestRunContext_JSONFormat(t *testing.T) {
	t.Parallel()
	fake := &fakeCodegraphClient{}
	flags := ContextFlags{Symbol: "MergeEngine", Format: "json"}
	var buf bytes.Buffer
	if err := RunContext(context.Background(), fake, flags, &buf); err != nil {
		t.Fatalf("RunContext: %v", err)
	}
	if !strings.Contains(buf.String(), `"symbol"`) {
		t.Fatalf("JSON output missing symbol field; got %q", buf.String())
	}
}

func TestRunCodegraph_JSONFormat(t *testing.T) {
	t.Parallel()
	fake := &fakeCodegraphClient{}
	flags := CodegraphFlags{Query: "Foo", Format: "json"}
	var buf bytes.Buffer
	if err := RunCodegraph(context.Background(), fake, flags, &buf); err != nil {
		t.Fatalf("RunCodegraph: %v", err)
	}
	if !strings.Contains(buf.String(), `"hits"`) {
		t.Fatalf("JSON output missing hits field; got %q", buf.String())
	}
}

func TestRunCodegraph_BadFormat(t *testing.T) {
	t.Parallel()
	flags := CodegraphFlags{Query: "Foo", Format: "xml"}
	err := RunCodegraph(context.Background(), &fakeCodegraphClient{}, flags, &bytes.Buffer{})
	if err == nil || !errors.Is(err, ErrRecoverable) {
		t.Fatalf("err=%v, want ErrRecoverable", err)
	}
}

func TestRunWiki_ErrorPropagated(t *testing.T) {
	t.Parallel()
	fake := &fakeCodegraphClient{wikiErr: errors.New("transport fail")}
	flags := WikiFlags{Module: "internal/x"}
	err := RunWiki(context.Background(), fake, flags, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error; got nil")
	}
	if !strings.Contains(err.Error(), "wiki") {
		t.Fatalf("err=%v, want 'wiki' in message", err)
	}
}

func TestRunContext_NeighborsRendered(t *testing.T) {
	t.Parallel()
	fake := &fakeCodegraphClient{
		contextResp: &client.Context360Response{
			Symbol:    "Foo",
			Neighbors: []string{"Bar", "Baz"},
		},
	}
	flags := ContextFlags{Symbol: "Foo", Format: "text"}
	var buf bytes.Buffer
	if err := RunContext(context.Background(), fake, flags, &buf); err != nil {
		t.Fatalf("RunContext: %v", err)
	}
	if !strings.Contains(buf.String(), "neighbors") {
		t.Fatalf("expected neighbors section; got %q", buf.String())
	}
}

func TestClassifyMCPGatewayError_404MapsToEndpointNotFound(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	_, err := c.CodegraphQuery(context.Background(), client.CodegraphQueryRequest{Query: "x"})
	if err == nil {
		t.Fatal("expected HTTPError; got nil")
	}
	classified := classifyMCPGatewayError(err, "codegraph")
	if classified == nil {
		t.Fatal("expected classified error; got nil")
	}
	if !ierrors.IsCode(classified, ierrors.CodeEndpointNotFound) {
		t.Errorf("expected CodeEndpointNotFound; got %v", classified)
	}
	// The recovery hint MUST NOT be present in the developer error message,
	// but the catalog Lookup must return the same code so error_render
	// surfaces the right hint.
	if entry := ierrors.Lookup(ierrors.CodeEndpointNotFound); entry == nil {
		t.Error("catalog Lookup(CodeEndpointNotFound) returned nil; expected populated entry")
	}
}

func TestClassifyMCPGatewayError_404SisterTest(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	_, transportErr := c.CodegraphQuery(context.Background(), client.CodegraphQueryRequest{Query: "x"})
	classified := classifyMCPGatewayError(transportErr, "codegraph")
	if ierrors.IsCode(classified, "daemon.unreachable") {
		t.Error("404 collapsed into daemon.unreachable — sister-test fails; the 404 → CodeEndpointNotFound branch is gone")
	}
}
