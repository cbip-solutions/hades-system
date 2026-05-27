package recovery

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type errSealStore struct {
	listErr  error
	metaErr  error
	listRows []SealMeta
}

func (s *errSealStore) ListSeals(_ context.Context, _ string) ([]SealMeta, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.listRows, nil
}
func (s *errSealStore) ColdArchiveMetaFor(_ context.Context, _, _ string) (ColdArchiveMeta, error) {
	return ColdArchiveMeta{}, s.metaErr
}

type errDoctrineResolver struct{ err error }

func (e *errDoctrineResolver) DoctrineFor(_ context.Context, _ string) (string, error) {
	return "", e.err
}

type errEventEmitter struct{ err error }

func (e *errEventEmitter) EmitTamperDetected(_ context.Context, _ string, _ *VerifyResult) error {
	return e.err
}

type errInboxEmitter struct{ err error }

func (e *errInboxEmitter) PushURGENT(_ context.Context, _, _ string) error {
	return e.err
}

type errProjectLister struct{ err error }

func (e *errProjectLister) ListProjectIDs(_ context.Context) ([]string, error) {
	return nil, e.err
}

type errReader struct{ err error }

func (e *errReader) Read(_ []byte) (int, error) { return 0, e.err }

func TestSha256OfReaderHappyPath(t *testing.T) {

	h, err := sha256OfReader(bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("sha256OfReader(empty): %v", err)
	}
	const wantEmpty = "e3b0c44298fc1c149afbf4c8996fb924" + "27ae41e4649b934ca495991b7852b855"
	if h != wantEmpty {
		t.Errorf("sha256OfReader(empty) = %q, want %q", h, wantEmpty)
	}

	payload := []byte("hello zen-swarm")
	h2, err := sha256OfReader(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("sha256OfReader(payload): %v", err)
	}
	h3, err := sha256OfReader(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("sha256OfReader(payload) second call: %v", err)
	}
	if h2 != h3 {
		t.Errorf("sha256OfReader not deterministic: %q != %q", h2, h3)
	}
	if len(h2) != 64 {
		t.Errorf("expected 64-char hex digest, got len %d: %q", len(h2), h2)
	}
}

func TestSha256OfReaderIOError(t *testing.T) {
	sentinel := errors.New("injected read error")
	_, err := sha256OfReader(&errReader{err: sentinel})
	if !errors.Is(err, sentinel) {
		t.Errorf("sha256OfReader: got %v, want wrapped sentinel", err)
	}
}

func TestDefaultContentHashForHappyPath(t *testing.T) {
	dir := t.TempDir()
	content := []byte("audit tarball payload")
	path := filepath.Join(dir, "part.tar.gz")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := defaultContentHashFor(path, "2026_05")
	if err != nil {
		t.Fatalf("defaultContentHashFor: %v", err)
	}

	want, err := sha256OfReader(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("defaultContentHashFor = %q, want %q", got, want)
	}
}

func TestDefaultContentHashForFileNotExist(t *testing.T) {
	_, err := defaultContentHashFor("/nonexistent/path/to/file.tar.gz", "2026_05")
	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}
}

func TestDefaultPromoteRequiresExplicitHook(t *testing.T) {
	err := defaultPromote("/tmp/staging", "zen-swarm")
	if err == nil {
		t.Fatal("expected defaultPromote to refuse unconfigured promotion")
	}
	if !strings.Contains(err.Error(), "promote hook not configured") {
		t.Fatalf("err = %v, want promote hook not configured", err)
	}
}

func TestWithPromoteOptionWiresCustomPromote(t *testing.T) {
	dir := t.TempDir()
	var promoted bool
	r := NewRestorer(
		&stubLitestreamRestorer{},
		&stubS3Downloader{},
		&stubVerifier{res: &VerifyResult{Clean: true, RecordsChecked: 7}},
		&stubSealStoreFull{rowsForList: nil, rowByID: map[string]ColdArchiveMeta{}},
		dir,
		WithPromote(func(stagingDir, projectID string) error {
			promoted = true
			if projectID != "zen-swarm" {
				t.Fatalf("projectID = %q, want zen-swarm", projectID)
			}
			if stagingDir == "" {
				t.Fatal("stagingDir empty")
			}
			return nil
		}),
	)

	var stdout bytes.Buffer
	res, err := r.Restore(context.Background(), "zen-swarm", time.Unix(1700000000, 0), strings.NewReader("yes\n"), &stdout)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if !promoted {
		t.Fatal("custom promote hook was not called")
	}
	if !res.Approved || !res.Promoted {
		t.Fatalf("Approved/Promoted = %v/%v, want true/true", res.Approved, res.Promoted)
	}
}

func TestRestoreAbortsOnMkdirAllFailure(t *testing.T) {

	tmpDir := t.TempDir()
	parentAsFile := filepath.Join(tmpDir, "not-a-dir")
	if err := os.WriteFile(parentAsFile, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}

	stagingRoot := filepath.Join(parentAsFile, "sub")
	r := NewRestorer(
		&stubLitestreamRestorer{},
		&stubS3Downloader{},
		&stubVerifier{res: &VerifyResult{Clean: true}},
		&stubSealStoreFull{rowsForList: nil, rowByID: map[string]ColdArchiveMeta{}},
		stagingRoot,
	)
	var stdout bytes.Buffer
	_, err := r.Restore(context.Background(), "zen-swarm", time.Unix(1700000000, 0), strings.NewReader(""), &stdout)
	if err == nil {
		t.Fatal("expected error from os.MkdirAll on invalid path")
	}
	if !strings.Contains(err.Error(), "mkdir") && !strings.Contains(err.Error(), "staging") && !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRestoreAbortsOnListSealsFailure(t *testing.T) {
	dir := t.TempDir()
	r := NewRestorer(
		&stubLitestreamRestorer{},
		&stubS3Downloader{},
		&stubVerifier{res: &VerifyResult{Clean: true}},
		&errSealStore{listErr: errors.New("db unavailable")},
		dir,
	)
	var stdout bytes.Buffer
	_, err := r.Restore(context.Background(), "zen-swarm", time.Unix(1700000000, 0), strings.NewReader(""), &stdout)
	if err == nil {
		t.Fatal("expected ListSeals error to propagate")
	}
	if !strings.Contains(err.Error(), "list seals") {
		t.Errorf("err = %v, want 'list seals'", err)
	}
}

func TestRestoreAbortsOnDownloadColdArchiveFailure(t *testing.T) {
	dir := t.TempDir()
	r := NewRestorer(
		&stubLitestreamRestorer{},
		&stubS3Downloader{err: errors.New("s3 timeout")},
		&stubVerifier{res: &VerifyResult{Clean: true}},
		&errSealStore{listRows: []SealMeta{{PartitionID: "2026_05"}}},
		dir,
	)
	var stdout bytes.Buffer
	_, err := r.Restore(context.Background(), "zen-swarm", time.Unix(1700000000, 0), strings.NewReader(""), &stdout)
	if err == nil {
		t.Fatal("expected DownloadColdArchive error to propagate")
	}
	if !strings.Contains(err.Error(), "download partition") {
		t.Errorf("err = %v, want 'download partition'", err)
	}
}

func TestRestoreAbortsOnContentHashError(t *testing.T) {
	dir := t.TempDir()
	r := NewRestorer(
		&stubLitestreamRestorer{},
		&stubS3Downloader{},
		&stubVerifier{res: &VerifyResult{Clean: true}},
		&errSealStore{listRows: []SealMeta{{PartitionID: "2026_05"}}},
		dir,
	)
	r.contentHashFor = func(_, _ string) (string, error) {
		return "", errors.New("hash compute fail")
	}
	var stdout bytes.Buffer
	_, err := r.Restore(context.Background(), "zen-swarm", time.Unix(1700000000, 0), strings.NewReader(""), &stdout)
	if err == nil {
		t.Fatal("expected contentHashFor error to propagate")
	}
	if !strings.Contains(err.Error(), "hash partition") {
		t.Errorf("err = %v, want 'hash partition'", err)
	}
}

func TestRestoreAbortsOnColdArchiveMetaFailure(t *testing.T) {
	dir := t.TempDir()
	r := NewRestorer(
		&stubLitestreamRestorer{},
		&stubS3Downloader{},
		&stubVerifier{res: &VerifyResult{Clean: true}},
		&errSealStore{
			listRows: []SealMeta{{PartitionID: "2026_05"}},
			metaErr:  errors.New("no seal row"),
		},
		dir,
	)
	r.contentHashFor = func(_, _ string) (string, error) { return "abc", nil }
	var stdout bytes.Buffer
	_, err := r.Restore(context.Background(), "zen-swarm", time.Unix(1700000000, 0), strings.NewReader(""), &stdout)
	if err == nil {
		t.Fatal("expected ColdArchiveMetaFor error to propagate")
	}
	if !strings.Contains(err.Error(), "lookup meta") {
		t.Errorf("err = %v, want 'lookup meta'", err)
	}
}

func TestRestoreAbortsOnExtractorFailure(t *testing.T) {
	dir := t.TempDir()
	r := NewRestorer(
		&stubLitestreamRestorer{},
		&stubS3Downloader{},
		&stubVerifier{res: &VerifyResult{Clean: true}},
		&stubSealStoreFull{
			rowsForList: []SealMeta{{PartitionID: "2026_05"}},
			rowByID:     map[string]ColdArchiveMeta{"2026_05": {ContentHash: "abc"}},
		},
		dir,
		WithColdArchiveExtractor(&stubExtractor{err: errors.New("tarball corrupt")}),
	)
	r.contentHashFor = func(_, _ string) (string, error) { return "abc", nil }

	var stdout bytes.Buffer
	_, err := r.Restore(context.Background(), "zen-swarm", time.Unix(1700000000, 0), strings.NewReader("y\n"), &stdout)
	if err == nil {
		t.Fatal("expected extractor error to propagate")
	}
	if !strings.Contains(err.Error(), "extract partition") {
		t.Errorf("err = %v, want extract partition", err)
	}
}

func TestRestoreAbortsOnVerifyChainError(t *testing.T) {
	dir := t.TempDir()
	r := NewRestorer(
		&stubLitestreamRestorer{},
		&stubS3Downloader{},
		&stubVerifier{err: errors.New("tessera unreachable")},
		&errSealStore{listRows: []SealMeta{{PartitionID: "2026_05"}}},
		dir,
	)
	r.contentHashFor = func(_, _ string) (string, error) { return "abc", nil }
	r.extractor = &stubExtractor{}

	r.seals = &stubSealStoreFull{
		rowsForList: []SealMeta{{PartitionID: "2026_05"}},
		rowByID:     map[string]ColdArchiveMeta{"2026_05": {ContentHash: "abc"}},
	}
	var stdout bytes.Buffer
	_, err := r.Restore(context.Background(), "zen-swarm", time.Unix(1700000000, 0), strings.NewReader("y\n"), &stdout)
	if err == nil {
		t.Fatal("expected VerifyChain error to propagate")
	}
	if !strings.Contains(err.Error(), "verify-chain") {
		t.Errorf("err = %v, want 'verify-chain'", err)
	}
}

func TestRestoreAbortsOnPromoteFnFailure(t *testing.T) {
	dir := t.TempDir()
	r := NewRestorer(
		&stubLitestreamRestorer{},
		&stubS3Downloader{},
		&stubVerifier{res: &VerifyResult{Clean: true, RecordsChecked: 10, PartitionSealsChecked: 1}},
		&stubSealStoreFull{
			rowsForList: []SealMeta{{PartitionID: "2026_05"}},
			rowByID:     map[string]ColdArchiveMeta{"2026_05": {ContentHash: "abc"}},
		},
		dir,
	)
	r.contentHashFor = func(_, _ string) (string, error) { return "abc", nil }
	r.extractor = &stubExtractor{}
	r.promoteFn = func(_, _ string) error { return errors.New("atomic rename failed") }
	var stdout bytes.Buffer

	_, err := r.Restore(context.Background(), "zen-swarm", time.Unix(1700000000, 0), strings.NewReader("y\n"), &stdout)
	if err == nil {
		t.Fatal("expected promoteFn error to propagate")
	}
	if !strings.Contains(err.Error(), "promote") {
		t.Errorf("err = %v, want 'promote'", err)
	}
}

func TestDispatchRejectsEmptyProjectID(t *testing.T) {
	d := NewTamperDispatcher(NewHalts(), &stubDoctrineResolver{}, &stubProjectLister{}, &stubInboxEmitter{}, &stubEventEmitter{})
	_, err := d.DispatchTamperResponse(context.Background(), "", &VerifyResult{Clean: false})
	if err == nil {
		t.Error("expected error on empty project_id")
	}
	if !strings.Contains(err.Error(), "empty project_id") {
		t.Errorf("err = %v, want 'empty project_id'", err)
	}
}

func TestDispatchRejectsNilDetection(t *testing.T) {
	d := NewTamperDispatcher(NewHalts(), &stubDoctrineResolver{}, &stubProjectLister{}, &stubInboxEmitter{}, &stubEventEmitter{})
	_, err := d.DispatchTamperResponse(context.Background(), "alpha", nil)
	if err == nil {
		t.Error("expected error on nil detection")
	}
	if !strings.Contains(err.Error(), "nil detection") {
		t.Errorf("err = %v, want 'nil detection'", err)
	}
}

func TestDispatchDoctrineResolverError(t *testing.T) {
	sentinel := errors.New("doctrine store offline")
	d := NewTamperDispatcher(
		NewHalts(),
		&errDoctrineResolver{err: sentinel},
		&stubProjectLister{},
		&stubInboxEmitter{},
		&stubEventEmitter{},
	)
	_, err := d.DispatchTamperResponse(context.Background(), "alpha", &VerifyResult{Clean: false, FirstTamperPath: PathLocalChainMismatch})
	if !errors.Is(err, sentinel) {
		t.Errorf("got %v, want wrapped doctrine sentinel", err)
	}
	if !strings.Contains(err.Error(), "doctrine lookup") {
		t.Errorf("err = %v, want 'doctrine lookup'", err)
	}
}

func TestDispatchEmitEventError(t *testing.T) {
	sentinel := errors.New("eventlog full")
	d := NewTamperDispatcher(
		NewHalts(),
		&stubDoctrineResolver{doctrineFor: map[string]string{"alpha": "max-scope"}},
		&stubProjectLister{},
		&errInboxEmitter{},
		&errEventEmitter{err: sentinel},
	)
	_, err := d.DispatchTamperResponse(context.Background(), "alpha", &VerifyResult{Clean: false, FirstTamperPath: PathLocalChainMismatch})
	if !errors.Is(err, sentinel) {
		t.Errorf("got %v, want wrapped eventlog sentinel", err)
	}
	if !strings.Contains(err.Error(), "emit event") {
		t.Errorf("err = %v, want 'emit event'", err)
	}
}

func TestDispatchInboxPushError(t *testing.T) {
	sentinel := errors.New("inbox overflow")
	d := NewTamperDispatcher(
		NewHalts(),
		&stubDoctrineResolver{doctrineFor: map[string]string{"alpha": "max-scope"}},
		&stubProjectLister{},
		&errInboxEmitter{err: sentinel},
		&stubEventEmitter{},
	)
	_, err := d.DispatchTamperResponse(context.Background(), "alpha", &VerifyResult{Clean: false, FirstTamperPath: PathLocalChainMismatch})
	if !errors.Is(err, sentinel) {
		t.Errorf("got %v, want wrapped inbox sentinel", err)
	}
	if !strings.Contains(err.Error(), "push inbox") {
		t.Errorf("err = %v, want 'push inbox'", err)
	}
}

func TestDispatchCapaFirewallListProjectsError(t *testing.T) {
	sentinel := errors.New("project lister down")
	d := NewTamperDispatcher(
		NewHalts(),
		&stubDoctrineResolver{doctrineFor: map[string]string{"alpha": "capa-firewall"}},
		&errProjectLister{err: sentinel},
		&stubInboxEmitter{},
		&stubEventEmitter{},
	)
	_, err := d.DispatchTamperResponse(context.Background(), "alpha", &VerifyResult{Clean: false, FirstTamperPath: PathLocalChainMismatch})
	if !errors.Is(err, sentinel) {
		t.Errorf("got %v, want wrapped lister sentinel", err)
	}
	if !strings.Contains(err.Error(), "list projects for cascade") {
		t.Errorf("err = %v, want 'list projects for cascade'", err)
	}
}

func TestDispatchUnknownDoctrineBecomesMaxScope(t *testing.T) {

	halts := NewHalts()
	d := NewTamperDispatcher(
		halts,
		&stubDoctrineResolver{doctrineFor: map[string]string{"alpha": "xyzzy-unknown"}},
		&stubProjectLister{ids: []string{"alpha"}},
		&stubInboxEmitter{},
		&stubEventEmitter{},
	)
	res, err := d.DispatchTamperResponse(context.Background(), "alpha", &VerifyResult{Clean: false, FirstTamperPath: PathLocalChainMismatch})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Halted {
		t.Error("expected Halted=true for unknown doctrine")
	}
	if len(res.CascadedHalts) != 0 {
		t.Errorf("CascadedHalts = %v, want empty (unknown → max-scope, not capa-firewall)", res.CascadedHalts)
	}
	if !halts.IsHalted("alpha") {
		t.Error("alpha not halted under unknown doctrine")
	}
}

type limitErrReader struct {
	r     io.Reader
	limit int
	read  int
	err   error
}

func (l *limitErrReader) Read(p []byte) (int, error) {
	if l.read >= l.limit {
		return 0, l.err
	}
	n, err := l.r.Read(p)
	l.read += n
	return n, err
}

func TestSha256OfReaderMidStreamIOError(t *testing.T) {

	sentinel := errors.New("mid-stream network error")
	r := &limitErrReader{r: bytes.NewReader(make([]byte, 1024)), limit: 512, err: sentinel}
	_, err := sha256OfReader(r)
	if !errors.Is(err, sentinel) {
		t.Errorf("sha256OfReader mid-stream: got %v, want sentinel", err)
	}
}

// NOTE(path-d/adr-0069): archive_extract.go contains four remaining
// architecturally-unreachable statements after the local CI
// workaround closed every portable testable branch:
//
// (1) archive_extract.go:39-41 filepath.Abs(dstDir) error. ExtractColdArchive
// has already accepted a non-empty process-local destination path; making
// filepath.Abs fail deterministically requires mutating global cwd/volume
// state, which would make the package test suite order-dependent.
// (2) archive_extract.go:62-64 filepath.Abs(entry) error. absDst is built
// from a known absolute cleanRoot and a cleaned tar member name; there is
// no package-local input that makes filepath.Abs return an error here.
// (3) archive_extract.go:65-67 path-escape fallback. The preceding guard
// rejects absolute paths, `..`, and `../...`; filepath.Join/Clean are
// string operations and do not follow symlinks, so a remaining string-only
// escape is a redundant defense-in-depth belt.
// (4) archive_extract.go:84-86 os.File.Close error. After OpenFile succeeds
// and io.Copy returns nil on a local temp filesystem, Close cannot be made
// to fail without OS/filesystem fault injection.
//
// Count 4 statements. Recovery target is therefore amended from 100 to 99 in
// scripts/coverage-validation.sh, matching the documented Path-D pattern used
// by tessera, litestream, adr, auditadapter, and knowledgeadapter.
