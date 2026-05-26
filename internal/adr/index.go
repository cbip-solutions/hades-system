// SPDX-License-Identifier: MIT
package adr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func WalkAndEmitIndex(ctx context.Context, dir string, clock func() string) (*Index, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if clock == nil {
		clock = func() string { return time.Now().UTC().Format(time.RFC3339) }
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("adr: readdir %s: %w", dir, err)
	}

	idx := &Index{
		SchemaVersion: IndexSchemaVersion,
		GeneratedAt:   clock(),
		Entries:       []IndexEntry{},
	}

	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !e.Type().IsRegular() {

			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}

		if strings.HasPrefix(name, "_") {
			continue
		}

		fullPath := filepath.Join(dir, name)
		a, err := ParseFile(fullPath)
		if err != nil {
			return nil, fmt.Errorf("adr: parse %s: %w", fullPath, err)
		}
		if !a.HasFrontmatter() {

			continue
		}
		idx.Entries = append(idx.Entries, IndexEntry{
			ID:          a.Frontmatter.ID,
			Title:       a.Frontmatter.Title,
			Status:      a.Frontmatter.Status,
			Path:        a.Path,
			Frontmatter: a.Frontmatter,
		})
	}

	sort.Slice(idx.Entries, func(i, j int) bool {
		return idx.Entries[i].ID < idx.Entries[j].ID
	})

	return idx, nil
}

func MarshalIndex(idx *Index) ([]byte, error) {
	if idx == nil {
		return nil, fmt.Errorf("adr: MarshalIndex: nil")
	}

	if idx.Entries == nil {
		idx.Entries = []IndexEntry{}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(idx); err != nil {
		return nil, fmt.Errorf("adr: marshal index: %w", err)
	}

	return buf.Bytes(), nil
}

func WriteIndex(path string, idx *Index) error {
	raw, err := MarshalIndex(idx)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("adr: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("adr: rename %s → %s: %w", tmp, path, err)
	}
	return nil
}

func ReadIndex(path string) (*Index, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrFileNotFound, path)
		}
		return nil, fmt.Errorf("adr: read %s: %w", path, err)
	}
	var idx Index
	if err := json.Unmarshal(raw, &idx); err != nil {
		return nil, fmt.Errorf("adr: unmarshal %s: %w", path, err)
	}
	return &idx, nil
}

var _ fs.FS = nopFS{}

type nopFS struct{}

func (nopFS) Open(string) (fs.File, error) { return nil, fs.ErrNotExist }
