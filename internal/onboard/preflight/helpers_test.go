package preflight_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/onboard/preflight"
)

func TestCCDetectPresent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("setup mkdir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("setup write settings.json: %v", err)
	}
	present, root, err := preflight.CCDetect()
	if err != nil {
		t.Fatalf("CCDetect: %v", err)
	}
	if !present {
		t.Errorf("expected present=true; got %v", present)
	}
	if root != claudeDir {
		t.Errorf("configRoot mismatch: got %q, want %q", root, claudeDir)
	}
}

func TestCCDetectPresentViaCommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(filepath.Join(claudeDir, "commands"), 0o755); err != nil {
		t.Fatalf("setup mkdir commands: %v", err)
	}
	present, root, err := preflight.CCDetect()
	if err != nil {
		t.Fatalf("CCDetect: %v", err)
	}
	if !present {
		t.Errorf("expected present=true for commands/ child; got false")
	}
	if root != claudeDir {
		t.Errorf("configRoot mismatch: %q", root)
	}
}

func TestCCDetectPresentViaSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(filepath.Join(claudeDir, "skills"), 0o755); err != nil {
		t.Fatalf("setup mkdir skills: %v", err)
	}
	present, root, err := preflight.CCDetect()
	if err != nil {
		t.Fatalf("CCDetect: %v", err)
	}
	if !present {
		t.Errorf("expected present=true for skills/ child")
	}
	if root != claudeDir {
		t.Errorf("configRoot mismatch: %q", root)
	}
}

func TestCCDetectAbsent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	present, _, err := preflight.CCDetect()
	if err != nil {
		t.Fatalf("CCDetect absent: %v", err)
	}
	if present {
		t.Errorf("expected absent; got present=true")
	}
}

func TestCCDetectExistsButEmpty(t *testing.T) {

	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	present, _, err := preflight.CCDetect()
	if err != nil {
		t.Fatalf("CCDetect empty: %v", err)
	}
	if present {
		t.Errorf("expected present=false for empty ~/.claude/; got true")
	}
}

func TestCCDetectFileInsteadOfDir(t *testing.T) {

	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, ".claude"), []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	present, root, err := preflight.CCDetect()
	if err != nil {
		t.Fatalf("CCDetect file: %v", err)
	}
	if present {
		t.Errorf("expected present=false for ~/.claude file; got true (root=%q)", root)
	}
}

func TestCheckHermesInstalledErrorWhenAbsent(t *testing.T) {

	t.Setenv("PATH", "")
	if err := preflight.CheckHermesInstalled(context.Background()); err == nil {
		t.Errorf("expected error when hermes absent; got nil")
	}
}

func TestCheckBashInstalledOK(t *testing.T) {
	// Do not touch PATH here; bash is on the host PATH.
	if err := preflight.CheckBashInstalled(context.Background()); err != nil {
		t.Errorf("CheckBashInstalled: want nil on host (bash should be present), got %v", err)
	}
}

func TestCheckBashInstalledMissing(t *testing.T) {
	emptyDir := t.TempDir()
	t.Setenv("PATH", emptyDir)
	err := preflight.CheckBashInstalled(context.Background())
	if err == nil {
		t.Fatal("CheckBashInstalled: want error when PATH is empty, got nil")
	}
	if !strings.Contains(err.Error(), "bash") {
		t.Errorf("error %q does not mention 'bash'", err.Error())
	}
}

func TestCheckPluginFormatRemnantsCleanDir(t *testing.T) {
	tmp := t.TempDir()
	if err := preflight.CheckPluginFormatRemnants(context.Background(), tmp); err != nil {
		t.Errorf("clean dir should be nil; got %v", err)
	}
}

func TestCheckPluginFormatRemnantsDetectsCC(t *testing.T) {

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := preflight.CheckPluginFormatRemnants(context.Background(), tmp); err == nil {
		t.Error("expected error for CC remnant; got nil")
	}
}

func TestCheckPluginFormatRemnantsSkipsAfterMigration(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)

	artifactDir := filepath.Join(xdg, "zen-swarm", "doctrines")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "imported-from-claude-code.toml"), []byte("# imported\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := preflight.CheckPluginFormatRemnants(context.Background(), tmp); err != nil {
		t.Errorf("with migration artifact present, CheckPluginFormatRemnants must pass; got %v", err)
	}
}

func TestCheckPluginFormatRemnantsDefaultsWhenNoDirs(t *testing.T) {

	home := t.TempDir()
	t.Setenv("HOME", home)
	emptyCwd := t.TempDir()
	prevWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(emptyCwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWd) })
	if err := preflight.CheckPluginFormatRemnants(context.Background()); err != nil {
		t.Errorf("default roots on clean env: got %v, want nil", err)
	}
}

func TestHermesCheckMissingBinaryReturnsOkFalse(t *testing.T) {
	t.Setenv("PATH", "")
	ok, version, err := preflight.HermesCheck(context.Background())
	if err != nil {
		t.Errorf("HermesCheck missing: err = %v, want nil", err)
	}
	if ok {
		t.Errorf("HermesCheck missing: ok = true, want false")
	}
	if version != "" {
		t.Errorf("HermesCheck missing: version = %q, want empty", version)
	}
}

func TestHermesVersionMissingBinary(t *testing.T) {
	t.Setenv("PATH", "")
	v, err := preflight.HermesVersion()
	if err == nil {
		t.Errorf("HermesVersion missing: err = nil, want non-nil; got version=%+v", v)
	}
	if v != nil {
		t.Errorf("HermesVersion missing: v = %+v, want nil", v)
	}
}
