// SPDX-License-Identifier: MIT
package recovery

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type TarGzipExtractor struct{}

func (TarGzipExtractor) ExtractColdArchive(ctx context.Context, archivePath, dstDir string) error {
	if archivePath == "" || dstDir == "" {
		return fmt.Errorf("recovery: archive path and destination required")
	}
	if err := os.MkdirAll(dstDir, 0o700); err != nil {
		return fmt.Errorf("mkdir destination: %w", err)
	}
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	cleanRoot, err := filepath.Abs(dstDir)
	if err != nil {
		return fmt.Errorf("abs destination: %w", err)
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("tar next: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			return fmt.Errorf("unsupported archive entry %q type %d", hdr.Name, hdr.Typeflag)
		}
		name := filepath.Clean(hdr.Name)
		if filepath.IsAbs(name) || strings.HasPrefix(name, ".."+string(filepath.Separator)) || name == ".." {
			return fmt.Errorf("unsafe archive path %q", hdr.Name)
		}
		dst := filepath.Join(cleanRoot, name)
		absDst, err := filepath.Abs(dst)
		if err != nil {
			return fmt.Errorf("abs entry: %w", err)
		}
		if absDst != cleanRoot && !strings.HasPrefix(absDst, cleanRoot+string(filepath.Separator)) {
			return fmt.Errorf("archive path escapes destination %q", hdr.Name)
		}
		if err := os.MkdirAll(filepath.Dir(absDst), 0o700); err != nil {
			return fmt.Errorf("mkdir entry parent: %w", err)
		}
		mode := hdr.FileInfo().Mode().Perm()
		if mode == 0 {
			mode = 0o600
		}
		out, err := os.OpenFile(absDst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			return fmt.Errorf("create entry: %w", err)
		}
		_, copyErr := io.Copy(out, tr)
		closeErr := out.Close()
		if copyErr != nil {
			return fmt.Errorf("write entry: %w", copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close entry: %w", closeErr)
		}
	}
}
