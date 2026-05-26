// SPDX-License-Identifier: MIT
//
// Usage: go run ./tools/gen-tessera-fixture --size N --out <dir>
//
// Emits two files into <dir>:
//
//   - leaves.json: ordered slice of Leaf records (event_id, event_type,
//     payload_hash_hex, record_hash_hex, project_id). Lets consumers
//     reload deterministic fixture state without re-running the mock.
//   - sth.json: the STH captured AFTER appending all leaves and calling
//     FlushAndPublishSTH (size, root_hash_hex, timestamp_unix_nanos,
//     project_id). Pinned timestamp for byte-for-byte determinism.
//
// Determinism: clock pinned to 2026-05-07T00:00:00Z; payload/record
// hashes derived from sha256("leaf-%05d") so the same --size always
// produces the same fixture bytes (regenerate idempotence per spec §5.6).
//
// Why JSON not the raw tile-log binary format: tile-log on-disk shape
// is owned by the real tessera library (POSIX path structure +
// transparency-dev encoding); the mock is purely in-memory and has no
// equivalent on-disk shape. Fixtures therefore expose the *semantic*
// state (leaves + STH) — sufficient for K-6 integration tests to
// rehydrate a mock instance via AppendLeaf in the same order.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/tests/testhelpers/tesseramock"
)

type LeafFixture struct {
	Index          int    `json:"index"`
	LeafID         string `json:"leaf_id"`
	EventID        string `json:"event_id"`
	EventType      string `json:"event_type"`
	PayloadHashHex string `json:"payload_hash_hex"`
	RecordHashHex  string `json:"record_hash_hex"`
	ProjectID      string `json:"project_id"`
}

type STHFixture struct {
	ProjectID         string `json:"project_id"`
	Size              uint64 `json:"size"`
	RootHashHex       string `json:"root_hash_hex"`
	TimestampNanos    int64  `json:"timestamp_unix_nanos"`
	CanonicalBytesHex string `json:"canonical_bytes_hex"`
}

func main() {
	size := flag.Int("size", 10, "number of leaves to append")
	out := flag.String("out", "", "output directory (required)")
	project := flag.String("project", "fixture-project", "tessera projectID")
	flag.Parse()

	if *out == "" {
		fmt.Fprintln(os.Stderr, "ERROR: --out is required")
		os.Exit(1)
	}
	if *size <= 0 {
		fmt.Fprintln(os.Stderr, "ERROR: --size must be > 0")
		os.Exit(1)
	}

	if err := os.MkdirAll(*out, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: mkdir %s: %v\n", *out, err)
		os.Exit(1)
	}

	adapter := tesseramock.New(*project)

	pinnedTime := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	adapter.SetClock(func() time.Time { return pinnedTime })

	ctx := context.Background()

	leaves := make([]LeafFixture, 0, *size)
	for i := 0; i < *size; i++ {
		payload := fmt.Sprintf("leaf-%05d", i)
		payloadHash := sha256.Sum256([]byte(payload))
		recordHash := sha256.Sum256(append([]byte("record:"), payloadHash[:]...))
		leaf := tessera.Leaf{
			EventID:     fmt.Sprintf("evt-%05d", i),
			EventType:   "audit.event_appended",
			PayloadHash: payloadHash[:],
			RecordHash:  recordHash[:],
			ProjectID:   *project,
		}
		id, err := adapter.AppendLeaf(ctx, leaf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: AppendLeaf %d: %v\n", i, err)
			os.Exit(1)
		}
		leaves = append(leaves, LeafFixture{
			Index:          i,
			LeafID:         string(id),
			EventID:        leaf.EventID,
			EventType:      leaf.EventType,
			PayloadHashHex: hex.EncodeToString(leaf.PayloadHash),
			RecordHashHex:  hex.EncodeToString(leaf.RecordHash),
			ProjectID:      leaf.ProjectID,
		})
	}

	var captured tessera.STH
	var captureErr error
	if err := adapter.SubscribeSTH(tesseramock.SubscriberFunc(func(_ context.Context, sth tessera.STH) error {
		captured = sth
		return nil
	})); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: SubscribeSTH: %v\n", err)
		os.Exit(1)
	}
	if err := adapter.FlushAndPublishSTH(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: FlushAndPublishSTH: %v\n", err)
		os.Exit(1)
	}
	if captureErr != nil {
		fmt.Fprintf(os.Stderr, "ERROR: subscriber captured error: %v\n", captureErr)
		os.Exit(1)
	}
	if captured.Size != uint64(*size) {
		fmt.Fprintf(os.Stderr, "ERROR: captured STH.Size %d != requested size %d\n", captured.Size, *size)
		os.Exit(1)
	}

	sth := STHFixture{
		ProjectID:         captured.ProjectID,
		Size:              captured.Size,
		RootHashHex:       hex.EncodeToString(captured.RootHash),
		TimestampNanos:    captured.Timestamp.UnixNano(),
		CanonicalBytesHex: hex.EncodeToString(captured.CanonicalBytes()),
	}

	if err := writeJSON(filepath.Join(*out, "leaves.json"), leaves); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: write leaves.json: %v\n", err)
		os.Exit(1)
	}
	if err := writeJSON(filepath.Join(*out, "sth.json"), sth); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: write sth.json: %v\n", err)
		os.Exit(1)
	}
	if err := adapter.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: adapter.Close: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Wrote %d leaves + 1 STH to %s (root=%s...)\n",
		*size, *out, sth.RootHashHex[:16])
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}
