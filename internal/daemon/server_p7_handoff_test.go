// server_p7_handoff_test.go — Task I-2 server-level
// integration of the HandoffPosted route + RequireDaemonBearer
// middleware wiring.
//
// Splits the test surface into three concerns:
//
// 1. SetHandoffEmitter / HandoffEmitter accessor round-trip (mirrors
// the SetDayGenerator / SetInboxStore nil-safety contract).
// 2. SetDaemonBearer round-trip (bearer hash + audit emitter pair).
// 3. Wired-route integration: 503 before SetHandoffEmitter; 401 when
// bearer is configured + caller omits / mismatches the header;
// 202 on the happy path with a valid bearer.
//
// The fall-open posture of requireDaemonBearer (when daemonBearer is
// nil, the helper logs a warning + lets the request through) is also
// covered explicitly — without that branch the entire server_p7_*
// + server_test.go matrix would have to set up the bearer pipeline
// just to exercise the existing routes (a surface the production
// daemon main.go enforces at Start, not the test fixture).
package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/auth"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
)

type fakeHandoffEmitterServerTest struct {
	calls []handlers.HandoffPostedEvent
}

func (f *fakeHandoffEmitterServerTest) Emit(_ context.Context, ev handlers.HandoffPostedEvent) (string, error) {
	f.calls = append(f.calls, ev)
	return "evt-srv-001", nil
}

type fakeAuditEmitterServerTest struct {
	events []map[string]any
}

func (f *fakeAuditEmitterServerTest) Emit(_ context.Context, event map[string]any) error {
	f.events = append(f.events, event)
	return nil
}

func validHandoffBody(t *testing.T) []byte {
	t.Helper()
	body := map[string]any{
		"project_id":          strings.Repeat("a", 64),
		"project_alias":       "internal-platform-x",
		"timestamp":           time.Now().UTC().Format(time.RFC3339),
		"summary":             "Resumed after context reset; ready for execution.",
		"recent_commits":      []string{"abc handoff sample"},
		"autonomous_state":    "idle",
		"blockers":            []string{},
		"next_session_action": "Run /start.",
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return raw
}

func TestServer_HandoffEmitterAccessorRoundTrip(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})

	if got := srv.HandoffEmitter(); got != nil {
		t.Fatalf("zero-value HandoffEmitter() should be nil, got %v", got)
	}

	fakeE := &fakeHandoffEmitterServerTest{}
	srv.SetHandoffEmitter(fakeE)

	got := srv.HandoffEmitter()
	if got == nil {
		t.Fatal("HandoffEmitter() is nil after SetHandoffEmitter")
	}
	if _, ok := got.(handlers.HandoffEmitter); !ok {
		t.Errorf("HandoffEmitter() does not satisfy handlers.HandoffEmitter: %T", got)
	}

	srv.SetHandoffEmitter(nil)
	if got := srv.HandoffEmitter(); got != nil {
		t.Errorf("post-nil-reset HandoffEmitter() = %v, want nil", got)
	}
}

// TestServer_HandoffPosted_503BeforeWiring — POST
// /v1/events/handoff_posted MUST return 503 before SetHandoffEmitter
// has run. Canonical "feature not configured" signal so the plugin
// surfaces a clear error rather than a silent 404 when the daemon is
// up but the eventlog wiring hasn't completed.
func TestServer_HandoffPosted_503BeforeWiring(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	resp, err := ts.Client().Post(ts.URL+"/v1/events/handoff_posted",
		"application/json", bytes.NewReader(validHandoffBody(t)))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	var apiErr handlers.APIError
	_ = json.NewDecoder(resp.Body).Decode(&apiErr)
	if apiErr.Code != "emitter_unavailable" {
		t.Errorf("code = %q, want emitter_unavailable", apiErr.Code)
	}
}

// TestServer_HandoffPosted_FallOpenWithoutBearer — when SetDaemonBearer
// has NOT been called, the requireDaemonBearer helper falls open with
// a logged warning + the request reaches the handler. This is the
// test-fixture path; production main.go refuses Start when bearer is
// nil — that contract is enforced by daemon Start, not
// by registerRoutes.
func TestServer_HandoffPosted_FallOpenWithoutBearer(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})
	srv.SetHandoffEmitter(&fakeHandoffEmitterServerTest{})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	resp, err := ts.Client().Post(ts.URL+"/v1/events/handoff_posted",
		"application/json", bytes.NewReader(validHandoffBody(t)))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("fall-open status = %d, want 202", resp.StatusCode)
	}
}

func TestServer_HandoffPosted_401WhenBearerSetAndMissing(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})
	srv.SetHandoffEmitter(&fakeHandoffEmitterServerTest{})
	bearer := auth.NewDaemonBearer("good-token")
	auditE := &fakeAuditEmitterServerTest{}
	srv.SetDaemonBearer(bearer, auditE)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	resp, err := ts.Client().Post(ts.URL+"/v1/events/handoff_posted",
		"application/json", bytes.NewReader(validHandoffBody(t)))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing-bearer status = %d, want 401", resp.StatusCode)
	}

	req2, _ := http.NewRequest(http.MethodPost,
		ts.URL+"/v1/events/handoff_posted",
		bytes.NewReader(validHandoffBody(t)))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer wrong-token")
	resp2, err := ts.Client().Do(req2)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad-bearer status = %d, want 401", resp2.StatusCode)
	}

	// The audit emitter MUST have recorded both mismatches (Phase 2
	// audit aggregator escalates ≥5/1h to action-needed).
	if len(auditE.events) != 2 {
		t.Errorf("audit events recorded = %d, want 2", len(auditE.events))
	}
	for i, e := range auditE.events {
		if e["type"] != "DaemonBearerAuthFailed" {
			t.Errorf("event[%d] type = %v, want DaemonBearerAuthFailed", i, e["type"])
		}
	}
}

func TestServer_HandoffPosted_202WithGoodBearer(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})
	emitter := &fakeHandoffEmitterServerTest{}
	srv.SetHandoffEmitter(emitter)
	bearer := auth.NewDaemonBearer("good-token")
	auditE := &fakeAuditEmitterServerTest{}
	srv.SetDaemonBearer(bearer, auditE)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest(http.MethodPost,
		ts.URL+"/v1/events/handoff_posted",
		bytes.NewReader(validHandoffBody(t)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer good-token")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}
	if len(emitter.calls) != 1 {
		t.Fatalf("emit calls = %d, want 1", len(emitter.calls))
	}
	if emitter.calls[0].ProjectAlias != "internal-platform-x" {
		t.Errorf("recorded alias = %q", emitter.calls[0].ProjectAlias)
	}
	// Audit emitter MUST NOT have logged any mismatch on the happy path.
	if len(auditE.events) != 0 {
		t.Errorf("audit events on happy path = %d, want 0; events=%v",
			len(auditE.events), auditE.events)
	}
}

func TestServer_HandoffPosted_500WhenBearerSetButAuditNil(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})
	srv.SetHandoffEmitter(&fakeHandoffEmitterServerTest{})
	bearer := auth.NewDaemonBearer("good-token")
	srv.SetDaemonBearer(bearer, nil)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest(http.MethodPost,
		ts.URL+"/v1/events/handoff_posted",
		bytes.NewReader(validHandoffBody(t)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer good-token")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
}
