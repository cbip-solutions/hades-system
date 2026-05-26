package writer

import (
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/migrate/mapping"
)

func TestRenderPluginYAML_HasRequiredFields(t *testing.T) {
	t.Parallel()
	plan := &mapping.Plan{Entries: []mapping.PlanEntry{
		{Kind: mapping.EntryKindSkill},
		{Kind: mapping.EntryKindCommand},
		{Kind: mapping.EntryKindHook},
	}}
	body := string(renderPluginYAML(plan))
	for _, req := range []string{
		"name:", "version:", "description:",
		"imported_skill_count: 1",
		"imported_command_count: 1",
		"imported_hook_count: 1",
	} {
		if !strings.Contains(body, req) {
			t.Errorf("missing %q: %s", req, body)
		}
	}
}

func TestRenderPluginYAML_NoTimestamps(t *testing.T) {
	t.Parallel()
	plan := &mapping.Plan{}
	a := string(renderPluginYAML(plan))
	b := string(renderPluginYAML(plan))
	if a != b {
		t.Errorf("non-deterministic (timestamp leaked?):\n%s\n%s", a, b)
	}
}

func TestRenderPluginYAML_EmptyCountsZero(t *testing.T) {
	t.Parallel()
	plan := &mapping.Plan{}
	body := string(renderPluginYAML(plan))
	for _, req := range []string{
		"imported_skill_count: 0",
		"imported_command_count: 0",
		"imported_hook_count: 0",
	} {
		if !strings.Contains(body, req) {
			t.Errorf("missing %q: %s", req, body)
		}
	}
}
