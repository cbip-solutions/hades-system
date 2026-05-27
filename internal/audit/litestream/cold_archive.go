// SPDX-License-Identifier: MIT
package litestream

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type SealEvent struct {
	ProjectID   string
	PartitionID string
}

type PartitionSealUpdater interface {
	UpdateColdArchive(ctx context.Context, projectID, partitionID, coldArchiveURL, contentHash string) error
}

type ColdArchiveWorker struct {
	starter       ExecStarter
	sealStore     PartitionSealUpdater
	stagingDir    string
	endpoint      string
	tesseraDirFor func(projectID string) string
	doctrineFor   func(projectID string) string
	onFailure     func(projectID, partitionID string, err error)
}

func NewColdArchiveWorker(
	starter ExecStarter,
	sealStore PartitionSealUpdater,
	stagingDir string,
	endpoint string,
) *ColdArchiveWorker {
	return &ColdArchiveWorker{
		starter:    starter,
		sealStore:  sealStore,
		stagingDir: stagingDir,
		endpoint:   endpoint,
		tesseraDirFor: func(projectID string) string {
			home, _ := os.UserHomeDir()
			return filepath.Join(home, ".local", "share", "hades-system", "projects", projectID, "audit", "tessera")
		},
		doctrineFor: func(string) string { return "max-scope" },
		onFailure:   func(string, string, error) {},
	}
}

func (w *ColdArchiveWorker) Run(ctx context.Context, events <-chan SealEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			if err := w.processOne(ctx, ev); err != nil {
				w.onFailure(ev.ProjectID, ev.PartitionID, err)
			}
		}
	}
}

func (w *ColdArchiveWorker) processOne(ctx context.Context, ev SealEvent) error {
	if err := os.MkdirAll(w.stagingDir, 0o700); err != nil {
		return fmt.Errorf("cold-archive: mkdir staging: %w", err)
	}
	tessDir := w.tesseraDirFor(ev.ProjectID)
	body, hash, err := BuildColdArchiveTarball(tessDir, ev.PartitionID)
	if err != nil {
		return fmt.Errorf("cold-archive: build tarball: %w", err)
	}
	stagingPath := filepath.Join(w.stagingDir, ev.ProjectID+"-"+TarballNameFor(ev.PartitionID))
	if err := os.WriteFile(stagingPath, body, 0o600); err != nil {
		return fmt.Errorf("cold-archive: stage tarball: %w", err)
	}
	defer os.Remove(stagingPath)

	dst := S3KeyForColdArchive(ev.ProjectID, ev.PartitionID)
	args := []string{"s3", "cp", stagingPath, dst, "--quiet"}
	if w.endpoint != "" {
		args = append(args, "--endpoint-url", w.endpoint)
	}
	cmd := w.starter(ctx, "aws", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cold-archive: aws s3 cp: %w", err)
	}

	if strings.EqualFold(w.doctrineFor(ev.ProjectID), "capa-firewall") {
		bucket := "hades-system-audit-" + ev.ProjectID
		key := "cold-archive/" + TarballNameFor(ev.PartitionID)
		retentionArgs := []string{
			"s3api", "put-object-retention",
			"--bucket", bucket,
			"--key", key,
			"--retention", `Mode=COMPLIANCE,RetainUntilDate=2056-01-01T00:00:00Z`,
		}
		if w.endpoint != "" {
			retentionArgs = append(retentionArgs, "--endpoint-url", w.endpoint)
		}
		retCmd := w.starter(ctx, "aws", retentionArgs...)
		if err := retCmd.Run(); err != nil {
			return fmt.Errorf("cold-archive: object-lock retention: %w", err)
		}
	}

	if err := w.sealStore.UpdateColdArchive(ctx, ev.ProjectID, ev.PartitionID, dst, hash); err != nil {
		return fmt.Errorf("cold-archive: update seal store: %w", err)
	}
	return nil
}

func BuildColdArchiveTarball(tesseraDir, partitionID string) ([]byte, string, error) {
	if _, err := os.Stat(tesseraDir); err != nil {
		return nil, "", fmt.Errorf("tessera dir: %w", err)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	filesWritten := 0
	err := filepath.Walk(tesseraDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(tesseraDir, path)
		if err != nil {
			return err
		}
		hdr := &tar.Header{
			Name:    filepath.ToSlash(rel),
			Mode:    int64(info.Mode().Perm()),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := io.Copy(tw, f); err != nil {
			return err
		}
		filesWritten++
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	if err := tw.Close(); err != nil {
		return nil, "", err
	}
	if err := gz.Close(); err != nil {
		return nil, "", err
	}
	if filesWritten == 0 {
		return nil, "", errors.New("tessera dir empty (no files to archive)")
	}
	body := buf.Bytes()
	sum := sha256.Sum256(body)
	return body, hex.EncodeToString(sum[:]), nil
}

func TarballNameFor(partitionID string) string {
	return partitionID + ".tar.gz"
}

func S3KeyForColdArchive(projectID, partitionID string) string {
	return "s3://hades-system-audit-" + projectID + "/cold-archive/" + TarballNameFor(partitionID)
}
