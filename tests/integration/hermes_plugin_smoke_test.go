// go:build integration
//go:build integration
// +build integration

package integration_test

import (
	"testing"

	"github.com/cbip-solutions/hades-system/tests/testharness/hermesplugin"
)

func TestHermesPluginSmoke_PluginDiscovery(t *testing.T) {
	h := hermesplugin.NewFakeHermes(t)
	defer h.Close()

	plugins := h.ListPlugins()
	if !plugins.Contains("zen-swarm") {
		t.Fatalf("zen-swarm plugin not discovered")
	}
}

func TestHermesPluginSmoke_SlashCommands(t *testing.T) {
	h := hermesplugin.NewFakeHermes(t)
	defer h.Close()

	expected := []string{
		"/codegraph", "/impact", "/context", "/wiki", "/cypher", "/augment",
		"/start", "/handoff", "/brainstorm", "/write-plan", "/execute-plan",
		"/doctrine", "/amendment", "/impact-pre-merge", "/audit-impact",
		"/doctrine-drift-check", "/knowledge", "/full", "/voice",
		"/openspec-apply", "/openspec-archive", "/openspec-propose", "/openspec-resume",
	}
	for _, cmd := range expected {
		if !h.SlashCommandRegistered(cmd) {
			t.Errorf("slash command %s not registered", cmd)
		}
	}
}

func TestHermesPluginSmoke_SkillsLoaded(t *testing.T) {
	h := hermesplugin.NewFakeHermes(t)
	defer h.Close()

	skills := h.ListSkills()
	required := []string{
		"writing-plans", "executing-plans", "brainstorming",
		"subagent-driven-development",
	}
	for _, s := range required {
		if !skills.Contains(s) {
			t.Errorf("skill %s not loaded", s)
		}
	}
}

func TestHermesPluginSmoke_AgentTemplates(t *testing.T) {
	h := hermesplugin.NewFakeHermes(t)
	defer h.Close()

	templates := h.ListAgentTemplates()
	if len(templates) < 4 {
		t.Errorf("expected at least 4 agent templates, got %d: %v", len(templates), templates)
	}
}

func TestHermesPluginSmoke_MCPServers(t *testing.T) {
	h := hermesplugin.NewFakeHermes(t)
	defer h.Close()

	mcps := h.MCPServersList()
	for _, mcp := range []string{"zen-swarm", "gitnexus"} {
		if !mcps.Contains(mcp) {
			t.Errorf("mcp server %s not registered", mcp)
		}
	}
}

func TestHermesPluginSmoke_CitationRoundTrip(t *testing.T) {
	h := hermesplugin.NewFakeHermes(t)
	defer h.Close()

	citation := hermesplugin.NewTestCitation()
	renderers := []string{"ink", "telegram", "slack", "html_email", "voice", "web", "markdown_fallback"}
	for _, r := range renderers {
		rendered, err := h.RenderCitation(citation, r)
		if err != nil {
			t.Errorf("renderer %s failed: %v", r, err)
			continue
		}
		if rendered.IsEmpty() {
			t.Errorf("renderer %s produced empty output", r)
		}
	}
}
