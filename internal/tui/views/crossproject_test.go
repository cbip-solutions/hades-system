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

func TestCrossProjectViewSkeleton(t *testing.T) {
	v := NewCrossProjectView(nil)
	if !strings.Contains(v.View(), "CROSS-PROJECT") {
		t.Errorf("expected CROSS-PROJECT header, got: %s", v.View())
	}
}

func TestCrossProjectViewSatisfiesTeaModel(t *testing.T) {
	var _ tea.Model = NewCrossProjectView(nil)
}

func TestCrossProjectViewRefetchNil(t *testing.T) {
	if NewCrossProjectView(nil).Refetch() != nil {
		t.Error("expected nil")
	}
}

func TestCrossProjectViewInit(t *testing.T) {
	if NewCrossProjectView(nil).Init() != nil {
		t.Error("expected nil")
	}
	if NewCrossProjectView(client.NewWithBaseURL("http://x")).Init() == nil {
		t.Error("expected non-nil")
	}
}

func TestCrossProjectUpdateDataMsg(t *testing.T) {
	v := NewCrossProjectView(nil)
	v.Update(crossProjectDataMsg{
		projects: []client.Project{
			{Alias: "internal-platform-x", Path: "/p/internal-platform-x", AutonomousState: "active",
				LastActivatedAt: time.Now().Add(-1 * time.Hour)},
		},
	})
	out := v.View()
	for _, want := range []string{"internal-platform-x", "/p/internal-platform-x", "active"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestCrossProjectUpdateErr(t *testing.T) {
	v := NewCrossProjectView(nil)
	v.Update(crossProjectDataMsg{err: errors.New("proj-down")})
	if !strings.Contains(v.View(), "proj-down") {
		t.Error("expected error visible")
	}
}

func TestCrossProjectEmpty(t *testing.T) {
	if !strings.Contains(NewCrossProjectView(nil).View(), "no projects") {
		t.Error("expected (no projects) hint")
	}
}

func TestCrossProjectRefetchHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"projects": []client.Project{
				{Alias: "a", Path: "/p"},
			},
		})
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	msg := NewCrossProjectView(c).Refetch()()
	if (msg.(crossProjectDataMsg)).err != nil {
		t.Fatalf("err: %v", (msg.(crossProjectDataMsg)).err)
	}
}

func TestCrossProjectRefetchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()
	if (NewCrossProjectView(client.NewWithBaseURL(srv.URL)).Refetch()().(crossProjectDataMsg)).err == nil {
		t.Fatal("expected error")
	}
}

func TestCrossProjectUpdateIgnoresOther(t *testing.T) {
	updated, _ := NewCrossProjectView(nil).Update(tea.KeyMsg{})
	if _, ok := updated.(*CrossProjectView); !ok {
		t.Error("expected *CrossProjectView")
	}
}

func TestCrossProjectHADESPrefix(t *testing.T) {
	v := NewCrossProjectView(nil)
	if !strings.Contains(v.View(), "HADES") {
		t.Errorf("CrossProjectView missing HADES prefix:\n%s", v.View())
	}
}
