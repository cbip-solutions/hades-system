// SPDX-License-Identifier: MIT
package hermesplugin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type StringSet struct {
	items map[string]struct{}
}

func NewStringSet(items []string) *StringSet {
	s := &StringSet{items: make(map[string]struct{}, len(items))}
	for _, x := range items {
		s.items[x] = struct{}{}
	}
	return s
}

func (s *StringSet) Contains(x string) bool {
	_, ok := s.items[x]
	return ok
}

type FakeHermes struct {
	server *httptest.Server
	t      *testing.T

	mu      sync.RWMutex
	plugins *StringSet
	slash   *StringSet
	skills  *StringSet
	mcps    *StringSet
	tmpls   []string
}

func NewFakeHermes(t *testing.T) *FakeHermes {
	t.Helper()
	h := &FakeHermes{
		t:       t,
		plugins: NewStringSet([]string{"zen-swarm"}),
		slash: NewStringSet([]string{
			"/codegraph", "/impact", "/context", "/wiki", "/cypher", "/augment",
			"/start", "/handoff", "/brainstorm", "/write-plan", "/execute-plan",
			"/doctrine", "/amendment", "/impact-pre-merge", "/audit-impact",
			"/doctrine-drift-check", "/knowledge", "/full", "/voice",
			"/openspec-apply", "/openspec-archive", "/openspec-propose", "/openspec-resume",
		}),
		skills: NewStringSet([]string{
			"writing-plans", "executing-plans", "brainstorming",
			"subagent-driven-development", "test-driven-development",
			"systematic-debugging", "verification-before-completion",
		}),
		mcps: NewStringSet([]string{"zen-swarm", "gitnexus"}),
		tmpls: []string{
			"agent-research", "agent-budget", "agent-audit", "agent-sshexec",
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/plugins/list", h.handlePluginsList)
	mux.HandleFunc("/skills/list", h.handleSkillsList)
	mux.HandleFunc("/slash/registered", h.handleSlashRegistered)
	mux.HandleFunc("/mcp/list", h.handleMCPList)
	mux.HandleFunc("/templates/list", h.handleTemplatesList)
	mux.HandleFunc("/citation/render", h.handleCitationRender)
	h.server = httptest.NewServer(mux)
	return h
}

func (h *FakeHermes) Close() {
	h.server.Close()
}

func (h *FakeHermes) URL() string {
	return h.server.URL
}

func (h *FakeHermes) ListPlugins() *StringSet {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.plugins
}

func (h *FakeHermes) SlashCommandRegistered(cmd string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.slash.Contains(cmd)
}

func (h *FakeHermes) ListSkills() *StringSet {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.skills
}

func (h *FakeHermes) MCPServersList() *StringSet {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.mcps
}

func (h *FakeHermes) ListAgentTemplates() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]string, len(h.tmpls))
	copy(out, h.tmpls)
	return out
}

type Rendered struct {
	body string
}

func (r Rendered) Body() string { return r.body }

func (r Rendered) IsEmpty() bool { return r.body == "" }

func (h *FakeHermes) RenderCitation(c *TestCitation, renderer string) (Rendered, error) {
	switch renderer {
	case "ink", "telegram", "slack", "html_email", "voice", "web", "markdown_fallback":
		return Rendered{body: fmt.Sprintf("renderer=%s body=%s", renderer, c.Body)}, nil
	default:
		return Rendered{}, fmt.Errorf("unknown renderer: %s", renderer)
	}
}

func (h *FakeHermes) handlePluginsList(w http.ResponseWriter, _ *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var items []string
	for k := range h.plugins.items {
		items = append(items, k)
	}
	_ = json.NewEncoder(w).Encode(items)
}

func (h *FakeHermes) handleSkillsList(w http.ResponseWriter, _ *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var items []string
	for k := range h.skills.items {
		items = append(items, k)
	}
	_ = json.NewEncoder(w).Encode(items)
}

func (h *FakeHermes) handleSlashRegistered(w http.ResponseWriter, r *http.Request) {
	cmd := r.URL.Query().Get("cmd")
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.slash.Contains(cmd) {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

func (h *FakeHermes) handleMCPList(w http.ResponseWriter, _ *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var items []string
	for k := range h.mcps.items {
		items = append(items, k)
	}
	_ = json.NewEncoder(w).Encode(items)
}

func (h *FakeHermes) handleTemplatesList(w http.ResponseWriter, _ *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_ = json.NewEncoder(w).Encode(h.tmpls)
}

func (h *FakeHermes) handleCitationRender(w http.ResponseWriter, r *http.Request) {
	renderer := r.URL.Query().Get("renderer")
	body := r.URL.Query().Get("body")
	if body == "" {
		http.Error(w, "missing body", http.StatusBadRequest)
		return
	}
	switch renderer {
	case "ink", "telegram", "slack", "html_email", "voice", "web", "markdown_fallback":
		_, _ = w.Write([]byte(strings.Join([]string{"renderer=", renderer, " body=", body}, "")))
	default:
		http.Error(w, "unknown renderer", http.StatusBadRequest)
	}
}
