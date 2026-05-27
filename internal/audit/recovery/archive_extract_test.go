// SPDX-License-Identifier: MIT
package recovery

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTarGzipExtractorExtractsRegularFiles(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "archive.tar.gz")
	writeTarGzip(t, archive, map[string]string{
		"tiles/00/entry": "leaf",
	})

	dst := filepath.Join(dir, "out")
	if err := (TarGzipExtractor{}).ExtractColdArchive(context.Background(), archive, dst); err != nil {
		t.Fatalf("ExtractColdArchive: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "tiles", "00", "entry"))
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	if string(got) != "leaf" {
		t.Fatalf("extracted content = %q, want leaf", string(got))
	}
}

func TestTarGzipExtractorRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "archive.tar.gz")
	writeTarGzip(t, archive, map[string]string{
		"../escape": "bad",
	})

	dst := filepath.Join(dir, "out")
	err := (TarGzipExtractor{}).ExtractColdArchive(context.Background(), archive, dst)
	if err == nil {
		t.Fatal("expected traversal archive to be rejected")
	}
	if !strings.Contains(err.Error(), "unsafe archive path") {
		t.Fatalf("err = %v, want unsafe archive path", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "escape")); !os.IsNotExist(statErr) {
		t.Fatalf("escape file stat err = %v, want not exist", statErr)
	}
}

func TestTarGzipExtractorRejectsDirectoryEntries(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "archive.tar.gz")
	var body bytes.Buffer
	gz := gzip.NewWriter(&body)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "tiles", Mode: 0o700, Typeflag: tar.TypeDir}); err != nil {
		t.Fatalf("write dir header: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := os.WriteFile(archive, body.Bytes(), 0o600); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	err := (TarGzipExtractor{}).ExtractColdArchive(context.Background(), archive, filepath.Join(dir, "out"))
	if err == nil {
		t.Fatal("expected directory entry to be rejected")
	}
	if !strings.Contains(err.Error(), "unsupported archive entry") {
		t.Fatalf("err = %v, want unsupported archive entry", err)
	}
}

func TestTarGzipExtractorRejectsMissingPaths(t *testing.T) {
	if err := (TarGzipExtractor{}).ExtractColdArchive(context.Background(), "", t.TempDir()); err == nil {
		t.Fatal("expected empty archive path to be rejected")
	}
	if err := (TarGzipExtractor{}).ExtractColdArchive(context.Background(), filepath.Join(t.TempDir(), "x.tar.gz"), ""); err == nil {
		t.Fatal("expected empty destination to be rejected")
	}
}

func TestTarGzipExtractorReportsDestinationMkdirFailure(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("file-not-dir"), 0o600); err != nil {
		t.Fatalf("seed blocker: %v", err)
	}
	err := (TarGzipExtractor{}).ExtractColdArchive(context.Background(), filepath.Join(dir, "archive.tar.gz"), filepath.Join(blocker, "out"))
	if err == nil {
		t.Fatal("expected destination mkdir failure")
	}
	if !strings.Contains(err.Error(), "mkdir destination") {
		t.Fatalf("err = %v, want mkdir destination", err)
	}
}

func TestTarGzipExtractorReportsOpenArchiveFailure(t *testing.T) {
	err := (TarGzipExtractor{}).ExtractColdArchive(context.Background(), filepath.Join(t.TempDir(), "missing.tar.gz"), t.TempDir())
	if err == nil {
		t.Fatal("expected missing archive error")
	}
	if !strings.Contains(err.Error(), "open archive") {
		t.Fatalf("err = %v, want open archive", err)
	}
}

func TestTarGzipExtractorReportsInvalidGzip(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "not-gzip.tar.gz")
	if err := os.WriteFile(archive, []byte("not gzip"), 0o600); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	err := (TarGzipExtractor{}).ExtractColdArchive(context.Background(), archive, filepath.Join(dir, "out"))
	if err == nil {
		t.Fatal("expected gzip reader error")
	}
	if !strings.Contains(err.Error(), "gzip reader") {
		t.Fatalf("err = %v, want gzip reader", err)
	}
}

func TestTarGzipExtractorHonorsCanceledContext(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "archive.tar.gz")
	writeTarGzip(t, archive, map[string]string{"tile": "data"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := (TarGzipExtractor{}).ExtractColdArchive(ctx, archive, filepath.Join(dir, "out"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestTarGzipExtractorReportsCorruptTar(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "corrupt.tar.gz")
	writeRawGzip(t, archive, []byte("not a tar stream"))

	err := (TarGzipExtractor{}).ExtractColdArchive(context.Background(), archive, filepath.Join(dir, "out"))
	if err == nil {
		t.Fatal("expected tar reader error")
	}
	if !strings.Contains(err.Error(), "tar next") {
		t.Fatalf("err = %v, want tar next", err)
	}
}

func TestTarGzipExtractorDefaultsZeroMode(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "zero-mode.tar.gz")
	writeTarGzipHeaders(t, archive, []tar.Header{{Name: "tile", Typeflag: tar.TypeReg, Size: int64(len("data"))}}, []string{"data"})

	dst := filepath.Join(dir, "out")
	if err := (TarGzipExtractor{}).ExtractColdArchive(context.Background(), archive, dst); err != nil {
		t.Fatalf("ExtractColdArchive: %v", err)
	}
	info, err := os.Stat(filepath.Join(dst, "tile"))
	if err != nil {
		t.Fatalf("stat extracted tile: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 0600 default", got)
	}
}

func TestTarGzipExtractorReportsParentCollision(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "collision.tar.gz")
	writeTarGzipHeaders(t, archive, []tar.Header{
		{Name: "tiles", Mode: 0o600, Size: int64(len("file blocks child directory")), Typeflag: tar.TypeReg},
		{Name: "tiles/leaf", Mode: 0o600, Size: int64(len("leaf")), Typeflag: tar.TypeReg},
	}, []string{"file blocks child directory", "leaf"})

	err := (TarGzipExtractor{}).ExtractColdArchive(context.Background(), archive, filepath.Join(dir, "out"))
	if err == nil {
		t.Fatal("expected parent collision failure")
	}
	if !strings.Contains(err.Error(), "mkdir entry parent") {
		t.Fatalf("err = %v, want mkdir entry parent", err)
	}
}

func TestTarGzipExtractorReportsCreateEntryFailure(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "archive.tar.gz")
	writeTarGzip(t, archive, map[string]string{"tile": "data"})
	dst := filepath.Join(dir, "out")
	if err := os.MkdirAll(filepath.Join(dst, "tile"), 0o700); err != nil {
		t.Fatalf("seed directory at target path: %v", err)
	}

	err := (TarGzipExtractor{}).ExtractColdArchive(context.Background(), archive, dst)
	if err == nil {
		t.Fatal("expected create-entry failure")
	}
	if !strings.Contains(err.Error(), "create entry") {
		t.Fatalf("err = %v, want create entry", err)
	}
}

func TestTarGzipExtractorReportsTruncatedEntryWrite(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "truncated.tar.gz")
	writeTruncatedTarGzip(t, archive)

	err := (TarGzipExtractor{}).ExtractColdArchive(context.Background(), archive, filepath.Join(dir, "out"))
	if err == nil {
		t.Fatal("expected write-entry failure")
	}
	if !strings.Contains(err.Error(), "write entry") {
		t.Fatalf("err = %v, want write entry", err)
	}
}

func writeTarGzip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	var body bytes.Buffer
	gz := gzip.NewWriter(&body)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatalf("write header %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("write content %s: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := os.WriteFile(path, body.Bytes(), 0o600); err != nil {
		t.Fatalf("write archive: %v", err)
	}
}

func writeTarGzipHeaders(t *testing.T, path string, headers []tar.Header, contents []string) {
	t.Helper()
	if len(headers) != len(contents) {
		t.Fatalf("headers/content mismatch: %d != %d", len(headers), len(contents))
	}
	var body bytes.Buffer
	gz := gzip.NewWriter(&body)
	tw := tar.NewWriter(gz)
	for i := range headers {
		h := headers[i]
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatalf("write header %s: %v", h.Name, err)
		}
		if _, err := tw.Write([]byte(contents[i])); err != nil {
			t.Fatalf("write content %s: %v", h.Name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := os.WriteFile(path, body.Bytes(), 0o600); err != nil {
		t.Fatalf("write archive: %v", err)
	}
}

func writeRawGzip(t *testing.T, path string, payload []byte) {
	t.Helper()
	var body bytes.Buffer
	gz := gzip.NewWriter(&body)
	if _, err := gz.Write(payload); err != nil {
		t.Fatalf("write gzip payload: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := os.WriteFile(path, body.Bytes(), 0o600); err != nil {
		t.Fatalf("write archive: %v", err)
	}
}

func writeTruncatedTarGzip(t *testing.T, path string) {
	t.Helper()
	var tarBody bytes.Buffer
	tw := tar.NewWriter(&tarBody)
	if err := tw.WriteHeader(&tar.Header{Name: "truncated", Mode: 0o600, Size: int64(len("longer-than-body")), Typeflag: tar.TypeReg}); err != nil {
		t.Fatalf("write truncated header: %v", err)
	}
	if _, err := tw.Write([]byte("short")); err != nil {
		t.Fatalf("write short tar payload: %v", err)
	}
	writeRawGzip(t, path, tarBody.Bytes())
}
