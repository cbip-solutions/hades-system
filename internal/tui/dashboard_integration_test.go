//go:build integration

package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/tui/views"
)

func mockDaemonServer(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.HealthResponse{
			Status: "ok", Version: "test-v0.12", UptimeSeconds: 42,
		})
	})

	mux.HandleFunc("/v1/workforce/workers", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.WorkforceWorker{
				{ID: "alice", SpecID: "spec-impl", Status: "active", TaskID: "t-1", StartedAt: time.Now().Unix() - 60},
				{ID: "bob", SpecID: "spec-review", Status: "idle", StartedAt: time.Now().Unix() - 120},
			},
			"count": 2,
		})
	})

	mux.HandleFunc("/v1/budget", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.BudgetSummaryResp{
			Range:    "24h",
			TotalUSD: 12.34,
			ByTier: []client.BudgetTierSpend{
				{Project: "internal-platform-x", Profile: "implementer", Tier: "anthropic-bypass", SpendUSD: 5.50},
			},
		})
	})
	mux.HandleFunc("/v1/augment/summary", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.AugmentSummaryResponse{
			Date:           "2026-05-12",
			TotalCost:      0.42,
			CacheHitRate:   0.82,
			KGQueriesFired: 256,
			TokensConsumed: 1024,
		})
	})

	mux.HandleFunc("/v1/audit/events", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.AuditEvent{
				{ID: "evt-001", ProjectID: "internal-platform-x", Type: "task.complete", EmittedAt: time.Now().Unix()},
			},
			"count": 1,
		})
	})

	mux.HandleFunc("/v1/orchestrator/state", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.SessionInfo{
			SessionID: "sess-test-123", State: "running", Mode: "autonomous",
			RecentTransitions: []client.StateTransition{
				{From: "idle", To: "running", Reason: "operator-start", Timestamp: time.Now().Unix()},
			},
		})
	})

	mux.HandleFunc("/v1/doctrine/propose-list", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.DoctrineProposalList{
			Proposals: []client.DoctrineProposal{
				{ID: "ADR-001", Title: "test proposal", Status: "proposed", ProposedAt: time.Now().Unix() - 60},
			},
		})
	})

	mux.HandleFunc("/v1/mcpgateway/codegraph", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.CodegraphQueryResponse{
			Hits: []client.CodegraphHit{
				{Symbol: "Dispatch", File: "internal/orchestrator/dispatch.go", Line: 42, Kind: "func"},
			},
		})
	})
	mux.HandleFunc("/v1/mcpgateway/context", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.Context360Response{
			Symbol: "Dispatch", Callers: []string{"a.go"}, Community: "C-001",
		})
	})
	mux.HandleFunc("/v1/mcpgateway/impact", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.ImpactResponse{
			Symbol: "Dispatch", BlastRadius: "medium", Score: 55,
		})
	})

	mux.HandleFunc("/v1/knowledge/stats", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.KnowledgeStatsResponse{
			TotalDocs:       42,
			ByType:          map[string]int{"memory": 30, "spec": 12},
			LastIndexedUnix: time.Now().Unix(),
		})
	})

	mux.HandleFunc("/v1/hermes/probe", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.HermesProbeResp{
			Status: "ok", Detail: "5 skills registered",
		})
	})

	mux.HandleFunc("/v1/doctrine/active", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.DoctrineV2ActiveResp{
			Name: "default", SchemaVersion: "v1", Source: "embed",
		})
	})
	mux.HandleFunc("/v1/doctrine/list", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.DoctrineV2ListResp{
			Items: []client.DoctrineV2ListItem{
				{Name: "default", Source: "embed", SchemaVersion: "v1"},
			},
		})
	})

	mux.HandleFunc("/v1/projects", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"projects": []client.Project{
				{ID: "p-1", Alias: "internal-platform-x", Path: "/projects/internal-platform-x", AutonomousState: "active"},
			},
		})
	})

	mux.HandleFunc("/v1/inbox/list", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.InboxListResponse{
			Rows: []client.InboxCacheRow{
				{CacheID: 1, Severity: "info", ProjectAlias: "internal-platform-x",
					EventType: "task.complete", CreatedAt: time.Now()},
			},
		})
	})

	return httptest.NewServer(mux)
}

func newIntegrationModel(srv *httptest.Server) Model {
	c := client.NewWithBaseURL(srv.URL)
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

func drainCmd(t *testing.T, m Model, cmd tea.Cmd, depth int) Model {
	if cmd == nil || depth > 4 {
		return m
	}

	ch := make(chan tea.Msg, 1)
	go func() {
		ch <- cmd()
	}()
	select {
	case msg := <-ch:
		return applyMsg(t, m, msg, depth+1)
	case <-time.After(50 * time.Millisecond):

		return m
	}
}

func applyMsg(t *testing.T, m Model, msg tea.Msg, depth int) Model {
	if msg == nil || depth > 4 {
		return m
	}

	if _, ok := msg.(panelTickMsg); ok {
		return m
	}

	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			m = drainCmd(t, m, c, depth+1)
		}
		return m
	}
	updated, cmd := m.Update(msg)
	mm := updated.(Model)
	if cmd != nil {
		mm = drainCmd(t, mm, cmd, depth+1)
	}
	return mm
}

func TestDashboardIntegrationFKeys(t *testing.T) {
	srv := mockDaemonServer(t)
	defer srv.Close()

	m := newIntegrationModel(srv)
	m.codegraph.SetCurrentFile("internal/orchestrator/dispatch.go")

	cases := []struct {
		key          string
		panel        panelKey
		wantContains []string
	}{
		{"F2", panelWorkforce, []string{"WORKFORCE", "alice"}},
		{"F3", panelCost, []string{"COST", "$12.34", "82%"}},
		{"F4", panelAudit, []string{"AUDIT", "task.complete"}},
		{"F5", panelHRA, []string{"HRA QUEUE", "sess-test-123", "running"}},
		{"F6", panelConfirmations, []string{"CONFIRMATIONS", "ADR-001"}},
		{"F7", panelCodegraph, []string{"CODE GRAPH", "Dispatch", "C-001"}},
		{"F8", panelMemory, []string{"MEMORY", "42"}},
		{"F9", panelSkills, []string{"SKILLS", "ok"}},
		{"F10", panelDoctrine, []string{"DOCTRINE", "default"}},
		{"F11", panelCrossProject, []string{"CROSS-PROJECT", "internal-platform-x"}},
		{"F12", panelInbox, []string{"INBOX", "task.complete"}},
	}

	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {

			updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tc.key)})
			m = updated.(Model)
			if m.activePanel != tc.panel {
				t.Errorf("key %s: activePanel = %v, want %v", tc.key, m.activePanel, tc.panel)
				return
			}
			m = drainCmd(t, m, cmd, 0)

			view := m.View()
			for _, want := range tc.wantContains {
				if !strings.Contains(view, want) {
					t.Errorf("F-key %s: view missing %q\n--- got ---\n%s",
						tc.key, want, view)
				}
			}
		})
	}
}

func TestDashboardIntegrationFetchDoctrineModeHappyPath(t *testing.T) {
	srv := mockDaemonServer(t)
	defer srv.Close()

	m := newIntegrationModel(srv)

	if m.codegraph.DoctrineMode() != "capa-firewall" {
		t.Fatalf("pre-Init: expected fail-closed default, got %q",
			m.codegraph.DoctrineMode())
	}

	cmd := m.fetchDoctrineMode()
	if cmd == nil {
		t.Fatal("expected non-nil fetchDoctrineMode Cmd")
	}
	msg := cmd()
	applied, ok := msg.(doctrineModeAppliedMsg)
	if !ok {
		t.Fatalf("expected doctrineModeAppliedMsg, got %T", msg)
	}

	if applied.mode != "default" {
		t.Errorf("happy-path Cmd returned mode = %q, want %q (mock daemon's active doctrine name)",
			applied.mode, "default")
	}

	updated, _ := m.Update(applied)
	mm := updated.(Model)
	if got := mm.codegraph.DoctrineMode(); got != "default" {
		t.Errorf("post-Update: codegraph anchor mode = %q, want %q",
			got, "default")
	}
}

func TestDashboardIntegrationF1HelpStatic(t *testing.T) {
	srv := mockDaemonServer(t)
	defer srv.Close()

	m := newIntegrationModel(srv)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("F1")})
	m = updated.(Model)
	if m.activePanel != panelHelp {
		t.Errorf("expected panelHelp, got: %v", m.activePanel)
	}

	m = drainCmd(t, m, cmd, 0)
	view := m.View()
	if !strings.Contains(view, "HELP") {
		t.Errorf("expected HELP header, got: %s", view)
	}
}
