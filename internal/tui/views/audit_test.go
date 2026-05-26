package views

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func TestAuditViewSkeleton(t *testing.T) {
	v := NewAuditView(nil)
	if !strings.Contains(v.View(), "AUDIT") {
		t.Errorf("expected AUDIT header, got: %s", v.View())
	}
}

func TestAuditViewSatisfiesTeaModel(t *testing.T) {
	var _ tea.Model = NewAuditView(nil)
}

func TestAuditViewRefetchNil(t *testing.T) {
	if NewAuditView(nil).Refetch() != nil {
		t.Error("expected nil")
	}
}

func TestAuditViewInit(t *testing.T) {
	if NewAuditView(nil).Init() != nil {
		t.Error("expected nil Init for nil client")
	}
	if NewAuditView(client.NewWithBaseURL("http://x")).Init() == nil {
		t.Error("expected non-nil Init for non-nil client")
	}
}

func TestAuditUpdateDataMsg(t *testing.T) {
	v := NewAuditView(nil)
	v.Update(auditDataMsg{
		events: []client.AuditEvent{
			{ID: "evt-1", ProjectID: "p", Type: "task.complete", EmittedAt: 1715000000},
		},
	})
	out := v.View()
	for _, want := range []string{"task.complete", "evt-1", "AUDIT"} {
		if !strings.Contains(out, want) {
			t.Errorf("View missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestAuditUpdateErrSurfaces(t *testing.T) {
	v := NewAuditView(nil)
	v.Update(auditDataMsg{err: errors.New("audit down")})
	if !strings.Contains(v.View(), "audit down") {
		t.Errorf("expected err: %s", v.View())
	}
}

func TestAuditEmptyEvents(t *testing.T) {
	v := NewAuditView(nil)
	if !strings.Contains(v.View(), "no events") {
		t.Errorf("expected (no events) hint")
	}
}

func TestAuditRefetchHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.AuditEvent{
				{ID: "evt-x", Type: "test.event", ProjectID: "p", EmittedAt: 1},
			},
			"count": 1,
		})
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	msg := NewAuditView(c).Refetch()()
	m := msg.(auditDataMsg)
	if m.err != nil {
		t.Fatalf("err = %v", m.err)
	}
	if len(m.events) != 1 {
		t.Errorf("events = %d", len(m.events))
	}
}

func TestAuditRefetchHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	if (NewAuditView(c).Refetch()().(auditDataMsg)).err == nil {
		t.Fatal("expected error")
	}
}

func TestAuditUpdateIgnoresKey(t *testing.T) {
	v := NewAuditView(nil)
	updated, _ := v.Update(tea.KeyMsg{})
	if _, ok := updated.(*AuditView); !ok {
		t.Error("expected *AuditView")
	}
}

func TestAuditViewHADESPrefix(t *testing.T) {
	v := NewAuditView(nil)
	out := v.View()
	if !strings.Contains(out, "HADES") {
		t.Errorf("AuditView missing HADES prefix:\n%s", out)
	}
}
