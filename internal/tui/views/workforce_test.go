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

func TestWorkforceViewSkeleton(t *testing.T) {
	v := NewWorkforceView(nil)
	out := v.View()
	if !strings.Contains(out, "WORKFORCE") {
		t.Errorf("expected WORKFORCE header, got: %s", out)
	}
}

func TestWorkforceViewSatisfiesTeaModel(t *testing.T) {
	var _ tea.Model = NewWorkforceView(nil)
}

func TestWorkforceViewRefetchNil(t *testing.T) {
	if NewWorkforceView(nil).Refetch() != nil {
		t.Errorf("expected nil Refetch for nil client")
	}
}

func TestWorkforceViewInitWithNilClient(t *testing.T) {
	if NewWorkforceView(nil).Init() != nil {
		t.Errorf("expected nil Init for nil client")
	}
}

func TestWorkforceViewInitWithClientReturnsCmd(t *testing.T) {

	c := client.NewWithBaseURL("http://nowhere")
	if NewWorkforceView(c).Init() == nil {
		t.Errorf("expected non-nil Init for non-nil client")
	}
}

func TestWorkforceUpdateDataMsg(t *testing.T) {
	v := NewWorkforceView(nil)
	updated, _ := v.Update(workforceDataMsg{
		workers: []workforceWorker{
			{ID: "alice", SpecID: "spec-1", Status: "active", TaskID: "task-1", StartedAt: time.Now()},
		},
	})
	wv := updated.(*WorkforceView)
	if len(wv.workers) != 1 || wv.workers[0].ID != "alice" {
		t.Errorf("workers not populated: %+v", wv.workers)
	}
	out := wv.View()
	if !strings.Contains(out, "alice") {
		t.Errorf("expected alice in view, got: %s", out)
	}
}

func TestWorkforceUpdateErrSurfaces(t *testing.T) {
	v := NewWorkforceView(nil)
	v.Update(workforceDataMsg{err: errors.New("test error")})
	if !strings.Contains(v.View(), "test error") {
		t.Errorf("error not surfaced in view: %s", v.View())
	}
}

func TestWorkforceUpdateIgnoresOtherMessages(t *testing.T) {
	v := NewWorkforceView(nil)
	updated, _ := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if _, ok := updated.(*WorkforceView); !ok {
		t.Errorf("expected *WorkforceView from Update")
	}
}

func TestWorkforceRefetchHTTPSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.WorkforceWorker{
				{ID: "w-1", SpecID: "s-1", Status: "active", TaskID: "t-1", StartedAt: 1715000000},
			},
		})
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	v := NewWorkforceView(c)
	cmd := v.Refetch()
	msg := cmd()
	m, ok := msg.(workforceDataMsg)
	if !ok {
		t.Fatalf("expected workforceDataMsg, got %T", msg)
	}
	if m.err != nil {
		t.Errorf("err = %v", m.err)
	}
	if len(m.workers) != 1 || m.workers[0].ID != "w-1" {
		t.Errorf("unexpected workers: %+v", m.workers)
	}
}

func TestWorkforceRefetchHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	v := NewWorkforceView(c)
	msg := v.Refetch()()
	if (msg.(workforceDataMsg)).err == nil {
		t.Fatal("expected error from 503")
	}
}

func TestWorkforceEmptyShowsHint(t *testing.T) {
	v := NewWorkforceView(nil)
	if !strings.Contains(v.View(), "no active workers") {
		t.Errorf("expected (no active workers) hint")
	}
}

func TestTruncateView(t *testing.T) {
	cases := []struct {
		in   string
		w    int
		want string
	}{
		{"abc", 10, "abc"},
		{"abcdefghij", 5, "abcd…"},
		{"x", 1, "x"},
		{"", 5, ""},
		{"xyz", 0, "xyz"},
		{"abc", -1, "abc"},
		{"hello", 1, "h"},
	}
	for _, tc := range cases {
		if got := truncateView(tc.in, tc.w); got != tc.want {
			t.Errorf("truncateView(%q, %d) = %q, want %q", tc.in, tc.w, got, tc.want)
		}
	}
}

func TestWorkforceHADESPrefix(t *testing.T) {
	v := NewWorkforceView(nil)
	if !strings.Contains(v.View(), "HADES") {
		t.Errorf("WorkforceView missing HADES prefix:\n%s", v.View())
	}
}
