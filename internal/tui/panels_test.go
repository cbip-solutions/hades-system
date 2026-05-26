package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPanelKeyCadence(t *testing.T) {
	cases := []struct {
		key      panelKey
		wantMS   int
		isStatic bool
	}{
		{panelHelp, 0, true},
		{panelWorkforce, 1000, false},
		{panelCost, 2000, false},
		{panelAudit, 1000, false},
		{panelHRA, 1000, false},
		{panelConfirmations, 1000, false},
		{panelCodegraph, 5000, false},
		{panelMemory, 5000, false},
		{panelSkills, 5000, false},
		{panelDoctrine, 5000, false},
		{panelCrossProject, 5000, false},
		{panelInbox, 2000, false},
	}
	for _, tc := range cases {
		got := panelCadence(tc.key)
		if tc.isStatic {
			if got != 0 {
				t.Errorf("%v static expected, got %v", tc.key, got)
			}
			continue
		}
		gotMS := int(got / time.Millisecond)
		if gotMS != tc.wantMS {
			t.Errorf("%v cadence = %dms, want %dms", tc.key, gotMS, tc.wantMS)
		}
	}
}

func TestPanelCadenceUnknownDefaults(t *testing.T) {

	unknown := panelKey(99)
	got := panelCadence(unknown)
	if got != 5*time.Second {
		t.Errorf("expected 5s default for unknown panel, got %v", got)
	}
}

func TestPanelKeyString(t *testing.T) {
	cases := map[panelKey]string{
		panelHelp:          "help",
		panelWorkforce:     "workforce",
		panelCost:          "cost",
		panelAudit:         "audit",
		panelHRA:           "hra",
		panelConfirmations: "confirm",
		panelCodegraph:     "codegraph",
		panelMemory:        "memory",
		panelSkills:        "skills",
		panelDoctrine:      "doctrine",
		panelCrossProject:  "xproject",
		panelInbox:         "inbox",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("panelKey(%d).String() = %q, want %q", k, got, want)
		}
	}
	if panelKey(99).String() != "unknown" {
		t.Errorf("expected 'unknown' for out-of-range key")
	}
}

func TestScheduleTickStaticReturnsNil(t *testing.T) {
	cmd := scheduleTick(panelHelp)
	if cmd != nil {
		t.Errorf("expected nil Cmd for static panel, got: %T", cmd)
	}
}

func TestScheduleTickReturnsCmd(t *testing.T) {

	cmd := scheduleTick(panelCodegraph)
	if cmd == nil {
		t.Fatal("expected non-nil Cmd for cold panel")
	}
}

func TestScheduleTickPerPanel(t *testing.T) {

	for _, k := range []panelKey{
		panelWorkforce, panelCost, panelAudit, panelHRA,
		panelConfirmations, panelCodegraph, panelMemory, panelSkills,
		panelDoctrine, panelCrossProject, panelInbox,
	} {
		if cmd := scheduleTick(k); cmd == nil {
			t.Errorf("%v: expected non-nil Cmd", k)
		}
	}
}

func TestPanelTickMsgIsTeaMsg(t *testing.T) {
	var _ tea.Msg = panelTickMsg{key: panelHelp}
}
