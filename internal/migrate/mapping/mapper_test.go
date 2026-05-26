package mapping

import (
	"errors"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/migrate/source"
)

func TestMap_PublicAPI(t *testing.T) {
	t.Parallel()
	inv := &source.Inventory{
		Skills: []source.SkillSource{
			{Name: "research-cheap", Path: "/x/skills/research-cheap/SKILL.md", Body: []byte("# research-cheap\nDescribes a cheap research helper.\n")},
		},
	}
	plan, err := Map(inv, PresetLenient)
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	if len(plan.Entries) != 1 {
		t.Errorf("entries: got %d, want 1", len(plan.Entries))
	}
	e := plan.Entries[0]
	if e.Kind != EntryKindSkill {
		t.Errorf("kind: got %s, want skill", e.Kind)
	}
	if e.TargetPath != "plugin/zen-swarm/skills/research-cheap/SKILL.md" {
		t.Errorf("target: %s", e.TargetPath)
	}
	if e.Frontmatter == nil {
		t.Errorf("frontmatter nil")
	}
	if e.Frontmatter["name"] != "research-cheap" {
		t.Errorf("frontmatter.name: %s", e.Frontmatter["name"])
	}
	if !strings.Contains(e.RegisterCall, "register_skill") {
		t.Errorf("register_call: %s", e.RegisterCall)
	}
}

func TestMap_NilInventoryReturnsEmptyPlan(t *testing.T) {
	t.Parallel()
	plan, err := Map(nil, PresetLenient)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Entries) != 0 {
		t.Errorf("entries: got %d, want 0", len(plan.Entries))
	}
	if plan.SchemaVersion != "1.0" {
		t.Errorf("schema: %s", plan.SchemaVersion)
	}
}

func TestMap_StrictHaltsOnUnmappedSettingsField(t *testing.T) {
	t.Parallel()
	inv := &source.Inventory{
		Settings: &source.SettingsSource{
			Path: "/x/settings.json",
			Raw:  map[string]interface{}{"unknown_field": "value"},
		},
	}
	_, err := Map(inv, PresetStrict)
	if !errors.Is(err, ErrUnmappedSurface) {
		t.Errorf("err: got %v, want ErrUnmappedSurface", err)
	}
}

func TestMap_LenientWarnsOnUnmappedSettingsField(t *testing.T) {
	t.Parallel()
	inv := &source.Inventory{
		Settings: &source.SettingsSource{
			Path: "/x/settings.json",
			Raw:  map[string]interface{}{"unknown_field": "value"},
		},
	}
	plan, err := Map(inv, PresetLenient)
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	found := false
	for _, w := range plan.Warnings {
		if strings.Contains(w, "unknown_field") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected warning containing 'unknown_field': %v", plan.Warnings)
	}
}

// TestPostSpike2026_05_16_ApprovalHooksReclassifiedConfirmed asserts the
// spec §8.4 reclassification (live Hermes head 395e9dd9 verified 2026-05-16
// via 13-A0 spike): pre_approval_request + post_approval_response moved
// from risk-flagged → confirmed. Under strict mode they MUST NOT halt and
// MUST be migrated to canonical Hermes hook paths.
//
// Regression guard against spec drift: if these hooks become risk-flagged
// again in a future spec evolution, this test fails loudly so the migrate
// behavior is updated in lockstep with the spec.
func TestPostSpike2026_05_16_ApprovalHooksReclassifiedConfirmed(t *testing.T) {
	t.Parallel()
	inv := &source.Inventory{
		Hooks: []source.HookSource{
			{EventName: "permission.asked", Path: "/x/hooks/permission.asked.sh", Lang: "bash", Body: []byte("#!/bin/bash")},
			{EventName: "permission.replied", Path: "/x/hooks/permission.replied.sh", Lang: "bash", Body: []byte("#!/bin/bash")},
		},
	}
	plan, err := Map(inv, PresetStrict)
	if err != nil {
		t.Fatalf("strict + reclassified-confirmed approval hooks: unexpected halt: %v", err)
	}
	if errors.Is(err, ErrHookRiskFlagged) {
		t.Errorf("ErrHookRiskFlagged must NOT fire post-spike 2026-05-16 (spec §8.4)")
	}
	if len(plan.Entries) != 2 {
		t.Fatalf("entries: got %d, want 2 (both approval hooks mapped)", len(plan.Entries))
	}
	wantTargets := map[string]bool{
		"plugin/zen-swarm/hooks/pre_approval_request.py":   false,
		"plugin/zen-swarm/hooks/post_approval_response.py": false,
	}
	for _, e := range plan.Entries {
		if _, ok := wantTargets[e.TargetPath]; ok {
			wantTargets[e.TargetPath] = true
		}
	}
	for tgt, seen := range wantTargets {
		if !seen {
			t.Errorf("missing target %q in Plan.Entries", tgt)
		}
	}
	// No risk-flagged warning should fire for these two specifically; warnings
	// list may still contain other content (e.g., source-lang notes are in
	// Notes, not Warnings), so we assert NO warning mentions risk-flagged.
	for _, w := range plan.Warnings {
		if strings.Contains(w, "risk-flagged") {
			t.Errorf("unexpected risk-flagged warning: %q", w)
		}
	}
}

// TestMap_LenientMigratesApprovalHooks_PostSpike asserts approval hooks
// migrate cleanly under lenient mode too: present in Plan.Entries, no
// risk-flagged warning.
func TestMap_LenientMigratesApprovalHooks_PostSpike(t *testing.T) {
	t.Parallel()
	inv := &source.Inventory{
		Hooks: []source.HookSource{
			{EventName: "permission.asked", Path: "/x/hooks/permission.asked.sh", Lang: "bash", Body: []byte("#!/bin/bash")},
		},
	}
	plan, err := Map(inv, PresetLenient)
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	if len(plan.Entries) != 1 {
		t.Errorf("entries: got %d, want 1 (approval hook mapped)", len(plan.Entries))
	}
	for _, w := range plan.Warnings {
		if strings.Contains(w, "risk-flagged") {
			t.Errorf("unexpected risk-flagged warning post-spike: %q", w)
		}
	}
}

func TestMap_StrictHaltsOnUnmappedHook(t *testing.T) {
	t.Parallel()
	inv := &source.Inventory{
		Hooks: []source.HookSource{
			{EventName: "completely.fictional", Path: "/x/hooks/x.sh", Lang: "bash", Body: []byte("#!/bin/bash")},
		},
	}
	_, err := Map(inv, PresetStrict)
	if !errors.Is(err, ErrUnmappedSurface) {
		t.Errorf("err: got %v, want ErrUnmappedSurface", err)
	}
}

func TestMap_LenientWarnsOnUnmappedHook(t *testing.T) {
	t.Parallel()
	inv := &source.Inventory{
		Hooks: []source.HookSource{
			{EventName: "completely.fictional", Path: "/x/hooks/x.sh", Lang: "bash", Body: []byte("#!/bin/bash")},
		},
	}
	plan, err := Map(inv, PresetLenient)
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	if len(plan.Entries) != 0 {
		t.Errorf("entries: got %d, want 0 (skip unmapped)", len(plan.Entries))
	}
	if len(plan.Warnings) == 0 {
		t.Errorf("expected warning")
	}
}

func TestMap_InvalidPreset(t *testing.T) {
	t.Parallel()
	_, err := Map(&source.Inventory{}, Preset("garbage"))
	if !errors.Is(err, ErrInvalidPreset) {
		t.Errorf("err: got %v, want ErrInvalidPreset", err)
	}
}

func TestMappingTableCoversAllSurfaces(t *testing.T) {
	t.Parallel()
	covered := map[EntryKind]bool{}
	for _, e := range allTableEntries() {
		covered[e.Kind] = true
	}
	required := []EntryKind{
		EntryKindSkill, EntryKindCommand, EntryKindHook,
		EntryKindDoctrine, EntryKindMemory, EntryKindMCPServer,
		EntryKindHermesConfig,
	}
	for _, k := range required {
		if !covered[k] {
			t.Errorf("EntryKind %s not in mapping table", k)
		}
	}
}

func TestMap_AllSurfacesProduceExpectedEntries(t *testing.T) {
	t.Parallel()
	inv := &source.Inventory{
		Skills: []source.SkillSource{
			{Name: "alpha", Path: "/x/skills/alpha/SKILL.md", Body: []byte("# alpha\nbody")},
		},
		Commands: []source.CommandSource{
			{Name: "hello", Path: "/x/commands/hello.md", Body: []byte("# hello\ndesc")},
		},
		Hooks: []source.HookSource{
			{EventName: "tool.execute.before", Path: "/x/hooks/tool.execute.before.sh", Lang: "bash", Body: []byte("#!/bin/bash")},
		},
		Settings: &source.SettingsSource{
			Path:        "/x/settings.json",
			Permissions: source.PermissionsSource{Allow: []string{"Read(*)"}},
			Model:       "opus[1m]",
			Raw:         map[string]interface{}{"permissions": map[string]interface{}{}, "model": "opus[1m]"},
		},
		MemoryFiles: []source.MemorySource{
			{ProjectSlug: "proj-a", Path: "/x/projects/proj-a/memory/MEMORY.md", Body: []byte("# mem")},
		},
		MCPServers: &source.MCPSource{
			Path: "/x/.mcp.json",
			MCPServers: map[string]source.MCPServer{
				"playwright": {Command: "npx", Args: []string{"@playwright/mcp"}},
			},
		},
	}
	plan, err := Map(inv, PresetLenient)
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	gotKinds := map[EntryKind]int{}
	for _, e := range plan.Entries {
		gotKinds[e.Kind]++
	}
	required := []EntryKind{
		EntryKindSkill, EntryKindCommand, EntryKindHook,
		EntryKindDoctrine, EntryKindHermesConfig, EntryKindMemory, EntryKindMCPServer,
	}
	for _, k := range required {
		if gotKinds[k] == 0 {
			t.Errorf("missing entry kind %s in plan", k)
		}
	}
}

func TestMap_PropagatesInventoryWarnings(t *testing.T) {
	t.Parallel()
	inv := &source.Inventory{
		Warnings: []string{"skills/orphan: missing SKILL.md"},
	}
	plan, err := Map(inv, PresetLenient)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Warnings) == 0 {
		t.Errorf("inventory warnings did not propagate to plan")
	}
}

func TestSerializeHermesConfig_Deterministic(t *testing.T) {
	t.Parallel()
	s := &source.SettingsSource{
		Model: "opus[1m]",
		MCPServers: map[string]source.MCPServer{
			"b": {Command: "b", Args: []string{"x"}},
			"a": {Command: "a", Args: nil},
		},
	}
	got1 := serializeHermesConfig(s)
	got2 := serializeHermesConfig(s)
	if string(got1) != string(got2) {
		t.Errorf("non-deterministic:\n%s\n%s", got1, got2)
	}
}

func TestSerializePermissions_Deterministic(t *testing.T) {
	t.Parallel()
	s := &source.SettingsSource{
		Permissions: source.PermissionsSource{
			Allow: []string{"Read(*)", "Bash(make:*)"},
			Deny:  []string{"Write(.env)"},
		},
		Env: map[string]string{"FOO": "bar"},
	}
	got1 := serializePermissions(s)
	got2 := serializePermissions(s)
	if string(got1) != string(got2) {
		t.Errorf("non-deterministic")
	}
	if !strings.Contains(string(got1), `"Read(*)"`) {
		t.Errorf("allow missing: %s", got1)
	}
}

func TestMap_MCPMapOrderDeterministic(t *testing.T) {
	t.Parallel()
	inv := &source.Inventory{
		MCPServers: &source.MCPSource{
			Path: "/x/.mcp.json",
			MCPServers: map[string]source.MCPServer{
				"zeta":   {Command: "z"},
				"alpha":  {Command: "a"},
				"middle": {Command: "m"},
			},
		},
	}
	plan1, _ := Map(inv, PresetLenient)
	plan2, _ := Map(inv, PresetLenient)
	if len(plan1.Entries) != len(plan2.Entries) {
		t.Fatalf("entry count mismatch: %d vs %d", len(plan1.Entries), len(plan2.Entries))
	}
	for i := range plan1.Entries {
		if plan1.Entries[i].TargetPath != plan2.Entries[i].TargetPath {
			t.Errorf("non-deterministic entry %d: %q vs %q",
				i, plan1.Entries[i].TargetPath, plan2.Entries[i].TargetPath)
		}
	}
}
