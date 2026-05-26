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

func TestMemoryViewSkeleton(t *testing.T) {
	v := NewMemoryView(nil)
	if !strings.Contains(v.View(), "MEMORY") {
		t.Errorf("expected MEMORY header, got: %s", v.View())
	}
}

func TestMemoryViewSatisfiesTeaModel(t *testing.T) {
	var _ tea.Model = NewMemoryView(nil)
}

func TestMemoryViewRefetchNil(t *testing.T) {
	if NewMemoryView(nil).Refetch() != nil {
		t.Error("expected nil")
	}
}

func TestMemoryViewInit(t *testing.T) {
	if NewMemoryView(nil).Init() != nil {
		t.Error("expected nil")
	}
	if NewMemoryView(client.NewWithBaseURL("http://x")).Init() == nil {
		t.Error("expected non-nil")
	}
}

func TestMemoryUpdateDataMsg(t *testing.T) {
	v := NewMemoryView(nil)
	v.Update(memoryDataMsg{
		stats: &client.KnowledgeStatsResponse{
			TotalDocs:       42,
			ByType:          map[string]int{"memory": 30, "spec": 12},
			LastIndexedUnix: 1715000000,
		},
	})
	out := v.View()
	for _, want := range []string{"42", "memory", "spec", "30", "12"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestMemoryUpdateErr(t *testing.T) {
	v := NewMemoryView(nil)
	v.Update(memoryDataMsg{err: errors.New("kg-down")})
	if !strings.Contains(v.View(), "kg-down") {
		t.Errorf("expected error visible")
	}
}

func TestMemoryLoading(t *testing.T) {
	v := NewMemoryView(nil)
	if !strings.Contains(v.View(), "loading") {
		t.Errorf("expected loading hint")
	}
}

func TestMemoryRefetchHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.KnowledgeStatsResponse{TotalDocs: 5})
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	msg := NewMemoryView(c).Refetch()()
	m := msg.(memoryDataMsg)
	if m.err != nil {
		t.Fatalf("err = %v", m.err)
	}
	if m.stats.TotalDocs != 5 {
		t.Errorf("TotalDocs = %d", m.stats.TotalDocs)
	}
}

func TestMemoryRefetchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	if (NewMemoryView(c).Refetch()().(memoryDataMsg)).err == nil {
		t.Fatal("expected error")
	}
}

func TestMemoryUpdateIgnoresOther(t *testing.T) {
	updated, _ := NewMemoryView(nil).Update(tea.KeyMsg{})
	if _, ok := updated.(*MemoryView); !ok {
		t.Error("expected *MemoryView")
	}
}

func TestMemoryHADESPrefix(t *testing.T) {
	v := NewMemoryView(nil)
	if !strings.Contains(v.View(), "HADES") {
		t.Errorf("MemoryView missing HADES prefix:\n%s", v.View())
	}
}
