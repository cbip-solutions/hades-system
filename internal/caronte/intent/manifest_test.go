package intent

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleManifest = `
# caronte-intent.toml — package↔ADR coverage manifest
schema_version = 1

[[coverage]]
package = "internal/caronte/intent"
adrs = ["ADR-0111", "ADR-0113"]

[[coverage]]
package = "internal/caronte/store"
adrs = ["ADR-0111"]
`

func TestLoadCoverageManifestParses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "caronte-intent.toml")
	if err := os.WriteFile(path, []byte(sampleManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := LoadCoverageManifest(path)
	if err != nil {
		t.Fatalf("LoadCoverageManifest: %v", err)
	}
	if m.SchemaVersion != 1 {
		t.Errorf("schema_version = %d; want 1", m.SchemaVersion)
	}
	adrs := m.ADRsForPackage("internal/caronte/intent")
	if len(adrs) != 2 || adrs[0] != "ADR-0111" || adrs[1] != "ADR-0113" {
		t.Errorf("ADRsForPackage(intent) = %v; want [ADR-0111 ADR-0113]", adrs)
	}
	if got := m.ADRsForPackage("internal/caronte/store"); len(got) != 1 || got[0] != "ADR-0111" {
		t.Errorf("ADRsForPackage(store) = %v; want [ADR-0111]", got)
	}
}

func TestLoadCoverageManifestAbsentIsEmpty(t *testing.T) {
	m, err := LoadCoverageManifest(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatalf("absent manifest should not error: %v", err)
	}
	if m == nil {
		t.Fatal("absent manifest returned nil; want empty manifest")
	}
	if got := m.ADRsForPackage("anything"); len(got) != 0 {
		t.Errorf("empty manifest ADRsForPackage = %v; want []", got)
	}
}

func TestLoadCoverageManifestRejectsBadADR(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "caronte-intent.toml")
	bad := `schema_version = 1
[[coverage]]
package = "internal/x"
adrs = ["ADR-100", "not-an-adr"]
`
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCoverageManifest(path); err == nil {
		t.Error("LoadCoverageManifest accepted malformed ADR id; want error")
	}
}

func TestLoadCoverageManifestRejectsBadSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "caronte-intent.toml")
	bad := `schema_version = 99
[[coverage]]
package = "internal/x"
adrs = ["ADR-0111"]
`
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCoverageManifest(path); err == nil {
		t.Error("LoadCoverageManifest accepted schema_version=99; want error")
	}
}

func TestManifestPathFor(t *testing.T) {
	got := ManifestPathFor("/proj/root")
	want := filepath.Join("/proj/root", ".zen", "caronte-intent.toml")
	if got != want {
		t.Errorf("ManifestPathFor = %q; want %q", got, want)
	}
}

func TestLoadCoverageManifestRejectsEmptyPackage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "caronte-intent.toml")
	bad := `schema_version = 1
[[coverage]]
package = ""
adrs = ["ADR-0111"]
`
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCoverageManifest(path); err == nil {
		t.Error("LoadCoverageManifest accepted empty package; want error")
	}
}

func TestLoadCoverageManifestRejectsBadTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "caronte-intent.toml")
	if err := os.WriteFile(path, []byte("schema_version = [[[[["), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCoverageManifest(path); err == nil {
		t.Error("LoadCoverageManifest accepted invalid TOML; want error")
	}
}

func TestLoadCoverageManifestUnreadableFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "caronte-intent.toml")
	if err := os.WriteFile(path, []byte("schema_version = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	if _, err := LoadCoverageManifest(path); err == nil {
		t.Error("LoadCoverageManifest should error on unreadable file; got nil")
	}
}

func TestADRsForPackageNilManifest(t *testing.T) {
	var m *CoverageManifest
	got := m.ADRsForPackage("anything")
	if len(got) != 0 {
		t.Errorf("nil manifest ADRsForPackage = %v; want []", got)
	}
}

func TestDefaultIntentParamsZeroYieldsDefaults(t *testing.T) {
	p := DefaultIntentParams(IntentParams{})
	if p.SemanticThreshold != 0.30 {
		t.Errorf("SemanticThreshold = %v; want 0.30", p.SemanticThreshold)
	}
	if p.SemanticTopK != 5 {
		t.Errorf("SemanticTopK = %d; want 5", p.SemanticTopK)
	}
	if p.KNNFanout != 20 {
		t.Errorf("KNNFanout = %d; want 20", p.KNNFanout)
	}
	if p.ChunkRunes != 1200 {
		t.Errorf("ChunkRunes = %d; want 1200", p.ChunkRunes)
	}
}

func TestDefaultIntentParamsNonZeroUnchanged(t *testing.T) {
	in := IntentParams{SemanticThreshold: 0.5, SemanticTopK: 10, KNNFanout: 50, ChunkRunes: 800}
	out := DefaultIntentParams(in)
	if out != in {
		t.Errorf("DefaultIntentParams clobbered non-zero fields: got %+v; want %+v", out, in)
	}
}
