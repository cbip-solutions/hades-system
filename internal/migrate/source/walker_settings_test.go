package source

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadSettings_ModernHooksSchema(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	body := []byte(`{
		"hooks": {
			"SessionStart": [
				{
					"matcher": "",
					"hooks": [
						{"type": "command", "command": "echo hello"}
					]
				}
			],
			"PostToolUse": [
				{
					"matcher": "Bash",
					"hooks": [
						{"type": "command", "command": "echo post-bash"}
					]
				}
			]
		}
	}`)
	if err := os.WriteFile(filepath.Join(root, "settings.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	inv := &Inventory{}
	if err := readSettings(root, inv); err != nil {
		t.Fatalf("readSettings must accept the modern CC hooks schema: %v", err)
	}
	if inv.Settings == nil {
		t.Fatal("Settings nil")
	}
	got := inv.Settings.Hooks
	if len(got) != 2 {
		t.Fatalf("hooks: got %d events, want 2 (SessionStart, PostToolUse)", len(got))
	}
	ss, ok := got["SessionStart"]
	if !ok || len(ss) != 1 {
		t.Fatalf("SessionStart: got %d matchers, want 1", len(ss))
	}
	if ss[0].Matcher != "" {
		t.Errorf("SessionStart[0].matcher = %q, want empty", ss[0].Matcher)
	}
	if len(ss[0].Hooks) != 1 || ss[0].Hooks[0].Type != "command" || ss[0].Hooks[0].Command != "echo hello" {
		t.Errorf("SessionStart[0].hooks = %+v", ss[0].Hooks)
	}
	ptu := got["PostToolUse"]
	if len(ptu) != 1 || ptu[0].Matcher != "Bash" || len(ptu[0].Hooks) != 1 || ptu[0].Hooks[0].Command != "echo post-bash" {
		t.Errorf("PostToolUse mismatch: %+v", ptu)
	}
}

func TestReadSettings_Wellformed(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	body := []byte(`{"permissions":{"allow":["Read(*)"],"deny":["Write(.env)"]},"model":"opus[1m]","env":{"FOO":"bar"}}`)
	if err := os.WriteFile(filepath.Join(root, "settings.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	inv := &Inventory{}
	if err := readSettings(root, inv); err != nil {
		t.Fatal(err)
	}
	if inv.Settings == nil {
		t.Fatal("Settings nil")
	}
	if len(inv.Settings.Permissions.Allow) != 1 || inv.Settings.Permissions.Allow[0] != "Read(*)" {
		t.Errorf("allow: %v", inv.Settings.Permissions.Allow)
	}
	if len(inv.Settings.Permissions.Deny) != 1 || inv.Settings.Permissions.Deny[0] != "Write(.env)" {
		t.Errorf("deny: %v", inv.Settings.Permissions.Deny)
	}
	if inv.Settings.Model != "opus[1m]" {
		t.Errorf("model: %s", inv.Settings.Model)
	}
	if inv.Settings.Env["FOO"] != "bar" {
		t.Errorf("env: %v", inv.Settings.Env)
	}

	if inv.Settings.Raw == nil {
		t.Errorf("Raw is nil")
	}
	if inv.Settings.Path == "" {
		t.Errorf("Path empty")
	}
}

func TestReadSettings_Malformed(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "settings.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	inv := &Inventory{}
	err := readSettings(root, inv)
	if !errors.Is(err, ErrMalformedSettings) {
		t.Errorf("err: got %v, want ErrMalformedSettings", err)
	}
}

func TestReadSettings_Missing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	inv := &Inventory{}
	if err := readSettings(root, inv); err != nil {
		t.Fatal(err)
	}
	if inv.Settings != nil {
		t.Errorf("Settings: got %v, want nil", inv.Settings)
	}
}

func TestReadSettings_UnknownFieldsPreservedInRaw(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	body := []byte(`{"permissions":{"allow":["Read(*)"]},"unknown_field":"value","another":42}`)
	if err := os.WriteFile(filepath.Join(root, "settings.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	inv := &Inventory{}
	if err := readSettings(root, inv); err != nil {
		t.Fatal(err)
	}
	if inv.Settings == nil {
		t.Fatal("Settings nil")
	}
	if _, ok := inv.Settings.Raw["unknown_field"]; !ok {
		t.Errorf("unknown_field missing from Raw: %v", inv.Settings.Raw)
	}
	if _, ok := inv.Settings.Raw["another"]; !ok {
		t.Errorf("another missing from Raw: %v", inv.Settings.Raw)
	}
}
