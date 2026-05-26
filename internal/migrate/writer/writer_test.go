package writer

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/migrate/mapping"
)

func TestApply_PublicAPI(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	plan := &mapping.Plan{
		SchemaVersion: "1.0",
		Preset:        mapping.PresetLenient,
		Entries: []mapping.PlanEntry{
			{
				Kind:         mapping.EntryKindSkill,
				SourcePath:   "/x/skills/research-cheap/SKILL.md",
				TargetPath:   "plugin/zen-swarm/skills/research-cheap/SKILL.md",
				BodyBytes:    []byte("# research-cheap\nbody"),
				Frontmatter:  map[string]string{"name": "research-cheap", "description": "cheap helper", "version": "0.0.1", "license": "imported", "keywords": "research, cheap"},
				RegisterCall: `ctx.register_skill("research-cheap", "skills/research-cheap/SKILL.md", description="cheap helper")`,
			},
		},
	}
	w := New(WriterConfig{
		HermesPluginRoot: filepath.Join(tmp, "plugin", "zen-swarm"),
		ZenConfigRoot:    filepath.Join(tmp, "config"),
		HermesConfigPath: filepath.Join(tmp, "hermes-config.yaml"),
	})
	if err := w.Apply(plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	skillPath := filepath.Join(tmp, "plugin", "zen-swarm", "skills", "research-cheap", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Errorf("skill: %v", err)
	}

	initPath := filepath.Join(tmp, "plugin", "zen-swarm", "__init__.py")
	body, err := os.ReadFile(initPath)
	if err != nil {
		t.Fatalf("read __init__.py: %v", err)
	}
	if !strings.Contains(string(body), `ctx.register_skill("research-cheap"`) {
		t.Errorf("__init__.py missing register_skill: %s", body)
	}

	yamlPath := filepath.Join(tmp, "plugin", "zen-swarm", "plugin.yaml")
	if _, err := os.Stat(yamlPath); err != nil {
		t.Errorf("plugin.yaml: %v", err)
	}
}

func TestApply_NilPlanReturnsNoError(t *testing.T) {
	t.Parallel()
	w := New(WriterConfig{})
	if err := w.Apply(nil); err != nil {
		t.Errorf("Apply(nil): %v", err)
	}
}

func TestApply_BackupBeforeModify_invZen177(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	backupRoot := filepath.Join(tmp, "backups")
	pluginRoot := filepath.Join(tmp, "plugin", "zen-swarm")

	if err := os.MkdirAll(filepath.Join(pluginRoot, "skills", "existing"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, "skills", "existing", "SKILL.md"), []byte("operator-edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := &mapping.Plan{Entries: []mapping.PlanEntry{
		{Kind: mapping.EntryKindSkill, TargetPath: "plugin/zen-swarm/skills/new/SKILL.md", BodyBytes: []byte("# new"), Frontmatter: map[string]string{"name": "new"}},
	}}
	w := New(WriterConfig{
		HermesPluginRoot: pluginRoot,
		BackupRoot:       backupRoot,
		ForceOverwrite:   true,
	})
	if err := w.Apply(plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	entries, err := os.ReadDir(backupRoot)
	if err != nil {
		t.Fatalf("read backup dir: %v", err)
	}
	if len(entries) == 0 {
		t.Errorf("no backup tarball created")
	}

	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("backup %s mode %o, want 0600", e.Name(), info.Mode().Perm())
		}
		if !strings.HasSuffix(e.Name(), ".tar.gz") {
			t.Errorf("backup %s not .tar.gz", e.Name())
		}
	}
}

func TestApply_RefusesNonEmptyWithoutForce(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	pluginRoot := filepath.Join(tmp, "plugin", "zen-swarm")
	skillDir := filepath.Join(pluginRoot, "skills", "new")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "operator.md"), []byte("op"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := &mapping.Plan{Entries: []mapping.PlanEntry{
		{Kind: mapping.EntryKindSkill, TargetPath: "plugin/zen-swarm/skills/new/SKILL.md", BodyBytes: []byte("# new")},
	}}
	w := New(WriterConfig{HermesPluginRoot: pluginRoot, ForceOverwrite: false})
	err := w.Apply(plan)
	if !errors.Is(err, ErrTargetNotEmpty) {
		t.Errorf("err: got %v, want ErrTargetNotEmpty", err)
	}
}

func TestApply_EmptyTargetNoBackup(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	pluginRoot := filepath.Join(tmp, "plugin", "zen-swarm")
	backupRoot := filepath.Join(tmp, "backups")
	plan := &mapping.Plan{Entries: []mapping.PlanEntry{
		{Kind: mapping.EntryKindSkill, TargetPath: "plugin/zen-swarm/skills/new/SKILL.md", BodyBytes: []byte("# new"), Frontmatter: map[string]string{"name": "new"}},
	}}
	w := New(WriterConfig{
		HermesPluginRoot: pluginRoot,
		BackupRoot:       backupRoot,
		ForceOverwrite:   false,
	})
	if err := w.Apply(plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	entries, err := os.ReadDir(backupRoot)
	if err == nil && len(entries) > 0 {
		t.Errorf("unexpected backup tarball: %v", entries)
	}
}

func TestApply_UnknownEntryKind(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	plan := &mapping.Plan{Entries: []mapping.PlanEntry{
		{Kind: mapping.EntryKind("bogus"), TargetPath: "x"},
	}}
	w := New(WriterConfig{HermesPluginRoot: tmp, ForceOverwrite: true})
	err := w.Apply(plan)
	if !errors.Is(err, ErrUnknownEntryKind) {
		t.Errorf("err: got %v, want ErrUnknownEntryKind", err)
	}
}

func TestWritePlugin_Idempotent(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	target := filepath.Join(tmp, "plugin", "zen-swarm")
	manifest := []byte("name: zen-swarm\nversion: 0.0.1\n")
	if err := WritePlugin(target, manifest); err != nil {
		t.Fatal(err)
	}
	if err := WritePlugin(target, manifest); err != nil {
		t.Fatalf("second call: %v", err)
	}

	for _, sub := range []string{"plugin.yaml", "__init__.py", "skills", "commands", "hooks"} {
		if _, err := os.Stat(filepath.Join(target, sub)); err != nil {
			t.Errorf("missing %s: %v", sub, err)
		}
	}
}

func TestWritePlugin_EmptyTargetDirErrors(t *testing.T) {
	t.Parallel()
	err := WritePlugin("", []byte("..."))
	if err == nil {
		t.Errorf("expected error for empty targetDir")
	}
}

func TestApply_AllSurfaceKindsRoundTrip(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	pluginRoot := filepath.Join(tmp, "plugin", "zen-swarm")
	hermesCfg := filepath.Join(tmp, "hermes", "config.yaml")
	zenCfg := filepath.Join(tmp, "config")
	plan := &mapping.Plan{Entries: []mapping.PlanEntry{
		{Kind: mapping.EntryKindSkill, TargetPath: "plugin/zen-swarm/skills/a/SKILL.md", BodyBytes: []byte("# a"), Frontmatter: map[string]string{"name": "a"}},
		{Kind: mapping.EntryKindCommand, TargetPath: "plugin/zen-swarm/commands/hello.py", BodyBytes: []byte("# hello cmd")},
		{Kind: mapping.EntryKindHook, HookEvent: "pre_tool_call", TargetPath: "plugin/zen-swarm/hooks/pre_tool_call.py", BodyBytes: []byte("#!/bin/bash"), Notes: []string{"source-lang=bash"}},
		{Kind: mapping.EntryKindMemory, TargetPath: "projects/p/memory/M.md", BodyBytes: []byte("# mem")},
		{Kind: mapping.EntryKindDoctrine, TargetPath: "doctrines/imported-from-claude-code.toml", BodyBytes: []byte(`{"allow":["Read(*)"],"deny":[],"env":{}}`)},
		{Kind: mapping.EntryKindHermesConfig, TargetPath: "config.yaml", SourcePath: "/x/settings.json"},
		{Kind: mapping.EntryKindMCPServer, TargetPath: "config.yaml#mcp_servers/p"},
	}}
	w := New(WriterConfig{
		HermesPluginRoot: pluginRoot,
		HermesConfigPath: hermesCfg,
		ZenConfigRoot:    zenCfg,
		ForceOverwrite:   true,
	})
	if err := w.Apply(plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	checks := []string{
		filepath.Join(pluginRoot, "skills", "a", "SKILL.md"),
		filepath.Join(pluginRoot, "commands", "hello.py"),
		filepath.Join(pluginRoot, "hooks", "pre_tool_call.py"),
		filepath.Join(zenCfg, "projects", "p", "memory", "M.md"),
		filepath.Join(zenCfg, "doctrines", "imported-from-claude-code.toml"),
		hermesCfg,
		filepath.Join(pluginRoot, "plugin.yaml"),
		filepath.Join(pluginRoot, "__init__.py"),
	}
	for _, p := range checks {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
}

func TestApply_RegisterCallsSorted(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	pluginRoot := filepath.Join(tmp, "plugin", "zen-swarm")
	plan := &mapping.Plan{Entries: []mapping.PlanEntry{
		{Kind: mapping.EntryKindSkill, TargetPath: "plugin/zen-swarm/skills/z/SKILL.md", BodyBytes: []byte("# z"), Frontmatter: map[string]string{"name": "z"}, RegisterCall: `ctx.register_skill("z", "skills/z/SKILL.md", description="z")`},
		{Kind: mapping.EntryKindSkill, TargetPath: "plugin/zen-swarm/skills/a/SKILL.md", BodyBytes: []byte("# a"), Frontmatter: map[string]string{"name": "a"}, RegisterCall: `ctx.register_skill("a", "skills/a/SKILL.md", description="a")`},
	}}
	w := New(WriterConfig{HermesPluginRoot: pluginRoot, ForceOverwrite: true})
	if err := w.Apply(plan); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(pluginRoot, "__init__.py"))
	if err != nil {
		t.Fatal(err)
	}

	idxA := strings.Index(string(body), `register_skill("a"`)
	idxZ := strings.Index(string(body), `register_skill("z"`)
	if idxA == -1 || idxZ == -1 {
		t.Fatalf("missing one of register calls: %s", body)
	}
	if idxA >= idxZ {
		t.Errorf("not sorted lex: %s", body)
	}
}

func TestStripPluginPrefix_HadesAndLegacy(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input string
		want  string
	}{

		{"plugin/hades/skills/foo/SKILL.md", "skills/foo/SKILL.md"},

		{"plugin/zen-swarm/skills/foo/SKILL.md", "skills/foo/SKILL.md"},

		{"skills/foo/SKILL.md", "skills/foo/SKILL.md"},

		{"some/other/path", "some/other/path"},
	}
	for _, tc := range cases {
		got := stripPluginPrefix(tc.input)
		if got != tc.want {
			t.Errorf("stripPluginPrefix(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestRegisterCallsSortedHelper(t *testing.T) {
	t.Parallel()
	w := &Writer{registerCalls: []string{"zeta", "alpha"}}
	sorted := w.registerCallsSorted()
	if len(sorted) != 2 || sorted[0] != "alpha" || sorted[1] != "zeta" {
		t.Errorf("sort: %v", sorted)
	}
}
