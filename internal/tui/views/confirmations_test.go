package views

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func TestConfirmationsViewSkeleton(t *testing.T) {
	v := NewConfirmationsView(nil)
	if !strings.Contains(v.View(), "CONFIRMATIONS") {
		t.Errorf("expected CONFIRMATIONS header, got: %s", v.View())
	}
}

func TestConfirmationsViewSatisfiesTeaModel(t *testing.T) {
	var _ tea.Model = NewConfirmationsView(nil)
}

func TestConfirmationsViewRefetchNil(t *testing.T) {
	if NewConfirmationsView(nil).Refetch() != nil {
		t.Error("expected nil")
	}
}

func TestConfirmationsViewInit(t *testing.T) {
	if NewConfirmationsView(nil).Init() != nil {
		t.Error("expected nil")
	}
	if NewConfirmationsView(client.NewWithBaseURL("http://x")).Init() == nil {
		t.Error("expected non-nil")
	}
}

func TestConfirmationsUpdateDataMsg(t *testing.T) {
	v := NewConfirmationsView(nil)
	v.Update(confirmationsDataMsg{
		list: &client.DoctrineProposalList{
			Proposals: []client.DoctrineProposal{
				{ID: "ADR-001", Title: "test proposal", Status: "proposed", ProposedAt: time.Now().Unix()},
			},
		},
	})
	out := v.View()
	for _, want := range []string{"ADR-001", "proposed", "test proposal"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestConfirmationsUpdateErr(t *testing.T) {
	v := NewConfirmationsView(nil)
	v.Update(confirmationsDataMsg{err: errors.New("fail")})
	if !strings.Contains(v.View(), "fail") {
		t.Error("expected error visible")
	}
}

func TestConfirmationsEmpty(t *testing.T) {
	v := NewConfirmationsView(nil)
	if !strings.Contains(v.View(), "all clear") {
		t.Error("expected (all clear) hint")
	}
}

func TestConfirmationsRefetchHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.DoctrineProposalList{
			Proposals: []client.DoctrineProposal{{ID: "x", Title: "t", Status: "s"}},
		})
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	msg := NewConfirmationsView(c).Refetch()()
	if (msg.(confirmationsDataMsg)).err != nil {
		t.Fatalf("err: %v", (msg.(confirmationsDataMsg)).err)
	}
}

func TestConfirmationsRefetchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	if (NewConfirmationsView(c).Refetch()().(confirmationsDataMsg)).err == nil {
		t.Fatal("expected error")
	}
}

func TestConfirmationsUpdateIgnoresOther(t *testing.T) {
	updated, _ := NewConfirmationsView(nil).Update(tea.KeyMsg{})
	if _, ok := updated.(*ConfirmationsView); !ok {
		t.Error("expected *ConfirmationsView")
	}
}

func TestConfirmationsHADESPrefix(t *testing.T) {
	v := NewConfirmationsView(nil)
	if !strings.Contains(v.View(), "HADES") {
		t.Errorf("ConfirmationsView missing HADES prefix:\n%s", v.View())
	}
}
