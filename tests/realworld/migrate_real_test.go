// go:build realworld
//go:build realworld
// +build realworld

package realworld_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrateRealworld_RealisticTreeScale(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	source := filepath.Join(tmp, "claude-source")
	if err := generateRealisticTree(source, 50, 12, 3, 8); err != nil {
		t.Fatalf("generate: %v", err)
	}

	bin := buildZenForMigrateRealworld(t)
	target := filepath.Join(tmp, "target")
	start := time.Now()
	cmd := exec.Command(bin, "migrate", "claude-code",
		"--source", source,
		"--target-hermes", filepath.Join(target, "plugin", "zen-swarm"),
		"--target-config", filepath.Join(target, "hermes", "config.yaml"),
		"--target-zen-config", filepath.Join(target, "zen-config"),
		"--force")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("migrate: %v\n%s", err, out)
	}
	elapsed := time.Since(start)
	if elapsed > 30*time.Second {
		t.Errorf("migration took %v; expected <30s for realistic load", elapsed)
	}

	skillDir := filepath.Join(target, "plugin", "zen-swarm", "skills")
	entries, err := os.ReadDir(skillDir)
	if err != nil {
		t.Fatalf("read skill dir: %v", err)
	}
	if len(entries) != 50 {
		t.Errorf("got %d skill dirs, want 50", len(entries))
	}
}

func generateRealisticTree(root string, nSkills, nCommands, nHooks, nProjects int) error {
	for i := 0; i < nSkills; i++ {
		dir := filepath.Join(root, "skills", fmt.Sprintf("skill-%02d", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		body := fmt.Sprintf("# skill-%02d\n\nThis is skill %d body content.\n", i, i)
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "commands"), 0o755); err != nil {
		return err
	}
	for i := 0; i < nCommands; i++ {
		path := filepath.Join(root, "commands", fmt.Sprintf("cmd-%02d.md", i))
		body := fmt.Sprintf("# /cmd-%02d\n\nCommand %d body.\n", i, i)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "hooks"), 0o755); err != nil {
		return err
	}
	hookEvents := []string{"tool.execute.before", "tool.execute.after", "session.created"}
	for i := 0; i < nHooks; i++ {
		if i >= len(hookEvents) {
			break
		}
		path := filepath.Join(root, "hooks", hookEvents[i]+".sh")
		body := "#!/bin/bash\necho hook " + fmt.Sprintf("%d", i)
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			return err
		}
	}
	for i := 0; i < nProjects; i++ {
		dir := filepath.Join(root, "projects", fmt.Sprintf("proj-%02d", i), "memory")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		body := fmt.Sprintf("# memory for proj-%02d\n\nProject %d notes.\n", i, i)
		if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func buildZenForMigrateRealworld(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "zen")
	cwd, _ := os.Getwd()
	root, _ := filepath.Abs(filepath.Join(cwd, "..", ".."))
	cmd := exec.Command("go", "build", "-tags=sqlite_fts5", "-o", out, "./cmd/zen")
	cmd.Dir = root
	if buildOut, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build zen: %v\n%s", err, buildOut)
	}
	return out
}
