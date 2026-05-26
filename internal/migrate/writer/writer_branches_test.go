package writer

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/migrate/mapping"
)

func TestRouteTarget_AllKinds(t *testing.T) {
	t.Parallel()
	w := New(WriterConfig{
		HermesPluginRoot: "/p",
		HermesConfigPath: "/h/config.yaml",
		ZenConfigRoot:    "/z",
	})
	cases := []struct {
		kind     mapping.EntryKind
		wantRoot string
		joinAsIs bool
	}{
		{mapping.EntryKindSkill, "/p", true},
		{mapping.EntryKindCommand, "/p", true},
		{mapping.EntryKindHook, "/p", true},
		{mapping.EntryKindHermesConfig, "/h", false},
		{mapping.EntryKindDoctrine, "/z", true},
		{mapping.EntryKindMemory, "/z", true},
		{mapping.EntryKindMCPServer, "/h", false},
	}
	for _, c := range cases {
		gotRoot, gotJoin, err := w.routeTarget(mapping.PlanEntry{Kind: c.kind})
		if err != nil {
			t.Errorf("%s: err %v", c.kind, err)
		}
		if gotRoot != c.wantRoot || gotJoin != c.joinAsIs {
			t.Errorf("%s: got (%q,%v), want (%q,%v)", c.kind, gotRoot, gotJoin, c.wantRoot, c.joinAsIs)
		}
	}
}

func TestRouteTarget_UnknownKind(t *testing.T) {
	t.Parallel()
	w := New(WriterConfig{})
	_, _, err := w.routeTarget(mapping.PlanEntry{Kind: mapping.EntryKind("alien")})
	if !errors.Is(err, ErrUnknownEntryKind) {
		t.Errorf("err: got %v, want ErrUnknownEntryKind", err)
	}
}

func TestRouteJoined_EmptyRoot(t *testing.T) {
	t.Parallel()
	w := New(WriterConfig{HermesPluginRoot: ""})
	_, err := w.routeJoined(mapping.PlanEntry{Kind: mapping.EntryKindSkill, TargetPath: "plugin/zen-swarm/skills/a/SKILL.md"})
	if !errors.Is(err, ErrUnsupportedTarget) {
		t.Errorf("err: got %v, want ErrUnsupportedTarget", err)
	}
}

func TestRouteJoined_NotJoinAsIs(t *testing.T) {
	t.Parallel()
	w := New(WriterConfig{HermesConfigPath: "/h/config.yaml"})
	_, err := w.routeJoined(mapping.PlanEntry{Kind: mapping.EntryKindHermesConfig, TargetPath: "config.yaml"})
	if !errors.Is(err, ErrUnsupportedTarget) {
		t.Errorf("err: got %v, want ErrUnsupportedTarget", err)
	}
}

func TestApply_HermesConfigSkippedWhenUnconfigured(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	plan := &mapping.Plan{Entries: []mapping.PlanEntry{
		{Kind: mapping.EntryKindHermesConfig, SourcePath: "/x/settings.json", TargetPath: "config.yaml"},
	}}
	w := New(WriterConfig{
		HermesPluginRoot: filepath.Join(tmp, "plugin"),

		ForceOverwrite: true,
	})
	if err := w.Apply(plan); err != nil {
		t.Errorf("Apply should silently skip unconfigured hermes config: %v", err)
	}
}

func TestApply_DoctrineSkippedWhenUnconfigured(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	plan := &mapping.Plan{Entries: []mapping.PlanEntry{
		{Kind: mapping.EntryKindDoctrine, BodyBytes: []byte(`{"allow":[],"deny":[],"env":{}}`), TargetPath: "doctrines/x.toml"},
	}}
	w := New(WriterConfig{
		HermesPluginRoot: filepath.Join(tmp, "plugin"),

		ForceOverwrite: true,
	})
	if err := w.Apply(plan); err != nil {
		t.Errorf("Apply should silently skip unconfigured zen config: %v", err)
	}
}

func TestApply_MemorySkippedWhenUnconfigured(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	plan := &mapping.Plan{Entries: []mapping.PlanEntry{
		{Kind: mapping.EntryKindMemory, BodyBytes: []byte("# mem"), TargetPath: "projects/p/memory/M.md"},
	}}
	w := New(WriterConfig{
		HermesPluginRoot: filepath.Join(tmp, "plugin"),
		ForceOverwrite:   true,
	})
	if err := w.Apply(plan); err != nil {
		t.Errorf("Apply should silently skip unconfigured zen config (memory): %v", err)
	}
}

func TestApply_RouteTargetUnknownKindError(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	plan := &mapping.Plan{Entries: []mapping.PlanEntry{
		{Kind: mapping.EntryKindSkill, TargetPath: "plugin/zen-swarm/skills/a/SKILL.md", BodyBytes: []byte("# a"), Frontmatter: map[string]string{"name": "a"}},
		{Kind: mapping.EntryKind("alien"), TargetPath: "x"},
	}}
	w := New(WriterConfig{HermesPluginRoot: filepath.Join(tmp, "plugin"), ForceOverwrite: true})
	err := w.Apply(plan)
	if !errors.Is(err, ErrUnknownEntryKind) {
		t.Errorf("err: got %v, want ErrUnknownEntryKind", err)
	}
}

func TestApply_PreflightRouteTargetError(t *testing.T) {
	t.Parallel()
	plan := &mapping.Plan{Entries: []mapping.PlanEntry{
		{Kind: mapping.EntryKind("alien"), TargetPath: "x"},
	}}
	w := New(WriterConfig{})
	err := w.Apply(plan)
	if err == nil {
		t.Errorf("expected error for unknown kind")
	}
}

func TestStripPluginPrefix_Behavior(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"plugin/zen-swarm/skills/a/SKILL.md", "skills/a/SKILL.md"},
		{"skills/a/SKILL.md", "skills/a/SKILL.md"},
		{"", ""},
		{"plugin/zen-swarm/", ""},
	}
	for _, c := range cases {
		if got := stripPluginPrefix(c.in); got != c.want {
			t.Errorf("%q: got %q, want %q", c.in, got, c.want)
		}
	}
}

func TestApply_MCPServerIsNoop(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	pluginRoot := filepath.Join(tmp, "plugin", "zen-swarm")
	plan := &mapping.Plan{Entries: []mapping.PlanEntry{
		{Kind: mapping.EntryKindMCPServer, TargetPath: "config.yaml#mcp_servers/p"},
	}}
	w := New(WriterConfig{
		HermesPluginRoot: pluginRoot,
		HermesConfigPath: filepath.Join(tmp, "hermes", "config.yaml"),
		ForceOverwrite:   true,
	})
	if err := w.Apply(plan); err != nil {
		t.Errorf("Apply: %v", err)
	}
}

func TestWritePlugin_OverwritesExisting(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	target := filepath.Join(tmp, "plugin", "zen-swarm")
	if err := WritePlugin(target, []byte("name: v1\n")); err != nil {
		t.Fatal(err)
	}
	if err := WritePlugin(target, []byte("name: v2\n")); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(target, "plugin.yaml"))
	if !strings.Contains(string(body), "name: v2") {
		t.Errorf("manifest not overwritten: %s", body)
	}
}

func TestAtomicWriteFile_TempCleanupOnWriteSuccess(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "f.txt")
	if err := atomicWriteFile(path, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(tmp)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("leaked tmp file: %s", e.Name())
		}
	}
}
