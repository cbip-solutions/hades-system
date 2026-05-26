package daemon

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/auth"
	"github.com/cbip-solutions/hades-system/internal/daemon/inboxadapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/projectctxadapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/quotaadapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/scheduleradapter"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

func TestServer_Plan7Bootstrap_RoutesRecover(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})

	preBootstrapRoutes := []struct{ method, path string }{
		{http.MethodGet, "/v1/priority/list"},

		{http.MethodPost, "/v1/inbox/list"},
		{http.MethodGet, "/v1/schedules"},
	}
	for _, r := range preBootstrapRoutes {
		req := httptest.NewRequest(r.method, r.path, strings.NewReader("{}"))
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("pre-bootstrap %s %s: code=%d body=%s, want 503",
				r.method, r.path, rec.Code, rec.Body.String())
		}
	}

	srv.SetProjectStore(projectctxadapter.New(st))
	srv.SetOverrideStore(quotaadapter.NewOverrideStore(st))
	srv.SetInboxStore(inboxadapter.NewAdapter(nil, st))

	scheduleAdapter := scheduleradapter.New(st)
	srv.SetScheduleStore(scheduleradapter.NewHandlerStore(scheduleAdapter))

	for _, r := range preBootstrapRoutes {
		req := httptest.NewRequest(r.method, r.path, strings.NewReader("{}"))
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code == http.StatusServiceUnavailable {
			t.Errorf("post-bootstrap %s %s: still 503 (adapter not wired); body=%s",
				r.method, r.path, rec.Body.String())
		}
	}
}

func TestServer_Plan7Bootstrap_HandoffEmitterWired(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})

	log := eventlog.NewMemory(clock.Real{})
	em := NewHandoffEmitter(log)
	srv.SetHandoffEmitter(em)

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	srv.SetDaemonBearer(auth.NewDaemonBearer("test-bearer-token-abc"),
		NewSlogBearerAuditEmitter(logger))

	req := httptest.NewRequest(http.MethodPost, "/v1/events/handoff_posted",
		strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST /v1/events/handoff_posted (no bearer): code=%d body=%s, want 401",
			rec.Code, rec.Body.String())
	}

	if !strings.Contains(buf.String(), "auth audit event") {
		t.Errorf("bearer audit emitter did not fire on 401 path: log=%q", buf.String())
	}
}

func TestServer_Plan7Bootstrap_ContextCancellation_Smoke(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	srv.SetProjectStore(projectctxadapter.New(st))
	srv.SetOverrideStore(quotaadapter.NewOverrideStore(st))
	srv.SetInboxStore(inboxadapter.NewAdapter(nil, st))
	scheduleAdapter := scheduleradapter.New(st)
	srv.SetScheduleStore(scheduleradapter.NewHandlerStore(scheduleAdapter))
	srv.SetHandoffEmitter(NewHandoffEmitter(eventlog.NewMemory(clock.Real{})))

	if srv.ProjectStore() == nil {
		t.Error("ProjectStore() returned nil after Set")
	}
	if srv.OverrideStore() == nil {
		t.Error("OverrideStore() returned nil after Set")
	}
	if srv.InboxStore() == nil {
		t.Error("InboxStore() returned nil after Set")
	}
	if srv.ScheduleStore() == nil {
		t.Error("ScheduleStore() returned nil after Set")
	}
	if srv.HandoffEmitter() == nil {
		t.Error("HandoffEmitter() returned nil after Set")
	}

	_ = ctx

}
