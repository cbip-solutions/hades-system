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
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
)

type fakeKnowledgeClient struct {
	queryRows   []client.KnowledgeResultRow
	queryErr    error
	reindexResp *client.KnowledgeReindexResponse
	reindexErr  error
	stats       *client.KnowledgeStatsResponse
	statsErr    error

	promoteResp *client.KnowledgePromoteResponse
	promoteErr  error
	syncResp    *client.KnowledgeSyncResponse
	syncErr     error
	restoreResp *client.KnowledgeRestoreResponse
	restoreErr  error

	ecoResp   *client.EcosystemQueryResponse
	ecoErr    error
	ecoCalled int

	lastQueryReq   client.KnowledgeQueryRequest
	lastReindexReq client.KnowledgeReindexRequest
	lastPromoteID  string
	lastSyncReq    client.KnowledgeSyncRequest
	lastRestoreReq client.KnowledgeRestoreRequest
	lastEcoReq     client.EcosystemQueryRequest
}

func (f *fakeKnowledgeClient) KnowledgeQuery(_ context.Context, req client.KnowledgeQueryRequest) ([]client.KnowledgeResultRow, error) {
	f.lastQueryReq = req
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return f.queryRows, nil
}

func (f *fakeKnowledgeClient) KnowledgeReindex(_ context.Context, req client.KnowledgeReindexRequest) (*client.KnowledgeReindexResponse, error) {
	f.lastReindexReq = req
	if f.reindexErr != nil {
		return nil, f.reindexErr
	}
	if f.reindexResp == nil {
		return &client.KnowledgeReindexResponse{OK: true}, nil
	}
	return f.reindexResp, nil
}

func (f *fakeKnowledgeClient) KnowledgeStats(_ context.Context) (*client.KnowledgeStatsResponse, error) {
	if f.statsErr != nil {
		return nil, f.statsErr
	}
	if f.stats == nil {
		return &client.KnowledgeStatsResponse{ByType: map[string]int{}}, nil
	}
	return f.stats, nil
}

func (f *fakeKnowledgeClient) KnowledgePromote(_ context.Context, req client.KnowledgePromoteRequest) (*client.KnowledgePromoteResponse, error) {
	f.lastPromoteID = req.ID
	if f.promoteErr != nil {
		return nil, f.promoteErr
	}
	if f.promoteResp == nil {
		return &client.KnowledgePromoteResponse{ID: req.ID, Status: "promoted", Scope: "global"}, nil
	}
	return f.promoteResp, nil
}

func (f *fakeKnowledgeClient) KnowledgeSync(_ context.Context, req client.KnowledgeSyncRequest) (*client.KnowledgeSyncResponse, error) {
	f.lastSyncReq = req
	if f.syncErr != nil {
		return nil, f.syncErr
	}
	if f.syncResp == nil {
		return &client.KnowledgeSyncResponse{RowsIndexed: 0, DurationMs: 0}, nil
	}
	return f.syncResp, nil
}

func (f *fakeKnowledgeClient) KnowledgeRestore(_ context.Context, req client.KnowledgeRestoreRequest) (*client.KnowledgeRestoreResponse, error) {
	f.lastRestoreReq = req
	if f.restoreErr != nil {
		return nil, f.restoreErr
	}
	if f.restoreResp == nil {
		return &client.KnowledgeRestoreResponse{ProjectAlias: req.ProjectAlias, SnapshotID: "snap-test"}, nil
	}
	return f.restoreResp, nil
}

func (f *fakeKnowledgeClient) EcosystemQuery(_ context.Context, req client.EcosystemQueryRequest) (*client.EcosystemQueryResponse, error) {
	f.lastEcoReq = req
	f.ecoCalled++
	if f.ecoErr != nil {
		return nil, f.ecoErr
	}
	if f.ecoResp == nil {
		return &client.EcosystemQueryResponse{}, nil
	}
	return f.ecoResp, nil
}

func TestKnowledgeCmdRegistersSubcommands(t *testing.T) {
	cmd := NewKnowledgeCmdProd()
	if cmd.Use != "knowledge" {
		t.Errorf("Use = %q, want knowledge", cmd.Use)
	}
	wantSubs := []string{"query", "reindex", "stats", "promote", "sync", "restore"}
	for _, name := range wantSubs {
		found := false
		for _, sc := range cmd.Commands() {
			if strings.HasPrefix(sc.Use, name) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("subcommand %q not registered", name)
		}
	}
}

func TestKnowledgeQueryRemoteFlagDispatchesToEcosystem(t *testing.T) {
	c := &fakeKnowledgeClient{ecoResp: &client.EcosystemQueryResponse{}}
	var buf bytes.Buffer
	flags := KnowledgeQueryFlags{Remote: true, FreeText: "anything"}
	if err := RunKnowledgeQuery(context.Background(), c, flags, &buf); err != nil {
		t.Fatalf("RunKnowledgeQuery: %v", err)
	}
	if c.ecoCalled != 1 {
		t.Errorf("EcosystemQuery should be called once on --remote; called=%d", c.ecoCalled)
	}
	if c.lastQueryReq.FreeText != "" {
		t.Errorf("--remote leaked to Plan 7 aggregator (FreeText=%q); inv-zen-129 boundary violation", c.lastQueryReq.FreeText)
	}
	if c.lastEcoReq.Query != "anything" {
		t.Errorf("Query = %q, want anything", c.lastEcoReq.Query)
	}
}

func TestKnowledgeQueryAuditChainFlagShortCircuits(t *testing.T) {
	c := &fakeKnowledgeClient{}
	var buf bytes.Buffer
	flags := KnowledgeQueryFlags{AuditChain: true, FreeText: "anything"}
	if err := RunKnowledgeQuery(context.Background(), c, flags, &buf); err != nil {
		t.Fatalf("RunKnowledgeQuery: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Plan 9") {
		t.Errorf("missing 'Plan 9' marker: %s", out)
	}
	if !strings.Contains(out, "not yet shipped") {
		t.Errorf("missing 'not yet shipped' marker: %s", out)
	}
	if c.lastQueryReq.FreeText != "" {
		t.Errorf("--audit-chain leaked to client (FreeText=%q); inv-zen-129 violation", c.lastQueryReq.FreeText)
	}
}

func TestKnowledgeQueryFlagsToWire(t *testing.T) {
	c := &fakeKnowledgeClient{}
	var buf bytes.Buffer
	flags := KnowledgeQueryFlags{
		FreeText:   "abc",
		Since:      "24h",
		Projects:   []string{"internal-platform-x", "zen-swarm"},
		Types:      []string{"memory", "adr"},
		Limit:      7,
		Format:     "json",
		CodeSymbol: "fooFn",
	}
	if err := RunKnowledgeQuery(context.Background(), c, flags, &buf); err != nil {
		t.Fatalf("RunKnowledgeQuery: %v", err)
	}
	if c.lastQueryReq.FreeText != "abc" {
		t.Errorf("FreeText = %q", c.lastQueryReq.FreeText)
	}
	if len(c.lastQueryReq.ProjectAlias) != 2 || c.lastQueryReq.ProjectAlias[0] != "internal-platform-x" {
		t.Errorf("ProjectAlias = %v", c.lastQueryReq.ProjectAlias)
	}
	if len(c.lastQueryReq.Type) != 2 || c.lastQueryReq.Type[0] != "memory" {
		t.Errorf("Type = %v", c.lastQueryReq.Type)
	}
	if c.lastQueryReq.SinceSeconds != 86400 {
		t.Errorf("SinceSeconds = %d, want 86400", c.lastQueryReq.SinceSeconds)
	}
	if c.lastQueryReq.Limit != 7 {
		t.Errorf("Limit = %d", c.lastQueryReq.Limit)
	}
	if c.lastQueryReq.CodeSymbol != "fooFn" {
		t.Errorf("CodeSymbol = %q", c.lastQueryReq.CodeSymbol)
	}
}

func TestKnowledgeQueryInvalidSinceRejected(t *testing.T) {
	c := &fakeKnowledgeClient{}
	var buf bytes.Buffer
	flags := KnowledgeQueryFlags{Since: "not-a-duration"}
	err := RunKnowledgeQuery(context.Background(), c, flags, &buf)
	if err == nil {
		t.Fatal("expected error on invalid --since")
	}
	if !IsRecoverable(err) {
		t.Errorf("invalid --since should be recoverable: %v", err)
	}
}

func TestKnowledgeQueryNegativeSinceRejected(t *testing.T) {
	c := &fakeKnowledgeClient{}
	var buf bytes.Buffer
	flags := KnowledgeQueryFlags{Since: "-1h"}
	err := RunKnowledgeQuery(context.Background(), c, flags, &buf)
	if err == nil {
		t.Fatal("expected error on negative --since")
	}
	if !IsRecoverable(err) {
		t.Errorf("negative --since should be recoverable: %v", err)
	}
}

func TestKnowledgeQueryInvalidTypeRejected(t *testing.T) {
	c := &fakeKnowledgeClient{}
	var buf bytes.Buffer
	flags := KnowledgeQueryFlags{Types: []string{"banana"}}
	err := RunKnowledgeQuery(context.Background(), c, flags, &buf)
	if err == nil {
		t.Fatal("expected error on invalid --type")
	}
	if !IsRecoverable(err) {
		t.Errorf("invalid --type should be recoverable: %v", err)
	}
}

func TestKnowledgeQueryInvalidFormatRejected(t *testing.T) {
	c := &fakeKnowledgeClient{}
	var buf bytes.Buffer
	flags := KnowledgeQueryFlags{Format: "xml"}
	err := RunKnowledgeQuery(context.Background(), c, flags, &buf)
	if err == nil {
		t.Fatal("expected error on invalid --format")
	}
	if !IsRecoverable(err) {
		t.Errorf("invalid --format should be recoverable: %v", err)
	}
}

func TestKnowledgeQueryDefaultLimit(t *testing.T) {
	c := &fakeKnowledgeClient{}
	var buf bytes.Buffer
	if err := RunKnowledgeQuery(context.Background(), c, KnowledgeQueryFlags{}, &buf); err != nil {
		t.Fatalf("RunKnowledgeQuery: %v", err)
	}
	if c.lastQueryReq.Limit != defaultKnowledgeLimit {
		t.Errorf("default Limit = %d, want %d", c.lastQueryReq.Limit, defaultKnowledgeLimit)
	}
}

func TestKnowledgeQueryRendersJSON(t *testing.T) {
	c := &fakeKnowledgeClient{
		queryRows: []client.KnowledgeResultRow{
			{
				FilePath:     "/tmp/x.md",
				ProjectAlias: "internal-platform-x",
				FileType:     "memory",
				Title:        "X",
				Score:        0.5,
				Snippet:      "[hi] world",
				LastModified: time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC),
			},
		},
	}
	var buf bytes.Buffer
	flags := KnowledgeQueryFlags{Format: "json"}
	if err := RunKnowledgeQuery(context.Background(), c, flags, &buf); err != nil {
		t.Fatalf("RunKnowledgeQuery: %v", err)
	}

	var rows []client.KnowledgeResultRow
	if err := json.Unmarshal(buf.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v\nout=%s", err, buf.String())
	}
	if len(rows) != 1 {
		t.Errorf("rows = %d, want 1", len(rows))
	}
	if rows[0].Title != "X" {
		t.Errorf("Title = %q", rows[0].Title)
	}
}

func TestKnowledgeQueryRendersMD(t *testing.T) {
	c := &fakeKnowledgeClient{
		queryRows: []client.KnowledgeResultRow{
			{
				FilePath:     "/tmp/x.md",
				ProjectAlias: "internal-platform-x",
				FileType:     "memory",
				Title:        "Title X",
				Snippet:      "snippet",
				LastModified: time.Now().UTC(),
			},
		},
	}
	var buf bytes.Buffer
	flags := KnowledgeQueryFlags{Format: "md"}
	if err := RunKnowledgeQuery(context.Background(), c, flags, &buf); err != nil {
		t.Fatalf("RunKnowledgeQuery: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "## Title X") {
		t.Errorf("md missing H2 title: %s", out)
	}
	if !strings.Contains(out, "internal-platform-x") {
		t.Errorf("md missing project: %s", out)
	}
	if !strings.Contains(out, "snippet") {
		t.Errorf("md missing snippet: %s", out)
	}
}

func TestKnowledgeQueryRendersText(t *testing.T) {
	c := &fakeKnowledgeClient{
		queryRows: []client.KnowledgeResultRow{
			{Title: "TitleAA", ProjectAlias: "internal-platform-x", FileType: "memory", Snippet: "snippet"},
		},
	}
	var buf bytes.Buffer
	flags := KnowledgeQueryFlags{Format: "text"}
	if err := RunKnowledgeQuery(context.Background(), c, flags, &buf); err != nil {
		t.Fatalf("RunKnowledgeQuery: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "TitleAA") {
		t.Errorf("text missing title: %s", out)
	}
}

func TestKnowledgeQueryEmptyRendersHelpfulText(t *testing.T) {
	c := &fakeKnowledgeClient{}
	var buf bytes.Buffer
	if err := RunKnowledgeQuery(context.Background(), c, KnowledgeQueryFlags{}, &buf); err != nil {
		t.Fatalf("RunKnowledgeQuery: %v", err)
	}
	if !strings.Contains(buf.String(), "no results") {
		t.Errorf("empty text-mode output should mention 'no results', got: %s", buf.String())
	}
}

func TestKnowledgeQuery503Unrecoverable(t *testing.T) {
	c := &fakeKnowledgeClient{queryErr: &client.HTTPError{Status: http.StatusServiceUnavailable, Path: "/v1/knowledge/query"}}
	var buf bytes.Buffer
	err := RunKnowledgeQuery(context.Background(), c, KnowledgeQueryFlags{}, &buf)
	if err == nil {
		t.Fatal("expected 503 to propagate")
	}
	if IsRecoverable(err) {
		t.Errorf("503 should be unrecoverable, got: %v", err)
	}
}

func TestKnowledgeReindexFullDefault(t *testing.T) {
	c := &fakeKnowledgeClient{reindexResp: &client.KnowledgeReindexResponse{OK: true, Indexed: 42}}
	var buf bytes.Buffer
	flags := KnowledgeReindexFlags{}
	if err := RunKnowledgeReindex(context.Background(), c, flags, &buf); err != nil {
		t.Fatalf("RunKnowledgeReindex: %v", err)
	}

	if !c.lastReindexReq.Full {
		t.Error("Full default = false, want true")
	}
	if !strings.Contains(buf.String(), "42") {
		t.Errorf("output missing indexed count: %s", buf.String())
	}
}

func TestKnowledgeReindexPerProject(t *testing.T) {
	c := &fakeKnowledgeClient{reindexResp: &client.KnowledgeReindexResponse{OK: true, Indexed: 5}}
	var buf bytes.Buffer
	flags := KnowledgeReindexFlags{Project: "internal-platform-x"}
	if err := RunKnowledgeReindex(context.Background(), c, flags, &buf); err != nil {
		t.Fatalf("RunKnowledgeReindex: %v", err)
	}
	if c.lastReindexReq.ProjectAlias != "internal-platform-x" {
		t.Errorf("ProjectAlias = %q", c.lastReindexReq.ProjectAlias)
	}
	if c.lastReindexReq.Full {
		t.Error("Full = true; project-scoped request should not flip Full")
	}
}

func TestKnowledgeReindex503Unrecoverable(t *testing.T) {
	c := &fakeKnowledgeClient{reindexErr: &client.HTTPError{Status: http.StatusServiceUnavailable, Path: "/v1/knowledge/reindex"}}
	var buf bytes.Buffer
	err := RunKnowledgeReindex(context.Background(), c, KnowledgeReindexFlags{}, &buf)
	if err == nil {
		t.Fatal("expected 503 to propagate")
	}
	if IsRecoverable(err) {
		t.Errorf("503 should be unrecoverable, got: %v", err)
	}
}

func TestKnowledgeStatsHappy(t *testing.T) {
	c := &fakeKnowledgeClient{
		stats: &client.KnowledgeStatsResponse{
			TotalDocs: 100,
			ByType:    map[string]int{"memory": 80, "adr": 20},
		},
	}
	var buf bytes.Buffer
	if err := RunKnowledgeStats(context.Background(), c, KnowledgeStatsFlags{}, &buf); err != nil {
		t.Fatalf("RunKnowledgeStats: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "100") {
		t.Errorf("output missing total: %s", out)
	}
	if !strings.Contains(out, "memory") {
		t.Errorf("output missing memory bucket: %s", out)
	}
}

func TestKnowledgeStats503Unrecoverable(t *testing.T) {
	c := &fakeKnowledgeClient{statsErr: &client.HTTPError{Status: http.StatusServiceUnavailable, Path: "/v1/knowledge/stats"}}
	var buf bytes.Buffer
	err := RunKnowledgeStats(context.Background(), c, KnowledgeStatsFlags{}, &buf)
	if err == nil {
		t.Fatal("expected 503 to propagate")
	}
	if IsRecoverable(err) {
		t.Errorf("503 should be unrecoverable, got: %v", err)
	}
}

func TestKnowledgeQuery_CLIToDaemonHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/knowledge/query" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"rows": []map[string]any{{"title": "Z"}}})
	}))
	defer srv.Close()

	c := &productionKnowledgeClient{c: client.NewWithBaseURL(srv.URL)}
	var buf bytes.Buffer
	if err := RunKnowledgeQuery(context.Background(), c, KnowledgeQueryFlags{Format: "json"}, &buf); err != nil {
		t.Fatalf("RunKnowledgeQuery: %v", err)
	}
	if !strings.Contains(buf.String(), `"title": "Z"`) && !strings.Contains(buf.String(), `"Title": "Z"`) {
		t.Errorf("output missing title Z: %s", buf.String())
	}
}

func TestKnowledgeStats_CLIToDaemonHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/knowledge/stats" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_docs":7,"by_type":{"memory":7},"last_indexed_unix":1715000000}`))
	}))
	defer srv.Close()

	c := &productionKnowledgeClient{c: client.NewWithBaseURL(srv.URL)}
	var buf bytes.Buffer
	if err := RunKnowledgeStats(context.Background(), c, KnowledgeStatsFlags{}, &buf); err != nil {
		t.Fatalf("RunKnowledgeStats: %v", err)
	}
	if !strings.Contains(buf.String(), "7") {
		t.Errorf("output missing 7: %s", buf.String())
	}
}

func TestKnowledgeQueryRemoteRoutesToEcosystemEndpoint(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"chunks":[],"abstained":false,"provenance":{}}`))
	}))
	defer srv.Close()

	c := &productionKnowledgeClient{c: client.NewWithBaseURL(srv.URL)}
	var buf bytes.Buffer
	if err := RunKnowledgeQuery(context.Background(), c, KnowledgeQueryFlags{Remote: true, FreeText: "anything"}, &buf); err != nil {
		t.Fatalf("RunKnowledgeQuery: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected 1 daemon call, got %d: %v", len(paths), paths)
	}
	if paths[0] != "/v1/knowledge/ecosystem/query" {
		t.Errorf("path = %q, want /v1/knowledge/ecosystem/query", paths[0])
	}
}

func TestKnowledgeQueryWrapsTransportAsUnrecoverable(t *testing.T) {

	c := &fakeKnowledgeClient{queryErr: errors.New("transport boom")}
	var buf bytes.Buffer
	err := RunKnowledgeQuery(context.Background(), c, KnowledgeQueryFlags{}, &buf)
	if err == nil {
		t.Fatal("expected error")
	}
	if IsRecoverable(err) {
		t.Errorf("transport error should be unrecoverable: %v", err)
	}
}

func TestKnowledgeQuery422Recoverable(t *testing.T) {
	c := &fakeKnowledgeClient{queryErr: &client.HTTPError{Status: http.StatusUnprocessableEntity, Path: "/v1/knowledge/query"}}
	var buf bytes.Buffer
	err := RunKnowledgeQuery(context.Background(), c, KnowledgeQueryFlags{}, &buf)
	if err == nil {
		t.Fatal("expected 422 to propagate")
	}
	if !IsRecoverable(err) {
		t.Errorf("422 should be recoverable, got: %v", err)
	}
}

func TestClassifyKnowledgeErrorNil(t *testing.T) {
	if err := classifyKnowledgeError(nil, "x"); err != nil {
		t.Errorf("nil input should yield nil, got: %v", err)
	}
}

func TestClassifyKnowledgeErrorPreservesRecoverable(t *testing.T) {
	rec := recoverable("synthetic")
	out := classifyKnowledgeError(rec, "x")
	if !IsRecoverable(out) {
		t.Errorf("classification dropped recoverable tag: %v", out)
	}
}

func TestKnowledgeReindex422Recoverable(t *testing.T) {
	c := &fakeKnowledgeClient{reindexErr: &client.HTTPError{Status: http.StatusUnprocessableEntity, Path: "/v1/knowledge/reindex"}}
	var buf bytes.Buffer
	err := RunKnowledgeReindex(context.Background(), c, KnowledgeReindexFlags{}, &buf)
	if err == nil {
		t.Fatal("expected 422 to propagate")
	}
	if !IsRecoverable(err) {
		t.Errorf("422 should be recoverable, got: %v", err)
	}
}

func TestKnowledgeQueryRendersMDEmpty(t *testing.T) {
	c := &fakeKnowledgeClient{}
	var buf bytes.Buffer
	if err := RunKnowledgeQuery(context.Background(), c, KnowledgeQueryFlags{Format: "md"}, &buf); err != nil {
		t.Fatalf("RunKnowledgeQuery: %v", err)
	}
	if !strings.Contains(buf.String(), "_no results_") {
		t.Errorf("md empty should render placeholder, got: %s", buf.String())
	}
}

func TestKnowledgeStatsEmptyRendersHelpfulText(t *testing.T) {
	c := &fakeKnowledgeClient{stats: &client.KnowledgeStatsResponse{ByType: map[string]int{}}}
	var buf bytes.Buffer
	if err := RunKnowledgeStats(context.Background(), c, KnowledgeStatsFlags{}, &buf); err != nil {
		t.Fatalf("RunKnowledgeStats: %v", err)
	}
	if !strings.Contains(buf.String(), "(empty)") {
		t.Errorf("expected '(empty)' marker for fresh index, got: %s", buf.String())
	}
}

func TestKnowledgeStatsWithSchema(t *testing.T) {
	c := &fakeKnowledgeClient{stats: &client.KnowledgeStatsResponse{TotalDocs: 1, ByType: map[string]int{"memory": 1}, LastIndexedUnix: 1715000000}}
	var buf bytes.Buffer
	if err := RunKnowledgeStats(context.Background(), c, KnowledgeStatsFlags{Schema: true}, &buf); err != nil {
		t.Fatalf("RunKnowledgeStats: %v", err)
	}
	if !strings.Contains(buf.String(), "061_knowledge_index_extension_hooks") {
		t.Errorf("expected migration filename in --schema output, got: %s", buf.String())
	}
}

func TestKnowledgeReindexCmdParsesFlags(t *testing.T) {
	got := client.KnowledgeReindexRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(client.KnowledgeReindexResponse{OK: true, Indexed: 1})
	}))
	defer srv.Close()

	cmd := NewKnowledgeCmd(func(_ *cobra.Command) KnowledgeClient {
		return &productionKnowledgeClient{c: client.NewWithBaseURL(srv.URL)}
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"reindex", "--project", "internal-platform-x"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.ProjectAlias != "internal-platform-x" {
		t.Errorf("ProjectAlias = %q", got.ProjectAlias)
	}
}

func TestKnowledgeStatsCmdParsesFlags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_docs":3,"by_type":{"memory":3},"last_indexed_unix":1715000000}`))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	cmd := NewKnowledgeCmd(func(_ *cobra.Command) KnowledgeClient {
		return &productionKnowledgeClient{c: client.NewWithBaseURL(srv.URL)}
	})
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"stats", "--schema"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "total_docs=3") {
		t.Errorf("missing total_docs=3: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "061_knowledge_index_extension_hooks") {
		t.Errorf("missing schema reference: %s", buf.String())
	}
}

func TestKnowledgeQueryCmdParsesAllFlags(t *testing.T) {
	got := client.KnowledgeQueryRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rows":[]}`))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	cmd := NewKnowledgeCmd(func(_ *cobra.Command) KnowledgeClient {
		return &productionKnowledgeClient{c: client.NewWithBaseURL(srv.URL)}
	})
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"query", "hello", "--type", "memory", "--project", "internal-platform-x",
		"--since", "7h", "--limit", "5", "--format", "json", "--code-symbol", "fooFn"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.FreeText != "hello" {
		t.Errorf("FreeText = %q", got.FreeText)
	}
	if got.Limit != 5 {
		t.Errorf("Limit = %d", got.Limit)
	}
	if got.SinceSeconds != int64((7 * time.Hour).Seconds()) {
		t.Errorf("SinceSeconds = %d, want %d", got.SinceSeconds, int64((7 * time.Hour).Seconds()))
	}
	if got.CodeSymbol != "fooFn" {
		t.Errorf("CodeSymbol = %q", got.CodeSymbol)
	}
}

func TestNewKnowledgeCmdProdViaTestOnlyClientFactory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_docs":1,"by_type":{},"last_indexed_unix":0}`))
	}))
	defer srv.Close()

	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(_ string) *client.Client {
		return client.NewWithBaseURL(srv.URL)
	}
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	root := NewRootCmd()
	root.SetArgs([]string{"knowledge", "stats"})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "total_docs=1") {
		t.Errorf("expected total_docs=1, got: %s", buf.String())
	}
}

func TestTruncateKnowledgeShort(t *testing.T) {
	if got := truncateKnowledge("abc", 10); got != "abc" {
		t.Errorf("truncateKnowledge short = %q, want abc", got)
	}
}

func TestTruncateKnowledgeLong(t *testing.T) {
	got := truncateKnowledge("abcdefghijklmnopqrstuvwxyz", 10)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncateKnowledge long missing ellipsis: %q", got)
	}

	if !strings.HasPrefix(got, "abcdefghi") {
		t.Errorf("truncateKnowledge long prefix wrong: %q", got)
	}
}

func TestKnowledgeQueryRealtimeFlagDispatchesViaRealtimePath(t *testing.T) {
	t.Parallel()

	fake := &fakeKnowledgeClient{
		queryRows: []client.KnowledgeResultRow{
			{Title: "live federation hit", ProjectAlias: "internal-platform-x", FilePath: "memory/x.md"},
		},
	}
	flags := KnowledgeQueryFlags{FreeText: "WFQ", Realtime: true}
	var buf bytes.Buffer
	if err := RunKnowledgeQuery(context.Background(), fake, flags, &buf); err != nil {
		t.Fatalf("RunKnowledgeQuery: %v", err)
	}
	if !fake.lastQueryReq.Realtime {
		t.Fatalf("KnowledgeQueryRequest.Realtime=false, want true (--realtime must propagate)")
	}
	if !strings.Contains(buf.String(), "live federation hit") {
		t.Fatalf("output missing real result; got %q", buf.String())
	}
}

func TestKnowledgeQueryCrossProjectFlagPropagates(t *testing.T) {
	t.Parallel()

	fake := &fakeKnowledgeClient{}
	flags := KnowledgeQueryFlags{FreeText: "anywhere", CrossProject: true}
	if err := RunKnowledgeQuery(context.Background(), fake, flags, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunKnowledgeQuery: %v", err)
	}
	if !fake.lastQueryReq.CrossProject {
		t.Fatalf("KnowledgeQueryRequest.CrossProject=false, want true")
	}
}

func TestKnowledgePromoteCmdExists(t *testing.T) {
	t.Parallel()
	root := NewKnowledgeCmd(func(*cobra.Command) KnowledgeClient { return &fakeKnowledgeClient{} })
	got, _, err := root.Find([]string{"promote"})
	if err != nil {
		t.Fatalf("`promote` subcommand not registered: %v", err)
	}
	if got.Use != "promote <id>" {
		t.Fatalf("promote.Use=%q, want %q", got.Use, "promote <id>")
	}
}

func TestKnowledgePromoteRunECallsClient(t *testing.T) {
	t.Parallel()
	fake := &fakeKnowledgeClient{
		promoteResp: &client.KnowledgePromoteResponse{ID: "abc", Status: "promoted", Scope: "global"},
	}
	flags := KnowledgePromoteFlags{ID: "abc"}
	var buf bytes.Buffer
	if err := RunKnowledgePromote(context.Background(), fake, flags, &buf); err != nil {
		t.Fatalf("RunKnowledgePromote: %v", err)
	}
	if fake.lastPromoteID != "abc" {
		t.Fatalf("client received id=%q, want abc", fake.lastPromoteID)
	}
	if !strings.Contains(buf.String(), "promoted") {
		t.Fatalf("output missing 'promoted' status; got %q", buf.String())
	}
}

func TestKnowledgePromoteRequiresID(t *testing.T) {
	t.Parallel()
	flags := KnowledgePromoteFlags{ID: ""}
	err := RunKnowledgePromote(context.Background(), &fakeKnowledgeClient{}, flags, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for empty id; got nil")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Fatalf("err=%v, want ErrRecoverable", err)
	}
}

func TestKnowledgeSyncCmdExists(t *testing.T) {
	t.Parallel()
	root := NewKnowledgeCmd(func(*cobra.Command) KnowledgeClient { return &fakeKnowledgeClient{} })
	got, _, err := root.Find([]string{"sync"})
	if err != nil {
		t.Fatalf("`sync` subcommand not registered: %v", err)
	}
	if got.Use != "sync" {
		t.Fatalf("sync.Use=%q, want %q", got.Use, "sync")
	}
}

func TestKnowledgeSyncRunESuccess(t *testing.T) {
	t.Parallel()
	fake := &fakeKnowledgeClient{
		syncResp: &client.KnowledgeSyncResponse{RowsIndexed: 42, DurationMs: 100},
	}
	flags := KnowledgeSyncFlags{}
	var buf bytes.Buffer
	if err := RunKnowledgeSync(context.Background(), fake, flags, &buf); err != nil {
		t.Fatalf("RunKnowledgeSync: %v", err)
	}
	if !strings.Contains(buf.String(), "sync complete") {
		t.Fatalf("output missing 'sync complete'; got %q", buf.String())
	}
}

func TestKnowledgeSyncProjectScopePropagates(t *testing.T) {
	t.Parallel()
	fake := &fakeKnowledgeClient{}
	flags := KnowledgeSyncFlags{Project: "internal-platform-x"}
	var buf bytes.Buffer
	if err := RunKnowledgeSync(context.Background(), fake, flags, &buf); err != nil {
		t.Fatalf("RunKnowledgeSync: %v", err)
	}
	if fake.lastSyncReq.ProjectAlias != "internal-platform-x" {
		t.Fatalf("project=%q, want internal-platform-x", fake.lastSyncReq.ProjectAlias)
	}
	if !strings.Contains(buf.String(), "project=internal-platform-x") {
		t.Fatalf("output missing project scope; got %q", buf.String())
	}
}

func TestKnowledgeRestoreCmdExists(t *testing.T) {
	t.Parallel()
	root := NewKnowledgeCmd(func(*cobra.Command) KnowledgeClient { return &fakeKnowledgeClient{} })
	got, _, err := root.Find([]string{"restore"})
	if err != nil {
		t.Fatalf("`restore` subcommand not registered: %v", err)
	}
	if got.Use != "restore" {
		t.Fatalf("restore.Use=%q, want %q", got.Use, "restore")
	}
}

func TestKnowledgeRestoreRunESuccess(t *testing.T) {
	t.Parallel()
	fake := &fakeKnowledgeClient{
		restoreResp: &client.KnowledgeRestoreResponse{
			ProjectAlias: "internal-platform-x", SnapshotID: "snap-123", RowsRestored: 500, DurationMs: 2000,
		},
	}
	flags := KnowledgeRestoreFlags{Project: "internal-platform-x"}
	var buf bytes.Buffer
	if err := RunKnowledgeRestore(context.Background(), fake, flags, &buf); err != nil {
		t.Fatalf("RunKnowledgeRestore: %v", err)
	}
	if !strings.Contains(buf.String(), "restored:") {
		t.Fatalf("output missing 'restored:'; got %q", buf.String())
	}
}

func TestKnowledgeRestoreDryRun(t *testing.T) {
	t.Parallel()
	fake := &fakeKnowledgeClient{
		restoreResp: &client.KnowledgeRestoreResponse{
			ProjectAlias: "internal-platform-x", SnapshotID: "snap-456", RowsRestored: 300,
		},
	}
	flags := KnowledgeRestoreFlags{Project: "internal-platform-x", DryRun: true}
	var buf bytes.Buffer
	if err := RunKnowledgeRestore(context.Background(), fake, flags, &buf); err != nil {
		t.Fatalf("RunKnowledgeRestore dry-run: %v", err)
	}
	if !strings.Contains(buf.String(), "dry-run:") {
		t.Fatalf("output missing 'dry-run:'; got %q", buf.String())
	}
}

func TestKnowledgeRestoreRequiresProject(t *testing.T) {
	t.Parallel()
	flags := KnowledgeRestoreFlags{Project: ""}
	err := RunKnowledgeRestore(context.Background(), &fakeKnowledgeClient{}, flags, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for empty project; got nil")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Fatalf("err=%v, want ErrRecoverable", err)
	}
}

func TestKnowledgeRestoreInvalidTimestampRecoverable(t *testing.T) {
	t.Parallel()
	flags := KnowledgeRestoreFlags{Project: "internal-platform-x", Timestamp: "not-a-timestamp"}
	err := RunKnowledgeRestore(context.Background(), &fakeKnowledgeClient{}, flags, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for bad timestamp; got nil")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Fatalf("err=%v, want ErrRecoverable", err)
	}
}

func TestKnowledgePromote_DispatchesHTTP(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/knowledge/promote" {
			t.Errorf("unexpected path %q; want /v1/knowledge/promote", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method=%q, want POST", r.Method)
		}
		var req client.KnowledgePromoteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(client.KnowledgePromoteResponse{
			ID:     req.ID,
			Status: "promoted",
			Scope:  "global",
		})
	}))
	defer srv.Close()

	prod := &productionKnowledgeClient{c: client.NewWithBaseURL(srv.URL)}
	resp, err := prod.KnowledgePromote(context.Background(), client.KnowledgePromoteRequest{
		ID:          "doc-abc",
		GlobalScope: true,
	})
	if err != nil {
		t.Fatalf("KnowledgePromote: %v", err)
	}
	if resp.ID != "doc-abc" {
		t.Fatalf("ID=%q, want doc-abc", resp.ID)
	}
	if resp.Status != "promoted" {
		t.Fatalf("Status=%q, want promoted", resp.Status)
	}
	if resp.Scope != "global" {
		t.Fatalf("Scope=%q, want global", resp.Scope)
	}
}

func TestKnowledgeSync_DispatchesHTTP(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/knowledge/sync" {
			t.Errorf("unexpected path %q; want /v1/knowledge/sync", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method=%q, want POST", r.Method)
		}
		var req client.KnowledgeSyncRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(client.KnowledgeSyncResponse{
			RowsIndexed: 42,
			DurationMs:  150,
			VerifyDelta: 0,
		})
	}))
	defer srv.Close()

	prod := &productionKnowledgeClient{c: client.NewWithBaseURL(srv.URL)}
	resp, err := prod.KnowledgeSync(context.Background(), client.KnowledgeSyncRequest{
		ProjectAlias: "internal-platform-x",
		Verify:       true,
	})
	if err != nil {
		t.Fatalf("KnowledgeSync: %v", err)
	}
	if resp.RowsIndexed != 42 {
		t.Fatalf("RowsIndexed=%d, want 42", resp.RowsIndexed)
	}
	if resp.DurationMs != 150 {
		t.Fatalf("DurationMs=%d, want 150", resp.DurationMs)
	}
}

func TestKnowledgeRestore_DispatchesHTTP(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/knowledge/restore" {
			t.Errorf("unexpected path %q; want /v1/knowledge/restore", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method=%q, want POST", r.Method)
		}
		var req client.KnowledgeRestoreRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(client.KnowledgeRestoreResponse{
			ProjectAlias: req.ProjectAlias,
			SnapshotID:   "snap-2026-05-01",
			RowsRestored: 1234,
			DurationMs:   320,
		})
	}))
	defer srv.Close()

	prod := &productionKnowledgeClient{c: client.NewWithBaseURL(srv.URL)}
	resp, err := prod.KnowledgeRestore(context.Background(), client.KnowledgeRestoreRequest{
		ProjectAlias: "zen-swarm",
	})
	if err != nil {
		t.Fatalf("KnowledgeRestore: %v", err)
	}
	if resp.ProjectAlias != "zen-swarm" {
		t.Fatalf("ProjectAlias=%q, want zen-swarm", resp.ProjectAlias)
	}
	if resp.SnapshotID != "snap-2026-05-01" {
		t.Fatalf("SnapshotID=%q, want snap-2026-05-01", resp.SnapshotID)
	}
	if resp.RowsRestored != 1234 {
		t.Fatalf("RowsRestored=%d, want 1234", resp.RowsRestored)
	}
}
