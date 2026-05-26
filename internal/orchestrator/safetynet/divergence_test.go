package safetynet

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeJSON(t *testing.T, p string, v any) {
	t.Helper()
	b, _ := json.Marshal(v)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDivergence_NoDiff_Empty(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	a := filepath.Join(tmp, "a.json")
	b := filepath.Join(tmp, "b.json")
	writeJSON(t, a, map[string]any{"k": "v", "n": 1.0})
	writeJSON(t, b, map[string]any{"k": "v", "n": 1.0})

	em := &fakeEmitter{}
	d := NewDivergence(em)
	rep, err := d.Compare(context.Background(), a, b)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if !rep.Equal {
		t.Fatalf("want equal, got %+v", rep)
	}
	if len(em.events) != 0 {
		t.Fatalf("no event when equal; got %+v", em.events)
	}
}

func TestDivergence_KeyMissing_EmitsEvent(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	a := filepath.Join(tmp, "a.json")
	b := filepath.Join(tmp, "b.json")
	writeJSON(t, a, map[string]any{"k": "v", "extra": true})
	writeJSON(t, b, map[string]any{"k": "v"})

	em := &fakeEmitter{}
	d := NewDivergence(em)
	rep, err := d.Compare(context.Background(), a, b)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Equal {
		t.Fatal("want diff")
	}
	if len(rep.OnlyInA) != 1 || rep.OnlyInA[0] != "extra" {
		t.Fatalf("OnlyInA = %v want [extra]", rep.OnlyInA)
	}
	if len(em.events) != 1 || em.events[0].Type != EventConfigDivergenceDetected {
		t.Fatalf("expected ConfigDivergenceDetected event; got %+v", em.events)
	}
}

func TestDivergence_ValueChanged_EmitsEvent(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	a := filepath.Join(tmp, "a.json")
	b := filepath.Join(tmp, "b.json")
	writeJSON(t, a, map[string]any{"timeout": 30.0})
	writeJSON(t, b, map[string]any{"timeout": 60.0})

	em := &fakeEmitter{}
	d := NewDivergence(em)
	rep, _ := d.Compare(context.Background(), a, b)
	if rep.Equal {
		t.Fatal("want diff")
	}
	if len(rep.Changed) != 1 || rep.Changed[0].Key != "timeout" {
		t.Fatalf("Changed = %v", rep.Changed)
	}
	if rep.Changed[0].A != 30.0 || rep.Changed[0].B != 60.0 {
		t.Fatalf("Changed values drift: %+v", rep.Changed[0])
	}
}

func TestDivergence_Symmetry_DiffInverts(t *testing.T) {

	t.Parallel()
	tmp := t.TempDir()
	a := filepath.Join(tmp, "a.json")
	b := filepath.Join(tmp, "b.json")
	writeJSON(t, a, map[string]any{"x": 1.0, "y": 2.0})
	writeJSON(t, b, map[string]any{"y": 9.0, "z": 3.0})

	em := &fakeEmitter{}
	d := NewDivergence(em)
	ab, _ := d.Compare(context.Background(), a, b)
	ba, _ := d.Compare(context.Background(), b, a)
	if len(ab.OnlyInA) != 1 || ab.OnlyInA[0] != "x" {
		t.Errorf("ab.OnlyInA = %v", ab.OnlyInA)
	}
	if len(ba.OnlyInB) != 1 || ba.OnlyInB[0] != "x" {
		t.Errorf("ba.OnlyInB = %v", ba.OnlyInB)
	}
	if len(ab.OnlyInB) != 1 || ab.OnlyInB[0] != "z" {
		t.Errorf("ab.OnlyInB = %v", ab.OnlyInB)
	}
	if len(ab.Changed) != 1 || ab.Changed[0].Key != "y" {
		t.Errorf("ab.Changed = %v", ab.Changed)
	}
}

func TestDivergence_FileMissing_Errors(t *testing.T) {
	t.Parallel()
	d := NewDivergence(&fakeEmitter{})
	_, err := d.Compare(context.Background(), "/nope/a.json", "/nope/b.json")
	if err == nil || !strings.Contains(err.Error(), "read") {
		t.Fatalf("want read error, got %v", err)
	}
}

func TestDivergence_PathBMissing_Errors(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	a := filepath.Join(tmp, "a.json")
	writeJSON(t, a, map[string]any{"k": "v"})
	d := NewDivergence(&fakeEmitter{})
	_, err := d.Compare(context.Background(), a, filepath.Join(tmp, "missing.json"))
	if err == nil || !strings.Contains(err.Error(), "read") {
		t.Fatalf("want read error on B, got %v", err)
	}
}

func TestDivergence_InvalidJSON_Errors(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	a := filepath.Join(tmp, "bad.json")
	if err := os.WriteFile(a, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := NewDivergence(&fakeEmitter{})
	_, err := d.Compare(context.Background(), a, a)
	if err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("want parse error, got %v", err)
	}
}

func TestDivergence_NestedObject_Flattens(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	a := filepath.Join(tmp, "a.json")
	b := filepath.Join(tmp, "b.json")
	writeJSON(t, a, map[string]any{
		"server": map[string]any{
			"port":    9100.0,
			"host":    "localhost",
			"timeout": map[string]any{"ms": 250.0},
		},
	})
	writeJSON(t, b, map[string]any{
		"server": map[string]any{
			"port":    9100.0,
			"host":    "localhost",
			"timeout": map[string]any{"ms": 500.0},
		},
	})
	d := NewDivergence(&fakeEmitter{})
	rep, err := d.Compare(context.Background(), a, b)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Equal {
		t.Fatal("nested ms changed should diverge")
	}
	if len(rep.Changed) != 1 || rep.Changed[0].Key != "server.timeout.ms" {
		t.Fatalf("Changed = %v want [server.timeout.ms]", rep.Changed)
	}
}

func TestDivergence_MultipleChanged_Sorted(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	a := filepath.Join(tmp, "a.json")
	b := filepath.Join(tmp, "b.json")
	writeJSON(t, a, map[string]any{"zebra": 1.0, "apple": 2.0, "mango": 3.0})
	writeJSON(t, b, map[string]any{"zebra": 9.0, "apple": 8.0, "mango": 7.0})
	d := NewDivergence(&fakeEmitter{})
	rep, _ := d.Compare(context.Background(), a, b)
	if len(rep.Changed) != 3 {
		t.Fatalf("len=%d want 3: %+v", len(rep.Changed), rep.Changed)
	}
	if rep.Changed[0].Key != "apple" || rep.Changed[1].Key != "mango" || rep.Changed[2].Key != "zebra" {
		t.Errorf("unsorted: %+v", rep.Changed)
	}
}

// errEmitter returns a hard error from Emit; Compare swallows it (audit
// pipe degradation MUST NOT block the report).
type errEmitter struct{}

func (errEmitter) Emit(_ context.Context, _ Event) error { return errors.New("audit pipe down") }

func TestDivergence_EmitFailureDoesNotBlockReport(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	a := filepath.Join(tmp, "a.json")
	b := filepath.Join(tmp, "b.json")
	writeJSON(t, a, map[string]any{"k": "old"})
	writeJSON(t, b, map[string]any{"k": "new"})
	d := NewDivergence(errEmitter{})
	rep, err := d.Compare(context.Background(), a, b)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if rep.Equal {
		t.Error("expected diff")
	}
}
