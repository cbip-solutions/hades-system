// SPDX-License-Identifier: MIT
//
// Usage: go run./tools/gen-audit-golden --count N --out <file>
//
// Emits a single JSON file at <file> containing an ordered sequence of
// audit events with their pre-computed chain hashes per Q3 C
// (chain.Compute spec §1.Q3 + §2.4 migration 059):
//
// record_hash = sha256(prev_hash || "|" || event_type || "|" || payload || "|" || ts_decimal)
//
// Determinism: timestamps anchored at 2026-05-07T00:00:00Z and increment
// 1 s per event. Payloads are stable JSON objects with deterministic
// fields ({"seq": <i>, "value": "sample-<i>"}). Event types cycle
// through a fixed enum so the fixture exercises chain stability under
// type variance.
//
// Consumers (K-6 integration tier, future regression tests) load this
// JSON and verify that re-running chain.Compute over the sequence
// reproduces the stored record_hash values byte-for-byte. Any drift in
// the chain algorithm fails fast.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/chain"
)

type AuditGoldenEvent struct {
	Seq        int    `json:"seq"`
	EventID    string `json:"event_id"`
	EventType  string `json:"event_type"`
	Payload    string `json:"payload"`
	EmittedAt  int64  `json:"emitted_at_unix_seconds"`
	PrevHash   string `json:"prev_hash"`
	RecordHash string `json:"record_hash"`
}

type AuditGoldenChain struct {
	Version     int                `json:"version"`
	Algorithm   string             `json:"algorithm"`
	GeneratedAt string             `json:"generated_at"`
	Count       int                `json:"count"`
	Events      []AuditGoldenEvent `json:"events"`
}

var eventTypes = []string{
	"audit.event_appended",
	"audit.partition_sealed",
	"tessera.sth_published",
	"knowledge.pin_promoted",
}

func main() {
	count := flag.Int("count", 100, "number of events to generate")
	out := flag.String("out", "", "output file path (required)")
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

	events := make([]AuditGoldenEvent, 0, *count)
	prev := ""
	for i := 0; i < *count; i++ {
		evtType := eventTypes[i%len(eventTypes)]

		payload := fmt.Sprintf(`{"seq":%d,"value":"sample-%d"}`, i, i)
		ts := baseTime + int64(i)
		recordHash, err := chain.Compute(prev, evtType, []byte(payload), ts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: chain.Compute seq=%d: %v\n", i, err)
			os.Exit(1)
		}
		events = append(events, AuditGoldenEvent{
			Seq:        i,
			EventID:    fmt.Sprintf("evt-%05d", i),
			EventType:  evtType,
			Payload:    payload,
			EmittedAt:  ts,
			PrevHash:   prev,
			RecordHash: recordHash,
		})
		prev = recordHash
	}

	chainFixture := AuditGoldenChain{
		Version:     1,
		Algorithm:   "sha256(prev|type|payload|ts_decimal)",
		GeneratedAt: time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
		Count:       *count,
		Events:      events,
	}

	if err := writeJSON(*out, chainFixture); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: write %s: %v\n", *out, err)
		os.Exit(1)
	}

	fmt.Printf("Wrote %d events to %s (final_hash=%s...)\n",
		*count, *out, events[len(events)-1].RecordHash[:16])
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}
