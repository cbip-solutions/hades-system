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

func TestSkillsViewSkeleton(t *testing.T) {
	v := NewSkillsView(nil)
	if !strings.Contains(v.View(), "SKILLS") {
		t.Errorf("expected SKILLS header, got: %s", v.View())
	}
}

func TestSkillsViewSatisfiesTeaModel(t *testing.T) {
	var _ tea.Model = NewSkillsView(nil)
}

func TestSkillsViewRefetchNil(t *testing.T) {
	if NewSkillsView(nil).Refetch() != nil {
		t.Error("expected nil")
	}
}

func TestSkillsViewInit(t *testing.T) {
	if NewSkillsView(nil).Init() != nil {
		t.Error("expected nil")
	}
	if NewSkillsView(client.NewWithBaseURL("http://x")).Init() == nil {
		t.Error("expected non-nil")
	}
}

func TestSkillsUpdateDataMsg(t *testing.T) {
	cases := []string{"ok", "warn", "fail"}
	for _, status := range cases {
		v := NewSkillsView(nil)
		v.Update(skillsDataMsg{
			probe: &client.HermesProbeResp{Status: status, Detail: "test " + status},
		})
		out := v.View()
		if !strings.Contains(out, status) {
			t.Errorf("status %q missing in:\n%s", status, out)
		}
	}
}

func TestSkillsUpdateErr(t *testing.T) {
	v := NewSkillsView(nil)
	v.Update(skillsDataMsg{err: errors.New("hermes down")})
	if !strings.Contains(v.View(), "hermes down") {
		t.Error("expected error visible")
	}
}

func TestSkillsLoading(t *testing.T) {
	if !strings.Contains(NewSkillsView(nil).View(), "loading") {
		t.Error("expected loading hint")
	}
}

func TestSkillsRefetchHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.HermesProbeResp{Status: "ok"})
	}))
	defer srv.Close()
	msg := NewSkillsView(client.NewWithBaseURL(srv.URL)).Refetch()()
	if (msg.(skillsDataMsg)).err != nil {
		t.Errorf("err = %v", (msg.(skillsDataMsg)).err)
	}
}

func TestSkillsRefetchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()
	if (NewSkillsView(client.NewWithBaseURL(srv.URL)).Refetch()().(skillsDataMsg)).err == nil {
		t.Fatal("expected error")
	}
}

func TestSkillsUpdateIgnoresOther(t *testing.T) {
	updated, _ := NewSkillsView(nil).Update(tea.KeyMsg{})
	if _, ok := updated.(*SkillsView); !ok {
		t.Error("expected *SkillsView")
	}
}

func TestSkillsHADESPrefix(t *testing.T) {
	v := NewSkillsView(nil)
	if !strings.Contains(v.View(), "HADES") {
		t.Errorf("SkillsView missing HADES prefix:\n%s", v.View())
	}
}
