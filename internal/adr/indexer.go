// SPDX-License-Identifier: MIT
package adr

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Indexer composes Validator, WalkAndEmitIndex, and WalkAndEmitGraph into the
// high-level API consumed by `zen adr index --check`
// and by `make verify-invariants`.
//
// Indexer is the single stable entry point for all index-and-diff operations.
// Callers construct it once (NewIndexer) and reuse across multiple Generate /
// GenerateAndDiff calls. The clock is injected for deterministic testing;
// production passes time.Now().UTC().Format(time.RFC3339).
//
// Boundary Indexer lives in internal/adr/ and MUST NOT import internal/store
// . It calls only the pure-functional walkers and the Validator
// defined in this same package.
type Indexer struct {
	v     *Validator
	clock func() string
}

func NewIndexer(v *Validator, clock func() string) *Indexer {
	if v == nil {
		panic("adr.NewIndexer: validator must not be nil")
	}
	return &Indexer{v: v, clock: clock}
}

func (ix *Indexer) Generate(ctx context.Context, root string) (*Index, *Graph, error) {

	adrs, err := ix.walkAndParse(ctx, root)
	if err != nil {
		return nil, nil, fmt.Errorf("adr: Generate walkAndParse: %w", err)
	}

	if err := ix.v.ValidateAll(ctx, adrs); err != nil {
		return nil, nil, fmt.Errorf("adr: Generate ValidateAll: %w", err)
	}

	idx, err := WalkAndEmitIndex(ctx, root, ix.clock)
	if err != nil {
		return nil, nil, fmt.Errorf("adr: Generate WalkAndEmitIndex: %w", err)
	}

	g, err := WalkAndEmitGraph(ctx, root, ix.clock)
	if err != nil {
		return nil, nil, fmt.Errorf("adr: Generate WalkAndEmitGraph: %w", err)
	}

	return idx, g, nil
}

type Diff struct {
	Path   string
	Reason string
}

func (ix *Indexer) GenerateAndDiff(ctx context.Context, root string) ([]Diff, error) {

	idx, g, err := ix.Generate(ctx, root)
	if err != nil {
		return nil, err
	}

	freshIdx, err := MarshalIndex(idx)
	if err != nil {
		return nil, fmt.Errorf("adr: GenerateAndDiff MarshalIndex: %w", err)
	}
	freshGraph, err := MarshalGraph(g)
	if err != nil {
		return nil, fmt.Errorf("adr: GenerateAndDiff MarshalGraph: %w", err)
	}

	var diffs []Diff

	checks := []struct {
		name  string
		fresh []byte
	}{
		{"_index.json", freshIdx},
		{"_graph.json", freshGraph},
	}
	for _, c := range checks {
		path := filepath.Join(root, c.name)
		disk, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				diffs = append(diffs, Diff{Path: path, Reason: "missing"})
				continue
			}
			return nil, fmt.Errorf("adr: GenerateAndDiff read %s: %w", path, err)
		}
		if !bytes.Equal(disk, c.fresh) {
			diffs = append(diffs, Diff{Path: path, Reason: "stale"})
		}
	}

	return diffs, nil
}

func (ix *Indexer) walkAndParse(ctx context.Context, root string) ([]*ADR, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("adr: walkAndParse readdir %s: %w", root, err)
	}

	var adrs []*ADR
	for _, e := range entries {
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

		fullPath := filepath.Join(root, name)
		a, err := ParseFile(fullPath)
		if err != nil {
			return nil, fmt.Errorf("adr: walkAndParse parse %s: %w", fullPath, err)
		}
		if !a.HasFrontmatter() {

			continue
		}
		adrs = append(adrs, a)
	}

	return adrs, nil
}
