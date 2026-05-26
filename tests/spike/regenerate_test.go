package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExtractValidHooks_TupleShape(t *testing.T) {
	src := `VALID_HOOKS = (
		'pre_tool_call',
		'post_tool_call',
		'pre_llm_call',
		'post_llm_call',
		'on_session_start',
	)`
	got := extractValidHooks(src)
	want := []string{"pre_tool_call", "post_tool_call", "pre_llm_call", "post_llm_call", "on_session_start"}
	assertStringSliceEqual(t, "TupleShape", got, want)
}

func TestExtractValidHooks_RealSetStrShape(t *testing.T) {
	src := `VALID_HOOKS: Set[str] = {
		"pre_tool_call",
		"post_tool_call",
		"on_session_start",
	}`
	got := extractValidHooks(src)
	want := []string{"pre_tool_call", "post_tool_call", "on_session_start"}
	assertStringSliceEqual(t, "RealSetStrShape", got, want)
}

func TestExtractValidHooks_BracesNoAnnotation(t *testing.T) {
	src := `VALID_HOOKS = {
		'pre_tool_call',
		'post_tool_call',
	}`
	got := extractValidHooks(src)
	want := []string{"pre_tool_call", "post_tool_call"}
	assertStringSliceEqual(t, "BracesNoAnnotation", got, want)
}

func TestExtractValidHooks_EmptyContent(t *testing.T) {
	src := "no hooks here"
	got := extractValidHooks(src)
	if got != nil {
		t.Errorf("EmptyContent: expected nil, got %v", got)
	}
}

func TestExtractValidHooks_CommentWithCloseBrace(t *testing.T) {
	src := `VALID_HOOKS: Set[str] = {
    "pre_tool_call",
    "post_tool_call",
    # {"action": "skip"} -> drop message (no reply)
    "on_session_start",
    "pre_gateway_dispatch",
}`
	got := extractValidHooks(src)
	want := []string{"pre_tool_call", "post_tool_call", "on_session_start", "pre_gateway_dispatch"}
	assertStringSliceEqual(t, "CommentWithCloseBrace", got, want)
}

func TestStripPythonComments(t *testing.T) {
	src := `line1
    # whole-line comment
line2 # inline comment kept
    # another comment
line3`
	got := stripPythonComments(src)
	if !strings.Contains(got, "line1") || !strings.Contains(got, "line2 # inline comment kept") || !strings.Contains(got, "line3") {
		t.Errorf("stripPythonComments dropped real content: %q", got)
	}
	if strings.Contains(got, "whole-line comment") {
		t.Errorf("stripPythonComments kept whole-line comment: %q", got)
	}
	if strings.Contains(got, "another comment") {
		t.Errorf("stripPythonComments kept whole-line comment: %q", got)
	}
}

func TestExtractManifestKeys_RealDataclassShape(t *testing.T) {
	src := `@dataclass
class PluginManifest:
    name: str
    version: str = ""
    description: str = ""
    license: str = ""`
	got := extractManifestKeys(src)
	want := []string{"name"}
	assertStringSliceEqual(t, "RealDataclassShape", got, want)
}

func TestExtractManifestKeys_MultipleRequired(t *testing.T) {
	src := `@dataclass
class PluginManifest:
    name: str
    version: str
    description: str = ""
    license: str = ""`
	got := extractManifestKeys(src)
	want := []string{"name", "version"}
	assertStringSliceEqual(t, "MultipleRequired", got, want)
}

func TestExtractManifestKeys_NoClassBlock(t *testing.T) {
	src := "no class block here"
	got := extractManifestKeys(src)
	if got != nil {
		t.Errorf("NoClassBlock: expected nil, got %v", got)
	}
}

func TestExtractManifestKeys_OnlyOptionalFields(t *testing.T) {
	src := `class PluginManifest:
    name: str = "default"
    version: str = "1.0"`
	got := extractManifestKeys(src)
	if got != nil {
		t.Errorf("OnlyOptionalFields: expected nil, got %v", got)
	}
}

func TestExtractManifestKeys_CommentLine(t *testing.T) {
	src := `class PluginManifest:
    # required
    name: str
    # optional below
    version: str = ""`
	got := extractManifestKeys(src)
	want := []string{"name"}
	assertStringSliceEqual(t, "CommentLine", got, want)
}

func TestExtractManifestKeys_DocstringAndBlankLine(t *testing.T) {
	src := `@dataclass
class PluginManifest:
    """Parsed representation of a plugin.yaml manifest."""

    name: str
    version: str = ""
    description: str = ""`
	got := extractManifestKeys(src)
	want := []string{"name"}
	assertStringSliceEqual(t, "DocstringAndBlankLine", got, want)
}

func TestExtractManifestKeys_RealHermesShape(t *testing.T) {
	src := `@dataclass
class PluginManifest:
    """Parsed representation of a plugin.yaml manifest."""

    name: str
    version: str = ""
    description: str = ""
    author: str = ""
    requires_env: List[Union[str, Dict[str, Any]]] = field(default_factory=list)
    provides_tools: List[str] = field(default_factory=list)
    provides_hooks: List[str] = field(default_factory=list)
    source: str = ""        # "user", "project", or "entrypoint"
    path: Optional[str] = None
    kind: str = "standalone"
`
	got := extractManifestKeys(src)
	want := []string{"name"}
	assertStringSliceEqual(t, "RealHermesShape", got, want)
}

func TestExtractCommittedHeadSHA_HappyPath(t *testing.T) {
	src := "# Plan 13 spike\n\n**Hermes head SHA**: `abc123def456`\n\n"
	got := extractCommittedHeadSHA(src)
	if got != "abc123def456" {
		t.Errorf("HappyPath: got %q want %q", got, "abc123def456")
	}
}

func TestExtractCommittedHeadSHA_NoBackticks(t *testing.T) {
	src := "**Hermes head SHA**: abc123def456\n"
	got := extractCommittedHeadSHA(src)
	if got != "abc123def456" {
		t.Errorf("NoBackticks: got %q want %q", got, "abc123def456")
	}
}

func TestExtractCommittedHeadSHA_MissingLine(t *testing.T) {
	src := "no sha line here"
	got := extractCommittedHeadSHA(src)
	if got != "" {
		t.Errorf("MissingLine: got %q want empty", got)
	}
}

func TestExtractCommittedHeadSHA_EmptyLine(t *testing.T) {
	src := "\n\n"
	got := extractCommittedHeadSHA(src)
	if got != "" {
		t.Errorf("EmptyLine: got %q want empty", got)
	}
}

func TestRenderArtifact_Deterministic(t *testing.T) {
	info := &hermesHeadInfo{
		HeadSHA:      "abc123",
		ValidHooks:   []string{"pre_tool_call", "post_tool_call"},
		ManifestKeys: []string{"name"},
		FetchedAt:    time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC),
	}
	got1 := renderArtifact(info)

	info.FetchedAt = time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	got2 := renderArtifact(info)

	if string(got1) != string(got2) {
		t.Errorf("renderArtifact not deterministic w.r.t. FetchedAt:\n--- got1 ---\n%s\n--- got2 ---\n%s", got1, got2)
	}
}

func TestRenderArtifact_ContainsSHA(t *testing.T) {
	info := &hermesHeadInfo{
		HeadSHA:      "abc123",
		ValidHooks:   []string{"pre_tool_call"},
		ManifestKeys: []string{"name"},
	}
	got := string(renderArtifact(info))
	if !strings.Contains(got, "abc123") {
		t.Errorf("renderArtifact: expected SHA in output; got:\n%s", got)
	}
	if !strings.Contains(got, "pre_tool_call") {
		t.Errorf("renderArtifact: expected hook in output; got:\n%s", got)
	}
	if !strings.Contains(got, "End of Plan 13 Phase 0 spike artifact.") {
		t.Errorf("renderArtifact: expected trailing sentinel; got:\n%s", got)
	}
}

func TestFindArtifact_NoMatch(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll(filepath.Join("docs", "superpowers", "specs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_, err := findArtifact()
	if err == nil {
		t.Fatal("findArtifact: expected error for empty dir, got nil")
	}
	if !strings.Contains(err.Error(), "no spike artifact found") {
		t.Errorf("findArtifact: expected 'no spike artifact found' error, got %v", err)
	}
}

func TestFindArtifact_SingleMatch(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	specsDir := filepath.Join("docs", "superpowers", "specs")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	artifactName := "2026-05-16-zen-swarm-plan-13-spike-hermes-mcp-contract.md"
	if err := os.WriteFile(filepath.Join(specsDir, artifactName), []byte("content"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := findArtifact()
	if err != nil {
		t.Fatalf("findArtifact: unexpected error: %v", err)
	}
	want := filepath.Join(specsDir, artifactName)
	if got != want {
		t.Errorf("findArtifact: got %q want %q", got, want)
	}
}

func TestFindArtifact_MultiMatch(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	specsDir := filepath.Join("docs", "superpowers", "specs")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, name := range []string{
		"2026-05-14-zen-swarm-plan-13-spike-hermes-mcp-contract.md",
		"2026-05-16-zen-swarm-plan-13-spike-hermes-mcp-contract.md",
	} {
		if err := os.WriteFile(filepath.Join(specsDir, name), []byte("content"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	_, err := findArtifact()
	if err == nil {
		t.Fatal("findArtifact: expected error for multi-match, got nil")
	}
	if !strings.Contains(err.Error(), "multiple spike artifacts found") {
		t.Errorf("findArtifact: expected 'multiple' error, got %v", err)
	}
}

func TestFetchHeadSHA_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/commits/HEAD") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"sha": "real_sha_12345"})
	}))
	defer srv.Close()

	got, err := fetchHeadSHA(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("fetchHeadSHA: %v", err)
	}
	if got != "real_sha_12345" {
		t.Errorf("fetchHeadSHA: got %q want real_sha_12345", got)
	}
}

func TestFetchHeadSHA_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := fetchHeadSHA(context.Background(), srv.Client(), srv.URL)
	if err == nil {
		t.Fatal("fetchHeadSHA: expected error for 404, got nil")
	}
	if !strings.Contains(err.Error(), "status=404") {
		t.Errorf("fetchHeadSHA: expected status=404 in error, got %v", err)
	}
}

func TestFetchHeadSHA_EmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"sha": ""})
	}))
	defer srv.Close()

	_, err := fetchHeadSHA(context.Background(), srv.Client(), srv.URL)
	if err == nil {
		t.Fatal("fetchHeadSHA: expected error for empty SHA, got nil")
	}
	if !strings.Contains(err.Error(), "empty SHA") {
		t.Errorf("fetchHeadSHA: expected 'empty SHA' in error, got %v", err)
	}
}

func TestFetchHeadSHA_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	_, err := fetchHeadSHA(context.Background(), srv.Client(), srv.URL)
	if err == nil {
		t.Fatal("fetchHeadSHA: expected decode error, got nil")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("fetchHeadSHA: expected decode error, got %v", err)
	}
}

func TestFetchPluginsPy_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("VALID_HOOKS: Set[str] = {'pre_tool_call'}"))
	}))
	defer srv.Close()

	got, err := fetchPluginsPy(context.Background(), srv.Client(), srv.URL, "deadbeef")
	if err != nil {
		t.Fatalf("fetchPluginsPy: %v", err)
	}
	if !strings.Contains(got, "VALID_HOOKS") {
		t.Errorf("fetchPluginsPy: missing VALID_HOOKS in output: %q", got)
	}
}

func TestFetchPluginsPy_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := fetchPluginsPy(context.Background(), srv.Client(), srv.URL, "deadbeef")
	if err == nil {
		t.Fatal("fetchPluginsPy: expected error for 404, got nil")
	}
}

func newMockHermesServers(t *testing.T, sha string, pluginsBody string) (apiBase, rawBase string, cleanup func()) {
	t.Helper()
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"sha": sha})
	}))
	rawSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(pluginsBody))
	}))
	return apiSrv.URL, rawSrv.URL, func() {
		apiSrv.Close()
		rawSrv.Close()
	}
}

const samplePluginsPy = `VALID_HOOKS: Set[str] = {
    "pre_tool_call",
    "post_tool_call",
    "on_session_start",
}

@dataclass
class PluginManifest:
    name: str
    version: str = ""
    description: str = ""
`

func TestRunSpike_RegenerateAndOfflineMutuallyExclusive(t *testing.T) {
	err := runSpike(context.Background(), spikeOpts{regenerate: true, offline: true})
	if err == nil {
		t.Fatal("expected mutually exclusive error, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected mutually exclusive error, got %v", err)
	}
}

func TestRunSpike_OfflineHappyPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	specsDir := filepath.Join("docs", "superpowers", "specs")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	artifactName := "2026-05-16-zen-swarm-plan-13-spike-hermes-mcp-contract.md"
	if err := os.WriteFile(filepath.Join(specsDir, artifactName), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := runSpike(context.Background(), spikeOpts{offline: true})
	if err != nil {
		t.Fatalf("runSpike offline: %v", err)
	}
}

func TestRunSpike_OfflineMissingArtifact(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll(filepath.Join("docs", "superpowers", "specs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	err := runSpike(context.Background(), spikeOpts{offline: true})
	if err == nil {
		t.Fatal("expected error for missing artifact, got nil")
	}
	if !strings.Contains(err.Error(), "offline check") {
		t.Errorf("expected 'offline check' wrap, got %v", err)
	}
}

func TestRunSpike_CheckHappyPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	specsDir := filepath.Join("docs", "superpowers", "specs")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	apiBase, rawBase, cleanup := newMockHermesServers(t, "matching_sha", samplePluginsPy)
	defer cleanup()

	info := &hermesHeadInfo{
		HeadSHA:      "matching_sha",
		ValidHooks:   []string{"pre_tool_call", "post_tool_call", "on_session_start"},
		ManifestKeys: []string{"name"},
	}
	expectedBytes := renderArtifact(info)
	artifactName := "2026-05-16-zen-swarm-plan-13-spike-hermes-mcp-contract.md"
	if err := os.WriteFile(filepath.Join(specsDir, artifactName), expectedBytes, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := runSpike(context.Background(), spikeOpts{
		check:   true,
		apiBase: apiBase,
		rawBase: rawBase,
	})
	if err != nil {
		t.Fatalf("runSpike check happy: %v", err)
	}
}

func TestRunSpike_CheckDrift(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	specsDir := filepath.Join("docs", "superpowers", "specs")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	apiBase, rawBase, cleanup := newMockHermesServers(t, "new_sha", samplePluginsPy)
	defer cleanup()

	staleBytes := renderArtifact(&hermesHeadInfo{
		HeadSHA:      "old_sha",
		ValidHooks:   []string{"pre_tool_call"},
		ManifestKeys: []string{"name"},
	})
	artifactName := "2026-05-16-zen-swarm-plan-13-spike-hermes-mcp-contract.md"
	if err := os.WriteFile(filepath.Join(specsDir, artifactName), staleBytes, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := runSpike(context.Background(), spikeOpts{
		check:   true,
		apiBase: apiBase,
		rawBase: rawBase,
	})
	if err == nil {
		t.Fatal("expected drift error, got nil")
	}
	if !strings.Contains(err.Error(), "spike drift detected") {
		t.Errorf("expected 'spike drift detected', got %v", err)
	}
	if !strings.Contains(err.Error(), "--regenerate") {
		t.Errorf("expected drift error to mention --regenerate, got %v", err)
	}
}

func TestRunSpike_CheckDriftBytesDiffer(t *testing.T) {

	dir := t.TempDir()
	t.Chdir(dir)
	specsDir := filepath.Join("docs", "superpowers", "specs")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	apiBase, rawBase, cleanup := newMockHermesServers(t, "same_sha", samplePluginsPy)
	defer cleanup()

	staleBytes := []byte(fmt.Sprintf("# Plan 13 Phase 0 spike\n\n**Hermes head SHA**: %s\n\nTRUNCATED.\n", "same_sha"))
	artifactName := "2026-05-16-zen-swarm-plan-13-spike-hermes-mcp-contract.md"
	if err := os.WriteFile(filepath.Join(specsDir, artifactName), staleBytes, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := runSpike(context.Background(), spikeOpts{
		check:   true,
		apiBase: apiBase,
		rawBase: rawBase,
	})
	if err == nil {
		t.Fatal("expected byte-drift error, got nil")
	}
	if !strings.Contains(err.Error(), "drift") {
		t.Errorf("expected drift error, got %v", err)
	}
}

func TestRunSpike_RegenerateCreatesArtifact(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll(filepath.Join("docs", "superpowers", "specs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	apiBase, rawBase, cleanup := newMockHermesServers(t, "fresh_sha", samplePluginsPy)
	defer cleanup()

	err := runSpike(context.Background(), spikeOpts{
		regenerate: true,
		apiBase:    apiBase,
		rawBase:    rawBase,
	})
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}

	today := time.Now().Format("2006-01-02")
	expectedName := fmt.Sprintf("%s-zen-swarm-plan-13-spike-hermes-mcp-contract.md", today)
	expectedPath := filepath.Join("docs", "superpowers", "specs", expectedName)
	body, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("artifact not created at %s: %v", expectedPath, err)
	}
	if !strings.Contains(string(body), "fresh_sha") {
		t.Errorf("artifact missing fresh SHA: %s", body)
	}
	if !strings.Contains(string(body), "pre_tool_call") {
		t.Errorf("artifact missing extracted hook: %s", body)
	}
	if !strings.Contains(string(body), "End of Plan 13 Phase 0 spike artifact.") {
		t.Errorf("artifact missing trailing sentinel: %s", body)
	}
}

func TestRunSpike_RegenerateOverwrites(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	specsDir := filepath.Join("docs", "superpowers", "specs")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	apiBase, rawBase, cleanup := newMockHermesServers(t, "regenerated_sha", samplePluginsPy)
	defer cleanup()

	artifactName := "2026-05-16-zen-swarm-plan-13-spike-hermes-mcp-contract.md"
	artifactPath := filepath.Join(specsDir, artifactName)
	if err := os.WriteFile(artifactPath, []byte("OLD CONTENT"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := runSpike(context.Background(), spikeOpts{
		regenerate: true,
		apiBase:    apiBase,
		rawBase:    rawBase,
	})
	if err != nil {
		t.Fatalf("regenerate overwrite: %v", err)
	}

	body, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	if strings.Contains(string(body), "OLD CONTENT") {
		t.Errorf("artifact still contains old content: %s", body)
	}
	if !strings.Contains(string(body), "regenerated_sha") {
		t.Errorf("artifact missing regenerated SHA: %s", body)
	}
}

func TestRunSpike_NetworkError(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll(filepath.Join("docs", "superpowers", "specs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	err := runSpike(context.Background(), spikeOpts{
		check:   true,
		apiBase: srv.URL,
		rawBase: srv.URL,
	})
	if err == nil {
		t.Fatal("expected network error, got nil")
	}
	if !strings.Contains(err.Error(), "fetch hermes head") {
		t.Errorf("expected fetch wrap, got %v", err)
	}
}

func TestRunSpike_DefaultsToProductionBaseURLs(t *testing.T) {

	if defaultGitHubAPIBase == "" {
		t.Error("defaultGitHubAPIBase must not be empty")
	}
	if defaultRawGitHubBase == "" {
		t.Error("defaultRawGitHubBase must not be empty")
	}
	if !strings.Contains(defaultGitHubAPIBase, "api.github.com") {
		t.Errorf("defaultGitHubAPIBase=%q expected to contain api.github.com", defaultGitHubAPIBase)
	}
	if !strings.Contains(defaultRawGitHubBase, "raw.githubusercontent.com") {
		t.Errorf("defaultRawGitHubBase=%q expected to contain raw.githubusercontent.com", defaultRawGitHubBase)
	}
}

func TestRunSpike_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"sha": "x"})
	}))
	defer srv.Close()

	_, err := fetchHeadSHA(ctx, srv.Client(), srv.URL)
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestRunMain_OfflineHappy(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	specsDir := filepath.Join("docs", "superpowers", "specs")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	artifactName := "2026-05-16-zen-swarm-plan-13-spike-hermes-mcp-contract.md"
	if err := os.WriteFile(filepath.Join(specsDir, artifactName), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	var buf bytes.Buffer
	got := runMain(context.Background(), []string{"--offline"}, &buf)
	if got != 0 {
		t.Errorf("runMain offline: exit code %d, want 0; stderr=%q", got, buf.String())
	}
}

func TestRunMain_MutexError(t *testing.T) {
	var buf bytes.Buffer
	got := runMain(context.Background(), []string{"--regenerate", "--offline"}, &buf)
	if got != 1 {
		t.Errorf("runMain mutex: exit code %d, want 1", got)
	}
	if !strings.Contains(buf.String(), "mutually exclusive") {
		t.Errorf("runMain mutex: expected 'mutually exclusive' in stderr, got %q", buf.String())
	}
}

func TestRunMain_DefaultModeIsCheck(t *testing.T) {

	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll(filepath.Join("docs", "superpowers", "specs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	var buf bytes.Buffer
	got := runMain(ctx, []string{}, &buf)
	if got != 1 {
		t.Errorf("runMain default (no mock, expect network/timeout error): exit code %d, want 1", got)
	}

	if !strings.Contains(buf.String(), "fetch hermes head") && !strings.Contains(buf.String(), "spike error") {
		t.Errorf("runMain default mode: expected 'fetch hermes head' in stderr, got %q", buf.String())
	}
}

func TestRunMain_FlagParseError(t *testing.T) {
	var buf bytes.Buffer
	got := runMain(context.Background(), []string{"--not-a-real-flag"}, &buf)
	if got != 1 {
		t.Errorf("runMain bad-flag: exit code %d, want 1", got)
	}
}

func TestRunSpike_RegenerateMissingArtifactCreatesNew(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll(filepath.Join("docs", "superpowers", "specs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	apiBase, rawBase, cleanup := newMockHermesServers(t, "auto_sha", samplePluginsPy)
	defer cleanup()

	err := runSpike(context.Background(), spikeOpts{
		regenerate: true,
		apiBase:    apiBase,
		rawBase:    rawBase,
	})
	if err != nil {
		t.Fatalf("runSpike regenerate: %v", err)
	}

	today := time.Now().Format("2006-01-02")
	expectedPath := filepath.Join("docs", "superpowers", "specs",
		fmt.Sprintf("%s-zen-swarm-plan-13-spike-hermes-mcp-contract.md", today))
	if _, err := os.Stat(expectedPath); err != nil {
		t.Errorf("expected artifact at %s: %v", expectedPath, err)
	}
}

func TestRunSpike_CheckArtifactMissing(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll(filepath.Join("docs", "superpowers", "specs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	apiBase, rawBase, cleanup := newMockHermesServers(t, "sha", samplePluginsPy)
	defer cleanup()

	err := runSpike(context.Background(), spikeOpts{
		check:   true,
		apiBase: apiBase,
		rawBase: rawBase,
	})
	if err == nil {
		t.Fatal("expected artifact-missing error, got nil")
	}
	if !strings.Contains(err.Error(), "artifact missing") {
		t.Errorf("expected 'artifact missing' wrap, got %v", err)
	}
}

func TestExtractManifestKeys_FieldNameInvalid(t *testing.T) {
	src := `class PluginManifest:
    name: str
    : invalid line with no name
    valid_one: str = "default"`
	got := extractManifestKeys(src)
	want := []string{"name"}
	assertStringSliceEqual(t, "FieldNameInvalid", got, want)
}

func assertStringSliceEqual(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d items, want %d: got=%v want=%v", label, len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s[%d]: got %q want %q", label, i, got[i], want[i])
		}
	}
}
