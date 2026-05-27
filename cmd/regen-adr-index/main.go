// SPDX-License-Identifier: MIT
// Command regen-adr-index regenerates architecture records and
// architecture records from the on-disk ADR files under
// architecture records
//
// Wraps the Q7 A Structured MADR substrate (internal/adr) which
// provides WalkAndEmitIndex + WriteIndex + WalkAndEmitGraph + WriteGraph
// but no CLI verb. Closes the cmd/zen-swarm-ctld/main.go:973 audit
// comment
//
// Composes invariant (regenerate-and-diff infrastructure) — does NOT
// replace; ADDS a manifest-regen CLI surface that exercises the
// existing internal/adr package.
//
// Usage
//
// regen-adr-index [--root <path>] [--check]
//
// Flags
//
// --root <path> Project root containing architecture records (default: ".")
// --check Dry-run: emit diffs if _index.json / _graph.json would
// change; non-zero exit if stale. Otherwise write files.
//
// Exit codes:
//
// 0 success (writes done, or --check found manifests fresh)
// 1 generation error (parse failure, IO error)
// 2 --check found stale manifest (used by CI)
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cbip-solutions/hades-system/internal/adr"
)

func main() {
	root := flag.String("root", ".", "project root containing docs/decisions/")
	check := flag.Bool("check", false, "dry-run; non-zero exit if manifest stale")
	flag.Parse()

	decisionsDir := filepath.Join(*root, "docs", "decisions")
	if _, err := os.Stat(decisionsDir); err != nil {
		fmt.Fprintf(os.Stderr, "regen-adr-index: %s missing: %v\n", decisionsDir, err)
		os.Exit(1)
	}

	ctx := context.Background()

	idx, err := adr.WalkAndEmitIndex(ctx, decisionsDir, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "regen-adr-index: WalkAndEmitIndex: %v\n", err)
		os.Exit(1)
	}

	graph, err := adr.WalkAndEmitGraph(ctx, decisionsDir, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "regen-adr-index: WalkAndEmitGraph: %v\n", err)
		os.Exit(1)
	}

	idxJSON, err := adr.MarshalIndex(idx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "regen-adr-index: MarshalIndex: %v\n", err)
		os.Exit(1)
	}
	graphJSON, err := adr.MarshalGraph(graph)
	if err != nil {
		fmt.Fprintf(os.Stderr, "regen-adr-index: MarshalGraph: %v\n", err)
		os.Exit(1)
	}

	idxPath := filepath.Join(decisionsDir, "_index.json")
	graphPath := filepath.Join(decisionsDir, "_graph.json")

	if *check {
		stale := false
		if existing, err := os.ReadFile(idxPath); err != nil || !bytesEqualIgnoreTrailingNewline(existing, idxJSON) {
			fmt.Fprintf(os.Stderr, "regen-adr-index: %s is stale (run without --check to regenerate)\n", idxPath)
			stale = true
		}
		if existing, err := os.ReadFile(graphPath); err != nil || !bytesEqualIgnoreTrailingNewline(existing, graphJSON) {
			fmt.Fprintf(os.Stderr, "regen-adr-index: %s is stale (run without --check to regenerate)\n", graphPath)
			stale = true
		}
		if stale {
			os.Exit(2)
		}
		fmt.Printf("regen-adr-index: %s + %s are in sync (%d entries)\n", idxPath, graphPath, len(idx.Entries))
		return
	}

	if err := adr.WriteIndex(idxPath, idx); err != nil {
		fmt.Fprintf(os.Stderr, "regen-adr-index: WriteIndex: %v\n", err)
		os.Exit(1)
	}
	if err := adr.WriteGraph(graphPath, graph); err != nil {
		fmt.Fprintf(os.Stderr, "regen-adr-index: WriteGraph: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("regen-adr-index: wrote %d entries to %s + graph to %s\n", len(idx.Entries), idxPath, graphPath)
}

func bytesEqualIgnoreTrailingNewline(a, b []byte) bool {
	aTrim := a
	bTrim := b
	for len(aTrim) > 0 && aTrim[len(aTrim)-1] == '\n' {
		aTrim = aTrim[:len(aTrim)-1]
	}
	for len(bTrim) > 0 && bTrim[len(bTrim)-1] == '\n' {
		bTrim = bTrim[:len(bTrim)-1]
	}
	if len(aTrim) != len(bTrim) {
		return false
	}
	for i := range aTrim {
		if aTrim[i] != bTrim[i] {
			return false
		}
	}
	return true
}
