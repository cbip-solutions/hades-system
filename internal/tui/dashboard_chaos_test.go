// go:build chaos

package tui

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/tui/views"
)

func newChaosModel(c *client.Client) Model {
	return Model{
		c:             c,
		activePanel:   panelHelp,
		help:          views.NewHelpView(),
		workforce:     views.NewWorkforceView(c),
		cost:          views.NewCostView(c),
		audit:         views.NewAuditView(c),
		hra:           views.NewHRAView(c),
		confirmations: views.NewConfirmationsView(c),
		codegraph:     views.NewCodegraphView(c),
		memory:        views.NewMemoryView(c),
		skills:        views.NewSkillsView(c),
		doctrine:      views.NewDoctrineView(c),
		crossProject:  views.NewCrossProjectView(c),
		inbox:         views.NewInboxView(c),
	}
}

func drainChaosCmd(t *testing.T, m Model, cmd tea.Cmd, depth int) Model {
	if cmd == nil || depth > 4 {
		return m
	}
	ch := make(chan tea.Msg, 1)
	go func() {
		ch <- cmd()
	}()
	select {
	case msg := <-ch:
		if msg == nil {
			return m
		}
		if _, ok := msg.(panelTickMsg); ok {
			return m
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				m = drainChaosCmd(t, m, c, depth+1)
			}
			return m
		}
		updated, cmd := m.Update(msg)
		mm := updated.(Model)
		if cmd != nil {
			mm = drainChaosCmd(t, mm, cmd, depth+1)
		}
		return mm
	case <-time.After(50 * time.Millisecond):
		return m
	}
}

func TestChaosDaemonConnectionRefused(t *testing.T) {

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	_ = ln.Close()

	c := client.NewWithBaseURL("http://" + addr)
	m := newChaosModel(c)
	m.codegraph.SetCurrentFile("foo.go")

	for _, key := range []string{"F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10", "F11", "F12"} {
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		m = updated.(Model)
		m = drainChaosCmd(t, m, cmd, 0)
		view := m.View()
		if view == "" {
			t.Errorf("F-key %s: view empty under chaos", key)
		}

		header := m.renderActivePanelBody()
		if header == "" {
			t.Errorf("F-key %s: panel body empty under chaos", key)
		}
	}
}

func TestChaosDaemonReturns503(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := client.NewWithBaseURL(srv.URL)
	m := newChaosModel(c)
	m.codegraph.SetCurrentFile("foo.go")

	for _, key := range []string{"F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10", "F11", "F12"} {
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		m = updated.(Model)
		m = drainChaosCmd(t, m, cmd, 0)
		view := m.View()

		if view == "" {
			t.Errorf("F-key %s: empty view on 503", key)
		}
	}
}

func TestChaosSlowResponseTriggersTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(10 * time.Second):
		case <-r.Context().Done():
			return
		}
	}))
	defer srv.Close()

	c := client.NewWithBaseURL(srv.URL)
	m := newChaosModel(c)
	m.codegraph.SetCurrentFile("foo.go")
	m.activePanel = panelCodegraph

	cmd := m.refetchActivePanel()
	if cmd == nil {
		t.Fatal("expected non-nil refetch")
	}

	start := time.Now()
	ch := make(chan tea.Msg, 1)
	go func() {
		ch <- cmd()
	}()
	select {
	case <-ch:

	case <-time.After(7 * time.Second):
		t.Errorf("Cmd took longer than 7s — timeout not enforced")
	}
	elapsed := time.Since(start)
	if elapsed > 6*time.Second {
		t.Errorf("elapsed %v exceeded 6s budget", elapsed)
	}
}

func TestChaosMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{ not valid json"))
	}))
	defer srv.Close()

	c := client.NewWithBaseURL(srv.URL)
	m := newChaosModel(c)
	m.codegraph.SetCurrentFile("foo.go")

	for _, key := range []string{"F2", "F3", "F7", "F11"} {
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		m = updated.(Model)
		m = drainChaosCmd(t, m, cmd, 0)
		view := m.View()
		if view == "" {
			t.Errorf("F-key %s: empty view on malformed JSON", key)
		}

	}
}

func TestChaosPanelPreservesHeaderOnErr(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := client.NewWithBaseURL(srv.URL)
	m := newChaosModel(c)

	expectedHeaders := map[panelKey]string{
		panelWorkforce:     "WORKFORCE",
		panelCost:          "COST",
		panelAudit:         "AUDIT",
		panelHRA:           "HRA",
		panelConfirmations: "CONFIRMATIONS",
		panelMemory:        "MEMORY",
		panelSkills:        "SKILLS",
		panelDoctrine:      "DOCTRINE",
		panelCrossProject:  "CROSS-PROJECT",
		panelInbox:         "INBOX",
	}
	for k, header := range expectedHeaders {
		m.activePanel = k
		cmd := m.refetchActivePanel()
		m = drainChaosCmd(t, m, cmd, 0)
		body := m.renderActivePanelBody()
		if !strings.Contains(body, header) {
			t.Errorf("panel %v: header %q missing on 503\n--- got ---\n%s", k, header, body)
		}
	}
}

func TestChaosCodegraphPreservesHeaderOnErr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := client.NewWithBaseURL(srv.URL)
	m := newChaosModel(c)
	m.activePanel = panelCodegraph
	m.codegraph.SetCurrentFile("foo.go")

	cmd := m.refetchActivePanel()
	m = drainChaosCmd(t, m, cmd, 0)
	body := m.renderActivePanelBody()
	if !strings.Contains(body, "CODE GRAPH") {
		t.Errorf("expected CODE GRAPH header preserved on chaos, got: %s", body)
	}
}
