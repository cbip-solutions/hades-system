package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeHandoffEmitter struct {
	calls []HandoffPostedEvent
	err   error
}

func (f *fakeHandoffEmitter) Emit(_ context.Context, ev HandoffPostedEvent) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.calls = append(f.calls, ev)
	return "evt-001", nil
}

type fakeHandoffCtx struct {
	emitter HandoffEmitter
}

func (f *fakeHandoffCtx) HandoffEmitter() HandoffEmitter { return f.emitter }

func validBody() map[string]any {
	return map[string]any{
		"project_id":    strings.Repeat("a", 64),
		"project_alias": "internal-platform-x",
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
		"summary":       "Resumed after context reset; ready for Plan 7 execution.",
		"recent_commits": []string{
			"e51f64b docs(handoff): refresh post Phase B",
			"bf5bc71 docs(handoff): refresh post Phase C F-6",
		},
		"autonomous_state":    "idle",
		"blockers":            []string{},
		"next_session_action": "Run /start; await operator direction.",
	}
}

func dispatch(t *testing.T, ctx HandoffEmitterCtx, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/events/handoff_posted", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	HandoffPosted(ctx)(w, req)
	return w
}

func TestHandoffPosted_OK(t *testing.T) {
	emitter := &fakeHandoffEmitter{}
	ctx := &fakeHandoffCtx{emitter: emitter}
	w := dispatch(t, ctx, validBody())

	if w.Code != http.StatusAccepted {
		t.Fatalf("code = %d, want 202; body=%s", w.Code, w.Body.String())
	}
	if len(emitter.calls) != 1 {
		t.Fatalf("emit calls = %d, want 1", len(emitter.calls))
	}
	got := emitter.calls[0]
	if got.ProjectAlias != "internal-platform-x" {
		t.Errorf("project_alias = %q", got.ProjectAlias)
	}
	if got.AutonomousState != "idle" {
		t.Errorf("autonomous_state = %q", got.AutonomousState)
	}
	if len(got.RecentCommits) != 2 {
		t.Errorf("recent_commits len = %d, want 2", len(got.RecentCommits))
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if resp["project_alias"] != "internal-platform-x" {
		t.Errorf("response project_alias = %v", resp["project_alias"])
	}
	if resp["event_id"] != "evt-001" {
		t.Errorf("response event_id = %v, want evt-001", resp["event_id"])
	}
	if w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q", w.Header().Get("Content-Type"))
	}
	if w.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID header missing")
	}
}

func TestHandoffPosted_BadJSON(t *testing.T) {
	ctx := &fakeHandoffCtx{emitter: &fakeHandoffEmitter{}}
	req := httptest.NewRequest(http.MethodPost, "/v1/events/handoff_posted", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	HandoffPosted(ctx)(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", w.Code)
	}
	var resp APIError
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != "bad_json" {
		t.Errorf("code = %q, want bad_json", resp.Code)
	}
	if resp.RequestID == "" {
		t.Error("request_id missing in error body")
	}
}

func TestHandoffPosted_BadProjectID(t *testing.T) {
	cases := []struct {
		name string
		val  string
	}{
		{"empty", ""},
		{"too_short", "deadbeef"},
		{"uppercase", strings.Repeat("A", 64)},
		{"non_hex", strings.Repeat("z", 64)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := validBody()
			body["project_id"] = tc.val
			w := dispatch(t, &fakeHandoffCtx{emitter: &fakeHandoffEmitter{}}, body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("code=%d, want 400", w.Code)
			}
			var resp APIError
			_ = json.Unmarshal(w.Body.Bytes(), &resp)
			if resp.Code != "bad_project_id" {
				t.Errorf("code = %q, want bad_project_id", resp.Code)
			}
		})
	}
}

func TestHandoffPosted_BadAlias(t *testing.T) {
	cases := []string{
		"",
		"INTERNAL_PLATFORM_X",
		"internal-platform-x uru",
		"internal_platform_x",
		"internal-platform-x!",
		strings.Repeat("a", 65),
	}
	for _, alias := range cases {
		t.Run(alias, func(t *testing.T) {
			body := validBody()
			body["project_alias"] = alias
			w := dispatch(t, &fakeHandoffCtx{emitter: &fakeHandoffEmitter{}}, body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("code=%d, want 400", w.Code)
			}
			var resp APIError
			_ = json.Unmarshal(w.Body.Bytes(), &resp)
			if resp.Code != "bad_alias" {
				t.Errorf("code = %q, want bad_alias", resp.Code)
			}
		})
	}
}

func TestHandoffPosted_BadTimestamp(t *testing.T) {
	body := validBody()
	body["timestamp"] = "0001-01-01T00:00:00Z"
	w := dispatch(t, &fakeHandoffCtx{emitter: &fakeHandoffEmitter{}}, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d, want 400", w.Code)
	}
	var resp APIError
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != "bad_timestamp" {
		t.Errorf("code = %q, want bad_timestamp", resp.Code)
	}
}

func TestHandoffPosted_SummaryTooLong(t *testing.T) {
	body := validBody()
	body["summary"] = strings.Repeat("x", maxSummary+1)
	w := dispatch(t, &fakeHandoffCtx{emitter: &fakeHandoffEmitter{}}, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d, want 400", w.Code)
	}
	var resp APIError
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != "summary_too_long" {
		t.Errorf("code = %q, want summary_too_long", resp.Code)
	}
}

func TestHandoffPosted_CommitsTooMany(t *testing.T) {
	body := validBody()
	commits := make([]string, maxCommits+1)
	for i := range commits {
		commits[i] = "abc def"
	}
	body["recent_commits"] = commits
	w := dispatch(t, &fakeHandoffCtx{emitter: &fakeHandoffEmitter{}}, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d, want 400", w.Code)
	}
	var resp APIError
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != "commits_too_many" {
		t.Errorf("code = %q, want commits_too_many", resp.Code)
	}
}

func TestHandoffPosted_CommitTooLong(t *testing.T) {
	body := validBody()
	body["recent_commits"] = []string{strings.Repeat("c", maxCommitLen+1)}
	w := dispatch(t, &fakeHandoffCtx{emitter: &fakeHandoffEmitter{}}, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d, want 400", w.Code)
	}
	var resp APIError
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != "commit_too_long" {
		t.Errorf("code = %q, want commit_too_long", resp.Code)
	}
}

func TestHandoffPosted_BadState(t *testing.T) {
	cases := []string{"", "marching-on-mars", "ACTIVE", "running"}
	for _, st := range cases {
		t.Run(st, func(t *testing.T) {
			body := validBody()
			body["autonomous_state"] = st
			w := dispatch(t, &fakeHandoffCtx{emitter: &fakeHandoffEmitter{}}, body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("code=%d, want 400", w.Code)
			}
			var resp APIError
			_ = json.Unmarshal(w.Body.Bytes(), &resp)
			if resp.Code != "bad_state" {
				t.Errorf("code = %q, want bad_state", resp.Code)
			}
		})
	}
}

func TestHandoffPosted_AcceptsAllValidStates(t *testing.T) {
	for _, st := range []string{"active", "paused", "idle", "complete"} {
		t.Run(st, func(t *testing.T) {
			emitter := &fakeHandoffEmitter{}
			body := validBody()
			body["autonomous_state"] = st
			w := dispatch(t, &fakeHandoffCtx{emitter: emitter}, body)
			if w.Code != http.StatusAccepted {
				t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestHandoffPosted_BlockersTooMany(t *testing.T) {
	body := validBody()
	bs := make([]string, maxBlockers+1)
	for i := range bs {
		bs[i] = "x"
	}
	body["blockers"] = bs
	w := dispatch(t, &fakeHandoffCtx{emitter: &fakeHandoffEmitter{}}, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d, want 400", w.Code)
	}
	var resp APIError
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != "blockers_too_many" {
		t.Errorf("code = %q, want blockers_too_many", resp.Code)
	}
}

func TestHandoffPosted_BlockerTooLong(t *testing.T) {
	body := validBody()
	body["blockers"] = []string{strings.Repeat("b", maxBlockerLen+1)}
	w := dispatch(t, &fakeHandoffCtx{emitter: &fakeHandoffEmitter{}}, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d, want 400", w.Code)
	}
	var resp APIError
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != "blocker_too_long" {
		t.Errorf("code = %q, want blocker_too_long", resp.Code)
	}
}

func TestHandoffPosted_NextSessionTooLong(t *testing.T) {
	body := validBody()
	body["next_session_action"] = strings.Repeat("n", maxNextSession+1)
	w := dispatch(t, &fakeHandoffCtx{emitter: &fakeHandoffEmitter{}}, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d, want 400", w.Code)
	}
	var resp APIError
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != "next_session_too_long" {
		t.Errorf("code = %q, want next_session_too_long", resp.Code)
	}
}

func TestHandoffPosted_EmitterUnwired(t *testing.T) {

	ctx := &fakeHandoffCtx{emitter: nil}
	w := dispatch(t, ctx, validBody())
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("code=%d, want 503", w.Code)
	}
	var resp APIError
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != "emitter_unavailable" {
		t.Errorf("code = %q, want emitter_unavailable", resp.Code)
	}
}

func TestHandoffPosted_EmitterError(t *testing.T) {
	emitter := &fakeHandoffEmitter{err: errors.New("eventlog: disk full")}
	ctx := &fakeHandoffCtx{emitter: emitter}
	w := dispatch(t, ctx, validBody())
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("code=%d, want 500", w.Code)
	}
	var resp APIError
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != "emit_failed" {
		t.Errorf("code = %q, want emit_failed", resp.Code)
	}

	if strings.Contains(resp.Error, "disk full") {
		t.Errorf("response leaked upstream error text: %q", resp.Error)
	}
}

func TestHandoffPostedEvent_AliasOfEventlog(t *testing.T) {

	var ev HandoffPostedEvent
	ev.ProjectID = strings.Repeat("a", 64)
	ev.ProjectAlias = "alias"
	ev.Timestamp = time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	ev.AutonomousState = "idle"
	a, _ := json.Marshal(ev)
	if !bytes.Contains(a, []byte(`"project_alias":"alias"`)) {
		t.Errorf("JSON shape unexpected: %s", a)
	}
}
