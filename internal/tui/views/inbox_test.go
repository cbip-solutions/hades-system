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

func TestInboxViewSkeleton(t *testing.T) {
	v := NewInboxView(nil)
	if !strings.Contains(v.View(), "INBOX") {
		t.Errorf("expected INBOX header, got: %s", v.View())
	}
}

func TestInboxViewSatisfiesTeaModel(t *testing.T) {
	var _ tea.Model = NewInboxView(nil)
}

func TestInboxViewRefetchNil(t *testing.T) {
	if NewInboxView(nil).Refetch() != nil {
		t.Error("expected nil")
	}
}

func TestInboxViewInit(t *testing.T) {
	if NewInboxView(nil).Init() != nil {
		t.Error("expected nil")
	}
	if NewInboxView(client.NewWithBaseURL("http://x")).Init() == nil {
		t.Error("expected non-nil")
	}
}

func TestInboxUpdateDataMsg(t *testing.T) {
	v := NewInboxView(nil)
	now := time.Now()
	v.Update(inboxDataMsg{
		rows: []client.InboxCacheRow{
			{CacheID: 1, Severity: "urgent", ProjectAlias: "internal-platform-x",
				EventType: "task.complete", CreatedAt: now},
			{CacheID: 2, Severity: "info-digest", ProjectAlias: "test",
				EventType: "info", CreatedAt: now, AckedAt: &now},
		},
	})
	out := v.View()
	for _, want := range []string{"urgent", "internal-platform-x", "task.complete", "1 unread"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestInboxUpdateErr(t *testing.T) {
	v := NewInboxView(nil)
	v.Update(inboxDataMsg{err: errors.New("inbox-down")})
	if !strings.Contains(v.View(), "inbox-down") {
		t.Error("expected error visible")
	}
}

func TestInboxEmpty(t *testing.T) {
	if !strings.Contains(NewInboxView(nil).View(), "no unread notifications") {
		t.Error("expected (no unread notifications) hint")
	}
}

func TestInboxRefetchHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.InboxListResponse{
			Rows: []client.InboxCacheRow{
				{CacheID: 1, Severity: "info", ProjectAlias: "x"},
			},
		})
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	msg := NewInboxView(c).Refetch()()
	m := msg.(inboxDataMsg)
	if m.err != nil {
		t.Fatalf("err: %v", m.err)
	}
	if len(m.rows) != 1 {
		t.Errorf("rows = %d", len(m.rows))
	}
}

func TestInboxRefetchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()
	if (NewInboxView(client.NewWithBaseURL(srv.URL)).Refetch()().(inboxDataMsg)).err == nil {
		t.Fatal("expected error")
	}
}

func TestInboxUpdateIgnoresOther(t *testing.T) {
	updated, _ := NewInboxView(nil).Update(tea.KeyMsg{})
	if _, ok := updated.(*InboxView); !ok {
		t.Error("expected *InboxView")
	}
}

func TestInboxHADESPrefix(t *testing.T) {
	v := NewInboxView(nil)
	if !strings.Contains(v.View(), "HADES") {
		t.Errorf("InboxView missing HADES prefix:\n%s", v.View())
	}
}
