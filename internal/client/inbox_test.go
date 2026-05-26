package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInboxListHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/inbox/list" {
			t.Errorf("path = %s, want /v1/inbox/list", r.URL.Path)
		}
		var req InboxListRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode req: %v", err)
		}
		if req.Severity != "urgent" {
			t.Errorf("Severity = %q, want urgent", req.Severity)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(InboxListResponse{
			Rows: []InboxCacheRow{
				{
					CacheID:        1,
					ProjectID:      strings.Repeat("a", 64),
					ProjectAlias:   "internal-platform-x",
					NotificationID: 234,
					Severity:       "urgent",
					EventType:      "hra.l4_alert",
					ContentHash:    strings.Repeat("a", 64),
					CreatedAt:      time.Date(2026, 5, 7, 11, 0, 0, 0, time.UTC),
				},
			},
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	rows, err := c.InboxList(context.Background(), InboxListRequest{Severity: "urgent"})
	if err != nil {
		t.Fatalf("InboxList: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].NotificationID != 234 {
		t.Errorf("NotificationID = %d, want 234", rows[0].NotificationID)
	}
	if rows[0].Severity != "urgent" {
		t.Errorf("Severity = %q, want urgent", rows[0].Severity)
	}
	if rows[0].ProjectAlias != "internal-platform-x" {
		t.Errorf("ProjectAlias = %q, want internal-platform-x", rows[0].ProjectAlias)
	}
}

func TestInboxListEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(InboxListResponse{Rows: []InboxCacheRow{}})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	rows, err := c.InboxList(context.Background(), InboxListRequest{})
	if err != nil {
		t.Fatalf("InboxList: %v", err)
	}
	if rows == nil {
		t.Error("rows is nil, want non-nil empty slice")
	}
	if len(rows) != 0 {
		t.Errorf("rows = %d, want 0", len(rows))
	}
}

func TestInboxListNullRowsTreatedAsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		_, _ = w.Write([]byte(`{"rows":null}`))
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	rows, err := c.InboxList(context.Background(), InboxListRequest{})
	if err != nil {
		t.Fatalf("InboxList: %v", err)
	}
	if rows == nil {
		t.Error("rows is nil; client should return non-nil empty slice")
	}
}

func TestInboxList503Propagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "inbox store not configured", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.InboxList(context.Background(), InboxListRequest{})
	if err == nil {
		t.Fatal("expected 503 to propagate as error")
	}
	if !IsHTTPStatus(err, http.StatusServiceUnavailable) {
		t.Errorf("err = %v, want HTTPError 503", err)
	}
}

func TestInboxAckHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/inbox/ack" {
			t.Errorf("path = %s, want /v1/inbox/ack", r.URL.Path)
		}
		var req InboxAckRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req.ID != 234 {
			t.Errorf("ID = %d, want 234", req.ID)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": 234})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	if err := c.InboxAck(context.Background(), 234); err != nil {
		t.Fatalf("InboxAck: %v", err)
	}
}

func TestInboxAck404Propagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "inbox: notification not found", http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	err := c.InboxAck(context.Background(), 999)
	if err == nil {
		t.Fatal("expected 404 to propagate")
	}
	if !IsHTTPStatus(err, http.StatusNotFound) {
		t.Errorf("err = %v, want HTTPError 404", err)
	}
}

func TestInboxAck422Propagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "id must be positive", http.StatusUnprocessableEntity)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	err := c.InboxAck(context.Background(), 1)
	if err == nil {
		t.Fatal("expected 422 to propagate")
	}
	if !IsHTTPStatus(err, http.StatusUnprocessableEntity) {
		t.Errorf("err = %v, want HTTPError 422", err)
	}
}

func TestInboxSnoozeHappyPath(t *testing.T) {
	until := time.Date(2026, 5, 8, 8, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/inbox/snooze" {
			t.Errorf("path = %s, want /v1/inbox/snooze", r.URL.Path)
		}
		var req InboxSnoozeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req.ID != 230 {
			t.Errorf("ID = %d, want 230", req.ID)
		}
		if !req.Until.Equal(until) {
			t.Errorf("Until = %v, want %v", req.Until, until)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "id": 230, "until": until.Format(time.RFC3339),
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	if err := c.InboxSnooze(context.Background(), 230, until); err != nil {
		t.Fatalf("InboxSnooze: %v", err)
	}
}

func TestInboxSnooze404Propagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "inbox: notification not found", http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	err := c.InboxSnooze(context.Background(), 999, time.Now().UTC().Add(8*time.Hour))
	if err == nil {
		t.Fatal("expected 404 to propagate")
	}
	if !IsHTTPStatus(err, http.StatusNotFound) {
		t.Errorf("err = %v, want HTTPError 404", err)
	}
}

func TestInboxAckRejectsZeroID(t *testing.T) {
	c := NewWithBaseURL("http://invalid")
	err := c.InboxAck(context.Background(), 0)
	if err == nil {
		t.Fatal("expected error on id=0")
	}
}

func TestInboxSnoozeRejectsZeroID(t *testing.T) {
	c := NewWithBaseURL("http://invalid")
	err := c.InboxSnooze(context.Background(), 0, time.Now().Add(8*time.Hour))
	if err == nil {
		t.Fatal("expected error on id=0")
	}
}

func TestInboxSnoozeRejectsZeroUntil(t *testing.T) {
	c := NewWithBaseURL("http://invalid")
	err := c.InboxSnooze(context.Background(), 1, time.Time{})
	if err == nil {
		t.Fatal("expected error on zero until")
	}
}
