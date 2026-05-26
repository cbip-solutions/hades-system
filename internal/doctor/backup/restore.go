// SPDX-License-Identifier: MIT
// Package backup — restore.go ships the restoration path consumed by
// `zen doctor restore <ID>` CLI (internal/cli/doctor_restore.go).
//
// Conflict semantics: by default, RestoreFromManifest halts when any
// target file already exists (operator must explicitly --overwrite).
// This avoids silent destruction of post-backup state changes the
// operator may have made.
//
// Defense-in-depth: tar extraction rejects path-traversal entries via
// isPathWithin guard (catches ../etc/passwd entries even on hostile
// tarballs that bypass writeTarGz's symlink-skip on the source side).
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type RestoreOptions struct {
	TargetOverride string

	Overwrite bool
}

var ErrConflict = errors.New("backup: target file exists; re-run with --overwrite to force")

func IsConflictError(err error) bool {
	return errors.Is(err, ErrConflict)
}

func (b *Backuper) RestoreFromManifest(_ context.Context, m Manifest, opts RestoreOptions) error {
	target := opts.TargetOverride
	if target == "" {
		target = m.SourcePath
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		return fmt.Errorf("restore: mkdir %s: %w", target, err)
	}

	f, err := os.Open(m.TarballPath)
	if err != nil {
		return fmt.Errorf("restore: open tarball %s: %w", m.TarballPath, err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("restore: gunzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("restore: tar.Next: %w", err)
		}

		cleanName := filepath.Clean(hdr.Name)
		if strings.HasPrefix(cleanName, "..") || filepath.IsAbs(cleanName) {
			return fmt.Errorf("restore: tar entry %q escapes target (path traversal rejected)", hdr.Name)
		}
		targetPath := filepath.Join(target, cleanName)
		if !isPathWithin(targetPath, target) {
			return fmt.Errorf("restore: tar entry %q escapes target %s", hdr.Name, target)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, fs.FileMode(hdr.Mode)|0o100); err != nil {
				return fmt.Errorf("restore: mkdir %s: %w", targetPath, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if _, sterr := os.Stat(targetPath); sterr == nil && !opts.Overwrite {
				return fmt.Errorf("%w: %s", ErrConflict, targetPath)
			}
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
				return fmt.Errorf("restore: mkdir parent %s: %w", targetPath, err)
			}
			tmp := targetPath + ".restore-tmp"
			out, oerr := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fs.FileMode(hdr.Mode)&0o777)
			if oerr != nil {
				return fmt.Errorf("restore: open %s: %w", tmp, oerr)
			}
			if _, cerr := io.Copy(out, tr); cerr != nil {
				_ = out.Close()
				_ = os.Remove(tmp)
				return fmt.Errorf("restore: copy %s: %w", targetPath, cerr)
			}
			if cerr := out.Close(); cerr != nil {
				return fmt.Errorf("restore: close %s: %w", tmp, cerr)
			}

			_ = os.Chmod(tmp, fs.FileMode(hdr.Mode)&0o777)
			if err := os.Rename(tmp, targetPath); err != nil {
				return fmt.Errorf("restore: rename %s: %w", targetPath, err)
			}
			_ = os.Chmod(targetPath, fs.FileMode(hdr.Mode)&0o777)
		default:

			continue
		}
	}
	return nil
}

func isPathWithin(candidate, base string) bool {
	c := filepath.Clean(candidate)
	bs := filepath.Clean(base)
	rel, err := filepath.Rel(bs, c)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return false
	}
	return true
}
