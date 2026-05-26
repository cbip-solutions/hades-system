package handlers

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestIDContext_Roundtrip(t *testing.T) {
	ctx := WithRequestID(context.Background(), "abcd1234")
	got := RequestIDFromContext(ctx)
	if got != "abcd1234" {
		t.Errorf("roundtrip: got %q, want abcd1234", got)
	}
}

func TestRequestIDContext_Absent(t *testing.T) {
	got := RequestIDFromContext(context.Background())
	if got != "" {
		t.Errorf("absent: got %q, want empty", got)
	}
}

func TestRequestIDContext_WrongType(t *testing.T) {
	type otherKey struct{}
	ctx := context.WithValue(context.Background(), otherKey{}, "stranger")
	got := RequestIDFromContext(ctx)
	if got != "" {
		t.Errorf("wrong-type: got %q, want empty", got)
	}
}

func TestGenerateRequestID(t *testing.T) {
	id := generateRequestID()
	if len(id) != 16 {
		t.Errorf("len = %d, want 16", len(id))
	}
	if _, err := hex.DecodeString(id); err != nil {
		t.Errorf("not valid hex: %v", err)
	}

	id2 := generateRequestID()
	if id == id2 {
		t.Errorf("two calls produced identical IDs (extremely unlikely): %q", id)
	}
}

func TestAPIError_JSON_Shape(t *testing.T) {
	e := APIError{Error: "schedule not found", Code: "schedule_not_found", RequestID: "abcd1234"}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)

	if !strings.Contains(s, `"error":"schedule not found"`) {
		t.Errorf("missing error field: %s", s)
	}
	if !strings.Contains(s, `"code":"schedule_not_found"`) {
		t.Errorf("missing code field: %s", s)
	}
	if !strings.Contains(s, `"request_id":"abcd1234"`) {
		t.Errorf("missing request_id field: %s", s)
	}
}

func TestRenderError_FullRoundtrip(t *testing.T) {
	w := httptest.NewRecorder()
	ctx := WithRequestID(context.Background(), "deadbeef")
	RenderError(ctx, w, 404, "alias_unknown", "alias 'foo' not registered")

	if w.Code != 404 {
		t.Errorf("code = %d, want 404", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := w.Header().Get("X-Request-ID"); got != "deadbeef" {
		t.Errorf("X-Request-ID = %q", got)
	}

	var got APIError
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Error != "alias 'foo' not registered" {
		t.Errorf("Error: %q", got.Error)
	}
	if got.Code != "alias_unknown" {
		t.Errorf("Code: %q", got.Code)
	}
	if got.RequestID != "deadbeef" {
		t.Errorf("RequestID: %q", got.RequestID)
	}
}

func TestRenderError_GeneratedID_WhenAbsent(t *testing.T) {
	w := httptest.NewRecorder()
	RenderError(context.Background(), w, 400, "bad_json", "invalid JSON body")

	rid := w.Header().Get("X-Request-ID")
	if len(rid) != 16 {
		t.Errorf("generated rid length = %d, want 16", len(rid))
	}
	if _, err := hex.DecodeString(rid); err != nil {
		t.Errorf("rid not hex: %v", err)
	}

	var got APIError
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.RequestID != rid {
		t.Errorf("body rid (%q) != header rid (%q)", got.RequestID, rid)
	}
}

func TestRenderJSON_FullRoundtrip(t *testing.T) {
	w := httptest.NewRecorder()
	ctx := WithRequestID(context.Background(), "cafef00d")
	body := map[string]any{"id": "sched-001", "status": "queued"}
	RenderJSON(ctx, w, 202, body)

	if w.Code != 202 {
		t.Errorf("code = %d, want 202", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := w.Header().Get("X-Request-ID"); got != "cafef00d" {
		t.Errorf("X-Request-ID = %q", got)
	}

	var got map[string]any
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["id"] != "sched-001" {
		t.Errorf("id: %v", got["id"])
	}
	if got["status"] != "queued" {
		t.Errorf("status: %v", got["status"])
	}
}

func TestRenderJSON_GeneratedID_WhenAbsent(t *testing.T) {
	w := httptest.NewRecorder()
	RenderJSON(context.Background(), w, 200, map[string]string{"ok": "yes"})

	rid := w.Header().Get("X-Request-ID")
	if len(rid) != 16 {
		t.Errorf("rid len = %d, want 16", len(rid))
	}
}

func TestRenderJSON_NilBody(t *testing.T) {
	w := httptest.NewRecorder()
	RenderJSON(context.Background(), w, 204, nil)
	if w.Code != 204 {
		t.Errorf("code = %d, want 204", w.Code)
	}
	body := strings.TrimSpace(w.Body.String())
	if body != "null" {
		t.Errorf("body = %q, want \"null\"", body)
	}
}
