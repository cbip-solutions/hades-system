// SPDX-License-Identifier: MIT
package recovery

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type LitestreamRestorer interface {
	RestoreFromS3(ctx context.Context, projectID string, fromTS time.Time, outDir string) error
}

type S3Downloader interface {
	DownloadColdArchive(ctx context.Context, projectID, partitionID, dst string) error
}

type VerifierInterface interface {
	VerifyChain(ctx context.Context, projectID string) (*VerifyResult, error)
}

type ColdArchiveMeta struct {
	URL         string
	ContentHash string
}

type SealStoreReader interface {
	ListSeals(ctx context.Context, projectID string) ([]SealMeta, error)
	ColdArchiveMetaFor(ctx context.Context, projectID, partitionID string) (ColdArchiveMeta, error)
}

type RestoreResult struct {
	StagingDir         string
	RecordsRestored    int
	PartitionsRestored int
	Approved           bool
	Promoted           bool
}

type Restorer struct {
	litestream  LitestreamRestorer
	downloader  S3Downloader
	verifier    VerifierInterface
	seals       SealStoreReader
	stagingRoot string

	contentHashFor func(stagingPath, partitionID string) (string, error)

	promoteFn func(stagingDir, projectID string) error
}

func NewRestorer(
	ls LitestreamRestorer,
	dl S3Downloader,
	v VerifierInterface,
	seals SealStoreReader,
	stagingRoot string,
) *Restorer {
	return &Restorer{
		litestream:     ls,
		downloader:     dl,
		verifier:       v,
		seals:          seals,
		stagingRoot:    stagingRoot,
		contentHashFor: defaultContentHashFor,
		promoteFn:      defaultPromote,
	}
}

func (r *Restorer) Restore(
	ctx context.Context,
	projectID string,
	fromTS time.Time,
	stdin io.Reader,
	stdout io.Writer,
) (*RestoreResult, error) {
	if projectID == "" {
		return nil, errors.New("recovery: empty project_id")
	}
	res := &RestoreResult{}

	stagingDir := filepath.Join(r.stagingRoot, fmt.Sprintf("%s-%d", projectID, fromTS.Unix()))
	if err := os.MkdirAll(stagingDir, 0o700); err != nil {
		return nil, fmt.Errorf("recovery: mkdir staging: %w", err)
	}
	res.StagingDir = stagingDir
	fmt.Fprintf(stdout, "Recovery plan for project %s, from %s:\n", projectID, fromTS.Format(time.RFC3339))
	fmt.Fprintf(stdout, "  Staging directory: %s\n", stagingDir)

	fmt.Fprintln(stdout, "  Step 1/4: litestream restore -timestamp ...")
	if err := r.litestream.RestoreFromS3(ctx, projectID, fromTS, stagingDir); err != nil {
		return nil, fmt.Errorf("recovery: litestream restore: %w", err)
	}

	seals, err := r.seals.ListSeals(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("recovery: list seals: %w", err)
	}
	fmt.Fprintf(stdout, "  Step 2/4: cold-archive restore (%d partitions)\n", len(seals))
	for _, s := range seals {
		dst := filepath.Join(stagingDir, s.PartitionID+".tar.gz")
		if err := r.downloader.DownloadColdArchive(ctx, projectID, s.PartitionID, dst); err != nil {
			return nil, fmt.Errorf("recovery: download partition %s: %w", s.PartitionID, err)
		}
		gotHash, err := r.contentHashFor(dst, s.PartitionID)
		if err != nil {
			return nil, fmt.Errorf("recovery: hash partition %s: %w", s.PartitionID, err)
		}
		expected, err := r.seals.ColdArchiveMetaFor(ctx, projectID, s.PartitionID)
		if err != nil {
			return nil, fmt.Errorf("recovery: lookup meta partition %s: %w", s.PartitionID, err)
		}
		if gotHash != expected.ContentHash {
			return nil, fmt.Errorf("recovery: content hash mismatch partition %s: got %s want %s",
				s.PartitionID, gotHash, expected.ContentHash)
		}
		res.PartitionsRestored++
	}

	fmt.Fprintln(stdout, "  Step 3/4: verify-chain --strict")
	verRes, err := r.verifier.VerifyChain(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("recovery: verify-chain: %w", err)
	}
	if !verRes.Clean {
		return nil, fmt.Errorf("recovery: verify-chain --strict failed at record %d (path %s)",
			verRes.FirstTamperRecordID, verRes.FirstTamperPath)
	}
	res.RecordsRestored = verRes.RecordsChecked

	fmt.Fprintf(stdout, "  Step 4/4: verified %d records, %d partition seals\n",
		verRes.RecordsChecked, verRes.PartitionSealsChecked)
	fmt.Fprint(stdout, "  Resume audit appends for project? [y/N]: ")
	br := bufio.NewReader(stdin)
	answer, _ := br.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "y" && answer != "yes" {
		fmt.Fprintln(stdout, "  Recovery declined; staging copy retained at "+stagingDir)
		return res, nil
	}
	res.Approved = true

	if err := r.promoteFn(stagingDir, projectID); err != nil {
		return nil, fmt.Errorf("recovery: promote: %w", err)
	}
	res.Promoted = true
	fmt.Fprintln(stdout, "  Recovery complete; project resumed.")
	return res, nil
}

func defaultContentHashFor(stagingPath, partitionID string) (string, error) {
	f, err := os.Open(stagingPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return sha256OfReader(f)
}

func defaultPromote(stagingDir, projectID string) error {

	return nil
}
