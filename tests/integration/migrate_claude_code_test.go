package integration_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fixturePath(t *testing.T, name string) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs(filepath.Join(cwd, "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "internal", "migrate", "golden", "fixtures", name)
}

func buildZenForMigrate(t *testing.T) string {
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

func TestMigrateClaudeCode_EndToEnd(t *testing.T) {
	t.Parallel()
	bin := buildZenForMigrate(t)
	source := filepath.Join(fixturePath(t, "01-simple-skill"), "input")
	tmp := t.TempDir()
	pluginRoot := filepath.Join(tmp, "plugin", "zen-swarm")
	hermesCfg := filepath.Join(tmp, "hermes", "config.yaml")
	zenCfg := filepath.Join(tmp, "zen-config")

	cmd := exec.Command(bin, "migrate", "claude-code",
		"--source", source,
		"--target-hermes", pluginRoot,
		"--target-config", hermesCfg,
		"--target-zen-config", zenCfg,
		"--force")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("zen migrate failed: %v\n%s", err, out)
	}

	skillPath := filepath.Join(pluginRoot, "skills", "research-cheap", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Errorf("skill not written: %v", err)
	}

	initPath := filepath.Join(pluginRoot, "__init__.py")
	body, err := os.ReadFile(initPath)
	if err != nil {
		t.Fatalf("read __init__.py: %v", err)
	}
	if !strings.Contains(string(body), "ctx.register_skill") {
		t.Errorf("__init__.py missing register_skill: %s", body)
	}
}

func TestMigrateClaudeCode_BackupBeforeModify_invZen177(t *testing.T) {
	t.Parallel()
	bin := buildZenForMigrate(t)
	source := filepath.Join(fixturePath(t, "01-simple-skill"), "input")
	home := t.TempDir()
	pluginRoot := filepath.Join(home, ".hermes", "plugins", "zen-swarm")
	if err := os.MkdirAll(pluginRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, "operator-edit.md"), []byte("operator"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "migrate", "claude-code",
		"--source", source,
		"--target-hermes", pluginRoot,
		"--target-config", filepath.Join(home, ".hermes", "config.yaml"),
		"--target-zen-config", filepath.Join(home, ".config", "zen-swarm"),
		"--force",
		"--backup-target")
	cmd.Env = append(os.Environ(), "HOME="+home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("zen migrate failed: %v\n%s", err, out)
	}

	backupRoot := filepath.Join(home, ".local", "state", "zen-swarm", "migrate-backups")
	entries, err := os.ReadDir(backupRoot)
	if err != nil {
		t.Fatalf("read backup root: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no backup tarball created")
	}
	for _, e := range entries {
		info, _ := e.Info()
		if info.Mode().Perm() != 0o600 {
			t.Errorf("backup %s mode %o, want 0600", e.Name(), info.Mode().Perm())
		}
		if !strings.HasSuffix(e.Name(), ".tar.gz") {
			t.Errorf("backup %s not .tar.gz", e.Name())
		}
	}
}

func TestMigrateClaudeCode_PlanThenApply_Deterministic(t *testing.T) {
	t.Parallel()
	bin := buildZenForMigrate(t)
	source := filepath.Join(fixturePath(t, "01-simple-skill"), "input")
	tmp1 := t.TempDir()
	tmp2 := t.TempDir()

	if err := runMigrate(bin, source, tmp1, ""); err != nil {
		t.Fatalf("direct migrate: %v", err)
	}

	planPath := filepath.Join(t.TempDir(), "plan.json")
	if err := runMigratePlanOutput(bin, source, planPath); err != nil {
		t.Fatalf("plan-output: %v", err)
	}
	if err := runMigrate(bin, source, tmp2, planPath); err != nil {
		t.Fatalf("apply-plan: %v", err)
	}

	if !dirsEqual(t, tmp1, tmp2) {
		t.Errorf("direct migrate and plan-then-apply not byte-identical")
	}
}

func TestMigrateClaudeCode_VerifyHook_StubReturnsZero(t *testing.T) {
	t.Parallel()
	bin := buildZenForMigrate(t)
	source := filepath.Join(fixturePath(t, "01-simple-skill"), "input")
	tmp := t.TempDir()
	cmd := exec.Command(bin, "migrate", "claude-code",
		"--source", source,
		"--target-hermes", filepath.Join(tmp, "plugin", "zen-swarm"),
		"--target-config", filepath.Join(tmp, "hermes", "config.yaml"),
		"--target-zen-config", filepath.Join(tmp, "zen-config"),
		"--force",
		"--verify")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("verify: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Verify") {
		t.Errorf("verify hint missing: %s", out)
	}
}

func TestMigrateClaudeCode_DryRunNoFilesystemChanges(t *testing.T) {
	t.Parallel()
	bin := buildZenForMigrate(t)
	source := filepath.Join(fixturePath(t, "01-simple-skill"), "input")
	tmp := t.TempDir()
	pluginRoot := filepath.Join(tmp, "plugin", "zen-swarm")

	cmd := exec.Command(bin, "migrate", "claude-code",
		"--source", source,
		"--target-hermes", pluginRoot,
		"--target-config", filepath.Join(tmp, "hermes", "config.yaml"),
		"--target-zen-config", filepath.Join(tmp, "zen-config"),
		"--dry-run")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dry-run: %v\n%s", err, out)
	}

	entries, err := os.ReadDir(pluginRoot)
	if err == nil && len(entries) > 0 {
		t.Errorf("dry-run wrote files to target: %v", entries)
	}
}

func runMigrate(bin, source, target string, applyPlan string) error {
	args := []string{"migrate", "claude-code",
		"--target-hermes", filepath.Join(target, "plugin", "zen-swarm"),
		"--target-config", filepath.Join(target, "hermes", "config.yaml"),
		"--target-zen-config", filepath.Join(target, "zen-config"),
		"--force",
	}
	if applyPlan != "" {
		args = append(args, "--apply-plan", applyPlan)
	} else {
		args = append(args, "--source", source)
	}
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New(string(out))
	}
	return nil
}

func runMigratePlanOutput(bin, source, planPath string) error {
	cmd := exec.Command(bin, "migrate", "claude-code",
		"--source", source,
		"--plan-output", planPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New(string(out))
	}
	return nil
}

func dirsEqual(t *testing.T, a, b string) bool {
	t.Helper()
	aFiles, bFiles := map[string][]byte{}, map[string][]byte{}
	collect := func(root string, out map[string][]byte) error {
		return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			b, _ := os.ReadFile(path)
			out[rel] = b
			return nil
		})
	}
	if err := collect(a, aFiles); err != nil {
		return false
	}
	if err := collect(b, bFiles); err != nil {
		return false
	}
	if len(aFiles) != len(bFiles) {
		return false
	}
	for k, v := range aFiles {
		w, ok := bFiles[k]
		if !ok || !bytes.Equal(v, w) {
			return false
		}
	}
	return true
}
