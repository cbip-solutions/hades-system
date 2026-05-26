package mapping

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestPlan_MarshalJSON_Deterministic(t *testing.T) {
	t.Parallel()
	p := &Plan{
		SchemaVersion: "1.0",
		Source:        "/x",
		Preset:        PresetLenient,
		Entries: []PlanEntry{
			{Kind: EntryKindSkill, SourcePath: "/x/skills/a/SKILL.md", TargetPath: "plugin/zen-swarm/skills/a/SKILL.md"},
		},
	}
	a, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("non-deterministic marshal:\n%s\n%s", a, b)
	}
}

func TestPlanEntry_BodyBytesNotSerialized(t *testing.T) {
	t.Parallel()
	e := PlanEntry{Kind: EntryKindSkill, BodyBytes: []byte("private payload")}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "private payload") {
		t.Errorf("BodyBytes leaked into JSON: %s", b)
	}
}

func TestPlan_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	orig := &Plan{
		SchemaVersion: "1.0",
		Source:        "/source",
		Preset:        PresetStrict,
		Entries: []PlanEntry{
			{
				Kind:        EntryKindSkill,
				SourcePath:  "/x/skills/a/SKILL.md",
				TargetPath:  "plugin/zen-swarm/skills/a/SKILL.md",
				Frontmatter: map[string]string{"name": "a", "license": "imported"},
			},
		},
		Warnings: []string{"warn1"},
	}
	body, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var back Plan
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatal(err)
	}
	if back.SchemaVersion != orig.SchemaVersion {
		t.Errorf("schemaVersion: %s vs %s", back.SchemaVersion, orig.SchemaVersion)
	}
	if back.Preset != orig.Preset {
		t.Errorf("preset: %s vs %s", back.Preset, orig.Preset)
	}
	if len(back.Entries) != 1 || back.Entries[0].Frontmatter["name"] != "a" {
		t.Errorf("entries didn't round-trip: %v", back.Entries)
	}
	if len(back.Warnings) != 1 || back.Warnings[0] != "warn1" {
		t.Errorf("warnings didn't round-trip: %v", back.Warnings)
	}
}
