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

func TestHRAViewSkeleton(t *testing.T) {
	v := NewHRAView(nil)
	if !strings.Contains(v.View(), "HRA QUEUE") {
		t.Errorf("expected HRA QUEUE header, got: %s", v.View())
	}
}

func TestHRAViewSatisfiesTeaModel(t *testing.T) {
	var _ tea.Model = NewHRAView(nil)
}

func TestHRAViewRefetchNil(t *testing.T) {
	if NewHRAView(nil).Refetch() != nil {
		t.Error("expected nil")
	}
}

func TestHRAViewInit(t *testing.T) {
	if NewHRAView(nil).Init() != nil {
		t.Error("expected nil")
	}
	if NewHRAView(client.NewWithBaseURL("http://x")).Init() == nil {
		t.Error("expected non-nil")
	}
}

func TestHRAUpdateDataMsg(t *testing.T) {
	v := NewHRAView(nil)
	v.Update(hraDataMsg{
		session: &client.SessionInfo{
			SessionID: "sess-1", State: "hra_waiting", Mode: "autonomous",
			RecentTransitions: []client.StateTransition{
				{From: "running", To: "hra_waiting", Reason: "operator-attention"},
			},
		},
	})
	out := v.View()
	for _, want := range []string{"sess-1", "hra_waiting", "autonomous", "running"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestHRAUpdateErrSurfaces(t *testing.T) {
	v := NewHRAView(nil)
	v.Update(hraDataMsg{err: errors.New("orch down")})
	if !strings.Contains(v.View(), "orch down") {
		t.Errorf("expected err, got: %s", v.View())
	}
}

func TestHRALoadingShown(t *testing.T) {
	v := NewHRAView(nil)
	if !strings.Contains(v.View(), "loading") {
		t.Errorf("expected loading hint")
	}
}

func TestHRANoTransitionsShown(t *testing.T) {
	v := NewHRAView(nil)
	v.Update(hraDataMsg{session: &client.SessionInfo{SessionID: "x"}})
	if !strings.Contains(v.View(), "(none)") {
		t.Errorf("expected (none) hint when no transitions")
	}
}

func TestHRARefetchHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.SessionInfo{SessionID: "test-id", State: "idle"})
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	msg := NewHRAView(c).Refetch()()
	m := msg.(hraDataMsg)
	if m.err != nil {
		t.Fatalf("err = %v", m.err)
	}
	if m.session.SessionID != "test-id" {
		t.Errorf("sessionID = %q", m.session.SessionID)
	}
}

func TestHRARefetchHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()
	if (NewHRAView(client.NewWithBaseURL(srv.URL)).Refetch()().(hraDataMsg)).err == nil {
		t.Fatal("expected error")
	}
}

func TestHRAUpdateIgnoresOther(t *testing.T) {
	v := NewHRAView(nil)
	updated, _ := v.Update(tea.KeyMsg{})
	if _, ok := updated.(*HRAView); !ok {
		t.Error("expected *HRAView")
	}
}

func TestHRAHADESPrefix(t *testing.T) {
	v := NewHRAView(nil)
	if !strings.Contains(v.View(), "HADES") {
		t.Errorf("HRAView missing HADES prefix:\n%s", v.View())
	}
}
