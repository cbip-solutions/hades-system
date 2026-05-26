package manifest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func makeFreshManifest() Manifest {
	return Manifest{
		ZenSwarm: ZenSwarmSection{
			Version:   "0.9.0",
			Substrate: "openclaude",
		},
		Plans: PlansSection{
			Released:          []string{"plan-9@v0.9.0"},
			InProgress:        []string{},
			BrainstormPending: []string{"plan-10"},
		},
		Invariants: InvariantsSection{Count: 152, VerifyCmd: "make verify-invariants"},
		Doctrines: DoctrinesSection{
			Declared: []string{"capa-firewall", "default", "max-scope"},
		},
		MCPs: MCPsSection{Entries: map[string]MCPEntry{"research": {Plan: 4, Status: "production"}}},
		ADR:  ADRSection{Count: 69, Location: "docs/decisions/"},
		AutonomousMode: AutonomousModeSection{

			PrerequisitesMet: true,
			LastCheck:        time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC),
		},
		Provenance: Provenance{
			LastRegenerate: time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC),
		},
	}
}

func writeExistingManifest(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newRegenerator(t *testing.T) (*Regenerator, string) {
	t.Helper()
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(schemaPath, []byte(fixtureSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	schema, err := LoadSchema(schemaPath)
	if err != nil {
		t.Fatalf("LoadSchema: %v", err)
	}
	return NewRegenerator(schema), dir
}

func TestRegenerator_PreservesManualFields(t *testing.T) {
	r, dir := newRegenerator(t)
	manifestPath := filepath.Join(dir, "system-state.toml")
	existing := `[zen-swarm]
version = "0.8.0"
substrate = "openclaude"
substrate_min_version = "0.7.1"

[doctrines]
declared = []
default = "max-scope"

[autonomous-mode]
status = "enabled"
prerequisites-met = false
last-check = 2026-05-01T00:00:00Z
`
	writeExistingManifest(t, manifestPath, existing)

	fresh := makeFreshManifest()
	merged, err := r.Regenerate(context.Background(), fresh, manifestPath)
	if err != nil {
		t.Fatalf("Regenerate: %v", err)
	}

	if merged.ZenSwarm.SubstrateMinVersion != "0.7.1" {
		t.Errorf("SubstrateMinVersion: got %q, want preserved 0.7.1", merged.ZenSwarm.SubstrateMinVersion)
	}
	if merged.Doctrines.Default != "max-scope" {
		t.Errorf("Doctrines.Default: got %q, want preserved max-scope", merged.Doctrines.Default)
	}
	if merged.AutonomousMode.Status != "enabled" {
		t.Errorf("AutonomousMode.Status: got %q, want preserved enabled", merged.AutonomousMode.Status)
	}

	if merged.ZenSwarm.Version != "0.9.0" {
		t.Errorf("Version: got %q, want fresh 0.9.0", merged.ZenSwarm.Version)
	}
	if merged.Invariants.Count != 152 {
		t.Errorf("Invariants.Count: got %d, want fresh 152", merged.Invariants.Count)
	}
}

func TestRegenerator_NoExistingFile_UsesFreshValues(t *testing.T) {
	r, dir := newRegenerator(t)
	manifestPath := filepath.Join(dir, "missing.toml")
	fresh := makeFreshManifest()
	merged, err := r.Regenerate(context.Background(), fresh, manifestPath)
	if err != nil {
		t.Fatalf("Regenerate: %v", err)
	}

	if merged.ZenSwarm.SubstrateMinVersion != "" {
		t.Errorf("SubstrateMinVersion: got %q, want empty", merged.ZenSwarm.SubstrateMinVersion)
	}
}

func TestRegenerator_EmitDeterministic(t *testing.T) {
	r, _ := newRegenerator(t)
	fresh := makeFreshManifest()
	out1, err := r.Emit(fresh)
	if err != nil {
		t.Fatal(err)
	}
	out2, err := r.Emit(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if string(out1) != string(out2) {
		t.Errorf("Emit non-deterministic:\n--- run1 ---\n%s\n--- run2 ---\n%s",
			out1, out2)
	}
}

func TestRegenerator_WriteAtomic(t *testing.T) {
	r, dir := newRegenerator(t)
	manifestPath := filepath.Join(dir, "system-state.toml")
	fresh := makeFreshManifest()
	if err := r.RegenerateAndWrite(context.Background(), fresh, manifestPath); err != nil {
		t.Fatalf("RegenerateAndWrite: %v", err)
	}
	body, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "version = \"0.9.0\"") {
		t.Errorf("expected version in written file, got:\n%s", body)
	}
}

func TestRegenerator_DryRun_DoesNotWrite(t *testing.T) {
	r, dir := newRegenerator(t)
	manifestPath := filepath.Join(dir, "missing.toml")
	fresh := makeFreshManifest()
	out, err := r.Emit(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Error("Emit returned empty bytes")
	}
	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
		t.Errorf("Emit must not touch disk; file exists at %s", manifestPath)
	}
}

func TestRegenerator_MalformedExisting(t *testing.T) {
	r, dir := newRegenerator(t)
	manifestPath := filepath.Join(dir, "system-state.toml")
	writeExistingManifest(t, manifestPath, "this is not [[valid toml }}")
	fresh := makeFreshManifest()
	_, err := r.Regenerate(context.Background(), fresh, manifestPath)
	if err == nil {
		t.Fatal("Regenerate with malformed existing should return error")
	}
}

func TestRegenerator_EmitContainsMCPKeys(t *testing.T) {
	r, _ := newRegenerator(t)
	m := makeFreshManifest()
	m.MCPs.Entries = map[string]MCPEntry{
		"ssh-exec": {Plan: 4, Status: "production"},
		"budget":   {Plan: 4, Status: "production"},
		"audit":    {Plan: 4, Status: "production"},
		"research": {Plan: 4, Status: "production"},
	}
	out, err := r.Emit(m)
	if err != nil {
		t.Fatal(err)
	}
	body := string(out)
	for _, key := range []string{"ssh-exec", "budget", "audit", "research"} {
		if !strings.Contains(body, key) {
			t.Errorf("Emit output missing MCP key %q", key)
		}
	}

	out2, err := r.Emit(m)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(out2) {
		t.Errorf("multi-key MCP emit is non-deterministic")
	}
}

func TestRegenerator_DryRun_ReturnsBytes(t *testing.T) {
	r, dir := newRegenerator(t)

	manifestPath := filepath.Join(dir, "system-state.toml")
	existing := `[zen-swarm]
version = "0.8.0"
substrate = "openclaude"
substrate_min_version = "1.2.3"

[doctrines]
declared = []
default = "max-scope"

[autonomous-mode]
status = "enabled"
prerequisites-met = false
last-check = 2026-05-01T00:00:00Z
`
	writeExistingManifest(t, manifestPath, existing)

	fresh := makeFreshManifest()
	out, err := r.DryRun(context.Background(), fresh, manifestPath)
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("DryRun returned empty bytes")
	}

	if !strings.Contains(string(out), "1.2.3") {
		t.Errorf("DryRun output missing preserved substrate_min_version=1.2.3:\n%s", out)
	}

	body, _ := os.ReadFile(manifestPath)
	if !strings.Contains(string(body), "0.8.0") {
		t.Errorf("DryRun must not overwrite existing file")
	}
}

func TestRegenerator_DryRun_NoExisting(t *testing.T) {
	r, dir := newRegenerator(t)
	manifestPath := filepath.Join(dir, "nonexistent.toml")
	fresh := makeFreshManifest()
	out, err := r.DryRun(context.Background(), fresh, manifestPath)
	if err != nil {
		t.Fatalf("DryRun with no existing: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("DryRun returned empty bytes")
	}
	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
		t.Errorf("DryRun must not create file at %s", manifestPath)
	}
}

func TestRegenerator_CachedManualPaths(t *testing.T) {
	r, dir := newRegenerator(t)
	manifestPath := filepath.Join(dir, "missing.toml")
	fresh := makeFreshManifest()

	if _, err := r.Regenerate(context.Background(), fresh, manifestPath); err != nil {
		t.Fatalf("first Regenerate: %v", err)
	}

	if _, err := r.Regenerate(context.Background(), fresh, manifestPath); err != nil {
		t.Fatalf("second Regenerate: %v", err)
	}
}

func TestRegenerator_CopyManualField_UnknownSection(t *testing.T) {
	var dst, src Manifest

	copyManualField(&dst, &src, "nonexistent-section.some-field")
}

func TestRegenerator_CopyManualField_UnknownLeaf(t *testing.T) {
	var dst, src Manifest

	copyManualField(&dst, &src, "zen-swarm.nonexistent_leaf")
}

func TestRegenerator_CopyManualField_ShortPath(t *testing.T) {
	var dst, src Manifest

	copyManualField(&dst, &src, "zen-swarm")
}

func TestRegenerator_TomlTagName_WithComma(t *testing.T) {
	got := tomlTagName("entries,omitempty")
	if got != "entries" {
		t.Errorf("tomlTagName: got %q, want entries", got)
	}
	got2 := tomlTagName("plain")
	if got2 != "plain" {
		t.Errorf("tomlTagName no-comma: got %q, want plain", got2)
	}
}

func TestAtomicWrite_NonWritableDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	dir := t.TempDir()

	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	target := filepath.Join(dir, "state.toml")
	err := atomicWrite(target, []byte("data"), 0o644)
	if err == nil {
		t.Error("atomicWrite into non-writable dir should return error")
	}
}

func TestAtomicWrite_TargetIsDirectory(t *testing.T) {
	dir := t.TempDir()

	target := filepath.Join(dir, "subdir")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	err := atomicWrite(target, []byte("data"), 0o644)
	if err == nil {
		t.Error("atomicWrite with target=existing-directory should return rename error")
	}
}

func TestRegenerateAndWrite_NonWritableDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	r, dir := newRegenerator(t)

	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	manifestPath := filepath.Join(dir, "state.toml")
	err := r.RegenerateAndWrite(context.Background(), makeFreshManifest(), manifestPath)
	if err == nil {
		t.Error("RegenerateAndWrite to non-writable dir should return error")
	}
}

func TestDryRun_MalformedExisting(t *testing.T) {
	r, dir := newRegenerator(t)
	manifestPath := filepath.Join(dir, "bad.toml")
	writeExistingManifest(t, manifestPath, "this is not [[valid toml }}")
	_, err := r.DryRun(context.Background(), makeFreshManifest(), manifestPath)
	if err == nil {
		t.Error("DryRun with malformed existing TOML should return error")
	}
}
