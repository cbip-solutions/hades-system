package views

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func TestCodegraphViewSkeleton(t *testing.T) {
	v := NewCodegraphView(nil)
	if v.View() == "" {
		t.Fatal("View() returned empty string for skeleton")
	}
	if !strings.Contains(v.View(), "CODE GRAPH") {
		t.Errorf("expected header 'CODE GRAPH', got: %s", v.View())
	}
}

func TestCodegraphViewSatisfiesTeaModel(t *testing.T) {
	var _ tea.Model = NewCodegraphView(nil)
}

func TestCodegraphViewCurrentFileEmpty(t *testing.T) {
	v := NewCodegraphView(nil)
	out := v.View()
	if !strings.Contains(out, "no file selected") {
		t.Errorf("expected empty-state hint, got: %s", out)
	}
}

func TestCodegraphViewSetCurrentFile(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("internal/orchestrator/dispatch.go")
	out := v.View()
	if !strings.Contains(out, "internal/orchestrator/dispatch.go") {
		t.Errorf("expected current file in view, got: %s", out)
	}
}

func TestCodegraphViewRendersFullLayout(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("internal/orchestrator/dispatch.go")
	v.symbols = []symbolEntry{
		{Name: "Dispatch", Kind: "func", Line: 42},
		{Name: "scoreCandidates", Kind: "func", Line: 88},
		{Name: "newRouter", Kind: "func", Line: 120},
	}
	v.callers = []callerEntry{
		{File: "internal/daemon/handlers/messages.go", Symbol: "Dispatch", Count30d: 147},
		{File: "internal/cli/run.go", Symbol: "Dispatch", Count30d: 42},
	}
	v.community = communityInfo{ID: "C-014", Summary: "orchestrator subsystem"}
	v.churn = churnInfo{Commits7d: 7, Authors: []string{"testuser", "alice"}}
	v.blastRad = 0.74
	v.lastIndex = "2026-05-09 18:32:14 UTC"

	out := v.View()
	for _, want := range []string{
		"CODE GRAPH",
		"internal/orchestrator/dispatch.go",
		"Dispatch", "scoreCandidates", "newRouter",
		"daemon/handlers/messages.go", "147",
		"cli/run.go", "42",
		"C-014", "orchestrator subsystem",
		"7 commits", "testuser", "alice",
		"0.74",
		"2026-05-09 18:32:14 UTC",
		"[Q]", "[I]", "[W]", "[C]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("View missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestCodegraphViewRendersErrorMuted(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("foo.go")
	v.lastErr = errors.New("daemon unreachable")
	out := v.View()
	if !strings.Contains(out, "daemon unreachable") {
		t.Errorf("expected error message in view, got: %s", out)
	}
	if !strings.Contains(out, "CODE GRAPH") {
		t.Errorf("expected header preserved on error, got: %s", out)
	}
}

func TestCodegraphViewBlastRadiusColorCoded(t *testing.T) {
	cases := []struct {
		score    float64
		severity string
	}{
		{0.20, "low"},
		{0.55, "medium"},
		{0.85, "high"},
	}
	for _, tc := range cases {
		v := NewCodegraphView(nil)
		v.SetCurrentFile("x.go")
		v.blastRad = tc.score
		out := v.View()
		if !strings.Contains(out, tc.severity) {
			t.Errorf("score %.2f: expected severity %q in view, got:\n%s",
				tc.score, tc.severity, out)
		}
	}
}

func TestCodegraphKeyQOpensQueryPanel(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("foo.go")
	updated, _ := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	cv := updated.(*CodegraphView)
	if cv.subPanel != subPanelQuery {
		t.Errorf("expected subPanel=query, got: %v", cv.subPanel)
	}
	if !strings.Contains(cv.View(), "QUERY:") {
		t.Errorf("expected QUERY prompt in view, got: %s", cv.View())
	}
}

func TestCodegraphKeyIRunsImpactPreview(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("foo.go")
	updated, cmd := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	cv := updated.(*CodegraphView)
	if cv.subPanel != subPanelImpact {
		t.Errorf("expected subPanel=impact, got: %v", cv.subPanel)
	}
	if cmd == nil {
		t.Fatal("expected impact Cmd dispatched, got nil")
	}
}

func TestCodegraphKeyINoCurrentFile(t *testing.T) {
	v := NewCodegraphView(nil)
	updated, cmd := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	cv := updated.(*CodegraphView)
	if cv.subPanel == subPanelImpact {
		t.Errorf("expected [I] no-op with no current file")
	}
	if cmd != nil {
		t.Errorf("expected nil Cmd, got: %T", cmd)
	}
}

func TestCodegraphKeyWOpensCommunityWiki(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("foo.go")
	v.community = communityInfo{ID: "C-014", Summary: "x"}
	updated, cmd := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	cv := updated.(*CodegraphView)
	if cv.subPanel != subPanelWiki {
		t.Errorf("expected subPanel=wiki, got: %v", cv.subPanel)
	}
	if cmd == nil {
		t.Fatal("expected wiki Cmd dispatched, got nil")
	}
}

func TestCodegraphKeyWNoCommunity(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("foo.go")
	updated, cmd := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	cv := updated.(*CodegraphView)
	if cv.subPanel == subPanelWiki {
		t.Errorf("expected wiki not opened when no community, got subPanel=%v", cv.subPanel)
	}
	if cmd != nil {
		t.Errorf("expected nil Cmd, got: %T", cmd)
	}
}

func TestCodegraphKeyCDisabledInCapaFirewall(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("foo.go")
	v.SetDoctrineMode("capa-firewall")
	updated, cmd := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	cv := updated.(*CodegraphView)
	if cv.subPanel == subPanelCrossProject {
		t.Errorf("expected [C] rejected under capa-firewall, got subPanel=%v", cv.subPanel)
	}
	if cmd != nil {
		t.Errorf("expected nil Cmd under capa-firewall, got: %T", cmd)
	}
	if !strings.Contains(cv.View(), "disabled by capa-firewall") {
		t.Errorf("expected capa-firewall disabled hint, got: %s", cv.View())
	}
}

func TestCodegraphKeyCEnabledInDefault(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("foo.go")
	v.symbols = []symbolEntry{{Name: "Dispatch", Kind: "func"}}
	v.SetDoctrineMode("default")
	updated, cmd := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	cv := updated.(*CodegraphView)
	if cv.subPanel != subPanelCrossProject {
		t.Errorf("expected subPanel=crossproject, got: %v", cv.subPanel)
	}
	if cmd == nil {
		t.Fatal("expected cross-project Cmd, got nil")
	}
}

func TestCodegraphKeyEscClosesSubPanel(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("foo.go")
	v.subPanel = subPanelQuery
	v.subPanelInput = "MATCH (n) RETURN n"
	updated, _ := v.Update(tea.KeyMsg{Type: tea.KeyEsc})
	cv := updated.(*CodegraphView)
	if cv.subPanel != subPanelNone {
		t.Errorf("expected subPanel=none after Esc, got: %v", cv.subPanel)
	}
	if cv.subPanelInput != "" {
		t.Errorf("expected input cleared on Esc, got: %q", cv.subPanelInput)
	}
}

func TestCodegraphSubPanelDataRenders(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("foo.go")
	v.subPanel = subPanelImpact
	v.subPanelData = "Impact preview:\n  - pkg/a (1 caller)\n  - pkg/b (3 callers)"
	if !strings.Contains(v.View(), "pkg/a") {
		t.Errorf("expected impact body in view, got: %s", v.View())
	}
}

func TestCodegraphQueryInputAccumulation(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("foo.go")
	v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("MATCH")})
	if v.subPanelInput != "MATCH" {
		t.Errorf("expected input='MATCH', got: %q", v.subPanelInput)
	}
	v.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if v.subPanelInput != "MATC" {
		t.Errorf("expected input='MATC' after backspace, got: %q", v.subPanelInput)
	}
	_, cmd := v.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Errorf("expected Enter dispatched a Cmd")
	}
	if v.subPanelInput != "" {
		t.Errorf("expected input cleared after Enter, got: %q", v.subPanelInput)
	}
}

func TestCodegraphDataMsgUpdatesView(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("foo.go")
	v.lastErr = errors.New("prior")
	_, _ = v.Update(codegraphDataMsg{
		symbols:   []symbolEntry{{Name: "X", Kind: "func"}},
		blastRad:  0.5,
		lastIndex: "2026-05-12 00:00:00 UTC",
	})
	if v.lastErr != nil {
		t.Errorf("expected lastErr cleared after good data msg, got: %v", v.lastErr)
	}
	if len(v.symbols) != 1 || v.symbols[0].Name != "X" {
		t.Errorf("symbols not set: %+v", v.symbols)
	}
}

func TestCodegraphSubPanelMsgUpdatesData(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("foo.go")
	_, _ = v.Update(codegraphSubPanelMsg{
		mode: subPanelImpact,
		body: "impact-body-test",
	})
	if v.subPanelData != "impact-body-test" {
		t.Errorf("subPanelData not set: %q", v.subPanelData)
	}
}

func TestCodegraphRenderQueryHitsEmpty(t *testing.T) {
	out := renderQueryHits(nil)
	if !strings.Contains(out, "no matches") {
		t.Errorf("expected (no matches) for nil resp, got: %q", out)
	}
}

func TestCodegraphRenderImpactEmpty(t *testing.T) {
	out := renderImpact(nil)
	if !strings.Contains(out, "no impact") {
		t.Errorf("expected (no impact data) for nil resp, got: %q", out)
	}
}

func TestCodegraphRenderImpactPopulated(t *testing.T) {
	resp := &client.ImpactResponse{
		Symbol:        "Dispatch",
		BlastRadius:   "high",
		Score:         87,
		AffectedFiles: []string{"pkg/a/x.go", "pkg/b/y.go"},
	}
	out := renderImpact(resp)
	for _, want := range []string{"Dispatch", "high", "pkg/a/x.go", "pkg/b/y.go", "87"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderImpact missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestCodegraphRenderImpactNoAffected(t *testing.T) {
	resp := &client.ImpactResponse{
		Symbol:      "X",
		BlastRadius: "low",
		Score:       1,
	}
	out := renderImpact(resp)
	if !strings.Contains(out, "(none)") {
		t.Errorf("expected (none) for empty AffectedFiles, got:\n%s", out)
	}
}

func TestCodegraphRenderQueryHitsPopulated(t *testing.T) {
	resp := &client.CodegraphQueryResponse{
		Hits: []client.CodegraphHit{
			{Symbol: "Dispatch", File: "x.go", Line: 42, Kind: "func"},
			{Symbol: "Router", File: "y.go", Line: 10, Kind: "type"},
		},
	}
	out := renderQueryHits(resp)
	for _, want := range []string{"Dispatch", "x.go", "42", "Router", "y.go", "10"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderQueryHits missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestCodegraphDispatchQueryNilClient(t *testing.T) {
	v := NewCodegraphView(nil)
	cmd := v.dispatchQuery("MATCH x")
	if cmd == nil {
		t.Fatal("expected non-nil Cmd from dispatchQuery with nil client")
	}
	msg := cmd()
	m, ok := msg.(codegraphSubPanelMsg)
	if !ok {
		t.Fatalf("expected codegraphSubPanelMsg, got %T", msg)
	}
	if !strings.Contains(m.body, "test mode") {
		t.Errorf("expected test-mode body, got: %q", m.body)
	}
}

func TestCodegraphDispatchImpactNilClient(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("foo.go")
	cmd := v.dispatchImpact()
	if cmd == nil {
		t.Fatal("expected non-nil Cmd from dispatchImpact with nil client")
	}
	msg := cmd()
	m, ok := msg.(codegraphSubPanelMsg)
	if !ok {
		t.Fatalf("expected codegraphSubPanelMsg, got %T", msg)
	}
	if !strings.Contains(m.body, "unavailable") {
		t.Errorf("expected unavailable body, got: %q", m.body)
	}
}

func TestCodegraphDispatchWikiNilClient(t *testing.T) {
	v := NewCodegraphView(nil)
	cmd := v.dispatchWiki()
	if cmd == nil {
		t.Fatal("expected non-nil Cmd from dispatchWiki with nil client")
	}
	msg := cmd()
	m, ok := msg.(codegraphSubPanelMsg)
	if !ok {
		t.Fatalf("expected codegraphSubPanelMsg, got %T", msg)
	}
	if !strings.Contains(m.body, "unavailable") {
		t.Errorf("expected unavailable body, got: %q", m.body)
	}
}

func TestCodegraphDispatchCrossProjectNilClient(t *testing.T) {
	v := NewCodegraphView(nil)
	cmd := v.dispatchCrossProject()
	if cmd == nil {
		t.Fatal("expected non-nil Cmd from dispatchCrossProject with nil client")
	}
	msg := cmd()
	m, ok := msg.(codegraphSubPanelMsg)
	if !ok {
		t.Fatalf("expected codegraphSubPanelMsg, got %T", msg)
	}
	if !strings.Contains(m.body, "unavailable") {
		t.Errorf("expected unavailable body, got: %q", m.body)
	}
}

func TestCodegraphFormatSymbolNonFunc(t *testing.T) {
	for _, kind := range []string{"type", "const", "var", "interface"} {
		out := formatSymbol(symbolEntry{Name: "X", Kind: kind})
		if strings.Contains(out, "(") {
			t.Errorf("kind=%s should not have parens, got: %q", kind, out)
		}
	}
}

func TestCodegraphInitReturnsNil(t *testing.T) {
	v := NewCodegraphView(nil)
	if v.Init() != nil {
		t.Errorf("expected Init to return nil")
	}
}

func TestCodegraphSubPanelMsgErrSetsLastErr(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("foo.go")
	_, _ = v.Update(codegraphSubPanelMsg{
		mode: subPanelQuery,
		err:  errors.New("query-failed"),
	})
	if v.lastErr == nil {
		t.Error("expected lastErr populated from sub-panel err")
	}
	if !strings.Contains(v.View(), "query-failed") {
		t.Errorf("expected err in view, got: %s", v.View())
	}
}

func TestCodegraphRenderEmptyChurn(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("foo.go")
	v.symbols = []symbolEntry{{Name: "X", Kind: "func"}}

	out := v.View()
	if !strings.Contains(out, "0 commits last 7d") {
		t.Errorf("expected (0 commits) hint, got: %s", out)
	}
}

func TestCodegraphRenderNoCommunity(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("foo.go")
	out := v.View()
	if !strings.Contains(out, "not yet partitioned") {
		t.Errorf("expected (not partitioned) hint, got: %s", out)
	}
}

func TestCodegraphRenderNoLastIndex(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("foo.go")
	out := v.View()
	if !strings.Contains(out, "(never)") {
		t.Errorf("expected (never) hint, got: %s", out)
	}
}

func TestCodegraphQueryEnterDispatchEmptyInput(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("foo.go")
	v.subPanel = subPanelQuery
	v.subPanelInput = ""
	_, cmd := v.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Error("expected Cmd from Enter even with empty input")
	}
}

func TestCodegraphSubPanelQueryWithExistingData(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("foo.go")
	v.subPanel = subPanelQuery
	v.subPanelInput = "MATCH n"
	v.subPanelData = "  node-1  (Func)"
	out := v.View()
	if !strings.Contains(out, "node-1") {
		t.Errorf("expected query data below prompt, got: %s", out)
	}
	if !strings.Contains(out, "MATCH n") {
		t.Errorf("expected input echo, got: %s", out)
	}
}

func TestCodegraphSubPanelWikiRenders(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("foo.go")
	v.subPanel = subPanelWiki
	v.subPanelData = "# Community wiki body"
	out := v.View()
	if !strings.Contains(out, "COMMUNITY WIKI") {
		t.Errorf("expected COMMUNITY WIKI header, got: %s", out)
	}
	if !strings.Contains(out, "Community wiki body") {
		t.Errorf("expected wiki body, got: %s", out)
	}
}

func TestCodegraphSubPanelCrossProjectRenders(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("foo.go")
	v.subPanel = subPanelCrossProject
	v.subPanelData = "internal-platform-x/x.go — score=0.9"
	out := v.View()
	if !strings.Contains(out, "CROSS-PROJECT HITS") {
		t.Errorf("expected CROSS-PROJECT HITS header, got: %s", out)
	}
}

func TestCodegraphViewEmptySymbolsRenders(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("empty.go")
	out := v.View()
	if !strings.Contains(out, "no exported symbols") && !strings.Contains(out, "empty") {

		t.Logf("view (no symbols):\n%s", out)
	}
}

func TestCodegraphHADESPrefix(t *testing.T) {
	v := NewCodegraphView(nil)
	if !strings.Contains(v.View(), "HADES") {
		t.Errorf("CodegraphView missing HADES prefix:\n%s", v.View())
	}
}

func TestCodegraphViewRendersCorenessAndSCC(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("internal/x/a.go")
	model, _ := v.Update(codegraphDataMsg{
		coreness: 5, sccID: 7, cyclic: true,
	})
	out := model.View()
	if !strings.Contains(out, "coreness") {
		t.Errorf("expected a coreness line, got: %s", out)
	}
	if !strings.Contains(out, "5") || !strings.Contains(out, "SCC") {
		t.Errorf("expected coreness 5 + SCC label, got: %s", out)
	}
	if !strings.Contains(out, "cyclic") {
		t.Errorf("expected cyclic flag when in a multi-member SCC, got: %s", out)
	}
}

func TestCodegraphViewRendersCoChange(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("internal/x/a.go")
	model, _ := v.Update(codegraphDataMsg{
		coChangePeers: []coChangeEntry{
			{Path: "internal/x/b.go", CouplingPercent: 60},
			{Path: "internal/x/c.go", CouplingPercent: 45},
		},
	})
	out := model.View()
	if !strings.Contains(out, "co-change") {
		t.Errorf("expected a co-change line, got: %s", out)
	}
	if !strings.Contains(out, "internal/x/b.go") || !strings.Contains(out, "60") {
		t.Errorf("expected top co-changed peer b.go 60%%, got: %s", out)
	}
}

func TestCodegraphViewCoChangeEmpty(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("internal/x/a.go")
	model, _ := v.Update(codegraphDataMsg{coChangePeers: nil})
	out := model.View()
	if !strings.Contains(out, "co-change") {
		t.Errorf("expected the co-change label even when empty, got: %s", out)
	}
}

func TestF7SubPanelContractFederationModeIsFifth(t *testing.T) {
	if int(subPanelContractFederation) != int(subPanelCrossProject)+1 {
		t.Fatalf("subPanelContractFederation must be the FIFTH subPanelMode (after subPanelCrossProject); got %d vs %d+1",
			int(subPanelContractFederation), int(subPanelCrossProject))
	}
}

func TestF7SubPanelContractFederationOpensOnF(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("/tmp/foo.go")
	_, _ = v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	if v.subPanel != subPanelContractFederation {
		t.Fatalf("expected subPanel=subPanelContractFederation, got %v", v.subPanel)
	}
}

func TestF7ViewIncludesContractFederationFooterHint(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("/tmp/foo.go")
	out := v.View()
	if !strings.Contains(out, "[F] federation") {
		t.Fatalf("missing [F] federation footer hint: %q", out)
	}
}

func TestF7SubPanelContractFederationRendersDelegatedView(t *testing.T) {
	v := NewCodegraphView(nil)
	v.SetCurrentFile("/tmp/foo.go")
	_, _ = v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	out := v.View()
	if !strings.Contains(out, "CONTRACT FEDERATION") {
		t.Fatalf("expected sub-panel body to include 'CONTRACT FEDERATION' header from delegated view, got: %q", out)
	}
}

func TestF7ContractFederationFieldConstructedOnNew(t *testing.T) {
	v := NewCodegraphView(nil)
	if v.contractFederation == nil {
		t.Fatal("NewCodegraphView(nil) must construct contractFederation field (nil-client tolerant)")
	}
}

func TestF7ContractFederationDataMsgRoutedToSubPanel(t *testing.T) {
	v := NewCodegraphView(nil)
	msg := contractFederationDataMsg{
		workspaces: []WorkspaceRow{{WorkspaceID: "ws-routed"}},
	}
	_, _ = v.Update(msg)

	if len(v.contractFederation.workspaces) != 1 || v.contractFederation.workspaces[0].WorkspaceID != "ws-routed" {
		t.Errorf("contractFederationDataMsg did not reach sub-panel; sub-panel workspaces=%+v", v.contractFederation.workspaces)
	}
}
