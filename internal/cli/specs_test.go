package cli

import (
	"bytes"
	"context"
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

type fakeSpecsDaemonClient struct {
	syncResp *client.SpecsSyncResponse
	syncErr  error

	lastSyncReq client.SpecsSyncRequest
}

func (f *fakeSpecsDaemonClient) SpecsSync(_ context.Context, req client.SpecsSyncRequest) (*client.SpecsSyncResponse, error) {
	f.lastSyncReq = req
	if f.syncErr != nil {
		return nil, f.syncErr
	}
	if f.syncResp == nil {
		return &client.SpecsSyncResponse{}, nil
	}
	return f.syncResp, nil
}

func writeSpecFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestSpecsListRendersMarkdownFiles(t *testing.T) {
	dir := t.TempDir()
	writeSpecFile(t, filepath.Join(dir, "adr-0001.md"), "# ADR-0001: Single egress\n\nbody\n")
	writeSpecFile(t, filepath.Join(dir, "design-v1.md"), "# Design v1\n\nbody\n")

	var buf bytes.Buffer
	if err := RunSpecsList(dir, "text", &buf); err != nil {
		t.Fatalf("RunSpecsList: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "adr-0001") {
		t.Errorf("missing adr-0001 in output: %s", out)
	}
	if !strings.Contains(out, "ADR-0001: Single egress") {
		t.Errorf("missing title: %s", out)
	}
	if !strings.Contains(out, "design-v1") {
		t.Errorf("missing design-v1 in output: %s", out)
	}
	if !strings.Contains(out, "ID") || !strings.Contains(out, "TITLE") {
		t.Errorf("missing header row: %s", out)
	}
}

func TestSpecsListJSONFormat(t *testing.T) {
	dir := t.TempDir()
	writeSpecFile(t, filepath.Join(dir, "adr-0001.md"), "# ADR-0001: Single egress\n\nbody\n")

	var buf bytes.Buffer
	if err := RunSpecsList(dir, "json", &buf); err != nil {
		t.Fatalf("RunSpecsList: %v", err)
	}

	var got []map[string]string
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, buf.String())
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 spec, got %d: %v", len(got), got)
	}
	if got[0]["id"] != "adr-0001" {
		t.Errorf("id mismatch: %s", got[0]["id"])
	}
	if got[0]["title"] != "ADR-0001: Single egress" {
		t.Errorf("title mismatch: %s", got[0]["title"])
	}
}

func TestSpecsListNoDirectory(t *testing.T) {
	var buf bytes.Buffer
	if err := RunSpecsList("/nonexistent/specs/dir/xyz", "text", &buf); err != nil {
		t.Fatalf("RunSpecsList should not error on missing dir: %v", err)
	}
	if !strings.Contains(buf.String(), "no specs directory found") {
		t.Errorf("expected fallback message: %s", buf.String())
	}
}

func TestSpecsListEmptyDir(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	if err := RunSpecsList(dir, "text", &buf); err != nil {
		t.Fatalf("RunSpecsList: %v", err)
	}
	if !strings.Contains(buf.String(), "(no specs)") {
		t.Errorf("expected (no specs): %s", buf.String())
	}
}

func TestSpecsListRejectsBadFormat(t *testing.T) {
	dir := t.TempDir()
	writeSpecFile(t, filepath.Join(dir, "x.md"), "# X\n")
	var buf bytes.Buffer
	err := RunSpecsList(dir, "yaml", &buf)
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable, got %v", err)
	}
}

func TestSpecsListSkipsNonMarkdownAndDirs(t *testing.T) {
	dir := t.TempDir()
	writeSpecFile(t, filepath.Join(dir, "adr-0001.md"), "# ADR-0001\n")
	writeSpecFile(t, filepath.Join(dir, "README.txt"), "not a spec")
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := RunSpecsList(dir, "text", &buf); err != nil {
		t.Fatalf("RunSpecsList: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "README") {
		t.Errorf("should not list .txt: %s", out)
	}
	if strings.Contains(out, "subdir") {
		t.Errorf("should not list subdir: %s", out)
	}
	if !strings.Contains(out, "adr-0001") {
		t.Errorf("should list adr-0001: %s", out)
	}
}

func TestSpecsListReadFirstLineFallback(t *testing.T) {
	dir := t.TempDir()

	writeSpecFile(t, filepath.Join(dir, "empty.md"), "\n\n\n")
	var buf bytes.Buffer
	if err := RunSpecsList(dir, "text", &buf); err != nil {
		t.Fatalf("RunSpecsList: %v", err)
	}
	if !strings.Contains(buf.String(), "empty") {
		t.Errorf("expected filename fallback: %s", buf.String())
	}
}

func TestSpecsListSkipsReadme(t *testing.T) {
	dir := t.TempDir()
	writeSpecFile(t, filepath.Join(dir, "README.md"), "# README\n")
	writeSpecFile(t, filepath.Join(dir, "adr-0001.md"), "# ADR-0001\n")
	var buf bytes.Buffer
	if err := RunSpecsList(dir, "text", &buf); err != nil {
		t.Fatalf("RunSpecsList: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "README") {
		t.Errorf("README.md should be filtered out: %s", out)
	}
	if !strings.Contains(out, "adr-0001") {
		t.Errorf("adr-0001 should still appear: %s", out)
	}
}

func TestSpecsShowRendersContent(t *testing.T) {
	dir := t.TempDir()
	body := "# ADR-0001\n\nFull body here.\n"
	writeSpecFile(t, filepath.Join(dir, "adr-0001.md"), body)

	var buf bytes.Buffer
	if err := RunSpecsShow(dir, "adr-0001", "text", &buf); err != nil {
		t.Fatalf("RunSpecsShow: %v", err)
	}
	if !strings.Contains(buf.String(), "Full body here.") {
		t.Errorf("body missing: %s", buf.String())
	}
}

func TestSpecsShowMissingSpec(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	err := RunSpecsShow(dir, "does-not-exist", "text", &buf)
	if err == nil {
		t.Fatal("expected error for missing spec")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable, got %v", err)
	}
}

func TestSpecsShowRejectsEmptyID(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	err := RunSpecsShow(dir, "", "text", &buf)
	if err == nil {
		t.Fatal("expected error for empty id")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable, got %v", err)
	}
}

func TestSpecsShowRejectsBadFormat(t *testing.T) {
	dir := t.TempDir()
	writeSpecFile(t, filepath.Join(dir, "x.md"), "# X\n")
	var buf bytes.Buffer
	err := RunSpecsShow(dir, "x", "yaml", &buf)
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable, got %v", err)
	}
}

func TestSpecsShowRejectsJSONFormat(t *testing.T) {
	dir := t.TempDir()
	writeSpecFile(t, filepath.Join(dir, "x.md"), "# X\n")
	var buf bytes.Buffer
	err := RunSpecsShow(dir, "x", "json", &buf)
	if err == nil {
		t.Fatal("expected error for json format (show only supports text|md)")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable, got %v", err)
	}
}

func TestSpecsShowRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	err := RunSpecsShow(dir, "../etc/passwd", "text", &buf)
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable, got %v", err)
	}
}

func TestSpecsDiffRendersDeltas(t *testing.T) {
	changesDir := t.TempDir()
	changeID := "feature-x"
	deltasDir := filepath.Join(changesDir, changeID, "deltas")
	writeSpecFile(t, filepath.Join(deltasDir, "architecture.md"), "## Added\n\n- New endpoint /v1/x\n")
	writeSpecFile(t, filepath.Join(deltasDir, "schema.md"), "## Modified\n\n- Column foo\n")

	var buf bytes.Buffer
	if err := RunSpecsDiff(changesDir, changeID, SpecsDiffFlags{}, &buf); err != nil {
		t.Fatalf("RunSpecsDiff: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "architecture.md") {
		t.Errorf("missing delta header architecture.md: %s", out)
	}
	if !strings.Contains(out, "New endpoint /v1/x") {
		t.Errorf("missing delta body: %s", out)
	}
	if !strings.Contains(out, "schema.md") {
		t.Errorf("missing delta header schema.md: %s", out)
	}
}

func TestSpecsDiffVersionRangeRecoverable(t *testing.T) {
	changesDir := t.TempDir()
	changeID := "feature-x"
	writeSpecFile(t, filepath.Join(changesDir, changeID, "deltas", "x.md"), "x")
	var buf bytes.Buffer
	err := RunSpecsDiff(changesDir, changeID, SpecsDiffFlags{VersionRange: "v1..v2"}, &buf)
	if err == nil {
		t.Fatal("expected --v to be recoverable (not yet supported)")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable, got %v", err)
	}
}

func TestSpecsDiffMissingChange(t *testing.T) {
	changesDir := t.TempDir()
	var buf bytes.Buffer
	err := RunSpecsDiff(changesDir, "no-such-change", SpecsDiffFlags{}, &buf)
	if err == nil {
		t.Fatal("expected error for missing change")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable, got %v", err)
	}
}

func TestSpecsDiffRejectsEmptyID(t *testing.T) {
	changesDir := t.TempDir()
	var buf bytes.Buffer
	err := RunSpecsDiff(changesDir, "", SpecsDiffFlags{}, &buf)
	if err == nil {
		t.Fatal("expected error for empty id")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable, got %v", err)
	}
}

func TestSpecsDiffNoDeltasDir(t *testing.T) {
	changesDir := t.TempDir()
	changeID := "feature-x"

	if err := os.MkdirAll(filepath.Join(changesDir, changeID), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	err := RunSpecsDiff(changesDir, changeID, SpecsDiffFlags{}, &buf)
	if err == nil {
		t.Fatal("expected error for missing deltas/")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable, got %v", err)
	}
}

func TestSpecsDiffRejectsTraversal(t *testing.T) {
	changesDir := t.TempDir()
	var buf bytes.Buffer
	err := RunSpecsDiff(changesDir, "../escape", SpecsDiffFlags{}, &buf)
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable, got %v", err)
	}
}

func TestSpecsDiffEmptyDeltasDir(t *testing.T) {
	changesDir := t.TempDir()
	changeID := "feature-x"
	if err := os.MkdirAll(filepath.Join(changesDir, changeID, "deltas"), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := RunSpecsDiff(changesDir, changeID, SpecsDiffFlags{}, &buf); err != nil {
		t.Fatalf("RunSpecsDiff (empty deltas): %v", err)
	}
	if !strings.Contains(buf.String(), "no delta files") {
		t.Errorf("expected '(no delta files...)' message: %s", buf.String())
	}
}

func TestSpecsSyncCallsDaemon(t *testing.T) {
	c := &fakeSpecsDaemonClient{
		syncResp: &client.SpecsSyncResponse{
			ChunksIndexed: 42,
			SpecsScanned:  7,
			ElapsedMs:     123,
		},
	}
	var buf bytes.Buffer
	err := RunSpecsSync(context.Background(), c, SpecsSyncFlags{Full: true}, &buf)
	if err != nil {
		t.Fatalf("RunSpecsSync: %v", err)
	}
	if !c.lastSyncReq.Full {
		t.Errorf("expected Full=true on request")
	}
	out := buf.String()
	if !strings.Contains(out, "specs=7") {
		t.Errorf("missing specs count: %s", out)
	}
	if !strings.Contains(out, "chunks=42") {
		t.Errorf("missing chunk count: %s", out)
	}
	if !strings.Contains(out, "elapsed=123ms") {
		t.Errorf("missing elapsed: %s", out)
	}
}

func TestSpecsSyncRendersMessage(t *testing.T) {
	c := &fakeSpecsDaemonClient{
		syncResp: &client.SpecsSyncResponse{
			Message:       "delta no-op",
			SpecsScanned:  0,
			ChunksIndexed: 0,
		},
	}
	var buf bytes.Buffer
	if err := RunSpecsSync(context.Background(), c, SpecsSyncFlags{}, &buf); err != nil {
		t.Fatalf("RunSpecsSync: %v", err)
	}
	if !strings.Contains(buf.String(), "delta no-op") {
		t.Errorf("expected message rendered: %s", buf.String())
	}
}

func TestSpecsSyncDaemonError422(t *testing.T) {
	c := &fakeSpecsDaemonClient{
		syncErr: &client.HTTPError{
			Method: "POST",
			Path:   "/v1/knowledge/ecosystem/specs-sync",
			Status: http.StatusUnprocessableEntity,
		},
	}
	var buf bytes.Buffer
	err := RunSpecsSync(context.Background(), c, SpecsSyncFlags{}, &buf)
	if err == nil {
		t.Fatal("expected error from daemon 422")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable, got %v", err)
	}
}

func TestSpecsSyncDaemonError503(t *testing.T) {
	c := &fakeSpecsDaemonClient{
		syncErr: &client.HTTPError{
			Method: "POST",
			Path:   "/v1/knowledge/ecosystem/specs-sync",
			Status: http.StatusServiceUnavailable,
		},
	}
	var buf bytes.Buffer
	err := RunSpecsSync(context.Background(), c, SpecsSyncFlags{}, &buf)
	if err == nil {
		t.Fatal("expected error from daemon 503")
	}

	if errors.Is(err, ErrRecoverable) {
		t.Errorf("503 should be unrecoverable, got %v", err)
	}
}

func TestSpecsSyncDaemonError404Recoverable(t *testing.T) {
	c := &fakeSpecsDaemonClient{
		syncErr: &client.HTTPError{
			Method: "POST",
			Path:   "/v1/knowledge/ecosystem/specs-sync",
			Status: http.StatusNotFound,
		},
	}
	var buf bytes.Buffer
	err := RunSpecsSync(context.Background(), c, SpecsSyncFlags{}, &buf)
	if err == nil {
		t.Fatal("expected error from daemon 404")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("404 (route not yet wired in phase G) should be recoverable, got %v", err)
	}
}

func TestNewSpecsCmdRegistersSubcommands(t *testing.T) {
	cmd := NewSpecsCmd(SpecsDaemonClientFactory(func(_ *cobra.Command) SpecsDaemonClient {
		return &fakeSpecsDaemonClient{}
	}))
	want := map[string]bool{
		"list": true,
		"show": true,
		"diff": true,
		"sync": true,
	}
	got := map[string]bool{}
	for _, sub := range cmd.Commands() {
		got[sub.Name()] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("missing subcommand: %s", name)
		}
	}
}

func TestNewSpecsCmdNilFactory(t *testing.T) {

	cmd := NewSpecsCmd(nil)
	got := map[string]bool{}
	for _, sub := range cmd.Commands() {
		got[sub.Name()] = true
	}
	for _, name := range []string{"list", "show", "diff", "sync"} {
		if !got[name] {
			t.Errorf("missing subcommand: %s", name)
		}
	}
}

func TestNewSpecsCmdProdRegistersSubcommands(t *testing.T) {
	cmd := NewSpecsCmdProd()
	if cmd == nil {
		t.Fatal("NewSpecsCmdProd returned nil")
	}
	if cmd.Name() != "specs" {
		t.Errorf("expected name 'specs', got %q", cmd.Name())
	}
	got := map[string]bool{}
	for _, sub := range cmd.Commands() {
		got[sub.Name()] = true
	}
	for _, name := range []string{"list", "show", "diff", "sync"} {
		if !got[name] {
			t.Errorf("missing subcommand: %s", name)
		}
	}
}

func TestSpecsListReadFirstLineMissing(t *testing.T) {

	got := readSpecsFirstLine("/no/such/path/file.md")
	if got != "file.md" {
		t.Errorf("expected filename fallback, got %s", got)
	}
}

func TestSpecsListReadFirstLineSkipsBlanks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.md")
	writeSpecFile(t, path, "\n\n## Heading 2\nbody\n")
	got := readSpecsFirstLine(path)
	if got != "## Heading 2" {
		t.Errorf("expected '## Heading 2' (second-level heading kept verbatim), got %s", got)
	}
}

func TestSpecsListReadFirstLineStripsH1(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.md")
	writeSpecFile(t, path, "# My Title\n\nbody\n")
	got := readSpecsFirstLine(path)
	if got != "My Title" {
		t.Errorf("expected 'My Title' (H1 prefix stripped), got %s", got)
	}
}

func TestClassifySpecsErrorPassThrough(t *testing.T) {
	if got := classifySpecsError(nil, "x"); got != nil {
		t.Errorf("nil pass-through, got %v", got)
	}
	rec := recoverable("already recoverable")
	if got := classifySpecsError(rec, "x"); !errors.Is(got, ErrRecoverable) {
		t.Errorf("recoverable should pass through; got %v", got)
	}
	plain := errors.New("plain")
	if got := classifySpecsError(plain, "x"); errors.Is(got, ErrRecoverable) {
		t.Errorf("plain should be unrecoverable; got %v", got)
	}
}

func TestResolveSpecsSubdirWithRoot(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("root", "/custom/root", "")
	_ = cmd.Flags().Set("root", "/custom/root")
	got := resolveSpecsSubdir(cmd, "specs")
	want := filepath.Join("/custom/root", "openspec", "specs")
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestResolveSpecsSubdirDefaultCwd(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Skipf("Getwd unavailable: %v", err)
	}
	cmd := &cobra.Command{}
	cmd.Flags().String("root", "", "")
	got := resolveSpecsSubdir(cmd, "changes")
	want := filepath.Join(cwd, "openspec", "changes")
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestResolveSpecsSubdirWithParent(t *testing.T) {

	parent := &cobra.Command{Use: "specs"}
	parent.PersistentFlags().String("root", "", "")
	leaf := &cobra.Command{Use: "list"}
	parent.AddCommand(leaf)

	if err := parent.PersistentFlags().Set("root", "/parent/root"); err != nil {
		t.Fatal(err)
	}
	got := resolveSpecsSubdir(leaf, "specs")
	want := filepath.Join("/parent/root", "openspec", "specs")
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestValidateSpecIDForwardSlash(t *testing.T) {
	if err := validateSpecID("a/b"); err == nil || !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable for forward slash, got %v", err)
	}
}

func TestValidateSpecIDBackslash(t *testing.T) {
	if err := validateSpecID(`a\b`); err == nil || !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable for backslash, got %v", err)
	}
}

func TestValidateSpecIDDotDot(t *testing.T) {
	if err := validateSpecID(".."); err == nil || !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable for '..', got %v", err)
	}
}

func TestValidateSpecIDAbsolute(t *testing.T) {

	if err := validateSpecID("/foo"); err == nil || !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable for absolute path, got %v", err)
	}
}

func TestValidateSpecIDValid(t *testing.T) {

	if err := validateSpecID("adr-0001"); err != nil {
		t.Errorf("plain stem should validate, got %v", err)
	}
	if err := validateSpecID("design_v1"); err != nil {
		t.Errorf("underscored stem should validate, got %v", err)
	}
}

func TestProductionSpecsDaemonClientWiring(t *testing.T) {
	c := client.NewWithBaseURL("http://localhost:1")
	p := &productionSpecsDaemonClient{c: c}
	if p.c == nil {
		t.Fatal("inner client must be set")
	}
}

func TestProductionSpecsDaemonClientDelegates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"chunks_indexed":5,"specs_scanned":3,"elapsed_ms":42}`))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	p := &productionSpecsDaemonClient{c: c}
	resp, err := p.SpecsSync(context.Background(), client.SpecsSyncRequest{Full: true})
	if err != nil {
		t.Fatalf("SpecsSync: %v", err)
	}
	if resp.ChunksIndexed != 5 {
		t.Errorf("ChunksIndexed: got %d want 5", resp.ChunksIndexed)
	}
}

func TestSpecsListCmdExecute(t *testing.T) {
	dir := t.TempDir()
	writeSpecFile(t, filepath.Join(dir, "adr-0001.md"), "# ADR-0001\n")
	cmd := NewSpecsCmd(nil)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"list", "--root", filepath.Dir(dir)})

	specsRoot := filepath.Join(filepath.Dir(dir), "openspec", "specs")
	writeSpecFile(t, filepath.Join(specsRoot, "adr-0001.md"), "# ADR-0001: from --root\n")
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "ADR-0001: from --root") {
		t.Errorf("expected --root resolution: %s", buf.String())
	}
}

func TestSpecsShowCmdExecute(t *testing.T) {
	rootDir := t.TempDir()
	specsRoot := filepath.Join(rootDir, "openspec", "specs")
	writeSpecFile(t, filepath.Join(specsRoot, "doc.md"), "# DocTitle\n\nBody.\n")
	cmd := NewSpecsCmd(nil)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"show", "doc", "--root", rootDir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "Body.") {
		t.Errorf("expected body: %s", buf.String())
	}
}

func TestSpecsDiffCmdExecute(t *testing.T) {
	rootDir := t.TempDir()
	changesRoot := filepath.Join(rootDir, "openspec", "changes")
	writeSpecFile(t, filepath.Join(changesRoot, "ch1", "deltas", "a.md"), "delta a\n")
	cmd := NewSpecsCmd(nil)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"diff", "ch1", "--root", rootDir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "delta a") {
		t.Errorf("expected delta body: %s", buf.String())
	}
}

func TestSpecsSyncCmdExecute(t *testing.T) {
	fake := &fakeSpecsDaemonClient{
		syncResp: &client.SpecsSyncResponse{
			ChunksIndexed: 1,
			SpecsScanned:  1,
			ElapsedMs:     1,
		},
	}
	cmd := NewSpecsCmd(SpecsDaemonClientFactory(func(_ *cobra.Command) SpecsDaemonClient {
		return fake
	}))
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"sync", "--full"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !fake.lastSyncReq.Full {
		t.Errorf("expected Full=true on wire")
	}
}

type errWriter struct{ err error }

func (w errWriter) Write(_ []byte) (int, error) { return 0, w.err }

func TestSpecsDiffSkipsNonMarkdownAndNestedDirs(t *testing.T) {
	t.Parallel()
	changesDir := t.TempDir()
	changeID := "feature-x"
	deltasDir := filepath.Join(changesDir, changeID, "deltas")
	writeSpecFile(t, filepath.Join(deltasDir, "kept.md"), "kept body\n")
	writeSpecFile(t, filepath.Join(deltasDir, "noise.txt"), "should not appear\n")
	writeSpecFile(t, filepath.Join(deltasDir, "README"), "ignored\n")
	if err := os.MkdirAll(filepath.Join(deltasDir, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	writeSpecFile(t, filepath.Join(deltasDir, "nested", "ignored.md"), "do not include\n")

	var buf bytes.Buffer
	if err := RunSpecsDiff(changesDir, changeID, SpecsDiffFlags{}, &buf); err != nil {
		t.Fatalf("RunSpecsDiff: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "kept.md") {
		t.Errorf("expected kept.md header: %s", out)
	}
	if !strings.Contains(out, "kept body") {
		t.Errorf("expected kept body: %s", out)
	}
	if strings.Contains(out, "should not appear") {
		t.Errorf("non-markdown file leaked: %s", out)
	}
	if strings.Contains(out, "do not include") {
		t.Errorf("nested .md file leaked: %s", out)
	}
}

func TestSpecsDiffRendersFileWithoutTrailingNewline(t *testing.T) {
	t.Parallel()
	changesDir := t.TempDir()
	changeID := "feature-x"
	deltasDir := filepath.Join(changesDir, changeID, "deltas")

	writeSpecFile(t, filepath.Join(deltasDir, "first.md"), "no-newline-here")
	writeSpecFile(t, filepath.Join(deltasDir, "second.md"), "second body\n")

	var buf bytes.Buffer
	if err := RunSpecsDiff(changesDir, changeID, SpecsDiffFlags{}, &buf); err != nil {
		t.Fatalf("RunSpecsDiff: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "=== first.md ===") {
		t.Errorf("missing first header: %s", out)
	}
	if !strings.Contains(out, "=== second.md ===") {
		t.Errorf("missing second header: %s", out)
	}

	if strings.Contains(out, "no-newline-here=== second.md ===") {
		t.Errorf("missing separator newline between files: %s", out)
	}
}

func TestSpecsDiffStatErrorOtherThanNotExist(t *testing.T) {
	t.Parallel()
	changesDir := t.TempDir()

	changeID := "feature\x00x"
	var buf bytes.Buffer
	err := RunSpecsDiff(changesDir, changeID, SpecsDiffFlags{}, &buf)
	if err == nil {
		t.Fatal("expected stat error for path with NUL byte")
	}

	if IsRecoverable(err) {
		t.Errorf("non-NotExist stat error should NOT be recoverable: %v", err)
	}
	if !strings.Contains(err.Error(), "specs diff") {
		t.Errorf("expected `specs diff` prefix, got: %v", err)
	}
}

func TestSpecsDiffReadDirErrorOtherThanNotExist(t *testing.T) {
	t.Parallel()
	changesDir := t.TempDir()
	changeID := "feature-x"
	changeRoot := filepath.Join(changesDir, changeID)
	if err := os.MkdirAll(changeRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(changeRoot, "deltas"), []byte("oops"), 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	err := RunSpecsDiff(changesDir, changeID, SpecsDiffFlags{}, &buf)
	if err == nil {
		t.Fatal("expected ReadDir error for file-shaped deltas/")
	}
	if IsRecoverable(err) {
		t.Errorf("non-NotExist ReadDir error should NOT be recoverable: %v", err)
	}
	if !strings.Contains(err.Error(), "specs diff") {
		t.Errorf("expected `specs diff` prefix, got: %v", err)
	}
}

func TestSpecsDiffWriterErrorOnHeader(t *testing.T) {
	t.Parallel()
	changesDir := t.TempDir()
	changeID := "feature-x"
	writeSpecFile(t, filepath.Join(changesDir, changeID, "deltas", "a.md"), "body\n")

	sentinel := errors.New("writer kaput")
	err := RunSpecsDiff(changesDir, changeID, SpecsDiffFlags{}, errWriter{err: sentinel})
	if err == nil {
		t.Fatal("expected writer error to propagate")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel writer error, got: %v", err)
	}
}

func TestSpecsDiffWriterErrorOnEmptyDeltasMessage(t *testing.T) {
	t.Parallel()
	changesDir := t.TempDir()
	changeID := "feature-x"
	if err := os.MkdirAll(filepath.Join(changesDir, changeID, "deltas"), 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("writer kaput")
	err := RunSpecsDiff(changesDir, changeID, SpecsDiffFlags{}, errWriter{err: sentinel})
	if err == nil {
		t.Fatal("expected writer error to propagate")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel writer error, got: %v", err)
	}
}

func TestSpecsDiffReadFileFailure(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("perm-denied test requires non-root euid")
	}
	changesDir := t.TempDir()
	changeID := "feature-x"
	deltasDir := filepath.Join(changesDir, changeID, "deltas")
	writeSpecFile(t, filepath.Join(deltasDir, "guarded.md"), "secret body\n")
	unreadable := filepath.Join(deltasDir, "guarded.md")
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatalf("chmod 000: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o600) })

	var buf bytes.Buffer
	err := RunSpecsDiff(changesDir, changeID, SpecsDiffFlags{}, &buf)
	if err == nil {
		t.Fatal("expected ReadFile permission error")
	}
	if IsRecoverable(err) {
		t.Errorf("file-read failure should NOT be recoverable: %v", err)
	}
	if !strings.Contains(err.Error(), "specs diff") {
		t.Errorf("expected `specs diff` prefix, got: %v", err)
	}
}
