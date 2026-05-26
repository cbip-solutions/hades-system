//go:build ignore

// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cbip-solutions/hades-system/internal/adr"
)

func main() {
	dir := "docs/decisions"

	v, err := adr.NewValidator(filepath.Join(dir, "_schema.json"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "NewValidator: %v\n", err)
		os.Exit(1)
	}

	ix := adr.NewIndexer(v, nil)
	idx, g, err := ix.Generate(context.Background(), dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Generate: %v\n", err)
		os.Exit(1)
	}

	if err := adr.WriteIndex(filepath.Join(dir, "_index.json"), idx); err != nil {
		fmt.Fprintf(os.Stderr, "WriteIndex: %v\n", err)
		os.Exit(1)
	}
	if err := adr.WriteGraph(filepath.Join(dir, "_graph.json"), g); err != nil {
		fmt.Fprintf(os.Stderr, "WriteGraph: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Generated: %d entries, %d nodes, %d edges\n", len(idx.Entries), len(g.Nodes), len(g.Edges))
}
