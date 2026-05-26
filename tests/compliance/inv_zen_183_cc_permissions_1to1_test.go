package compliance

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/migrate/mapping"
	"github.com/cbip-solutions/hades-system/internal/migrate/source"
	"github.com/cbip-solutions/hades-system/internal/migrate/writer"
)

func TestInvZen183_CCPermissionsOneToOne(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	settingsBody := `{
		"permissions": {
			"allow": [
				"Read(*)",
				"Bash(make:*)",
				"WebFetch(domain:github.com)",
				"Read(/home/op/special/path)",
				"playwright.browse(https://example.com)"
			],
			"deny": [
				"Write(.env)",
				"Bash(sudo:*)",
				"filesystem.write(/etc/passwd)"
			]
		},
		"env": {"FOO": "bar"}
	}`
	if err := os.WriteFile(filepath.Join(tmp, "settings.json"), []byte(settingsBody), 0o600); err != nil {
		t.Fatal(err)
	}
	inv, err := source.ReadAll(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Settings == nil {
		t.Fatal("Settings missing")
	}
	plan, err := mapping.Map(inv, mapping.PresetLenient)
	if err != nil {
		t.Fatal(err)
	}
	zenRoot := filepath.Join(tmp, "zen-config")
	w := writer.New(writer.WriterConfig{ZenConfigRoot: zenRoot, ForceOverwrite: true})
	if err := w.Apply(plan); err != nil {
		t.Fatal(err)
	}
	tomlPath := filepath.Join(zenRoot, "doctrines", "imported-from-claude-code.toml")
	body, err := os.ReadFile(tomlPath)
	if err != nil {
		t.Fatalf("read toml: %v", err)
	}
	s := string(body)

	all := append([]string{}, inv.Settings.Permissions.Allow...)
	all = append(all, inv.Settings.Permissions.Deny...)
	for _, perm := range all {
		quoted := `"` + perm + `"`
		if !strings.Contains(s, quoted) {
			t.Errorf("inv-zen-183 violation: permission %q dropped from doctrine TOML\nTOML:\n%s", perm, s)
		}
	}

	if !strings.Contains(s, `FOO = "bar"`) {
		t.Errorf("env FOO dropped from output: %s", s)
	}
}

func TestInvZen183_StrictModeHaltsOnUnmappableTool(t *testing.T) {
	t.Parallel()
	err := writer.ImportDoctrineStrict([]string{"completely.alien.opcode"}, nil)
	if !errors.Is(err, writer.ErrUnknownPermissionStrict) {
		t.Errorf("strict-mode halt: got %v, want ErrUnknownPermissionStrict", err)
	}
}

func TestInvZen183_StrictModeAcceptsAllKnownPrefixes(t *testing.T) {
	t.Parallel()
	allow := []string{
		"Read(*)",
		"Bash(make:*)",
		"WebFetch(domain:github.com)",
		"playwright.browse",
		"memory.recall",
		"sequential_thinking",
	}
	deny := []string{
		"Write(.env)",
		"filesystem.write(/etc)",
		"postgres.execute(*)",
		"github.write",
		"mysql.write",
	}
	if err := writer.ImportDoctrineStrict(allow, deny); err != nil {
		t.Errorf("strict mode rejected known prefixes: %v", err)
	}
}
