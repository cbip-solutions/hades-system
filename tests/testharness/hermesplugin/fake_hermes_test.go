package hermesplugin_test

import (
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/tests/testharness/hermesplugin"
)

func TestFakeHermes_ListPlugins(t *testing.T) {
	h := hermesplugin.NewFakeHermes(t)
	defer h.Close()

	plugins := h.ListPlugins()
	if !plugins.Contains("zen-swarm") {
		t.Fatalf("expected zen-swarm in plugin list; got: %v", plugins)
	}
}

func TestFakeHermes_SlashCommandRegistered(t *testing.T) {
	h := hermesplugin.NewFakeHermes(t)
	defer h.Close()

	required := []string{"/start", "/handoff", "/codegraph", "/impact", "/context"}
	for _, cmd := range required {
		if !h.SlashCommandRegistered(cmd) {
			t.Errorf("slash command %s not registered in FakeHermes", cmd)
		}
	}
}

func TestFakeHermes_ListSkills(t *testing.T) {
	h := hermesplugin.NewFakeHermes(t)
	defer h.Close()

	skills := h.ListSkills()
	required := []string{"writing-plans", "executing-plans", "brainstorming", "subagent-driven-development"}
	for _, skill := range required {
		if !skills.Contains(skill) {
			t.Errorf("skill %s not loaded in FakeHermes", skill)
		}
	}
}

func TestFakeHermes_MCPServersList(t *testing.T) {
	h := hermesplugin.NewFakeHermes(t)
	defer h.Close()

	mcps := h.MCPServersList()
	for _, mcp := range []string{"zen-swarm", "gitnexus"} {
		if !mcps.Contains(mcp) {
			t.Errorf("mcp server %s not registered", mcp)
		}
	}
}

func TestFakeHermes_RenderCitation(t *testing.T) {
	h := hermesplugin.NewFakeHermes(t)
	defer h.Close()

	citation := hermesplugin.NewTestCitation()
	renderers := []string{"ink", "telegram", "slack", "html_email", "voice", "web", "markdown_fallback"}
	for _, r := range renderers {
		rendered, err := h.RenderCitation(citation, r)
		if err != nil {
			t.Errorf("renderer %s failed: %v", r, err)
		}
		if rendered.IsEmpty() {
			t.Errorf("renderer %s produced empty output", r)
		}
		if !strings.Contains(rendered.Body(), "test-citation") {
			t.Errorf("renderer %s output missing citation body marker: %q", r, rendered.Body())
		}
	}
}
