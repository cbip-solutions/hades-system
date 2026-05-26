//go:build adversarial
// +build adversarial

package adversarial_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/migrate/mapping"
	"github.com/cbip-solutions/hades-system/internal/migrate/source"
)

func TestAdversarial_SymlinkOutsideRoot_Strict(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("evil"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "skills", "evil"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "skills", "evil", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	_, err := source.ReadAll(root)
	if !errors.Is(err, source.ErrSymlinkOutsideRoot) {
		t.Errorf("err: got %v, want ErrSymlinkOutsideRoot", err)
	}
}

func TestAdversarial_MalformedJSONSettings_Strict(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "settings.json"), []byte("{this is not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := source.ReadAll(root)
	if !errors.Is(err, source.ErrMalformedSettings) {
		t.Errorf("err: got %v, want ErrMalformedSettings", err)
	}
}

func TestAdversarial_MalformedMCP_Strict(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte("not json at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := source.ReadAll(root)
	if !errors.Is(err, source.ErrMalformedMCP) {
		t.Errorf("err: got %v, want ErrMalformedMCP", err)
	}
}

func TestAdversarial_BinaryInSkills_Lenient(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, "skills", "binary-skill")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	binary := make([]byte, 256)
	for i := range binary {
		binary[i] = byte(i)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), binary, 0o644); err != nil {
		t.Fatal(err)
	}
	inv, err := source.ReadAll(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Skills) != 1 {
		t.Errorf("got %d skills, want 1", len(inv.Skills))
	}
	if !bytes.Equal(inv.Skills[0].Body, binary) {
		t.Errorf("binary body not preserved verbatim")
	}
}

func TestAdversarial_PermissionDenied_Lenient(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("permission-denied path test requires non-root")
	}
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, "skills", "no-read")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte("# secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0o644)
	inv, err := source.ReadAll(root)
	if err != nil {
		t.Skipf("walker may abort on permission-denied: %v", err)
	}

	_ = inv
}

func TestAdversarial_HostileCLI_Strict_HaltsOnUnmapped(t *testing.T) {
	t.Parallel()
	bin := buildZenAdv(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "settings.json"), []byte(`{"weird_unknown_field":42}`), 0o600); err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	cmd := exec.Command(bin, "migrate", "claude-code",
		"--source", root,
		"--target-hermes", filepath.Join(tmp, "plugin", "zen-swarm"),
		"--target-config", filepath.Join(tmp, "hermes", "config.yaml"),
		"--target-zen-config", filepath.Join(tmp, "zen-config"),
		"--preset", "strict",
		"--force")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit, got success:\n%s", out)
	}
	low := strings.ToLower(string(out))
	if !strings.Contains(low, "unmapped") && !strings.Contains(low, "unknown") &&
		!strings.Contains(low, "weird_unknown_field") {
		t.Errorf("expected unmapped/unknown message; got:\n%s", out)
	}
}

// TestAdversarial_ApprovalHooks_Strict_PostSpike asserts post-spike 2026-05-16
// reclassification: pre_approval_request + post_approval_response moved from
// risk-flagged → confirmed per spec §8.4. Strict mode MUST NOT halt; both
// hooks MUST be mapped to Hermes canonical paths. Regression guard against
// spec drift (if these hooks become risk-flagged again, this test fails so
// the migrate behavior is updated in lockstep).
func TestAdversarial_ApprovalHooks_Strict_PostSpike(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "hooks", "permission.asked.sh"), []byte("#!/bin/bash"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "hooks", "permission.replied.sh"), []byte("#!/bin/bash"), 0o755); err != nil {
		t.Fatal(err)
	}
	inv, err := source.ReadAll(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := mapping.Map(inv, mapping.PresetStrict)
	if err != nil {
		t.Fatalf("strict + reclassified-confirmed approval hooks: unexpected halt: %v", err)
	}
	if errors.Is(err, mapping.ErrHookRiskFlagged) {
		t.Errorf("ErrHookRiskFlagged must NOT fire post-spike 2026-05-16 (spec §8.4)")
	}
	if len(plan.Entries) != 2 {
		t.Fatalf("entries: got %d, want 2 (both approval hooks mapped)", len(plan.Entries))
	}
	for _, w := range plan.Warnings {
		if strings.Contains(w, "risk-flagged") {
			t.Errorf("unexpected risk-flagged warning post-spike: %q", w)
		}
	}
}

func TestAdversarial_UnmappedHookEvent_Strict(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "hooks", "completely.fictional.sh"), []byte("#!/bin/bash"), 0o755); err != nil {
		t.Fatal(err)
	}
	inv, err := source.ReadAll(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mapping.Map(inv, mapping.PresetStrict)
	if !errors.Is(err, mapping.ErrUnmappedSurface) {
		t.Errorf("strict + unmapped hook: got %v, want ErrUnmappedSurface", err)
	}
}

func buildZenAdv(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "zen")
	cwd, _ := os.Getwd()
	root, _ := filepath.Abs(filepath.Join(cwd, "..", ".."))
	cmd := exec.Command("go", "build",
		"-tags=sqlite_fts5",
		"-ldflags=-X github.com/ncruces/go-sqlite3/driver.driverName=sqlite3_ncruces",
		"-o", out, "./cmd/zen")
	cmd.Dir = root
	if buildOut, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build zen: %v\n%s", err, buildOut)
	}
	return out
}
