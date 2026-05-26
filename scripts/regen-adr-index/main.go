// SPDX-License-Identifier: MIT
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/cbip-solutions/hades-system/internal/adr"
)

func main() {
	v, err := adr.NewValidator("docs/decisions/_schema.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "validator: %v\n", err)
		os.Exit(1)
	}
	ix := adr.NewIndexer(v, nil)
	idx, graph, err := ix.Generate(context.Background(), "docs/decisions")
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate: %v\n", err)
		os.Exit(1)
	}
	idxBody, _ := json.MarshalIndent(idx, "", "  ")
	if err := os.WriteFile("docs/decisions/_index.json", idxBody, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write idx: %v\n", err)
		os.Exit(1)
	}
	gBody, _ := json.MarshalIndent(graph, "", "  ")
	if err := os.WriteFile("docs/decisions/_graph.json", gBody, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write graph: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("ADR index regenerated:", len(idx.Entries), "entries")
}
