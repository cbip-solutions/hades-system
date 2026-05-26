// Package cli — docs_test.go (Plan 14 Phase F Task F-6).
//
// Coverage for the six `zen docs *` management subcommands:
//
//	reindex / pin / prune / status / sources / router-retrain
//
// Each subcommand's testable Run* function takes a DocsClient seam; tests
// inject *fakeDocsClient to assert wire-call shape + classify the
// recoverable / unrecoverable error mapping per spec §6.2.
//
// CRITICAL invariant tests (load-bearing):
//
//	TestDocsPruneRequiresExplicitFlag — `zen docs prune` with neither
//	  --dry-run nor --confirm MUST return ErrRecoverable so the process
//	  exit-code maps to 1 (operator-recoverable). No accidental deletion.
//
//	TestDocsPruneDryRun — `--dry-run` invokes the daemon with DryRun=true
//	  and prints a "(dry-run) would prune:" preview line.
//
//	TestDocsPruneConfirm — `--confirm` invokes the daemon with DryRun=false
//	  and prints the post-prune summary.
package cli

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/client"
)

type fakeDocsClient struct {
	reindexReq  client.DocsReindexRequest
	reindexResp *client.DocsReindexResponse
	reindexErr  error

	pinEco    string
	pinVer    string
	pinCalled bool
	pinErr    error

	prunePreviewEco    string
	prunePreviewVer    string
	prunePreviewCalled bool
	prunePreviewResp   *client.EcosystemPrunePreview
	prunePreviewErr    error

	pruneEco    string
	pruneVer    string
	pruneCalled bool
	pruneErr    error

	statusResp *client.DocsStatusResponse
	statusErr  error

	sourcesResp   *client.DocsSourcesResponse
	sourcesErr    error
	sourcesCalled bool

	retrainResp *client.RouterRetrainResponse
	retrainErr  error
}

func (f *fakeDocsClient) DocsReindex(_ context.Context, req client.DocsReindexRequest) (*client.DocsReindexResponse, error) {
	f.reindexReq = req
	if f.reindexErr != nil {
		return nil, f.reindexErr
	}
	if f.reindexResp == nil {
		return &client.DocsReindexResponse{
			PackagesIngested:   10,
			ChunksIngested:     100,
			SymbolsRegistered:  50,
			ChangeNodesCreated: 5,
			ElapsedMs:          5000,
		}, nil
	}
	return f.reindexResp, nil
}

func (f *fakeDocsClient) EcosystemPin(_ context.Context, eco, ver string) error {
	f.pinEco = eco
	f.pinVer = ver
	f.pinCalled = true
	return f.pinErr
}

func (f *fakeDocsClient) EcosystemPrunePreview(_ context.Context, eco, ver string) (*client.EcosystemPrunePreview, error) {
	f.prunePreviewEco = eco
	f.prunePreviewVer = ver
	f.prunePreviewCalled = true
	if f.prunePreviewErr != nil {
		return nil, f.prunePreviewErr
	}
	if f.prunePreviewResp == nil {
		return &client.EcosystemPrunePreview{
			Ecosystem:      eco,
			Version:        ver,
			ChunkCount:     42,
			ChunkFP32Count: 42,
			SymbolCount:    17,
			ChangeCount:    3,
			FTS5Count:      42,
			Pinned:         false,
		}, nil
	}
	return f.prunePreviewResp, nil
}

func (f *fakeDocsClient) EcosystemPrune(_ context.Context, eco, ver string) error {
	f.pruneEco = eco
	f.pruneVer = ver
	f.pruneCalled = true
	return f.pruneErr
}

func (f *fakeDocsClient) DocsStatus(_ context.Context) (*client.DocsStatusResponse, error) {
	if f.statusErr != nil {
		return nil, f.statusErr
	}
	if f.statusResp == nil {
		return &client.DocsStatusResponse{
			Ecosystems: []client.EcosystemStatus{
				{
					Ecosystem:     "go",
					ChunkCount:    1234,
					SymbolCount:   567,
					StorageBytes:  10 * 1024 * 1024,
					LastPolled:    1736424000,
					LastIndexed:   1736424000,
					RetentionDays: 90,
				},
				{
					Ecosystem:     "python",
					ChunkCount:    890,
					SymbolCount:   234,
					StorageBytes:  5 * 1024 * 1024,
					LastPolled:    1736500000,
					LastIndexed:   1736500000,
					RetentionDays: 90,
				},
			},
		}, nil
	}
	return f.statusResp, nil
}

func (f *fakeDocsClient) DocsSources(_ context.Context) (*client.DocsSourcesResponse, error) {
	f.sourcesCalled = true
	if f.sourcesErr != nil {
		return nil, f.sourcesErr
	}
	if f.sourcesResp == nil {
		return &client.DocsSourcesResponse{
			Sources: []client.SourceStatus{
				{
					Name:        "pkg.go.dev",
					Ecosystem:   "go",
					SourceType:  "registry",
					URL:         "https://pkg.go.dev/",
					TTLHours:    24,
					LastIndexed: 1736424000,
					Status:      "ok",
				},
				{
					Name:        "pypi",
					Ecosystem:   "python",
					SourceType:  "registry",
					URL:         "https://pypi.org/",
					TTLHours:    24,
					LastIndexed: 1736300000,
					Status:      "stale",
				},
			},
		}, nil
	}
	return f.sourcesResp, nil
}

func (f *fakeDocsClient) DocsRouterRetrain(_ context.Context) (*client.RouterRetrainResponse, error) {
	if f.retrainErr != nil {
		return nil, f.retrainErr
	}
	if f.retrainResp == nil {
		return &client.RouterRetrainResponse{
			CheckpointPath: "/home/user/.local/share/zen-swarm/router/classifier.bin",
			Accuracy:       0.987,
			ElapsedMs:      4500,
		}, nil
	}
	return f.retrainResp, nil
}

func TestDocsCmd_RegistersAllSubcommands(t *testing.T) {
	t.Parallel()
	cmd := NewDocsCmd()
	wantNames := []string{"reindex", "pin", "prune", "status", "sources", "router-retrain"}
	got := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		got[sub.Name()] = true
	}
	for _, want := range wantNames {
		if !got[want] {
			t.Errorf("docs cmd missing subcommand %q (have %v)", want, got)
		}
	}
}

func TestDocsReindexCallsDaemon(t *testing.T) {
	t.Parallel()
	fake := &fakeDocsClient{}
	flags := DocsReindexFlags{Ecosystem: "go", Version: "1.22.0", Full: false}
	var buf bytes.Buffer
	if err := RunDocsReindex(context.Background(), fake, flags, &buf); err != nil {
		t.Fatalf("RunDocsReindex: %v", err)
	}
	if fake.reindexReq.Ecosystem != "go" {
		t.Errorf("Ecosystem = %q, want go", fake.reindexReq.Ecosystem)
	}
	if fake.reindexReq.Version != "1.22.0" {
		t.Errorf("Version = %q, want 1.22.0", fake.reindexReq.Version)
	}

	if !fake.reindexReq.DeltaOnly {
		t.Errorf("DeltaOnly = false, want true (full=false)")
	}
	out := buf.String()
	if !strings.Contains(out, "packages_ingested=10") {
		t.Errorf("output missing packages_ingested; got %q", out)
	}
	if !strings.Contains(out, "chunks_ingested=100") {
		t.Errorf("output missing chunks_ingested; got %q", out)
	}
}

func TestDocsReindex_FullFlagOverridesDelta(t *testing.T) {
	t.Parallel()
	fake := &fakeDocsClient{}
	flags := DocsReindexFlags{Full: true}
	if err := RunDocsReindex(context.Background(), fake, flags, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunDocsReindex: %v", err)
	}
	if fake.reindexReq.DeltaOnly {
		t.Errorf("DeltaOnly = true, want false (full=true)")
	}
}

func TestDocsReindex_ErrorPropagates(t *testing.T) {
	t.Parallel()
	fake := &fakeDocsClient{reindexErr: errors.New("transport down")}
	err := RunDocsReindex(context.Background(), fake, DocsReindexFlags{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error; got nil")
	}
	if !strings.Contains(err.Error(), "reindex") {
		t.Errorf("err = %v, want substring 'reindex'", err)
	}
}

func TestDocsPin_HappyPath_ConfirmedY(t *testing.T) {
	t.Parallel()
	fake := &fakeDocsClient{}
	flags := DocsPinFlags{Ecosystem: "go", Version: "1.22.0"}
	var buf bytes.Buffer
	if err := RunDocsPin(context.Background(), fake, flags, strings.NewReader("y\n"), &buf); err != nil {
		t.Fatalf("RunDocsPin: %v", err)
	}
	if !fake.pinCalled {
		t.Error("daemon EcosystemPin not called despite y answer")
	}
	if fake.pinEco != "go" || fake.pinVer != "1.22.0" {
		t.Errorf("pinned %s@%s, want go@1.22.0", fake.pinEco, fake.pinVer)
	}
	out := buf.String()
	if !strings.Contains(out, "go@1.22.0") {
		t.Errorf("output missing go@1.22.0; got %q", out)
	}
	if !strings.Contains(out, "pinned:") {
		t.Errorf("output missing 'pinned:' success line; got %q", out)
	}
}

func TestDocsPin_AbortedAtPrompt(t *testing.T) {
	t.Parallel()
	fake := &fakeDocsClient{}
	flags := DocsPinFlags{Ecosystem: "python", Version: "3.11.9"}
	var buf bytes.Buffer

	if err := RunDocsPin(context.Background(), fake, flags, strings.NewReader("\n"), &buf); err != nil {
		t.Fatalf("RunDocsPin: %v", err)
	}
	if fake.pinCalled {
		t.Error("daemon called despite blank prompt input")
	}
	if !strings.Contains(buf.String(), "Pin aborted by operator.") {
		t.Errorf("output missing abort line; got %q", buf.String())
	}
}

func TestDocsPin_RejectedAtPrompt(t *testing.T) {
	t.Parallel()
	fake := &fakeDocsClient{}
	flags := DocsPinFlags{Ecosystem: "rust", Version: "1.70.0"}
	var buf bytes.Buffer
	if err := RunDocsPin(context.Background(), fake, flags, strings.NewReader("n\n"), &buf); err != nil {
		t.Fatalf("RunDocsPin: %v", err)
	}
	if fake.pinCalled {
		t.Error("daemon called despite n answer")
	}
}

func TestDocsPin_EmptyEcosystemRecoverable(t *testing.T) {
	t.Parallel()
	fake := &fakeDocsClient{}
	err := RunDocsPin(context.Background(), fake, DocsPinFlags{Version: "1.0.0"},
		strings.NewReader("y\n"), &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("err = %v, want ErrRecoverable", err)
	}
	if fake.pinCalled {
		t.Error("daemon called despite invalid args")
	}
}

func TestDocsPin_InvalidEcosystemRecoverable(t *testing.T) {
	t.Parallel()
	fake := &fakeDocsClient{}
	err := RunDocsPin(context.Background(), fake, DocsPinFlags{Ecosystem: "java", Version: "21"},
		strings.NewReader("y\n"), &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("err = %v, want ErrRecoverable", err)
	}
	if !strings.Contains(err.Error(), "java") {
		t.Errorf("err missing 'java'; got %v", err)
	}
}

func TestDocsPin_EmptyVersionRecoverable(t *testing.T) {
	t.Parallel()
	fake := &fakeDocsClient{}
	err := RunDocsPin(context.Background(), fake, DocsPinFlags{Ecosystem: "go"},
		strings.NewReader("y\n"), &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("err = %v, want ErrRecoverable", err)
	}
}

func TestDocsPin_DaemonErrorClassified(t *testing.T) {
	t.Parallel()
	fake := &fakeDocsClient{pinErr: errors.New("transport down")}
	err := RunDocsPin(context.Background(), fake,
		DocsPinFlags{Ecosystem: "go", Version: "1.22.0"},
		strings.NewReader("y\n"), &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "pin") {
		t.Errorf("err = %v, want substring 'pin'", err)
	}
}

func TestDocsPin_LongDescriptionMentionsPinSemantic(t *testing.T) {
	t.Parallel()
	cmd := NewDocsPinCmd(func(*cobra.Command) DocsClient { return &fakeDocsClient{} })
	long := strings.ToLower(cmd.Long)
	if !strings.Contains(long, "indefinite_retain") &&
		!strings.Contains(long, "never archived") &&
		!strings.Contains(long, "never auto-pruned") {
		t.Errorf("Long description should mention retention semantics; got: %s", cmd.Long)
	}
}

func TestDocsPin_PromptReadError(t *testing.T) {
	t.Parallel()
	fake := &fakeDocsClient{}
	flags := DocsPinFlags{Ecosystem: "go", Version: "1.0.0"}
	err := RunDocsPin(context.Background(), fake, flags, errReader{errors.New("read fail")}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "prompt") {
		t.Errorf("err missing 'prompt'; got %v", err)
	}
	if fake.pinCalled {
		t.Error("daemon called despite prompt error")
	}
}

func TestDocsPrune_PromptReadError(t *testing.T) {
	t.Parallel()
	fake := &fakeDocsClient{}
	flags := DocsPruneFlags{Ecosystem: "go", Version: "1.0.0", Confirm: true}
	err := RunDocsPrune(context.Background(), fake, flags, errReader{errors.New("read fail")}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "prompt") {
		t.Errorf("err missing 'prompt'; got %v", err)
	}
	if !fake.prunePreviewCalled {
		t.Error("preview should be dialed before prompt")
	}
	if fake.pruneCalled {
		t.Error("DELETE dialed despite prompt error")
	}
}

type errReader struct{ err error }

func (e errReader) Read(_ []byte) (int, error) { return 0, e.err }

func TestDocsPrune_SafetyGateNoFlags(t *testing.T) {
	t.Parallel()
	c := &fakeDocsClient{}
	flags := DocsPruneFlags{Ecosystem: "go", Version: "1.0.0"}
	err := RunDocsPrune(context.Background(), c, flags, strings.NewReader(""), &bytes.Buffer{})
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable, got %v", err)
	}
	if !strings.Contains(err.Error(), "refusing silent prune") {
		t.Errorf("err message missing safety hint; got %q", err.Error())
	}
	if c.prunePreviewCalled || c.pruneCalled {
		t.Errorf("daemon called despite safety gate; preview=%v prune=%v",
			c.prunePreviewCalled, c.pruneCalled)
	}
}

func TestDocsPrune_MutuallyExclusiveFlags(t *testing.T) {
	t.Parallel()
	c := &fakeDocsClient{}
	flags := DocsPruneFlags{Ecosystem: "go", Version: "1.0.0", DryRun: true, Confirm: true}
	err := RunDocsPrune(context.Background(), c, flags, strings.NewReader(""), &bytes.Buffer{})
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable, got %v", err)
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("err missing 'mutually exclusive'; got %q", err.Error())
	}
	if c.prunePreviewCalled || c.pruneCalled {
		t.Error("daemon called despite mutual exclusion gate")
	}
}

func TestDocsPrune_DryRunCallsPreview(t *testing.T) {
	t.Parallel()
	c := &fakeDocsClient{}
	flags := DocsPruneFlags{Ecosystem: "go", Version: "1.21.0", DryRun: true}
	var buf bytes.Buffer
	if err := RunDocsPrune(context.Background(), c, flags, strings.NewReader(""), &buf); err != nil {
		t.Fatalf("RunDocsPrune: %v", err)
	}
	if !c.prunePreviewCalled {
		t.Error("expected EcosystemPrunePreview to be called in dry-run")
	}
	if c.pruneCalled {
		t.Error("EcosystemPrune (DELETE) called in dry-run path; must not")
	}
	out := buf.String()
	if !strings.Contains(out, "Prune preview") {
		t.Errorf("output missing 'Prune preview' header; got %q", out)
	}
	if !strings.Contains(out, "go@1.21.0") {
		t.Errorf("output missing eco@ver; got %q", out)
	}
	if !strings.Contains(out, "chunks:") {
		t.Errorf("output missing 'chunks:' row count; got %q", out)
	}
}

func TestDocsPrune_DryRunOnPinnedSurfacesGuidance(t *testing.T) {
	t.Parallel()
	c := &fakeDocsClient{
		prunePreviewResp: &client.EcosystemPrunePreview{
			Ecosystem: "go", Version: "1.22.0",
			ChunkCount: 5, Pinned: true,
		},
	}
	flags := DocsPruneFlags{Ecosystem: "go", Version: "1.22.0", DryRun: true}
	var buf bytes.Buffer
	if err := RunDocsPrune(context.Background(), c, flags, strings.NewReader(""), &buf); err != nil {
		t.Fatalf("RunDocsPrune: %v", err)
	}
	if !strings.Contains(buf.String(), "pinned") {
		t.Errorf("dry-run on pinned should surface 'pinned' in output; got %q", buf.String())
	}
	if !strings.Contains(buf.String(), "zen docs unpin") {
		t.Errorf("dry-run on pinned should mention unpin guidance; got %q", buf.String())
	}
}

func TestDocsPrune_ConfirmRefusesPinned(t *testing.T) {
	t.Parallel()
	c := &fakeDocsClient{
		prunePreviewResp: &client.EcosystemPrunePreview{
			Ecosystem: "go", Version: "1.22.0",
			ChunkCount: 5, Pinned: true,
		},
	}
	flags := DocsPruneFlags{Ecosystem: "go", Version: "1.22.0", Confirm: true}
	err := RunDocsPrune(context.Background(), c, flags, strings.NewReader("y\n"), &bytes.Buffer{})
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable; got %v", err)
	}
	if !strings.Contains(err.Error(), "pinned") {
		t.Errorf("err missing 'pinned'; got %v", err)
	}
	if !strings.Contains(err.Error(), "unpin") {
		t.Errorf("err missing 'unpin' guidance; got %v", err)
	}
	if c.pruneCalled {
		t.Error("EcosystemPrune called despite pinned refusal")
	}
}

func TestDocsPrune_ConfirmYDeletes(t *testing.T) {
	t.Parallel()
	c := &fakeDocsClient{}
	flags := DocsPruneFlags{Ecosystem: "python", Version: "3.9.0", Confirm: true}
	var buf bytes.Buffer
	if err := RunDocsPrune(context.Background(), c, flags, strings.NewReader("y\n"), &buf); err != nil {
		t.Fatalf("RunDocsPrune: %v", err)
	}
	if !c.prunePreviewCalled {
		t.Error("preview not dialed before DELETE")
	}
	if !c.pruneCalled {
		t.Error("DELETE not dialed after y answer")
	}
	if c.pruneEco != "python" || c.pruneVer != "3.9.0" {
		t.Errorf("DELETE got %s@%s, want python@3.9.0", c.pruneEco, c.pruneVer)
	}
	out := buf.String()
	if !strings.Contains(out, "pruned:") {
		t.Errorf("output missing 'pruned:' summary; got %q", out)
	}
	if !strings.Contains(out, "zen docs reindex") {
		t.Errorf("output missing reindex hint; got %q", out)
	}
}

func TestDocsPrune_ConfirmBlankAborts(t *testing.T) {
	t.Parallel()
	c := &fakeDocsClient{}
	flags := DocsPruneFlags{Ecosystem: "typescript", Version: "5.0.0", Confirm: true}
	var buf bytes.Buffer
	if err := RunDocsPrune(context.Background(), c, flags, strings.NewReader("\n"), &buf); err != nil {
		t.Fatalf("RunDocsPrune: %v", err)
	}
	if !c.prunePreviewCalled {
		t.Error("preview should be dialed regardless of prompt outcome")
	}
	if c.pruneCalled {
		t.Error("DELETE dialed despite blank prompt")
	}
	if !strings.Contains(buf.String(), "Prune aborted by operator.") {
		t.Errorf("output missing abort line; got %q", buf.String())
	}
}

func TestDocsPrune_PreviewError(t *testing.T) {
	t.Parallel()
	c := &fakeDocsClient{prunePreviewErr: errors.New("preview disk error")}
	flags := DocsPruneFlags{Ecosystem: "go", Version: "1.0.0", DryRun: true}
	err := RunDocsPrune(context.Background(), c, flags, strings.NewReader(""), &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected preview error to propagate")
	}
	if !strings.Contains(err.Error(), "prune") {
		t.Errorf("err missing 'prune' op tag; got %v", err)
	}
	if c.pruneCalled {
		t.Error("DELETE dialed despite preview error")
	}
}

func TestDocsPrune_DeleteError(t *testing.T) {
	t.Parallel()
	c := &fakeDocsClient{pruneErr: errors.New("disk full")}
	flags := DocsPruneFlags{Ecosystem: "go", Version: "1.0.0", Confirm: true}
	err := RunDocsPrune(context.Background(), c, flags, strings.NewReader("y\n"), &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected DELETE error to propagate")
	}
	if !strings.Contains(err.Error(), "prune") {
		t.Errorf("err missing 'prune' op tag; got %v", err)
	}
}

func TestDocsPrune_InvalidEcosystem(t *testing.T) {
	t.Parallel()
	c := &fakeDocsClient{}
	flags := DocsPruneFlags{Ecosystem: "java", Version: "21", DryRun: true}
	err := RunDocsPrune(context.Background(), c, flags, strings.NewReader(""), &bytes.Buffer{})
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable; got %v", err)
	}
	if c.prunePreviewCalled {
		t.Error("preview dialed despite invalid ecosystem")
	}
}

func TestDocsPrune_MissingEcosystem(t *testing.T) {
	t.Parallel()
	c := &fakeDocsClient{}
	flags := DocsPruneFlags{Version: "1.0.0", DryRun: true}
	err := RunDocsPrune(context.Background(), c, flags, strings.NewReader(""), &bytes.Buffer{})
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable; got %v", err)
	}
}

func TestDocsPrune_MissingVersion(t *testing.T) {
	t.Parallel()
	c := &fakeDocsClient{}
	flags := DocsPruneFlags{Ecosystem: "go", DryRun: true}
	err := RunDocsPrune(context.Background(), c, flags, strings.NewReader(""), &bytes.Buffer{})
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable; got %v", err)
	}
}

func TestDocsStatusRendersTable(t *testing.T) {
	t.Parallel()
	fake := &fakeDocsClient{}
	var buf bytes.Buffer
	if err := RunDocsStatus(context.Background(), fake, &buf); err != nil {
		t.Fatalf("RunDocsStatus: %v", err)
	}
	out := buf.String()

	for _, want := range []string{"ECOSYSTEM", "CHUNKS", "SYMBOLS", "STORAGE"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing header %q; got %q", want, out)
		}
	}

	for _, want := range []string{"go", "python", "1234", "890", "MB"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q; got %q", want, out)
		}
	}
}

func TestDocsStatus_EmptyEcosystems(t *testing.T) {
	t.Parallel()
	fake := &fakeDocsClient{statusResp: &client.DocsStatusResponse{}}
	var buf bytes.Buffer
	if err := RunDocsStatus(context.Background(), fake, &buf); err != nil {
		t.Fatalf("RunDocsStatus: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "ECOSYSTEM") {
		t.Errorf("output missing header; got %q", out)
	}

	if strings.Contains(out, " go ") || strings.Contains(out, " python ") {
		t.Errorf("empty response should not have data rows; got %q", out)
	}
}

func TestDocsStatus_ErrorPropagates(t *testing.T) {
	t.Parallel()
	fake := &fakeDocsClient{statusErr: errors.New("daemon offline")}
	err := RunDocsStatus(context.Background(), fake, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error; got nil")
	}
	if !strings.Contains(err.Error(), "status") {
		t.Errorf("err = %v, want substring 'status'", err)
	}
}

func TestDocsSourcesListRenders(t *testing.T) {
	t.Parallel()
	fake := &fakeDocsClient{}
	flags := DocsSourcesFlags{List: true}
	var buf bytes.Buffer
	if err := RunDocsSources(context.Background(), fake, flags, &buf); err != nil {
		t.Fatalf("RunDocsSources: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"NAME", "ECOSYSTEM", "URL", "TTL",
		"pkg.go.dev", "https://pkg.go.dev/", "ok",
		"pypi", "https://pypi.org/", "stale",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q; got %q", want, out)
		}
	}
}

func TestDocsSources_NoListShowsHint(t *testing.T) {
	t.Parallel()
	fake := &fakeDocsClient{}
	flags := DocsSourcesFlags{List: false}
	var buf bytes.Buffer
	if err := RunDocsSources(context.Background(), fake, flags, &buf); err != nil {
		t.Fatalf("RunDocsSources: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "--list") {
		t.Errorf("expected usage hint mentioning --list; got %q", out)
	}

	if fake.sourcesCalled {
		t.Errorf("daemon should not be called without --list")
	}
}

func TestDocsSources_ErrorPropagates(t *testing.T) {
	t.Parallel()
	fake := &fakeDocsClient{sourcesErr: errors.New("registry down")}
	flags := DocsSourcesFlags{List: true}
	err := RunDocsSources(context.Background(), fake, flags, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error; got nil")
	}
	if !strings.Contains(err.Error(), "sources") {
		t.Errorf("err = %v, want substring 'sources'", err)
	}
}

func TestDocsRouterRetrainCallsDaemon(t *testing.T) {
	t.Parallel()
	fake := &fakeDocsClient{}
	var buf bytes.Buffer
	if err := RunDocsRouterRetrain(context.Background(), fake, &buf); err != nil {
		t.Fatalf("RunDocsRouterRetrain: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "classifier.bin") {
		t.Errorf("output missing checkpoint path; got %q", out)
	}
	if !strings.Contains(out, "0.987") {
		t.Errorf("output missing accuracy 0.987; got %q", out)
	}
}

func TestDocsRouterRetrain_ErrorPropagates(t *testing.T) {
	t.Parallel()
	fake := &fakeDocsClient{retrainErr: errors.New("training panicked")}
	err := RunDocsRouterRetrain(context.Background(), fake, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error; got nil")
	}
	if !strings.Contains(err.Error(), "router-retrain") {
		t.Errorf("err = %v, want substring 'router-retrain'", err)
	}
}

func TestClassifyDocsError(t *testing.T) {
	t.Parallel()

	makeHTTPErr := func(status int) error {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{}`))
		}))
		defer srv.Close()
		c := client.NewWithBaseURL(srv.URL)
		_, err := c.DocsStatus(context.Background())
		return err
	}

	cases := []struct {
		name      string
		err       error
		op        string
		wantNil   bool
		wantRecov bool
		wantSub   string
	}{
		{
			name:    "nil passes through",
			err:     nil,
			op:      "status",
			wantNil: true,
		},
		{
			name:      "already-recoverable preserved",
			err:       ErrRecoverable,
			op:        "status",
			wantRecov: true,
			wantSub:   "operator-recoverable",
		},
		{
			name:      "404 -> recoverable not found",
			err:       makeHTTPErr(http.StatusNotFound),
			op:        "pin",
			wantRecov: true,
			wantSub:   "not found",
		},
		{
			name:      "409 -> recoverable conflict (G-5: pinned/already-pinned)",
			err:       makeHTTPErr(http.StatusConflict),
			op:        "prune",
			wantRecov: true,
			wantSub:   "conflict",
		},
		{
			name:      "422 -> recoverable validation",
			err:       makeHTTPErr(http.StatusUnprocessableEntity),
			op:        "prune",
			wantRecov: true,
			wantSub:   "rejected",
		},
		{
			name:      "503 -> unrecoverable daemon offline",
			err:       makeHTTPErr(http.StatusServiceUnavailable),
			op:        "reindex",
			wantRecov: false,
			wantSub:   "reindex",
		},
		{
			name:      "500 -> unrecoverable generic wrap",
			err:       makeHTTPErr(http.StatusInternalServerError),
			op:        "router-retrain",
			wantRecov: false,
			wantSub:   "router-retrain",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := classifyDocsError(tc.err, tc.op)
			if tc.wantNil {
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
				t.Fatalf("want non-recoverable; got ErrRecoverable: %v", got)
			}
			if tc.wantSub != "" && !strings.Contains(got.Error(), tc.wantSub) {
				t.Errorf("error %q missing substring %q", got.Error(), tc.wantSub)
			}
		})
	}
}

func TestFormatDocsUnixTime(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   int64
		want string
	}{
		{"zero is never", 0, "(never)"},
		{"epoch 1736424000 utc", 1736424000, "2025-01-09T12:00:00Z"},
	}
	for _, tc := range cases {
		got := formatDocsUnixTime(tc.in)
		if !strings.HasPrefix(got, tc.want) {
			t.Errorf("formatDocsUnixTime(%d) = %q, want prefix %q", tc.in, got, tc.want)
		}
	}
}

func TestDocsCmd_BuildableWithRealFactory(t *testing.T) {
	t.Parallel()

	cmd := NewDocsCmd()
	if cmd == nil {
		t.Fatal("NewDocsCmd returned nil")
	}
	if cmd.Use != "docs" {
		t.Errorf("Use = %q, want docs", cmd.Use)
	}

	wantCount := 6
	got := len(cmd.Commands())
	if got != wantCount {
		t.Errorf("subcommand count = %d, want %d", got, wantCount)
	}
}

func TestNewDocsReindexCmd_FlagsRegistered(t *testing.T) {
	t.Parallel()
	c := NewDocsReindexCmd(func(*cobra.Command) DocsClient { return &fakeDocsClient{} })
	for _, want := range []string{"ecosystem", "version", "full"} {
		if c.Flag(want) == nil {
			t.Errorf("docs reindex missing --%s flag", want)
		}
	}
}

func TestNewDocsPruneCmd_FlagsRegistered(t *testing.T) {
	t.Parallel()
	c := NewDocsPruneCmd(func(*cobra.Command) DocsClient { return &fakeDocsClient{} })
	for _, want := range []string{"dry-run", "confirm", "ecosystem", "version"} {
		if c.Flag(want) == nil {
			t.Errorf("docs prune missing --%s flag", want)
		}
	}
}

func TestNewDocsPinCmd_FlagsRegistered(t *testing.T) {
	t.Parallel()
	c := NewDocsPinCmd(func(*cobra.Command) DocsClient { return &fakeDocsClient{} })
	for _, want := range []string{"ecosystem", "version"} {
		if c.Flag(want) == nil {
			t.Errorf("docs pin missing --%s flag", want)
		}
	}
}

func TestNewDocsSourcesCmd_FlagsRegistered(t *testing.T) {
	t.Parallel()
	c := NewDocsSourcesCmd(func(*cobra.Command) DocsClient { return &fakeDocsClient{} })
	if c.Flag("list") == nil {
		t.Error("docs sources missing --list flag")
	}
}

func TestProductionDocsClient_DelegatesAllMethods(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/knowledge/ecosystem/reindex":
			_, _ = w.Write([]byte(`{"packages_ingested":1}`))
		case "/v1/ecosystem/pin":
			w.WriteHeader(http.StatusNoContent)
		case "/v1/ecosystem/prune-preview":
			_, _ = w.Write([]byte(`{"ecosystem":"go","version":"1.22.0","chunk_count":2,"pinned":false}`))
		case "/v1/ecosystem/version":
			w.WriteHeader(http.StatusNoContent)
		case "/v1/knowledge/ecosystem/status":
			_, _ = w.Write([]byte(`{"ecosystems":[]}`))
		case "/v1/knowledge/ecosystem/sources":
			_, _ = w.Write([]byte(`{"sources":[]}`))
		case "/v1/knowledge/ecosystem/router/retrain":
			_, _ = w.Write([]byte(`{"checkpoint_path":"/tmp/x.bin","accuracy":0.9,"elapsed_ms":100}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	p := &productionDocsClient{c: c}
	ctx := context.Background()

	if r, err := p.DocsReindex(ctx, client.DocsReindexRequest{}); err != nil || r.PackagesIngested != 1 {
		t.Errorf("DocsReindex: r=%+v err=%v", r, err)
	}
	if err := p.EcosystemPin(ctx, "go", "1.22.0"); err != nil {
		t.Errorf("EcosystemPin: %v", err)
	}
	if r, err := p.EcosystemPrunePreview(ctx, "go", "1.22.0"); err != nil || r.ChunkCount != 2 {
		t.Errorf("EcosystemPrunePreview: r=%+v err=%v", r, err)
	}
	if err := p.EcosystemPrune(ctx, "go", "1.22.0"); err != nil {
		t.Errorf("EcosystemPrune: %v", err)
	}
	if r, err := p.DocsStatus(ctx); err != nil || r == nil {
		t.Errorf("DocsStatus: r=%+v err=%v", r, err)
	}
	if r, err := p.DocsSources(ctx); err != nil || r == nil {
		t.Errorf("DocsSources: r=%+v err=%v", r, err)
	}
	if r, err := p.DocsRouterRetrain(ctx); err != nil || r.Accuracy != 0.9 {
		t.Errorf("DocsRouterRetrain: r=%+v err=%v", r, err)
	}
}

func TestProductionDocsFactory_BuildsRealAdapter(t *testing.T) {
	t.Parallel()

	carrier := &cobra.Command{}
	carrier.PersistentFlags().String("uds", "", "")
	c := productionDocsFactory(carrier)
	if c == nil {
		t.Fatal("productionDocsFactory returned nil")
	}

	var _ DocsClient = c
}

func TestNewDocsPinCmd_NoArgs(t *testing.T) {
	t.Parallel()
	c := NewDocsPinCmd(func(*cobra.Command) DocsClient { return &fakeDocsClient{} })
	if c.Args == nil {
		t.Fatal("docs pin missing Args validator")
	}
	if err := c.Args(c, []string{"unexpected"}); err == nil {
		t.Error("docs pin should reject positional args")
	}
	if err := c.Args(c, []string{}); err != nil {
		t.Errorf("docs pin should accept 0 args, got %v", err)
	}
}

func TestNewDocsStatusCmd_NoArgs(t *testing.T) {
	t.Parallel()
	c := NewDocsStatusCmd(func(*cobra.Command) DocsClient { return &fakeDocsClient{} })
	if err := c.Args(c, []string{"unexpected"}); err == nil {
		t.Error("docs status should reject positional args")
	}
	if err := c.Args(c, []string{}); err != nil {
		t.Errorf("docs status should accept 0 args, got %v", err)
	}
}

func TestNewDocsRouterRetrainCmd_NoArgs(t *testing.T) {
	t.Parallel()
	c := NewDocsRouterRetrainCmd(func(*cobra.Command) DocsClient { return &fakeDocsClient{} })
	if err := c.Args(c, []string{"unexpected"}); err == nil {
		t.Error("docs router-retrain should reject positional args")
	}
}

func TestNewDocsReindexCmd_RunE_InvokesRunDocsReindex(t *testing.T) {
	t.Parallel()
	fake := &fakeDocsClient{}
	c := NewDocsReindexCmd(func(*cobra.Command) DocsClient { return fake })
	c.SetArgs([]string{"--ecosystem", "go", "--full"})
	c.SetOut(&bytes.Buffer{})
	if err := c.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if fake.reindexReq.Ecosystem != "go" {
		t.Errorf("flag --ecosystem not forwarded; got %q", fake.reindexReq.Ecosystem)
	}
	if fake.reindexReq.DeltaOnly {
		t.Errorf("--full should set DeltaOnly=false; got true")
	}
}

func TestNewDocsPinCmd_RunE_ForwardsFlags(t *testing.T) {
	t.Parallel()
	fake := &fakeDocsClient{}
	c := NewDocsPinCmd(func(*cobra.Command) DocsClient { return fake })
	c.SetArgs([]string{"--ecosystem", "rust", "--version", "1.70.0"})
	c.SetIn(strings.NewReader("y\n"))
	c.SetOut(&bytes.Buffer{})
	if err := c.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if fake.pinEco != "rust" || fake.pinVer != "1.70.0" {
		t.Errorf("flags not forwarded; got eco=%q ver=%q", fake.pinEco, fake.pinVer)
	}
}

func TestNewDocsPruneCmd_RunE_GateActive(t *testing.T) {
	t.Parallel()
	fake := &fakeDocsClient{}
	c := NewDocsPruneCmd(func(*cobra.Command) DocsClient { return fake })
	c.SetArgs([]string{"--ecosystem", "go", "--version", "1.0.0"})
	c.SetIn(strings.NewReader(""))
	c.SetOut(&bytes.Buffer{})
	c.SilenceErrors = true
	c.SilenceUsage = true
	err := c.Execute()
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable; got %v", err)
	}
	if fake.pruneCalled {
		t.Error("daemon EcosystemPrune was called despite safety gate")
	}
	if fake.prunePreviewCalled {
		t.Error("EcosystemPrunePreview was called despite safety gate")
	}
}

func TestNewDocsPruneCmd_RunE_DryRunFullPath(t *testing.T) {
	t.Parallel()
	fake := &fakeDocsClient{}
	c := NewDocsPruneCmd(func(*cobra.Command) DocsClient { return fake })
	c.SetArgs([]string{"--ecosystem", "go", "--version", "1.0.0", "--dry-run"})
	c.SetIn(strings.NewReader(""))
	var buf bytes.Buffer
	c.SetOut(&buf)
	if err := c.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !fake.prunePreviewCalled {
		t.Error("preview not dialed in dry-run")
	}
	if fake.pruneCalled {
		t.Error("DELETE dialed in dry-run path")
	}
	if !strings.Contains(buf.String(), "chunks:") {
		t.Errorf("output missing row counts; got %q", buf.String())
	}
}

func TestNewDocsPruneCmd_RunE_ConfirmFullPath(t *testing.T) {
	t.Parallel()
	fake := &fakeDocsClient{}
	c := NewDocsPruneCmd(func(*cobra.Command) DocsClient { return fake })
	c.SetArgs([]string{"--ecosystem", "python", "--version", "3.9.0", "--confirm"})
	c.SetIn(strings.NewReader("y\n"))
	var buf bytes.Buffer
	c.SetOut(&buf)
	if err := c.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !fake.prunePreviewCalled || !fake.pruneCalled {
		t.Errorf("expected preview+delete; preview=%v delete=%v",
			fake.prunePreviewCalled, fake.pruneCalled)
	}
}

func TestNewDocsSourcesCmd_RunE_HintWhenNoList(t *testing.T) {
	t.Parallel()
	fake := &fakeDocsClient{}
	var buf bytes.Buffer
	c := NewDocsSourcesCmd(func(*cobra.Command) DocsClient { return fake })
	c.SetArgs([]string{})
	c.SetOut(&buf)
	if err := c.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "--list") {
		t.Errorf("expected hint about --list; got %q", buf.String())
	}
	if fake.sourcesCalled {
		t.Error("daemon called despite --list missing")
	}
}

func TestNewDocsStatusCmd_RunE_CallsRunDocsStatus(t *testing.T) {
	t.Parallel()
	fake := &fakeDocsClient{}
	var buf bytes.Buffer
	c := NewDocsStatusCmd(func(*cobra.Command) DocsClient { return fake })
	c.SetArgs([]string{})
	c.SetOut(&buf)
	if err := c.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "ECOSYSTEM") {
		t.Errorf("status output missing header; got %q", buf.String())
	}
}

func TestNewDocsRouterRetrainCmd_RunE_CallsRunDocsRouterRetrain(t *testing.T) {
	t.Parallel()
	fake := &fakeDocsClient{}
	var buf bytes.Buffer
	c := NewDocsRouterRetrainCmd(func(*cobra.Command) DocsClient { return fake })
	c.SetArgs([]string{})
	c.SetOut(&buf)
	if err := c.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "classifier.bin") {
		t.Errorf("router-retrain output missing checkpoint; got %q", buf.String())
	}
}
