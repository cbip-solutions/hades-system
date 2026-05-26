package commenthygiene

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScan_FlagsRotPatterns(t *testing.T) {
	tmp := t.TempDir()
	rotFile := filepath.Join(tmp, "rot.go")
	if err := os.WriteFile(rotFile, []byte(`package x
// inv-zen-031: load-bearing
func A() {}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	reports, err := Scan(tmp)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1 (rot Plan 5)", len(reports))
	}
	if reports[0].Decision != DecisionDelete {
		t.Errorf("got decision %v, want DELETE", reports[0].Decision)
	}
}

func TestScan_SkipsVendorAndTestdata(t *testing.T) {
	tmp := t.TempDir()
	vendor := filepath.Join(tmp, "vendor")
	if err := os.MkdirAll(vendor, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vendor, "x.go"), []byte("// Plan 5 rot\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reports, err := Scan(tmp)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(reports) != 0 {
		t.Errorf("got %d reports, want 0 (vendor excluded)", len(reports))
	}
}

func TestScan_SkipsTestdata(t *testing.T) {
	tmp := t.TempDir()
	td := filepath.Join(tmp, "testdata")
	if err := os.MkdirAll(td, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(td, "x.go"), []byte("// Plan 5 rot\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reports, err := Scan(tmp)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(reports) != 0 {
		t.Errorf("got %d reports, want 0 (testdata excluded)", len(reports))
	}
}

func TestScan_PythonHashComments(t *testing.T) {
	tmp := t.TempDir()
	pyFile := filepath.Join(tmp, "rot.py")
	if err := os.WriteFile(pyFile, []byte(`# regular module
def f(): pass
`), 0o644); err != nil {
		t.Fatal(err)
	}
	reports, err := Scan(tmp)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if len(reports) != 0 {
		t.Errorf("got %d reports, want 0 (regular py # comment)", len(reports))
	}
}
