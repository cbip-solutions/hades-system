// SPDX-License-Identifier: MIT
package knowledge

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var dirEntryInfoFn = func(d fs.DirEntry) (fs.FileInfo, error) {
	return d.Info()
}

const MaxIndexableBytes = 5 * 1024 * 1024

var ErrFileTooLarge = errors.New("knowledge: file exceeds size cap")

type ScannerSource struct {
	Kind         FileType
	Root         string
	ProjectID    string
	ProjectAlias string
	Recursive    bool
}

type ScannedFile struct {
	Path         string
	Kind         FileType
	ProjectID    string
	ProjectAlias string
	Size         int64
	ModTime      int64
}

type ScannerError struct {
	Source ScannerSource
	Path   string
	Err    error
}

func (se ScannerError) Error() string {
	return fmt.Sprintf("knowledge scan: src=%s path=%q: %v", se.Source.Kind, se.Path, se.Err)
}

func (se ScannerError) Unwrap() error { return se.Err }

type Scanner struct {
	maxBytes int64
}

func NewScanner(maxBytes int64) *Scanner {
	if maxBytes <= 0 {
		maxBytes = MaxIndexableBytes
	}
	return &Scanner{maxBytes: maxBytes}
}

func (s *Scanner) Scan(sources []ScannerSource) ([]ScannedFile, []ScannerError, error) {
	var out []ScannedFile
	var errs []ScannerError

	for _, src := range sources {

		if !src.Recursive {
			info, err := os.Stat(src.Root)
			if err != nil {
				errs = append(errs, ScannerError{
					Source: src,
					Path:   src.Root,
					Err:    fmt.Errorf("stat: %w", err),
				})
				continue
			}
			if info.IsDir() {
				errs = append(errs, ScannerError{
					Source: src,
					Path:   src.Root,
					Err:    errors.New("expected file, got dir"),
				})
				continue
			}
			if info.Size() > s.maxBytes {
				errs = append(errs, ScannerError{
					Source: src,
					Path:   src.Root,
					Err:    ErrFileTooLarge,
				})
				continue
			}
			out = append(out, ScannedFile{
				Path:         src.Root,
				Kind:         src.Kind,
				ProjectID:    src.ProjectID,
				ProjectAlias: src.ProjectAlias,
				Size:         info.Size(),
				ModTime:      info.ModTime().UnixNano(),
			})
			continue
		}

		var srcFiles []ScannedFile
		walkErr := filepath.WalkDir(src.Root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				errs = append(errs, ScannerError{Source: src, Path: path, Err: walkErr})
				return nil
			}
			if d.IsDir() {

				name := d.Name()
				if path != src.Root && strings.HasPrefix(name, ".") {
					return filepath.SkipDir
				}
				return nil
			}

			name := d.Name()
			if strings.HasPrefix(name, ".") {
				return nil
			}
			if !strings.HasSuffix(name, ".md") {
				return nil
			}
			info, err := dirEntryInfoFn(d)
			if err != nil {

				errs = append(errs, ScannerError{
					Source: src,
					Path:   path,
					Err:    fmt.Errorf("info: %w", err),
				})
				return nil
			}
			if info.Size() > s.maxBytes {
				errs = append(errs, ScannerError{
					Source: src,
					Path:   path,
					Err:    ErrFileTooLarge,
				})
				return nil
			}
			srcFiles = append(srcFiles, ScannedFile{
				Path:         path,
				Kind:         src.Kind,
				ProjectID:    src.ProjectID,
				ProjectAlias: src.ProjectAlias,
				Size:         info.Size(),
				ModTime:      info.ModTime().UnixNano(),
			})
			return nil
		})
		if walkErr != nil {

			errs = append(errs, ScannerError{Source: src, Path: src.Root, Err: walkErr})
		}

		sort.Slice(srcFiles, func(i, j int) bool {
			return srcFiles[i].Path < srcFiles[j].Path
		})
		out = append(out, srcFiles...)
	}

	return out, errs, nil
}
