package walkers

import (
	"context"
	"path/filepath"
	"testing"
)

func TestInvariantsWalker_GrepsAndCountsUnique(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"internal/audit/chain.go":         "// inv-zen-143 enforced\n// inv-zen-144 enforced\n",
		"internal/audit/store.go":         "// inv-zen-143 cross-ref\n// inv-zen-145 enforced\n",
		"tests/compliance/inv_zen_146.go": "// inv-zen-146\n",
		"tests/integration/x.go":          "// no inv\n",
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := writeFile(p, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	w := NewInvariantsWalker(dir)
	r, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	if r.Count != 4 {
		t.Errorf("Count = %d, want 4 (unique ids)", r.Count)
	}
}

func TestInvariantsWalker_DeduplicatesAcrossFiles(t *testing.T) {
	dir := t.TempDir()

	files := map[string]string{
		"internal/a.go": "// inv-zen-001\n",
		"internal/b.go": "// inv-zen-001\n// inv-zen-002\n",
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := writeFile(p, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	w := NewInvariantsWalker(dir)
	r, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if r.Count != 2 {
		t.Errorf("Count = %d, want 2 (unique ids 001+002)", r.Count)
	}
}

func TestInvariantsWalker_MissingDir_ReportsMissing(t *testing.T) {
	w := NewInvariantsWalker("/nonexistent")
	r, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if !contains(r.MissingSources, "internal/") && !contains(r.MissingSources, "grep-roots") {
		t.Errorf("MissingSources: got %v", r.MissingSources)
	}
}

func TestInvariantsWalker_EmptyDir_CountZero(t *testing.T) {
	dir := t.TempDir()

	if err := writeFile(filepath.Join(dir, "internal", "placeholder.go"), []byte("package x\n")); err != nil {
		t.Fatal(err)
	}
	w := NewInvariantsWalker(dir)
	r, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if r.Count != 0 {
		t.Errorf("Count = %d, want 0 (no inv markers)", r.Count)
	}
	if len(r.MissingSources) != 0 {
		t.Errorf("MissingSources: got %v, want empty", r.MissingSources)
	}
}

func TestInvariantsWalker_SkipsNonSourceFiles(t *testing.T) {
	dir := t.TempDir()

	files := map[string]string{
		"internal/logic.go":    "// inv-zen-300\n",
		"internal/data.bin":    "inv-zen-301",
		"internal/config.toml": "inv-zen-302\n",
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := writeFile(p, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	w := NewInvariantsWalker(dir)
	r, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	if r.Count != 1 {
		t.Errorf("Count = %d, want 1 (only .go file scanned)", r.Count)
	}
}

func TestInvariantsWalker_ScansMarkdownAndSQL(t *testing.T) {
	dir := t.TempDir()

	files := map[string]string{
		"internal/schema.sql": "-- inv-zen-200 enforced\n-- inv-zen-201 referenced\n",
		"docs/design.md":      "inv-zen-200 is the boundary rule\n",
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := writeFile(p, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	w := NewInvariantsWalker(dir)
	r, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	if r.Count != 2 {
		t.Errorf("Count = %d, want 2 (unique ids from .sql file)", r.Count)
	}
}
