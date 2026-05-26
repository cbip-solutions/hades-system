package preflight

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPluginFormatCleanPasses(t *testing.T) {
	tmp := t.TempDir()
	c := &PluginFormatCheck{roots: []string{tmp}}
	r := c.Run(context.Background())
	if r.Status != StatusPass {
		t.Errorf("Clean tmp dir: Status = %v, want StatusPass; details=%s", r.Status, r.Details)
	}
	if r.ExitCode != 0 {
		t.Errorf("Clean tmp dir: ExitCode = %d, want 0", r.ExitCode)
	}
}

func TestPluginFormatDetectsCCSettingsJson(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &PluginFormatCheck{roots: []string{tmp}}
	r := c.Run(context.Background())
	if r.Status != StatusFail {
		t.Errorf("CC settings.json: Status = %v, want StatusFail", r.Status)
	}
	if r.ExitCode != 3 {
		t.Errorf("CC settings.json: ExitCode = %d, want 3", r.ExitCode)
	}
}

func TestPluginFormatDetectsCCRemnant(t *testing.T) {
	tmp := t.TempDir()
	skills := filepath.Join(tmp, "skills")
	if err := os.MkdirAll(skills, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skills, "test.md"), []byte("---\nname: x\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &PluginFormatCheck{roots: []string{tmp}}
	r := c.Run(context.Background())
	if r.Status != StatusFail {
		t.Errorf("CC remnant: Status = %v, want StatusFail; details=%s", r.Status, r.Details)
	}
	if r.ExitCode != 3 {
		t.Errorf("CC remnant: ExitCode = %d, want 3", r.ExitCode)
	}
	if r.RemediationHint == "" {
		t.Errorf("CC remnant: RemediationHint empty; expected migration hint")
	}
}

func TestPluginFormatDetectsCCCommandsDir(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &PluginFormatCheck{roots: []string{tmp}}
	r := c.Run(context.Background())
	if r.Status != StatusFail {
		t.Errorf("CC commands dir: Status = %v, want StatusFail", r.Status)
	}
}

func TestPluginFormatDetectsCCHooksDir(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &PluginFormatCheck{roots: []string{tmp}}
	r := c.Run(context.Background())
	if r.Status != StatusFail {
		t.Errorf("CC hooks dir: Status = %v, want StatusFail", r.Status)
	}
}

func TestPluginFormatDetectsCCMemoryDir(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &PluginFormatCheck{roots: []string{tmp}}
	r := c.Run(context.Background())
	if r.Status != StatusFail {
		t.Errorf("CC memory dir: Status = %v, want StatusFail", r.Status)
	}
}

func TestPluginFormatDetectsCCPluginShape(t *testing.T) {
	tmp := t.TempDir()
	pluginDir := filepath.Join(tmp, "some-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.js"), []byte("// CC plugin"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &PluginFormatCheck{roots: []string{tmp}}
	r := c.Run(context.Background())
	if r.Status != StatusFail {
		t.Errorf("CC plugin.js: Status = %v, want StatusFail; summary=%s", r.Status, r.Summary)
	}
}

func TestPluginFormatDetectsCCManifestJson(t *testing.T) {
	tmp := t.TempDir()
	pluginDir := filepath.Join(tmp, "another-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &PluginFormatCheck{roots: []string{tmp}}
	r := c.Run(context.Background())
	if r.Status != StatusFail {
		t.Errorf("CC manifest.json: Status = %v, want StatusFail", r.Status)
	}
}

func TestPluginFormatDetectsCCHooksJson(t *testing.T) {
	tmp := t.TempDir()
	pluginDir := filepath.Join(tmp, "plg")
	hooksDir := filepath.Join(pluginDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooksDir, "hooks.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &PluginFormatCheck{roots: []string{tmp}}
	r := c.Run(context.Background())
	if r.Status != StatusFail {
		t.Errorf("CC hooks/hooks.json: Status = %v, want StatusFail", r.Status)
	}
}

func TestPluginFormatDetectsCCNestedPlugins(t *testing.T) {

	tmp := t.TempDir()
	pluginDir := filepath.Join(tmp, "plugins", "myplugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.js"), []byte("// CC"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &PluginFormatCheck{roots: []string{tmp}}
	r := c.Run(context.Background())
	if r.Status != StatusFail {
		t.Errorf("CC nested plugins: Status = %v, want StatusFail", r.Status)
	}
}

func TestPluginFormatDetectsCCNestedManifest(t *testing.T) {
	tmp := t.TempDir()
	pluginDir := filepath.Join(tmp, "plugins", "p2")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &PluginFormatCheck{roots: []string{tmp}}
	r := c.Run(context.Background())
	if r.Status != StatusFail {
		t.Errorf("CC nested manifest.json: Status = %v, want StatusFail", r.Status)
	}
}

func TestPluginFormatDetectsCCNestedHooksJson(t *testing.T) {
	tmp := t.TempDir()
	hooksDir := filepath.Join(tmp, "plugins", "p3", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooksDir, "hooks.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &PluginFormatCheck{roots: []string{tmp}}
	r := c.Run(context.Background())
	if r.Status != StatusFail {
		t.Errorf("CC nested hooks.json: Status = %v, want StatusFail", r.Status)
	}
}

func TestPluginFormatDetectsOpenClaudeRemnant(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "openclaude.toml"), []byte("name = \"x\""), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &PluginFormatCheck{roots: []string{tmp}}
	r := c.Run(context.Background())
	if r.Status != StatusFail {
		t.Errorf("OpenClaude remnant: Status = %v, want StatusFail; details=%s", r.Status, r.Details)
	}
	if r.RemediationHint == "" {
		t.Error("OpenClaude remnant: RemediationHint empty")
	}
}

func TestPluginFormatDetectsOpenClaudeDir(t *testing.T) {

	tmp := t.TempDir()
	ocDir := filepath.Join(tmp, ".openclaude")
	if err := os.MkdirAll(ocDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ocDir, "plugin.toml"), []byte("name = \"x\""), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &PluginFormatCheck{roots: []string{ocDir}}
	r := c.Run(context.Background())
	if r.Status != StatusFail {
		t.Errorf("OpenClaude dir: Status = %v, want StatusFail", r.Status)
	}
}

func TestPluginFormatEmptyOpenClaudeDir(t *testing.T) {

	tmp := t.TempDir()
	ocDir := filepath.Join(tmp, ".openclaude")
	if err := os.MkdirAll(ocDir, 0o755); err != nil {
		t.Fatal(err)
	}
	c := &PluginFormatCheck{roots: []string{ocDir}}
	r := c.Run(context.Background())
	if r.Status != StatusPass {
		t.Errorf("Empty .openclaude/: Status = %v, want StatusPass", r.Status)
	}
}

func TestPluginFormatMissingRootsArePassed(t *testing.T) {

	c := &PluginFormatCheck{roots: []string{
		"/tmp/zen-swarm-preflight-test-does-not-exist-abc123",
		"/tmp/zen-swarm-preflight-test-does-not-exist-def456",
	}}
	r := c.Run(context.Background())
	if r.Status != StatusPass {
		t.Errorf("Missing roots: Status = %v, want StatusPass (skip missing)", r.Status)
	}
}

func TestPluginFormatHintForKind(t *testing.T) {
	if hintForKind("claude-code") == "" {
		t.Error("hintForKind(claude-code) empty")
	}
	if hintForKind("openclaude") == "" {
		t.Error("hintForKind(openclaude) empty")
	}
	if hintForKind("unknown") == "" {
		t.Error("hintForKind(unknown) empty; should default to generic hint")
	}
}

func TestPluginFormatScanForRemnantsEmptyRoot(t *testing.T) {
	found, kind, evidence := scanForRemnants("")
	if found || kind != "" || evidence != "" {
		t.Errorf("scanForRemnants(empty): got found=%v kind=%q evidence=%q; want all empty", found, kind, evidence)
	}
}

func TestPluginFormatScanForRemnantsFile(t *testing.T) {

	tmp := t.TempDir()
	file := filepath.Join(tmp, "not-a-dir.txt")
	if err := os.WriteFile(file, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	found, _, _ := scanForRemnants(file)
	if found {
		t.Error("scanForRemnants(file): got found=true, want false (must be a dir)")
	}
}

func TestPluginFormatProductionConstructor(t *testing.T) {
	c := NewPluginFormatCheck()
	if c == nil {
		t.Fatal("NewPluginFormatCheck returned nil")
	}
	if c.Name() != "plugin_format" {
		t.Errorf("Name = %q, want plugin_format", c.Name())
	}
	if len(c.roots) == 0 {
		t.Error("production roots empty")
	}

	_ = c.Run(context.Background())
}
