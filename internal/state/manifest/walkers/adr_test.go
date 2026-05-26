package walkers

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestADRWalker_ReadsIndexJSON(t *testing.T) {
	dir := t.TempDir()
	idx := filepath.Join(dir, "_index.json")

	if err := os.WriteFile(idx, []byte(`{"adrs":[{"id":"ADR-0001"},{"id":"ADR-0002"},{"id":"ADR-0003"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	w := NewADRWalker(idx)
	r, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if r.Count != 3 {
		t.Errorf("Count = %d, want 3", r.Count)
	}
	if r.Location != "docs/decisions/" {
		t.Errorf("Location = %q, want docs/decisions/", r.Location)
	}
}

func TestADRWalker_ReadsEntriesFormat(t *testing.T) {
	dir := t.TempDir()
	idx := filepath.Join(dir, "_index.json")

	if err := os.WriteFile(idx, []byte(`{"schema_version":1,"generated_at":"2026-05-07T00:00:00Z","entries":[{"id":"ADR-0001"},{"id":"ADR-0002"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	w := NewADRWalker(idx)
	r, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if r.Count != 2 {
		t.Errorf("Count = %d, want 2", r.Count)
	}
}

func TestADRWalker_MissingFile_ReportsMissing(t *testing.T) {
	w := NewADRWalker("/nonexistent/_index.json")
	r, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if !contains(r.MissingSources, "_index.json") {
		t.Errorf("MissingSources: got %v", r.MissingSources)
	}
	if r.Count != 0 {
		t.Errorf("Count: got %d, want 0 on missing file", r.Count)
	}
}

func TestADRWalker_MalformedJSON_ReportsMissing(t *testing.T) {
	dir := t.TempDir()
	idx := filepath.Join(dir, "_index.json")
	if err := os.WriteFile(idx, []byte("{not valid"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := NewADRWalker(idx)
	r, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if !contains(r.MissingSources, "_index.json") {
		t.Errorf("MissingSources: got %v on malformed json", r.MissingSources)
	}
}
