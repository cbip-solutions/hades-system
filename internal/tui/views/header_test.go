package views

import (
	"strings"
	"testing"
)

func TestPanelHeaderPrefix(t *testing.T) {
	got := panelHeader("PENDING CONFIRMATIONS")
	for _, want := range []string{"HADES", "·", "PENDING CONFIRMATIONS"} {
		if !strings.Contains(got, want) {
			t.Errorf("panelHeader missing %q in:\n%s", want, got)
		}
	}
}

func TestPanelHeaderEmptyTitle(t *testing.T) {
	got := panelHeader("")
	if !strings.Contains(got, "HADES") {
		t.Errorf("panelHeader missing HADES with empty title: %s", got)
	}
}
