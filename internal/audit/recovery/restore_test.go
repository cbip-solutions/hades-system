package recovery

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type stubLitestreamRestorer struct {
	called atomic.Int32
	err    error
}

func (s *stubLitestreamRestorer) RestoreFromS3(ctx context.Context, projectID string, fromTS time.Time, outDir string) error {
	s.called.Add(1)
	return s.err
}

type stubS3Downloader struct {
	called atomic.Int32
	err    error
}

func (s *stubS3Downloader) DownloadColdArchive(ctx context.Context, projectID, partitionID, dst string) error {
	s.called.Add(1)
	return s.err
}

type stubSealStoreFull struct {
	rowsForList []SealMeta
	rowByID     map[string]ColdArchiveMeta
}

func (s *stubSealStoreFull) ListSeals(ctx context.Context, projectID string) ([]SealMeta, error) {
	return s.rowsForList, nil
}
func (s *stubSealStoreFull) ColdArchiveMetaFor(ctx context.Context, projectID, partitionID string) (ColdArchiveMeta, error) {
	r, ok := s.rowByID[partitionID]
	if !ok {
		return ColdArchiveMeta{}, errors.New("no row")
	}
	return r, nil
}

type stubVerifier struct {
	res *VerifyResult
	err error
}

func (s *stubVerifier) VerifyChain(ctx context.Context, projectID, stagingDir string) (*VerifyResult, error) {
	return s.res, s.err
}

type stubExtractor struct {
	called atomic.Int32
	err    error
}

func (s *stubExtractor) ExtractColdArchive(ctx context.Context, archivePath, dstDir string) error {
	s.called.Add(1)
	return s.err
}

func TestRestoreHappyPathOperatorApproves(t *testing.T) {
	dir := t.TempDir()
	rls := &stubLitestreamRestorer{}
	dl := &stubS3Downloader{}
	seals := &stubSealStoreFull{
		rowsForList: []SealMeta{{PartitionID: "2026_05"}},
		rowByID: map[string]ColdArchiveMeta{
			"2026_05": {URL: "s3://x/2026_05.tar.gz", ContentHash: "abc"},
		},
	}
	verifier := &stubVerifier{res: &VerifyResult{Clean: true, RecordsChecked: 100, PartitionSealsChecked: 1}}

	stdin := strings.NewReader("y\n")
	var stdout bytes.Buffer
	extractor := &stubExtractor{}
	r := NewRestorer(rls, dl, verifier, seals, dir, WithColdArchiveExtractor(extractor))
	r.contentHashFor = func(staging, partitionID string) (string, error) { return "abc", nil }
	r.promoteFn = func(staging, projectID string) error { return nil }

	res, err := r.Restore(context.Background(), "zen-swarm", time.Unix(1700000000, 0).UTC(), stdin, &stdout)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if !res.Approved {
		t.Errorf("Approved = false")
	}
	if !res.Promoted {
		t.Errorf("Promoted = false")
	}
	if rls.called.Load() != 1 {
		t.Errorf("litestream restore called %d times", rls.called.Load())
	}
	if dl.called.Load() != 1 {
		t.Errorf("s3 cp called %d times", dl.called.Load())
	}
	if extractor.called.Load() != 1 {
		t.Errorf("extractor called %d times", extractor.called.Load())
	}
	out := stdout.String()
	if !strings.Contains(out, "Recovery plan") {
		t.Errorf("missing recovery plan; out = %s", out)
	}
}

func TestRestoreOperatorDeclines(t *testing.T) {
	dir := t.TempDir()
	r := NewRestorer(
		&stubLitestreamRestorer{},
		&stubS3Downloader{},
		&stubVerifier{res: &VerifyResult{Clean: true}},
		&stubSealStoreFull{rowsForList: []SealMeta{{PartitionID: "2026_05"}}, rowByID: map[string]ColdArchiveMeta{"2026_05": {ContentHash: "abc"}}},
		dir,
		WithColdArchiveExtractor(&stubExtractor{}),
	)
	r.contentHashFor = func(staging, partitionID string) (string, error) { return "abc", nil }
	r.extractor = &stubExtractor{}
	var promoted atomic.Bool
	r.promoteFn = func(staging, projectID string) error {
		promoted.Store(true)
		return nil
	}

	stdin := strings.NewReader("n\n")
	var stdout bytes.Buffer
	res, err := r.Restore(context.Background(), "zen-swarm", time.Unix(1700000000, 0).UTC(), stdin, &stdout)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if res.Approved {
		t.Errorf("Approved = true")
	}
	if res.Promoted {
		t.Errorf("Promoted = true on decline")
	}
	if promoted.Load() {
		t.Errorf("promoteFn called on decline")
	}
}

func TestRestoreAbortsOnContentHashMismatch(t *testing.T) {
	dir := t.TempDir()
	r := NewRestorer(
		&stubLitestreamRestorer{},
		&stubS3Downloader{},
		&stubVerifier{res: &VerifyResult{Clean: true}},
		&stubSealStoreFull{rowsForList: []SealMeta{{PartitionID: "2026_05"}}, rowByID: map[string]ColdArchiveMeta{"2026_05": {ContentHash: "expected"}}},
		dir,
	)
	r.contentHashFor = func(staging, partitionID string) (string, error) { return "DIFFERENT", nil }

	stdin := strings.NewReader("y\n")
	var stdout bytes.Buffer
	_, err := r.Restore(context.Background(), "zen-swarm", time.Unix(1700000000, 0).UTC(), stdin, &stdout)
	if err == nil {
		t.Fatal("expected error on content_hash mismatch")
	}
	if !strings.Contains(err.Error(), "content hash") && !strings.Contains(err.Error(), "hash mismatch") {
		t.Errorf("err = %v, want content-hash mismatch", err)
	}
}

func TestRestoreAbortsOnVerifyChainFail(t *testing.T) {
	dir := t.TempDir()
	r := NewRestorer(
		&stubLitestreamRestorer{},
		&stubS3Downloader{},
		&stubVerifier{res: &VerifyResult{Clean: false, FirstTamperPath: PathLocalChainMismatch, FirstTamperRecordID: 99}},
		&stubSealStoreFull{rowsForList: []SealMeta{{PartitionID: "2026_05"}}, rowByID: map[string]ColdArchiveMeta{"2026_05": {ContentHash: "abc"}}},
		dir,
	)
	r.contentHashFor = func(staging, partitionID string) (string, error) { return "abc", nil }
	r.extractor = &stubExtractor{}

	stdin := strings.NewReader("y\n")
	var stdout bytes.Buffer
	_, err := r.Restore(context.Background(), "zen-swarm", time.Unix(1700000000, 0).UTC(), stdin, &stdout)
	if err == nil {
		t.Fatal("expected error when verify-chain --strict fails")
	}
	if !strings.Contains(err.Error(), "verify") {
		t.Errorf("err = %v, want verify-fail message", err)
	}
}

func TestRestorePropagatesLitestreamFailure(t *testing.T) {
	dir := t.TempDir()
	r := NewRestorer(
		&stubLitestreamRestorer{err: errors.New("aws unreachable")},
		&stubS3Downloader{},
		&stubVerifier{res: &VerifyResult{Clean: true}},
		&stubSealStoreFull{rowsForList: []SealMeta{}, rowByID: map[string]ColdArchiveMeta{}},
		dir,
	)
	stdin := strings.NewReader("")
	var stdout bytes.Buffer
	_, err := r.Restore(context.Background(), "zen-swarm", time.Unix(1700000000, 0).UTC(), stdin, &stdout)
	if err == nil {
		t.Fatal("expected litestream error to propagate")
	}
	if !strings.Contains(err.Error(), "litestream") {
		t.Errorf("err = %v", err)
	}
}

func TestRestoreRejectsEmptyProjectID(t *testing.T) {
	dir := t.TempDir()
	r := NewRestorer(nil, nil, nil, nil, dir)
	stdin := strings.NewReader("y\n")
	var stdout bytes.Buffer
	_, err := r.Restore(context.Background(), "", time.Now(), stdin, &stdout)
	if err == nil {
		t.Error("expected error on empty project_id")
	}
}
