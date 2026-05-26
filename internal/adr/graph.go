// SPDX-License-Identifier: MIT
package adr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func WalkAndEmitGraph(ctx context.Context, dir string, clock func() string) (*Graph, error) {
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

	g := &Graph{
		SchemaVersion: GraphSchemaVersion,
		GeneratedAt:   clock(),
		Nodes:         []GraphNode{},
		Edges:         []GraphEdge{},
	}

	relatesSeen := make(map[string]struct{})

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

		fm := a.Frontmatter

		g.Nodes = append(g.Nodes, GraphNode{
			ID:     fm.ID,
			Title:  fm.Title,
			Status: fm.Status,
			Plan:   fm.Plan,
		})

		if fm.SupersededBy != "" {
			g.Edges = append(g.Edges, GraphEdge{
				From: fm.ID,
				To:   fm.SupersededBy,
				Kind: EdgeSupersedes,
			})
		}

		for _, ref := range fm.RelatesTo {
			lo, hi := fm.ID, ref
			if lo > hi {
				lo, hi = hi, lo
			}
			key := lo + "|" + hi
			if _, seen := relatesSeen[key]; seen {
				continue
			}
			relatesSeen[key] = struct{}{}
			g.Edges = append(g.Edges, GraphEdge{
				From: lo,
				To:   hi,
				Kind: EdgeRelatesTo,
			})
		}
	}

	sort.Slice(g.Nodes, func(i, j int) bool {
		return g.Nodes[i].ID < g.Nodes[j].ID
	})

	sort.Slice(g.Edges, func(i, j int) bool {
		ei, ej := g.Edges[i], g.Edges[j]
		if ei.From != ej.From {
			return ei.From < ej.From
		}
		if ei.Kind != ej.Kind {
			return string(ei.Kind) < string(ej.Kind)
		}
		return ei.To < ej.To
	})

	return g, nil
}

func MarshalGraph(g *Graph) ([]byte, error) {
	if g == nil {
		return nil, fmt.Errorf("adr: MarshalGraph: nil")
	}

	if g.Nodes == nil {
		g.Nodes = []GraphNode{}
	}
	if g.Edges == nil {
		g.Edges = []GraphEdge{}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(g); err != nil {
		return nil, fmt.Errorf("adr: marshal graph: %w", err)
	}

	return buf.Bytes(), nil
}

func WriteGraph(path string, g *Graph) error {
	raw, err := MarshalGraph(g)
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

func ReadGraph(path string) (*Graph, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrFileNotFound, path)
		}
		return nil, fmt.Errorf("adr: read %s: %w", path, err)
	}
	var g Graph
	if err := json.Unmarshal(raw, &g); err != nil {
		return nil, fmt.Errorf("adr: unmarshal %s: %w", path, err)
	}
	return &g, nil
}
