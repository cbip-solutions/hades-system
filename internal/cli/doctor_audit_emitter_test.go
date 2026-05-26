package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func TestDaemonAuditEmitter_EmitPostsToDaemon(t *testing.T) {
	type recorded struct {
		eventType string
		body      []byte
	}
	rec := &struct{ events []recorded }{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audit/emit" {
			t.Errorf("unexpected path %q; want /v1/audit/emit", r.URL.Path)
		}
		var req struct {
			Type    string         `json:"type"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		body, _ := json.Marshal(req.Payload)
		rec.events = append(rec.events, recorded{eventType: req.Type, body: body})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":       "evt-1",
			"accepted": true,
		})
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	em := NewDaemonAuditEmitter(c, nil)
	hash, err := em.Emit(context.Background(), "evt.doctor.full.run", []byte(`{"k":"v"}`))
	if err != nil {
		t.Fatalf("Emit returned err: %v", err)
	}
	if hash != "evt-1" {
		t.Errorf("hash = %q; want evt-1", hash)
	}
	if len(rec.events) != 1 {
		t.Fatalf("recorded events = %d; want 1", len(rec.events))
	}
	if rec.events[0].eventType != "evt.doctor.full.run" {
		t.Errorf("eventType = %q; want evt.doctor.full.run", rec.events[0].eventType)
	}
	if !strings.Contains(string(rec.events[0].body), `"k":"v"`) {
		t.Errorf("payload body = %q; should contain k:v", string(rec.events[0].body))
	}
}

func TestDaemonAuditEmitter_NilSafe(t *testing.T) {
	var em *DaemonAuditEmitter
	hash, err := em.Emit(context.Background(), "evt.test", []byte(`{}`))
	if hash != "" || err != nil {
		t.Errorf("nil emitter: hash=%q err=%v; want empty/nil", hash, err)
	}
	em2 := &DaemonAuditEmitter{c: nil}
	hash, err = em2.Emit(context.Background(), "evt.test", []byte(`{}`))
	if hash != "" || err != nil {
		t.Errorf("nil-client emitter: hash=%q err=%v; want empty/nil", hash, err)
	}
}

// TestDaemonAuditEmitter_DaemonErrorReturnsErr asserts the adapter
// returns the daemon's error + logs warning. Caller decides whether
// to surface (canonical pattern is "log + continue").
func TestDaemonAuditEmitter_DaemonErrorReturnsErr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	em := NewDaemonAuditEmitter(c, nil)
	hash, err := em.Emit(context.Background(), "evt.test", []byte(`{}`))
	if err == nil {
		t.Errorf("expected daemon error; got nil")
	}
	if hash != "" {
		t.Errorf("expected empty hash on err; got %q", hash)
	}
}

func TestDaemonAuditEmitter_MalformedPayloadFallback(t *testing.T) {
	rec := &struct{ rawBody []byte }{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Payload map[string]any `json:"payload"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		rec.rawBody, _ = json.Marshal(req.Payload)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "evt-1", "accepted": true})
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	em := NewDaemonAuditEmitter(c, nil)
	_, err := em.Emit(context.Background(), "evt.test", []byte(`not json`))
	if err != nil {
		t.Fatalf("Emit returned err: %v", err)
	}
	if !strings.Contains(string(rec.rawBody), `"_raw"`) {
		t.Errorf("expected _raw envelope in body; got %q", string(rec.rawBody))
	}
}
