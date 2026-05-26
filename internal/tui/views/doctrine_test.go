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

func TestDoctrineViewSkeleton(t *testing.T) {
	v := NewDoctrineView(nil)
	if !strings.Contains(v.View(), "DOCTRINE") {
		t.Errorf("expected DOCTRINE header, got: %s", v.View())
	}
}

func TestDoctrineViewSatisfiesTeaModel(t *testing.T) {
	var _ tea.Model = NewDoctrineView(nil)
}

func TestDoctrineViewRefetchNil(t *testing.T) {
	if NewDoctrineView(nil).Refetch() != nil {
		t.Error("expected nil")
	}
}

func TestDoctrineViewInit(t *testing.T) {
	if NewDoctrineView(nil).Init() != nil {
		t.Error("expected nil")
	}
	if NewDoctrineView(client.NewWithBaseURL("http://x")).Init() == nil {
		t.Error("expected non-nil")
	}
}

func TestDoctrineUpdateDataMsg(t *testing.T) {
	v := NewDoctrineView(nil)
	v.Update(doctrineDataMsg{
		active: &client.DoctrineV2ActiveResp{
			Name: "default", SchemaVersion: "v1", Source: "embed",
		},
		list: &client.DoctrineV2ListResp{
			Items: []client.DoctrineV2ListItem{
				{Name: "default", Source: "embed", SchemaVersion: "v1"},
				{Name: "max-scope", Source: "user", SchemaVersion: "v1"},
			},
		},
		amendments: &client.DoctrineProposalList{
			Proposals: []client.DoctrineProposal{
				{ID: "ADR-0", Title: "test", Status: "proposed"},
			},
		},
	})
	out := v.View()
	for _, want := range []string{"default", "embed", "max-scope", "ADR-0", "proposed"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestDoctrineUpdateErr(t *testing.T) {
	v := NewDoctrineView(nil)
	v.Update(doctrineDataMsg{err: errors.New("doc-down")})
	if !strings.Contains(v.View(), "doc-down") {
		t.Error("expected error visible")
	}
}

func TestDoctrineLoading(t *testing.T) {
	if !strings.Contains(NewDoctrineView(nil).View(), "loading") {
		t.Error("expected loading hint")
	}
}

func TestDoctrineNoAmendments(t *testing.T) {
	v := NewDoctrineView(nil)
	v.Update(doctrineDataMsg{
		active: &client.DoctrineV2ActiveResp{Name: "default"},
	})
	if !strings.Contains(v.View(), "none active") {
		t.Errorf("expected (none active) hint, got: %s", v.View())
	}
}

func TestDoctrineRefetchHTTP(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/active", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.DoctrineV2ActiveResp{Name: "default"})
	})
	mux.HandleFunc("/v1/doctrine/list", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.DoctrineV2ListResp{})
	})
	mux.HandleFunc("/v1/doctrine/propose-list", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.DoctrineProposalList{})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	msg := NewDoctrineView(client.NewWithBaseURL(srv.URL)).Refetch()()
	if (msg.(doctrineDataMsg)).err != nil {
		t.Fatalf("err: %v", (msg.(doctrineDataMsg)).err)
	}
}

func TestDoctrineRefetchActiveError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()
	if (NewDoctrineView(client.NewWithBaseURL(srv.URL)).Refetch()().(doctrineDataMsg)).err == nil {
		t.Fatal("expected error")
	}
}

func TestDoctrineUpdateIgnoresOther(t *testing.T) {
	updated, _ := NewDoctrineView(nil).Update(tea.KeyMsg{})
	if _, ok := updated.(*DoctrineView); !ok {
		t.Error("expected *DoctrineView")
	}
}

func TestDoctrineHADESPrefix(t *testing.T) {
	v := NewDoctrineView(nil)
	if !strings.Contains(v.View(), "HADES") {
		t.Errorf("DoctrineView missing HADES prefix:\n%s", v.View())
	}
}
