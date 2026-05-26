package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDaemonBearerValidate_OK(t *testing.T) {
	b := NewDaemonBearer("test-token-xyz")
	ok, err := b.Validate(context.Background(), "test-token-xyz")
	if !ok || err != nil {
		t.Fatalf("Validate(matching): got ok=%v err=%v, want true/nil", ok, err)
	}
}

func TestDaemonBearerValidate_Mismatch(t *testing.T) {
	b := NewDaemonBearer("test-token-xyz")
	ok, err := b.Validate(context.Background(), "wrong-token")
	if ok {
		t.Fatalf("Validate(mismatch): got ok=true, want false")
	}
	if !errors.Is(err, ErrBearerMismatch) {
		t.Fatalf("Validate(mismatch): err = %v, want ErrBearerMismatch", err)
	}
}

func TestDaemonBearerValidate_Empty(t *testing.T) {
	b := NewDaemonBearer("test-token-xyz")
	ok, err := b.Validate(context.Background(), "")
	if ok {
		t.Fatalf("Validate(empty): got ok=true, want false")
	}
	if !errors.Is(err, ErrBearerMismatch) {
		t.Fatalf("Validate(empty): err = %v, want ErrBearerMismatch", err)
	}
}

func TestPerRoutineBearerValidate_OK(t *testing.T) {
	store := &fakeTokenStore{tokens: map[string]string{
		"sched-001": hashToken("secret-route-token"),
	}}
	b := NewPerRoutineBearer(store, &fakeAuditEmitter{})
	ok, err := b.Validate(context.Background(), "sched-001", "secret-route-token", "127.0.0.1:55001")
	if !ok || err != nil {
		t.Fatalf("Validate(matching): got ok=%v err=%v", ok, err)
	}
}

func TestPerRoutineBearerValidate_Mismatch_EmitsAudit(t *testing.T) {
	emitter := &fakeAuditEmitter{}
	store := &fakeTokenStore{tokens: map[string]string{
		"sched-001": hashToken("secret-route-token"),
	}}
	b := NewPerRoutineBearer(store, emitter)
	ok, err := b.Validate(context.Background(), "sched-001", "wrong-token", "127.0.0.1:55001")
	if ok {
		t.Fatalf("Validate(mismatch): got ok=true, want false")
	}
	if !errors.Is(err, ErrBearerMismatch) {
		t.Fatalf("Validate(mismatch): err = %v, want ErrBearerMismatch", err)
	}
	if len(emitter.events) != 1 {
		t.Fatalf("audit events: got %d, want 1", len(emitter.events))
	}
	ev := emitter.events[0]
	if ev["type"] != "ScheduleHttpTriggerAuthFailed" {
		t.Errorf("event type: %v, want ScheduleHttpTriggerAuthFailed", ev["type"])
	}
	if ev["schedule_id"] != "sched-001" {
		t.Errorf("schedule_id: %v", ev["schedule_id"])
	}
	if ev["remote_addr"] != "127.0.0.1:55001" {
		t.Errorf("remote_addr: %v", ev["remote_addr"])
	}
	prefix, ok2 := ev["attempted_token_prefix"].(string)
	if !ok2 {
		t.Fatalf("attempted_token_prefix not a string: %T", ev["attempted_token_prefix"])
	}
	if !strings.HasPrefix(prefix, "wrong-tok") {
		t.Errorf("attempted_token_prefix: %v (must start with 'wrong-tok')", prefix)
	}
}

func TestPerRoutineBearerValidate_Mismatch_ShortToken_NoSlicePanic(t *testing.T) {

	emitter := &fakeAuditEmitter{}
	store := &fakeTokenStore{tokens: map[string]string{
		"sched-001": hashToken("secret-route-token"),
	}}
	b := NewPerRoutineBearer(store, emitter)
	ok, _ := b.Validate(context.Background(), "sched-001", "ab", "127.0.0.1:55001")
	if ok {
		t.Fatalf("Validate(short): got ok=true")
	}
	if len(emitter.events) != 1 {
		t.Fatalf("events: got %d, want 1", len(emitter.events))
	}
	if got := emitter.events[0]["attempted_token_prefix"]; got != "ab" {
		t.Errorf("short prefix: got %q, want \"ab\"", got)
	}
}

func TestPerRoutineBearerValidate_UnknownSchedule(t *testing.T) {
	emitter := &fakeAuditEmitter{}
	store := &fakeTokenStore{tokens: map[string]string{}}
	b := NewPerRoutineBearer(store, emitter)
	ok, err := b.Validate(context.Background(), "sched-unknown", "any-token", "127.0.0.1:55001")
	if ok {
		t.Fatalf("Validate(unknown): got ok=true, want false")
	}
	if !errors.Is(err, ErrScheduleNotFound) {
		t.Fatalf("err = %v, want ErrScheduleNotFound", err)
	}

	if len(emitter.events) != 0 {
		t.Errorf("audit events on unknown schedule: got %d, want 0 (anti-enumeration)", len(emitter.events))
	}
}

func TestPerRoutineBearerValidate_StoreError(t *testing.T) {
	emitter := &fakeAuditEmitter{}
	store := &fakeTokenStore{err: errors.New("db down")}
	b := NewPerRoutineBearer(store, emitter)
	ok, err := b.Validate(context.Background(), "sched-001", "any-token", "127.0.0.1:55001")
	if ok {
		t.Fatalf("Validate(store-err): ok=true")
	}
	if err == nil || !strings.Contains(err.Error(), "db down") {
		t.Fatalf("err = %v, want wrap of 'db down'", err)
	}
}

func TestPerRoutineBearerValidate_StoredHashMalformed(t *testing.T) {

	emitter := &fakeAuditEmitter{}
	store := &fakeTokenStore{tokens: map[string]string{
		"sched-001": "not-valid-hex-zzzz",
	}}
	b := NewPerRoutineBearer(store, emitter)
	ok, err := b.Validate(context.Background(), "sched-001", "any-token", "127.0.0.1:55001")
	if ok {
		t.Fatalf("malformed hash: ok=true")
	}
	if !errors.Is(err, ErrBearerMismatch) {
		t.Fatalf("err = %v, want ErrBearerMismatch", err)
	}
	if len(emitter.events) != 1 {
		t.Fatalf("events: got %d", len(emitter.events))
	}
	if emitter.events[0]["reason"] != "stored_hash_malformed" {
		t.Errorf("reason: %v", emitter.events[0]["reason"])
	}
}

func TestPerRoutineBearerValidate_StoredHashWrongLength(t *testing.T) {

	emitter := &fakeAuditEmitter{}
	store := &fakeTokenStore{tokens: map[string]string{
		"sched-001": "deadbeef",
	}}
	b := NewPerRoutineBearer(store, emitter)
	ok, err := b.Validate(context.Background(), "sched-001", "any-token", "127.0.0.1:55001")
	if ok {
		t.Fatalf("short hash: ok=true")
	}
	if !errors.Is(err, ErrBearerMismatch) {
		t.Fatalf("err = %v", err)
	}
	if len(emitter.events) != 1 {
		t.Fatalf("events: got %d", len(emitter.events))
	}
	if emitter.events[0]["reason"] != "stored_hash_malformed" {
		t.Errorf("reason: %v", emitter.events[0]["reason"])
	}
}

func TestDaemonBearer_TimingSafety(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timing test in short mode")
	}
	b := NewDaemonBearer("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	for _, candidate := range []string{
		"baaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaab",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa9",
	} {
		ok, _ := b.Validate(context.Background(), candidate)
		if ok {
			t.Fatalf("candidate %q matched", candidate)
		}
	}
}

func TestRequireDaemonBearer_Middleware(t *testing.T) {
	b := NewDaemonBearer("test-token")
	emitter := &fakeAuditEmitter{}
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	mw := RequireDaemonBearer(b, emitter)(next)

	t.Run("missing header", func(t *testing.T) {
		emitter.events = nil
		called = false
		req := httptest.NewRequest("POST", "/v1/events/handoff_posted", nil)
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("missing header: code=%d, want 401", w.Code)
		}
		if called {
			t.Fatalf("missing header: next was called")
		}
		if len(emitter.events) != 1 {
			t.Errorf("missing header: emitter events = %d, want 1", len(emitter.events))
		}
		if emitter.events[0]["type"] != "DaemonBearerAuthFailed" {
			t.Errorf("event type: %v", emitter.events[0]["type"])
		}
	})

	t.Run("malformed header", func(t *testing.T) {
		emitter.events = nil
		called = false
		req := httptest.NewRequest("POST", "/v1/events/handoff_posted", nil)
		req.Header.Set("Authorization", "Basic foo")
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("malformed: code=%d", w.Code)
		}
	})

	t.Run("scheme-only no token", func(t *testing.T) {
		emitter.events = nil
		called = false
		req := httptest.NewRequest("POST", "/v1/events/handoff_posted", nil)
		req.Header.Set("Authorization", "Bearer")
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("scheme-only: code=%d", w.Code)
		}
	})

	t.Run("case-insensitive scheme", func(t *testing.T) {
		emitter.events = nil
		called = false
		req := httptest.NewRequest("POST", "/v1/events/handoff_posted", nil)
		req.Header.Set("Authorization", "bearer test-token")
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("lower scheme: code=%d, want 200", w.Code)
		}
		if !called {
			t.Fatalf("lower scheme: next not called")
		}
	})

	t.Run("valid token", func(t *testing.T) {
		emitter.events = nil
		called = false
		req := httptest.NewRequest("POST", "/v1/events/handoff_posted", nil)
		req.Header.Set("Authorization", "Bearer test-token")
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("valid: code=%d", w.Code)
		}
		if !called {
			t.Fatalf("valid: next not called")
		}
		if len(emitter.events) != 0 {
			t.Errorf("valid: no audit expected, got %d", len(emitter.events))
		}
	})

	t.Run("wrong token", func(t *testing.T) {
		emitter.events = nil
		called = false
		req := httptest.NewRequest("POST", "/v1/events/handoff_posted", nil)
		req.Header.Set("Authorization", "Bearer wrong")
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("wrong: code=%d", w.Code)
		}
		if len(emitter.events) != 1 {
			t.Errorf("wrong: events = %d, want 1", len(emitter.events))
		}
	})
}

func TestRequirePerRoutineBearer_Middleware(t *testing.T) {
	store := &fakeTokenStore{tokens: map[string]string{
		"sched-001": hashToken("good-token"),
	}}
	b := NewPerRoutineBearer(store, &fakeAuditEmitter{})

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	idFromPath := func(r *http.Request) string {
		return r.Header.Get("X-Test-Schedule-Id")
	}
	mw := RequirePerRoutineBearer(b, idFromPath)(next)

	t.Run("missing schedule id", func(t *testing.T) {
		called = false
		req := httptest.NewRequest("POST", "/v1/schedules//fire", nil)
		req.Header.Set("Authorization", "Bearer good-token")
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("missing id: code=%d, want 400", w.Code)
		}
		if called {
			t.Fatalf("missing id: next called")
		}
	})

	t.Run("valid", func(t *testing.T) {
		called = false
		req := httptest.NewRequest("POST", "/v1/schedules/sched-001/fire", nil)
		req.Header.Set("X-Test-Schedule-Id", "sched-001")
		req.Header.Set("Authorization", "Bearer good-token")
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("valid: code=%d", w.Code)
		}
		if !called {
			t.Fatalf("valid: next not called")
		}
	})

	t.Run("wrong token", func(t *testing.T) {
		called = false
		req := httptest.NewRequest("POST", "/v1/schedules/sched-001/fire", nil)
		req.Header.Set("X-Test-Schedule-Id", "sched-001")
		req.Header.Set("Authorization", "Bearer wrong-token")
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("wrong: code=%d", w.Code)
		}
	})

	t.Run("unknown schedule returns 401 (anti-enumeration)", func(t *testing.T) {
		called = false
		req := httptest.NewRequest("POST", "/v1/schedules/unknown/fire", nil)
		req.Header.Set("X-Test-Schedule-Id", "unknown")
		req.Header.Set("Authorization", "Bearer any-token")
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("unknown: code=%d, want 401 (not 404 — anti-enumeration)", w.Code)
		}
	})
}

func TestExtractBearer(t *testing.T) {
	cases := []struct {
		name string
		hdr  string
		want string
	}{
		{"empty", "", ""},
		{"valid bearer", "Bearer abc", "abc"},
		{"lowercase scheme", "bearer abc", "abc"},
		{"basic scheme", "Basic foo", ""},
		{"single token no space", "Bearer", ""},
		{"trim whitespace", "Bearer   abc  ", "abc"},
		{"long token", "Bearer " + strings.Repeat("a", 100), strings.Repeat("a", 100)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tc.hdr != "" {
				req.Header.Set("Authorization", tc.hdr)
			}
			got := extractBearer(req)
			if got != tc.want {
				t.Errorf("extractBearer(%q) = %q, want %q", tc.hdr, got, tc.want)
			}
		})
	}
}

func TestSentinels(t *testing.T) {

	if err := httpAuthBoundarySentinel(); !errors.Is(err, ErrAuthBoundaryAnchor) {
		t.Errorf("httpAuthBoundarySentinel = %v, want ErrAuthBoundaryAnchor", err)
	}
	if err := perRoutineBearerSentinel(); !errors.Is(err, ErrPerRoutineBearerAnchor) {
		t.Errorf("perRoutineBearerSentinel = %v, want ErrPerRoutineBearerAnchor", err)
	}
}

type fakeTokenStore struct {
	tokens map[string]string
	err    error
}

func (f *fakeTokenStore) Get(_ context.Context, scheduleID string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	h, ok := f.tokens[scheduleID]
	if !ok {
		return "", ErrScheduleNotFound
	}
	return h, nil
}

type fakeAuditEmitter struct {
	events []map[string]any
}

func (f *fakeAuditEmitter) Emit(_ context.Context, event map[string]any) error {
	f.events = append(f.events, event)
	return nil
}

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}
