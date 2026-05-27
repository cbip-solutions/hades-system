package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestModelInitialView(t *testing.T) {
	m := NewModel("/nonexistent/sock", 100*time.Millisecond)
	view := m.View()
	if !strings.Contains(view, "HADES") {
		t.Errorf("expected title, got: %s", view)
	}
	if m.activePanel != panelHelp {
		t.Errorf("expected default activePanel=help, got: %v", m.activePanel)
	}
}

func TestModelHandlesQuitKey(t *testing.T) {
	m := NewModel("/nonexistent/sock", 1*time.Second)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("expected quit cmd")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg")
	}
}

func TestModelHandlesCtrlCQuit(t *testing.T) {
	m := NewModel("/nonexistent/sock", 1*time.Second)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected quit cmd on Ctrl+C")
	}
}

func TestFKeyCyclesPanel(t *testing.T) {
	cases := []struct {
		key  string
		want panelKey
	}{
		{"F1", panelHelp},
		{"F2", panelWorkforce},
		{"F3", panelCost},
		{"F4", panelAudit},
		{"F5", panelHRA},
		{"F6", panelConfirmations},
		{"F7", panelCodegraph},
		{"F8", panelMemory},
		{"F9", panelSkills},
		{"F10", panelDoctrine},
		{"F11", panelCrossProject},
		{"F12", panelInbox},
	}
	for _, tc := range cases {
		m := NewModel("/nonexistent/sock", 1*time.Second)
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tc.key)})
		mm := updated.(Model)
		if mm.activePanel != tc.want {
			t.Errorf("after %s: activePanel = %v, want %v", tc.key, mm.activePanel, tc.want)
		}
	}
}

func TestFKeyTypedConstants(t *testing.T) {

	cases := []struct {
		t    tea.KeyType
		want panelKey
	}{
		{tea.KeyF1, panelHelp},
		{tea.KeyF2, panelWorkforce},
		{tea.KeyF3, panelCost},
		{tea.KeyF4, panelAudit},
		{tea.KeyF5, panelHRA},
		{tea.KeyF6, panelConfirmations},
		{tea.KeyF7, panelCodegraph},
		{tea.KeyF8, panelMemory},
		{tea.KeyF9, panelSkills},
		{tea.KeyF10, panelDoctrine},
		{tea.KeyF11, panelCrossProject},
		{tea.KeyF12, panelInbox},
	}
	for _, tc := range cases {
		m := NewModel("/nonexistent/sock", 1*time.Second)
		updated, _ := m.Update(tea.KeyMsg{Type: tc.t})
		mm := updated.(Model)
		if mm.activePanel != tc.want {
			t.Errorf("KeyType %d: activePanel = %v, want %v", tc.t, mm.activePanel, tc.want)
		}
	}
}

func TestActivePanelRendersInBody(t *testing.T) {
	m := NewModel("/nonexistent/sock", 1*time.Second)
	m.activePanel = panelCodegraph
	out := m.View()
	if !strings.Contains(out, "CODE GRAPH") {
		t.Errorf("expected F7 panel rendered, got: %s", out)
	}
}

func TestFooterHighlightsActivePanel(t *testing.T) {
	m := NewModel("/nonexistent/sock", 1*time.Second)
	m.activePanel = panelCost
	out := m.View()
	if !strings.Contains(out, "[F3 cost]") {
		t.Errorf("expected [F3 cost] in footer, got: %s", out)
	}

	if strings.Contains(out, "[F1 help]") {
		t.Errorf("F1 should NOT be bracketed when F3 is active, got: %s", out)
	}
}

func TestRefetchDispatchTablePanelCodegraph(t *testing.T) {
	m := NewModel("/nonexistent/sock", 1*time.Second)
	m.activePanel = panelCodegraph
	m.codegraph.SetCurrentFile("foo.go")
	cmd := m.refetchActivePanel()
	if cmd == nil {
		t.Fatal("expected non-nil refetch Cmd for F7 with current file set")
	}
}

func TestRefetchDispatchTablePerPanel(t *testing.T) {

	m := NewModel("/nonexistent/sock", 1*time.Second)
	for _, k := range []panelKey{
		panelWorkforce, panelCost, panelAudit, panelHRA,
		panelConfirmations, panelMemory, panelSkills, panelDoctrine,
		panelCrossProject, panelInbox,
	} {
		m.activePanel = k
		if cmd := m.refetchActivePanel(); cmd == nil {
			t.Errorf("%v: expected non-nil refetch Cmd", k)
		}
	}
}

func TestRefetchDispatchHelpReturnsNil(t *testing.T) {
	m := NewModel("/nonexistent/sock", 1*time.Second)
	m.activePanel = panelHelp
	if m.refetchActivePanel() != nil {
		t.Errorf("expected nil refetch for static Help panel")
	}
}

func TestFKeyPressDispatchesRefetch(t *testing.T) {
	m := NewModel("/nonexistent/sock", 1*time.Second)
	m.activePanel = panelHelp

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("F7")})
	if cmd == nil {
		t.Fatal("expected scheduleTick + refetch Cmd on F-key switch")
	}
}

func TestPanelTickActivePanelDispatches(t *testing.T) {
	m := NewModel("/nonexistent/sock", 1*time.Second)
	m.activePanel = panelCodegraph
	m.codegraph.SetCurrentFile("foo.go")

	_, cmd := m.Update(panelTickMsg{key: panelCodegraph})
	if cmd == nil {
		t.Fatal("expected refetch Cmd for active panel tick")
	}
}

func TestPanelTickInactivePanelDrops(t *testing.T) {
	m := NewModel("/nonexistent/sock", 1*time.Second)
	m.activePanel = panelCodegraph

	_, cmd := m.Update(panelTickMsg{key: panelInbox})
	if cmd != nil {
		t.Errorf("expected nil for inactive panel tick, got: %T", cmd)
	}
}

func TestWindowSizeMsgUpdatesDimensions(t *testing.T) {
	m := NewModel("/nonexistent/sock", 1*time.Second)
	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if cmd != nil {
		t.Errorf("expected nil Cmd on WindowSizeMsg")
	}
	mm := updated.(Model)
	if mm.width != 80 || mm.height != 24 {
		t.Errorf("dimensions not updated: %dx%d", mm.width, mm.height)
	}
}

func TestParseFKeyUnknownReturnsFalse(t *testing.T) {
	_, ok := parseFKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if ok {
		t.Errorf("expected ok=false for non-F key")
	}
}

func TestInitReturnsCmd(t *testing.T) {
	m := NewModel("/nonexistent/sock", 1*time.Second)

	_ = m.Init()
}

func TestUnknownMessageRoutesToActivePanel(t *testing.T) {

	m := NewModel("/nonexistent/sock", 1*time.Second)
	m.activePanel = panelWorkforce
	updated, _ := m.Update("string-message-not-a-known-type")
	if _, ok := updated.(Model); !ok {
		t.Errorf("expected Model from Update")
	}
}

func TestNonFKeyRoutesToActivePanel(t *testing.T) {

	m := NewModel("/nonexistent/sock", 1*time.Second)
	m.activePanel = panelCodegraph
	m.codegraph.SetCurrentFile("foo.go")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if _, ok := updated.(Model); !ok {
		t.Errorf("expected Model from Update")
	}
}

func TestRenderActivePanelBodyForEveryPanel(t *testing.T) {
	expected := map[panelKey]string{
		panelHelp:          "HELP",
		panelWorkforce:     "WORKFORCE",
		panelCost:          "COST",
		panelAudit:         "AUDIT",
		panelHRA:           "HRA",
		panelConfirmations: "CONFIRMATIONS",
		panelCodegraph:     "CODE GRAPH",
		panelMemory:        "MEMORY",
		panelSkills:        "SKILLS",
		panelDoctrine:      "DOCTRINE",
		panelCrossProject:  "CROSS-PROJECT",
		panelInbox:         "INBOX",
	}
	for k, want := range expected {
		m := NewModel("/nonexistent/sock", 1*time.Second)
		m.activePanel = k
		body := m.renderActivePanelBody()
		if !strings.Contains(body, want) {
			t.Errorf("panel %v: body missing %q\n--- got ---\n%s", k, want, body)
		}
	}
}

func TestRouteToActivePanelEveryPanel(t *testing.T) {
	m := NewModel("/nonexistent/sock", 1*time.Second)
	for _, k := range []panelKey{
		panelHelp, panelWorkforce, panelCost, panelAudit, panelHRA,
		panelConfirmations, panelCodegraph, panelMemory, panelSkills,
		panelDoctrine, panelCrossProject, panelInbox,
	} {
		m.activePanel = k
		updated, _ := m.routeToActivePanel("test-payload-not-a-real-msg")
		if _, ok := updated.(Model); !ok {
			t.Errorf("panel %v: expected Model from routeToActivePanel", k)
		}
	}
}

func TestEscRoutesToCodegraphSubPanel(t *testing.T) {
	m := NewModel("/nonexistent/sock", 1*time.Second)
	m.activePanel = panelCodegraph
	m.codegraph.SetCurrentFile("foo.go")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Errorf("Esc should not quit when codegraph is active")
		}
	}
}

func TestPollEveryDefaultsTo1s(t *testing.T) {
	m := NewModel("/nonexistent/sock", 0)
	if m.pollEvery != 1*time.Second {
		t.Errorf("expected pollEvery default 1s, got: %v", m.pollEvery)
	}
}

// TestNewModelDefaultsToCapaFirewall verifies the fail-closed default
// for the codegraph panel's invariant client-side anchor. Construction
// must NOT leave the guard at a permissive mode; until Init resolves the
// daemon's active doctrine the F7 [C] cross-project key MUST be blocked
// client-side (defense-in-depth pairs with server-side enforcement).
//
// MAJOR-1 fix-cycle. Before the fix, NewModel left
// codegraph.doctrineMode = "default" (permissive) for the entire
// dashboard lifetime — Init never called SetDoctrineMode. This test
// pins the new contract: construction defaults to capa-firewall.
func TestNewModelDefaultsToCapaFirewall(t *testing.T) {
	m := NewModel("/nonexistent/sock", 1*time.Second)
	got := m.codegraph.DoctrineMode()
	if got != "capa-firewall" {
		t.Errorf("default doctrineMode = %q, want %q (fail-closed for inv-zen-163)",
			got, "capa-firewall")
	}
}

func TestDoctrineModeAppliedMsgUpdatesCodegraph(t *testing.T) {
	m := NewModel("/nonexistent/sock", 1*time.Second)

	updated, _ := m.Update(doctrineModeAppliedMsg{mode: "max-scope"})
	mm := updated.(Model)
	if got := mm.codegraph.DoctrineMode(); got != "max-scope" {
		t.Errorf("after doctrineModeAppliedMsg: codegraph mode = %q, want %q",
			got, "max-scope")
	}
}

func TestFetchDoctrineModeNilClientFailsClosed(t *testing.T) {
	m := Model{}
	cmd := m.fetchDoctrineMode()
	if cmd == nil {
		t.Fatal("expected non-nil Cmd even with nil client")
	}
	msg := cmd()
	applied, ok := msg.(doctrineModeAppliedMsg)
	if !ok {
		t.Fatalf("expected doctrineModeAppliedMsg, got %T", msg)
	}
	if applied.mode != "capa-firewall" {
		t.Errorf("nil-client Cmd returned mode = %q, want %q",
			applied.mode, "capa-firewall")
	}
}

func TestFetchDoctrineModeUnreachableFailsClosed(t *testing.T) {
	m := NewModel("/nonexistent/sock", 1*time.Second)
	cmd := m.fetchDoctrineMode()
	if cmd == nil {
		t.Fatal("expected non-nil fetchDoctrineMode Cmd")
	}
	msg := cmd()
	applied, ok := msg.(doctrineModeAppliedMsg)
	if !ok {
		t.Fatalf("expected doctrineModeAppliedMsg, got %T", msg)
	}
	if applied.mode != "capa-firewall" {
		t.Errorf("daemon-unreachable Cmd returned mode = %q, want %q",
			applied.mode, "capa-firewall")
	}
}

func TestInitDispatchesFetchDoctrineMode(t *testing.T) {
	m := NewModel("/nonexistent/sock", 1*time.Second)
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected non-nil Init Cmd that includes fetchDoctrineMode")
	}

	out := cmd()
	if out == nil {
		t.Fatal("Init Cmd returned nil msg")
	}
	if found := findDoctrineModeAppliedMsg(out); !found {
		t.Errorf("Init batch did not produce doctrineModeAppliedMsg; got %T", out)
	}
}

func findDoctrineModeAppliedMsg(msg tea.Msg) bool {
	if _, ok := msg.(doctrineModeAppliedMsg); ok {
		return true
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if c == nil {
				continue
			}
			ch := make(chan tea.Msg, 1)
			go func(cmd tea.Cmd) { ch <- cmd() }(c)
			select {
			case inner := <-ch:
				if inner == nil {
					continue
				}
				if findDoctrineModeAppliedMsg(inner) {
					return true
				}
			case <-time.After(2 * time.Second):

				continue
			}
		}
	}
	return false
}

func TestDashboardHeaderCompactMode(t *testing.T) {
	m := NewModel("/tmp/no-such-socket-phase-c-c7", 0)

	m.width = 40
	out := m.View()
	if !strings.Contains(out, "HADES") {
		t.Errorf("compact View() missing HADES brand:\n%s", out)
	}

	if strings.Contains(out, "⡇") {
		t.Errorf("compact View() must not contain Bident glyph at width=40:\n%s", out)
	}
}

func TestDashboardHeaderBidentGlyph(t *testing.T) {
	m := NewModel("/tmp/no-such-socket-phase-c-c7", 0)

	m.width = 120
	out := m.View()
	if !strings.Contains(out, "HADES") {
		t.Errorf("wide View() missing HADES brand:\n%s", out)
	}
	if !strings.Contains(out, "⡇") {
		t.Errorf("wide View() missing Bident glyph at width=120:\n%s", out)
	}
}

func TestDashboardHeaderHADES(t *testing.T) {
	m := NewModel("/tmp/no-such-socket-phase-c-c6", 0)
	out := m.View()
	if !strings.Contains(out, "HADES") {
		t.Errorf("Dashboard View() missing HADES brand:\n%s", out)
	}
	if strings.Contains(out, "zen-swarm") {
		t.Errorf("Dashboard View() still contains legacy zen-swarm brand:\n%s", out)
	}
}
