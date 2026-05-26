package views

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHelpView(t *testing.T) {
	v := NewHelpView()
	out := v.View()
	for _, want := range []string{"F1", "F7", "F12", "help", "codegraph"} {
		if !strings.Contains(out, want) {
			t.Errorf("HelpView missing %q in:\n%s", want, out)
		}
	}
}

func TestHelpViewSatisfiesTeaModel(t *testing.T) {
	var _ tea.Model = NewHelpView()
}

func TestHelpViewRefetchNil(t *testing.T) {
	if NewHelpView().Refetch() != nil {
		t.Errorf("expected nil Refetch for static panel")
	}
}

func TestHelpViewInitNil(t *testing.T) {
	if NewHelpView().Init() != nil {
		t.Errorf("expected nil Init for static panel")
	}
}

func TestHelpViewUpdateIsNoOp(t *testing.T) {
	v := NewHelpView()
	updated, cmd := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if cmd != nil {
		t.Errorf("expected nil Cmd from Update")
	}
	if _, ok := updated.(*HelpView); !ok {
		t.Errorf("expected *HelpView from Update")
	}
}

func TestHelpHADESPrefix(t *testing.T) {
	v := NewHelpView()
	if !strings.Contains(v.View(), "HADES") {
		t.Errorf("HelpView missing HADES prefix:\n%s", v.View())
	}
}
