// tests/integration/plugin_lifecycle/makefile_plugin_test.go
//go:build integration

package plugin_lifecycle_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMakePluginBuildsPosterBinary(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	posterBin := filepath.Join(root, "plugin", "hades", "bin", "zen-event-poster")

	_ = os.Remove(posterBin)

	cmd := exec.Command("make", "plugin")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make plugin failed: %v\noutput: %s", err, out)
	}

	info, err := os.Stat(posterBin)
	if err != nil {
		t.Fatalf("expected binary not produced at %s: %v\nmake output: %s", posterBin, err, out)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("binary at %s is not executable (mode=%v)", posterBin, info.Mode())
	}
}

func repoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
