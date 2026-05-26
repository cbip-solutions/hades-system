package compliance

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/onboard/preflight"
)

func isolateMigrationArtifact(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

func TestInvZen176PluginFormatRemnantHalts(t *testing.T) {
	isolateMigrationArtifact(t)
	tmp := t.TempDir()
	skills := filepath.Join(tmp, "skills")
	if err := os.MkdirAll(skills, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skills, "x.md"), []byte("---\nname: x\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := preflight.NewPluginFormatCheckForTest([]string{tmp})
	r := c.Run(context.Background())
	if r.Status != preflight.StatusFail {
		t.Fatalf("inv-zen-176: CC remnant did not halt: %+v", r)
	}
	if r.ExitCode != 3 {
		t.Errorf("inv-zen-176: ExitCode = %d, want 3 (preflight halt)", r.ExitCode)
	}
	if r.RemediationHint == "" {
		t.Errorf("inv-zen-176: RemediationHint empty; operator-facing migration hint required")
	}
}

func TestInvZen176CCPluginShape(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, root string)
	}{
		{
			name: "plugin.js",
			setup: func(t *testing.T, root string) {
				dir := filepath.Join(root, "myplugin")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "plugin.js"), []byte("// cc"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "manifest.json",
			setup: func(t *testing.T, root string) {
				dir := filepath.Join(root, "plugin-2")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("{}"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "hooks/hooks.json",
			setup: func(t *testing.T, root string) {
				dir := filepath.Join(root, "plg-3", "hooks")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "hooks.json"), []byte("{}"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateMigrationArtifact(t)
			tmp := t.TempDir()
			tc.setup(t, tmp)
			c := preflight.NewPluginFormatCheckForTest([]string{tmp})
			r := c.Run(context.Background())
			if r.Status != preflight.StatusFail {
				t.Errorf("inv-zen-176 %s: Status = %v, want StatusFail; details=%s", tc.name, r.Status, r.Details)
			}
			if r.ExitCode != 3 {
				t.Errorf("inv-zen-176 %s: ExitCode = %d, want 3", tc.name, r.ExitCode)
			}
		})
	}
}

func TestInvZen176OpenClaudeRemnantHalts(t *testing.T) {
	isolateMigrationArtifact(t)
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "openclaude.toml"), []byte("name = \"x\""), 0o644); err != nil {
		t.Fatal(err)
	}
	c := preflight.NewPluginFormatCheckForTest([]string{tmp})
	r := c.Run(context.Background())
	if r.Status != preflight.StatusFail {
		t.Fatalf("inv-zen-176: OpenClaude remnant did not halt: %+v", r)
	}
	if r.ExitCode != 3 {
		t.Errorf("inv-zen-176: ExitCode = %d, want 3", r.ExitCode)
	}
}

func TestInvZen176CleanDirPasses(t *testing.T) {
	tmp := t.TempDir()
	c := preflight.NewPluginFormatCheckForTest([]string{tmp})
	r := c.Run(context.Background())
	if r.Status != preflight.StatusPass {
		t.Errorf("inv-zen-176: clean dir did not pass: %+v", r)
	}
}
