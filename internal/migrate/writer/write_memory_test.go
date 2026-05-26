package writer

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/migrate/mapping"
)

func TestWriteMemory_Verbatim(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "projects", "a", "memory", "MEMORY.md")
	want := []byte("# memory\nbody with special chars: \"quotes\" and 'apos' and \\backslash")
	e := mapping.PlanEntry{Kind: mapping.EntryKindMemory, BodyBytes: want}
	if err := writeMemory(path, e); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, want) {
		t.Errorf("body mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestWriteMemory_NewlinePreservation(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "M.md")
	want := []byte("line1\n\nline3\n")
	e := mapping.PlanEntry{Kind: mapping.EntryKindMemory, BodyBytes: want}
	if err := writeMemory(path, e); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, want) {
		t.Errorf("newlines not preserved: %q", got)
	}
}
