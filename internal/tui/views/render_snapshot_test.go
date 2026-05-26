package views

import (
	"strings"
	"testing"
)

type renderSnapshotCase struct {
	name string
	view string
}

func allPanelViews() []renderSnapshotCase {
	return []renderSnapshotCase{
		{"Help", NewHelpView().View()},
		{"Workforce", NewWorkforceView(nil).View()},
		{"Cost", NewCostView(nil).View()},
		{"Audit", NewAuditView(nil).View()},
		{"HRA", NewHRAView(nil).View()},
		{"Confirmations", NewConfirmationsView(nil).View()},
		{"Codegraph", NewCodegraphView(nil).View()},
		{"Memory", NewMemoryView(nil).View()},
		{"Skills", NewSkillsView(nil).View()},
		{"Doctrine", NewDoctrineView(nil).View()},
		{"CrossProject", NewCrossProjectView(nil).View()},
		{"Inbox", NewInboxView(nil).View()},
	}
}

func TestRenderSnapshotNonEmpty(t *testing.T) {
	for _, tc := range allPanelViews() {
		if strings.TrimSpace(tc.view) == "" {
			t.Errorf("panel %s: View() returned empty output", tc.name)
		}
	}
}

func TestRenderSnapshotHADESBrand(t *testing.T) {
	for _, tc := range allPanelViews() {
		if !strings.Contains(tc.view, "HADES") {
			t.Errorf("panel %s: View() missing HADES brand:\n%s", tc.name, tc.view)
		}
	}
}

func TestRenderSnapshotNoLegacyBrand(t *testing.T) {
	for _, tc := range allPanelViews() {

		for _, line := range strings.Split(tc.view, "\n") {
			stripped := strings.TrimSpace(line)
			if strings.HasPrefix(stripped, "zen-swarm") {
				t.Errorf("panel %s: View() header line still uses legacy brand:\n  %q",
					tc.name, line)
			}
		}
	}
}

func TestRenderSnapshotNoInlineHex(t *testing.T) {
	hexPattern := "#"
	for _, tc := range allPanelViews() {

		for _, line := range strings.Split(tc.view, "\n") {
			if !strings.Contains(line, hexPattern) {
				continue
			}

			idx := strings.Index(line, hexPattern)
			for idx != -1 {
				if idx+7 <= len(line) {
					candidate := line[idx+1 : idx+7]
					if isHex6(candidate) {
						t.Errorf("panel %s: View() emits bare hex literal %q in rendered text:\n  %q",
							tc.name, "#"+candidate, line)
					}
				}
				idx = strings.Index(line[idx+1:], hexPattern)
				if idx != -1 {
					idx += strings.Index(line, hexPattern) + 1
				}
			}
		}
	}
}

func isHex6(s string) bool {
	if len(s) != 6 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
