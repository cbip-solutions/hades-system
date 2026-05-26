//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cbip-solutions/hades-system/internal/research/cache"
)

type FindingsCorpus struct {
	Version     int             `json:"version"`
	GeneratedAt string          `json:"generated_at"`
	Count       int             `json:"count"`
	Findings    []cache.Finding `json:"findings"`
}

func main() {
	count := flag.Int("count", 50, "number of findings to generate")
	out := flag.String("out", "", "output file path (required)")
	dispatch := flag.String("dispatch", "dsp-fixture-0001", "dispatch_id for all findings")
	flag.Parse()

	if *out == "" {
		fmt.Fprintln(os.Stderr, "ERROR: --out is required")
		os.Exit(1)
	}
	if *count <= 0 {
		fmt.Fprintln(os.Stderr, "ERROR: --count must be > 0")
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Dir(*out), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: mkdir %s: %v\n", filepath.Dir(*out), err)
		os.Exit(1)
	}

	baseTime := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC).Unix()

	findings := make([]cache.Finding, 0, *count)
	for i := 0; i < *count; i++ {
		snippet := fmt.Sprintf("Finding %d snippet", i)
		sum := sha256.Sum256([]byte(snippet))
		findings = append(findings, cache.Finding{
			ID:                 fmt.Sprintf("00000000-0000-7000-8000-%012x", i),
			DispatchID:         *dispatch,
			URL:                fmt.Sprintf("https://example.test/finding-%d", i),
			Title:              fmt.Sprintf("Finding %d", i),
			Snippet:            snippet,
			Freshness:          cache.FreshnessFresh,
			RetrievedAt:        baseTime + int64(60*i),
			ContentHash:        hex.EncodeToString(sum[:]),
			SourceURLCanonical: fmt.Sprintf("https://example.test/finding-%d", i),
		})
	}

	corpus := FindingsCorpus{
		Version:     1,
		GeneratedAt: time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
		Count:       *count,
		Findings:    findings,
	}

	if err := writeJSON(*out, corpus); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: write %s: %v\n", *out, err)
		os.Exit(1)
	}

	fmt.Printf("Wrote %d findings to %s\n", *count, *out)
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}
