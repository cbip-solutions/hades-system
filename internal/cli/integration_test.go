// go:build integration

package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
)

// rebindTestClient swaps TestOnlyClientFactory so newClientFromCmd
// dispatches every namespace's production wiring against the given
// httptest server. Restores in t.Cleanup so test bodies do not leak
// the override across the parallel suite. Mirrors resetQuietClient
// (quiet_test.go:445) — same pattern, narrowed for integration tests.
//
// Covers knowledge / docs / specs (sync) — all three resolve their
// daemon client via newClientFromCmd → client.New(uds) (workforce.go:78)
// which honours TestOnlyClientFactory. memory wires through the same
// path indirectly: productionMemoryClient embeds newClientFromCmd
// (memory.go:73) so rebinding TestOnlyClientFactory also rewires the
// memory namespace.
func rebindTestClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(_ string) *client.Client {
		return client.NewWithBaseURL(srv.URL)
	}
	t.Cleanup(func() { TestOnlyClientFactory = prev })
}

func runCLI(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := NewRootCmd()
	root.SetArgs(args)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	err := root.ExecuteContext(t.Context())
	return stdout.String(), stderr.String(), err
}

func runCLIStdin(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()
	root := NewRootCmd()
	root.SetArgs(args)
	root.SetIn(strings.NewReader(stdin))
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	err := root.ExecuteContext(t.Context())
	return stdout.String(), stderr.String(), err
}

func findSubcommand(parent *cobra.Command, name string) *cobra.Command {
	for _, sub := range parent.Commands() {
		if sub.Name() == name {
			return sub
		}
	}
	return nil
}

func TestIntegrationKnowledgeQueryRemote(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody client.EcosystemQueryRequest

	resp := client.EcosystemQueryResponse{
		Chunks: []client.EcosystemChunk{
			{
				PackageName:   "context",
				SymbolPath:    "context.Context",
				Kind:          "type",
				Version:       "1.22.0",
				ContentText:   "Context carries deadlines, cancellation signals, and request-scoped values.",
				SourceURL:     "https://pkg.go.dev/context#Context",
				RerankerScore: 0.92,
			},
		},
		Provenance: client.EcosystemProvenance{
			DetectedVersion: "1.22.0",
			DetectionLayer:  1,
			RoutingMethod:   "single",
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	rebindTestClient(t, srv)

	stdout, _, err := runCLI(t,
		"knowledge", "query",
		"--remote",
		"--ecosystem", "go",
		"--remote-format", "human",
		"context cancellation",
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/knowledge/ecosystem/query" {
		t.Errorf("path = %q, want /v1/knowledge/ecosystem/query", gotPath)
	}
	if gotBody.Query != "context cancellation" {
		t.Errorf("body.Query = %q, want %q", gotBody.Query, "context cancellation")
	}
	if gotBody.Ecosystem != "go" {
		t.Errorf("body.Ecosystem = %q, want %q", gotBody.Ecosystem, "go")
	}

	if !strings.Contains(stdout, "context.Context") {
		t.Errorf("stdout missing symbol path; got: %q", stdout)
	}
}

// TestIntegrationKnowledgeQueryRemoteRecoverableOn422 — daemon 422
// (validation rejected) MUST surface as ErrRecoverable (exit 1).
func TestIntegrationKnowledgeQueryRemoteRecoverableOn422(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"unknown ecosystem"}`))
	}))
	defer srv.Close()

	rebindTestClient(t, srv)

	_, _, err := runCLI(t,
		"knowledge", "query",
		"--remote",
		"--ecosystem", "nonexistent",
		"test",
	)
	if err == nil {
		t.Fatal("expected error on 422; got nil")
	}
	if !IsRecoverable(err) {
		t.Errorf("err=%v, want IsRecoverable=true (exit 1)", err)
	}
}

func TestIntegrationMemoryQueryFlagsRegistered(t *testing.T) {
	root := NewRootCmd()
	memory := findSubcommand(root, "memory")
	if memory == nil {
		t.Fatal("zen memory subcommand not registered on root")
	}
	query := findSubcommand(memory, "query")
	if query == nil {
		t.Fatal("zen memory query subcommand not registered")
	}
	for _, name := range []string{"remote", "limit", "format"} {
		if f := query.Flags().Lookup(name); f == nil {
			t.Errorf("zen memory query --%s flag not registered", name)
		}
	}
}

func TestIntegrationMemoryQueryCallsAggregator(t *testing.T) {
	var aggCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aggCalled = true
		if !strings.HasPrefix(r.URL.Path, "/v1/knowledge/aggregator") {
			t.Errorf("unexpected path: %q (want /v1/knowledge/aggregator/*)", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"note_id":"internal-platform-x/M0-doctrine","title":"Max-scope doctrine","snippet":"load-bearing"}]}`))
	}))
	defer srv.Close()

	rebindTestClient(t, srv)

	stdout, _, err := runCLI(t, "memory", "query", "max-scope")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !aggCalled {
		t.Error("aggregator endpoint was not called")
	}

	if !strings.Contains(stdout, "Max-scope doctrine") &&
		!strings.Contains(stdout, "internal-platform-x/M0-doctrine") &&
		!strings.Contains(stdout, "load-bearing") {
		t.Errorf("stdout missing aggregator hit; got: %q", stdout)
	}
}

func TestIntegrationMemoryListSubcommandRegistered(t *testing.T) {
	root := NewRootCmd()
	memory := findSubcommand(root, "memory")
	if memory == nil {
		t.Fatal("zen memory not registered")
	}
	wanted := []string{"query", "list", "pin", "unpin", "promote"}
	for _, name := range wanted {
		if sub := findSubcommand(memory, name); sub == nil {
			t.Errorf("zen memory %s not registered", name)
		}
	}
}

func TestIntegrationSpecsList(t *testing.T) {
	root := t.TempDir()
	specsDir := filepath.Join(root, "openspec", "specs")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(specsDir, "adr-0090.md"),
		"# ADR-0090: RRF fusion constant\n\nbody\n")
	writeTestFile(t, filepath.Join(specsDir, "adr-0091.md"),
		"# ADR-0091: Ecosystem dispatcher\n\nbody\n")

	stdout, _, err := runCLI(t, "specs", "--root", root, "list")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "adr-0090") {
		t.Errorf("stdout missing adr-0090; got: %q", stdout)
	}
	if !strings.Contains(stdout, "adr-0091") {
		t.Errorf("stdout missing adr-0091; got: %q", stdout)
	}
	if !strings.Contains(stdout, "RRF fusion") {
		t.Errorf("stdout missing first-line title; got: %q", stdout)
	}
}

func TestIntegrationSpecsListJSON(t *testing.T) {
	root := t.TempDir()
	specsDir := filepath.Join(root, "openspec", "specs")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(specsDir, "adr-0001.md"), "# ADR-0001: first\n")

	stdout, _, err := runCLI(t, "specs", "--root", root, "list", "--format", "json")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var out []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("json decode: %v\nstdout=%q", err, stdout)
	}
	if len(out) != 1 || out[0].ID != "adr-0001" {
		t.Errorf("json out = %+v, want one row id=adr-0001", out)
	}
}

// TestIntegrationSpecsListMissingDirNotAnError — a fresh repo without
// openspec/specs/ MUST print the sentinel "(no specs directory found)"
// and exit 0 (RunSpecsList contract; specs_list.go:62).
func TestIntegrationSpecsListMissingDirNotAnError(t *testing.T) {
	root := t.TempDir()

	stdout, _, err := runCLI(t, "specs", "--root", root, "list")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "(no specs directory found)") {
		t.Errorf("stdout missing sentinel; got: %q", stdout)
	}
}

func TestIntegrationSpecsShowSubcommandRegistered(t *testing.T) {
	root := NewRootCmd()
	specs := findSubcommand(root, "specs")
	if specs == nil {
		t.Fatal("zen specs not registered")
	}
	for _, name := range []string{"list", "show", "diff", "sync"} {
		if sub := findSubcommand(specs, name); sub == nil {
			t.Errorf("zen specs %s not registered", name)
		}
	}
}

func TestIntegrationDocsReindexCallsDaemon(t *testing.T) {
	var gotPath string
	var gotBody client.DocsReindexRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(client.DocsReindexResponse{
			PackagesIngested:   42,
			ChunksIngested:     500,
			SymbolsRegistered:  120,
			ChangeNodesCreated: 8,
			ElapsedMs:          1234,
		})
	}))
	defer srv.Close()

	rebindTestClient(t, srv)

	stdout, _, err := runCLI(t, "docs", "reindex", "--ecosystem", "go")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if gotPath != "/v1/knowledge/ecosystem/reindex" {
		t.Errorf("path = %q, want /v1/knowledge/ecosystem/reindex", gotPath)
	}
	if gotBody.Ecosystem != "go" {
		t.Errorf("body.Ecosystem = %q, want go", gotBody.Ecosystem)
	}

	if !gotBody.DeltaOnly {
		t.Errorf("body.DeltaOnly = false, want true (--full unset)")
	}
	if !strings.Contains(stdout, "packages_ingested=42") {
		t.Errorf("stdout missing summary line; got: %q", stdout)
	}
}

func TestIntegrationDocsReindexFullFlag(t *testing.T) {
	var gotBody client.DocsReindexRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	rebindTestClient(t, srv)

	_, _, err := runCLI(t, "docs", "reindex", "--full")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotBody.DeltaOnly {
		t.Errorf("body.DeltaOnly = true, want false (--full set)")
	}
}

// TestIntegrationDocsPruneRequiresFlag — G-5: `zen docs prune` with
// --ecosystem + --version set but neither --dry-run nor --confirm MUST
// surface an ErrRecoverable safety-gate error and MUST NOT call any
// daemon endpoint (docs_prune.go: safety gate contract).
//
// G-5 requires --ecosystem + --version always (MarkFlagRequired); the
// missing-flag failure cobra surfaces is separate from the safety gate
// which gates dry-run vs confirm.
func TestIntegrationDocsPruneRequiresFlag(t *testing.T) {
	var daemonCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		daemonCalled = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	rebindTestClient(t, srv)

	_, _, err := runCLI(t, "docs", "prune", "--ecosystem", "go", "--version", "1.0.0")
	if err == nil {
		t.Fatal("expected error for missing safety flag; got nil")
	}
	if !IsRecoverable(err) {
		t.Errorf("err=%v, want IsRecoverable=true (exit 1, safety gate)", err)
	}
	if daemonCalled {
		t.Error("daemon was called despite missing safety flag (gate breach)")
	}
}

func TestIntegrationDocsPruneDryRunCallsDaemon(t *testing.T) {
	var previewPath string
	var deletePath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			previewPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.EcosystemPrunePreview{
				Ecosystem:  r.URL.Query().Get("ecosystem"),
				Version:    r.URL.Query().Get("version"),
				ChunkCount: 17, ChunkFP32Count: 17,
				SymbolCount: 5, ChangeCount: 1, FTS5Count: 17,
				Pinned: false,
			})
		case http.MethodDelete:
			deletePath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected method %s on %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	rebindTestClient(t, srv)

	stdout, _, err := runCLI(t, "docs", "prune", "--ecosystem", "go", "--version", "1.21.0", "--dry-run")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if previewPath != "/v1/ecosystem/prune-preview" {
		t.Errorf("preview path = %q, want /v1/ecosystem/prune-preview", previewPath)
	}
	if deletePath != "" {
		t.Errorf("DELETE dialed in dry-run path; got %q", deletePath)
	}
	if !strings.Contains(stdout, "Prune preview") {
		t.Errorf("stdout missing 'Prune preview' header; got: %q", stdout)
	}
	if !strings.Contains(stdout, "go@1.21.0") {
		t.Errorf("stdout missing eco@ver; got: %q", stdout)
	}
	if !strings.Contains(stdout, "chunks:") {
		t.Errorf("stdout missing chunks count line; got: %q", stdout)
	}
}

func TestIntegrationDocsPinCallsDaemon(t *testing.T) {
	var gotPath string
	var gotBody client.EcosystemPinRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	rebindTestClient(t, srv)

	stdout, _, err := runCLIStdin(t, "y\n", "docs", "pin", "--ecosystem", "go", "--version", "1.22.0")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/v1/ecosystem/pin" {
		t.Errorf("path = %q, want /v1/ecosystem/pin", gotPath)
	}
	if gotBody.Ecosystem != "go" || gotBody.Version != "1.22.0" {
		t.Errorf("body = %+v, want go@1.22.0", gotBody)
	}
	if !strings.Contains(stdout, "pinned: go@1.22.0") {
		t.Errorf("stdout missing pinned line; got: %q", stdout)
	}
}

func TestIntegrationDocsPruneConfirmCallsDelete(t *testing.T) {
	var sawPreview, sawDelete bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			sawPreview = true
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.EcosystemPrunePreview{
				Ecosystem: "python", Version: "3.9.0", ChunkCount: 4,
			})
		case http.MethodDelete:
			sawDelete = true
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()

	rebindTestClient(t, srv)

	stdout, _, err := runCLIStdin(t, "y\n", "docs", "prune", "--ecosystem", "python", "--version", "3.9.0", "--confirm")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !sawPreview {
		t.Error("preview not dialed")
	}
	if !sawDelete {
		t.Error("DELETE not dialed after y prompt")
	}
	if !strings.Contains(stdout, "pruned:") {
		t.Errorf("stdout missing pruned summary; got: %q", stdout)
	}
}

func TestIntegrationDocsStatusRendersTable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/knowledge/ecosystem/status" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(client.DocsStatusResponse{
			Ecosystems: []client.EcosystemStatus{
				{Ecosystem: "go", ChunkCount: 1000, SymbolCount: 250, StorageBytes: 1024 * 1024, RetentionDays: 30},
				{Ecosystem: "python", ChunkCount: 800, SymbolCount: 200, StorageBytes: 2 * 1024 * 1024, RetentionDays: 30},
			},
		})
	}))
	defer srv.Close()

	rebindTestClient(t, srv)

	stdout, _, err := runCLI(t, "docs", "status")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "go") {
		t.Errorf("stdout missing 'go' row; got: %q", stdout)
	}
	if !strings.Contains(stdout, "python") {
		t.Errorf("stdout missing 'python' row; got: %q", stdout)
	}
}

func TestIntegrationExitCodeMapping(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		status   int
		wantRecv bool
	}{
		{
			name:     "knowledge_query_remote_422",
			args:     []string{"knowledge", "query", "--remote", "--ecosystem", "go", "test"},
			status:   http.StatusUnprocessableEntity,
			wantRecv: true,
		},
		{
			name:     "docs_reindex_422",
			args:     []string{"docs", "reindex", "--ecosystem", "unknown"},
			status:   http.StatusUnprocessableEntity,
			wantRecv: true,
		},
		{
			name:     "docs_prune_500",
			args:     []string{"docs", "prune", "--dry-run"},
			status:   http.StatusInternalServerError,
			wantRecv: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":"test"}`))
			}))
			defer srv.Close()
			rebindTestClient(t, srv)

			_, _, err := runCLI(t, tc.args...)
			if err == nil {
				t.Fatalf("expected error for status=%d", tc.status)
			}
			got := IsRecoverable(err)
			if got != tc.wantRecv {
				t.Errorf("IsRecoverable=%v, want %v (err=%v)", got, tc.wantRecv, err)
			}

			if !tc.wantRecv && errors.Is(err, ErrRecoverable) {
				t.Errorf("non-recoverable err satisfies ErrRecoverable: %v", err)
			}
		})
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
