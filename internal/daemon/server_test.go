package daemon

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/store"
)

type fakeCostStoreServerTest struct{}

func (fakeCostStoreServerTest) InsertCostLedger(_ orchestrator.CostLedgerRow) (int64, error) {
	return 0, nil
}
func (fakeCostStoreServerTest) QueryAllRecentCosts(_ time.Time) ([]orchestrator.CostLedgerRow, error) {
	return nil, nil
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return s
}

func TestNewServerRegistersRoutes(t *testing.T) {
	s := newTestStore(t)
	srv := New(s, Config{})

	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("/v1/health status = %d, want 200", rec.Code)
	}
}

func TestServerStartStopUDS(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	udsPath := filepath.Join(dir, "test.sock")
	srv := New(s, Config{UDSPath: udsPath, DisableAuditInfra: true})

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()

	time.Sleep(100 * time.Millisecond)

	c := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", udsPath)
			},
		},
		Timeout: 2 * time.Second,
	}
	resp, err := c.Get("http://unix/v1/health")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["status"] != "ok" {
		t.Errorf("status field = %v", got["status"])
	}

	if err := srv.Stop(context.Background()); err != nil {
		t.Errorf("Stop: %v", err)
	}
	if err := <-errCh; err != nil && err != http.ErrServerClosed {
		t.Errorf("Start returned %v", err)
	}
}

func TestStubEndpointReturns501WithXZenPlanHeader(t *testing.T) {
	s := newTestStore(t)
	srv := New(s, Config{})

	req := httptest.NewRequest(http.MethodPost, "/v1/swarms", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want 501", rec.Code)
	}
	if got := rec.Header().Get("X-Zen-Plan"); got != "5" {
		t.Errorf("X-Zen-Plan = %q, want 5", got)
	}
}

func TestServerAuditEmit_PersistsToStore(t *testing.T) {
	s := newTestStore(t)
	srv := New(s, Config{DisableAuditInfra: true})

	event := handlers.AuditEventIn{
		ID:        "test-id-001",
		ProjectID: "project-a",
		Type:      "test_event",
		Payload:   map[string]string{"k": "v"},
		EmittedAt: time.Now().Unix(),
	}
	if err := srv.AuditEmit(event); err != nil {
		t.Fatalf("AuditEmit: %v", err)
	}

	var count int
	err := s.DB().QueryRow(
		`SELECT count(*) FROM audit_events_raw WHERE project_id = ? AND type = ?`,
		"project-a", "test_event",
	).Scan(&count)
	if err != nil {
		t.Fatalf("query audit_events_raw: %v", err)
	}
	if count != 1 {
		t.Errorf("audit_events_raw row count for project-a/test_event = %d, want 1", count)
	}
}

func TestServerDoctrineReload_InvalidatesBuckets(t *testing.T) {
	s := newTestStore(t)
	srv := New(s, Config{DisableAuditInfra: true})

	req := httptest.NewRequest(http.MethodGet, "/v1/budget/cap_status?axis=project&value=test", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if srv.bucketRegistry.Len() == 0 {
		t.Skip("no buckets created by the request — cannot verify invalidation; skipping")
	}

	if err := srv.DoctrineReload(""); err != nil && !strings.Contains(err.Error(), "no active reload.Watcher") {
		t.Fatalf("DoctrineReload: %v", err)
	}
	if got := srv.bucketRegistry.Len(); got != 0 {
		t.Errorf("after DoctrineReload, bucket registry Len = %d, want 0", got)
	}
}

func TestSessionsStub501(t *testing.T) {
	s := newTestStore(t)
	srv := New(s, Config{})

	req := httptest.NewRequest(http.MethodPost, "/v1/sessions", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want 501", rec.Code)
	}
	if rec.Header().Get("X-Zen-Plan") != "7" {
		t.Errorf("X-Zen-Plan = %q, want 7", rec.Header().Get("X-Zen-Plan"))
	}
}

type fakeBypassAdminForServer struct{}

func (f *fakeBypassAdminForServer) InFlight() int64                  { return 0 }
func (f *fakeBypassAdminForServer) Probe(context.Context) error      { return nil }
func (f *fakeBypassAdminForServer) RefreshNow(context.Context) error { return nil }

func TestServer_OrchestratorAccessorRoundTrip(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})

	if got := srv.Orchestrator(); got != nil {
		t.Fatalf("zero-value Orchestrator() should be nil, got %v", got)
	}

	fake := &fakeOrchestrator{}
	srv.SetOrchestrator(fake)

	got := srv.Orchestrator()
	if got == nil {
		t.Fatal("Orchestrator() is nil after SetOrchestrator")
	}

	gotFwd, ok := got.(OrchestratorForwarder)
	if !ok {
		t.Fatalf("Orchestrator() does not satisfy OrchestratorForwarder: %T", got)
	}
	if gotFwd != fake {
		t.Errorf("Orchestrator() round-trip mismatch: got %p, want %p", gotFwd, fake)
	}

	srv.SetOrchestrator(nil)
	if got2 := srv.Orchestrator(); got2 != nil {
		t.Errorf("Orchestrator() after nil SetOrchestrator = %v, want nil", got2)
	}
}

func TestServer_BypassAccessorRoundTrip(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})

	if got := srv.Bypass(); got != nil {
		t.Fatalf("zero-value Bypass() should be nil, got %v", got)
	}

	fakeB := &fakeBypassAdminForServer{}
	srv.SetBypass(fakeB)

	got := srv.Bypass()
	if got == nil {
		t.Fatal("Bypass() is nil after SetBypass")
	}
	gotAdmin, ok := got.(BypassAdmin)
	if !ok {
		t.Fatalf("Bypass() does not satisfy BypassAdmin: %T", got)
	}
	if gotAdmin != fakeB {
		t.Errorf("Bypass() round-trip mismatch: got %p, want %p", gotAdmin, fakeB)
	}

	srv.SetBypass(nil)
	if got2 := srv.Bypass(); got2 != nil {
		t.Errorf("Bypass() after nil SetBypass = %v, want nil", got2)
	}
}

func TestServer_CostCountersAccessorRoundTrip(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})

	if got := srv.CostCounters(); got != nil {
		t.Fatalf("zero-value CostCounters() should be nil, got %v", got)
	}

	cc := orchestrator.NewCostCounters(fakeCostStoreServerTest{})
	ctx, cancel := context.WithCancel(context.Background())
	done := cc.StartHourlyMaintenance(ctx)
	srv.SetCostCounters(cc, cancel, done)

	if got := srv.CostCounters(); got != cc {
		t.Errorf("CostCounters() round-trip mismatch: got %p, want %p", got, cc)
	}

	srv.SetCostCounters(nil, nil, nil)
	if got := srv.CostCounters(); got != nil {
		t.Errorf("CostCounters() after nil SetCostCounters = %v, want nil", got)
	}

	srv.SetCostCounters(cc, cancel, done)

	// Stop without HTTP server: Stop MUST cancel the maintenance ctx and
	// wait on done. Use a short ctx deadline; the goroutine drains in
	// O(milliseconds) so 1s is generous.
	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := srv.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// done MUST be closed by now (Stop cancelled the goroutine ctx and
	// waited). A read returns immediately with zero value.
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Error("Stop did not drain the cost-maintenance goroutine within 100ms after returning")
	}
}

// TestServer_StopWithNilCostCountersIsSafe — Stop on a Server that never
// had SetCostCounters called MUST NOT panic / block. Mirrors the audit-
// purge nil-safety contract: tests that bypass infrastructure setup
// (DisableAuditInfra: true, no SetCostCounters call) must still Stop
// cleanly.
func TestServer_StopWithNilCostCountersIsSafe(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{DisableAuditInfra: true})
	// No SetCostCounters call. Stop MUST be a no-op (besides http.Server).
	if err := srv.Stop(context.Background()); err != nil {
		t.Errorf("Stop with nil cost counters: %v", err)
	}
}

func TestServer_RecoverySchedulerAccessorRoundTrip(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})

	if got := srv.RecoveryScheduler(); got != nil {
		t.Fatalf("zero-value RecoveryScheduler() should be nil, got %v", got)
	}

	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{})
	rs := orchestrator.NewRecoveryScheduler(cb, nil, 50*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := rs.Run(ctx)
	srv.SetRecoveryScheduler(rs, cancel, done)

	if got := srv.RecoveryScheduler(); got != rs {
		t.Errorf("RecoveryScheduler() round-trip mismatch: got %p, want %p", got, rs)
	}

	srv.SetRecoveryScheduler(nil, nil, nil)
	if got := srv.RecoveryScheduler(); got != nil {
		t.Errorf("RecoveryScheduler() after nil SetRecoveryScheduler = %v, want nil", got)
	}

	srv.SetRecoveryScheduler(rs, cancel, done)

	// Stop without HTTP server: Stop MUST cancel the recovery ctx and wait
	// on done. Use a short ctx deadline; the goroutine drains in
	// O(milliseconds) since the ticker check loops on ctx.Done.
	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := srv.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// done MUST be closed by now (Stop cancelled the goroutine ctx and
	// waited). A read returns immediately with zero value.
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Error("Stop did not drain the recovery-scheduler goroutine within 100ms after returning")
	}
}

// TestServer_StopWithNilRecoverySchedulerIsSafe — Stop on a Server that
// never had SetRecoveryScheduler called MUST NOT panic / block. Mirrors
// the audit + cost-maint nil-safety contract.
func TestServer_StopWithNilRecoverySchedulerIsSafe(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{DisableAuditInfra: true})
	// No SetRecoveryScheduler call. Stop MUST be a no-op.
	if err := srv.Stop(context.Background()); err != nil {
		t.Errorf("Stop with nil recovery scheduler: %v", err)
	}
}

type fakePinStoreServerTest struct{}

func (fakePinStoreServerTest) Insert(_ orchestrator.PinRow) error { return nil }
func (fakePinStoreServerTest) Delete(_, _ string) error           { return nil }
func (fakePinStoreServerTest) Query(_, _ string) (*orchestrator.PinRow, error) {
	return nil, nil
}
func (fakePinStoreServerTest) ListAll() ([]orchestrator.PinRow, error) {
	return nil, nil
}
func (fakePinStoreServerTest) PurgeExpired(_ time.Time) (int, error) { return 0, nil }

type fakeCapCountersServerTest struct{}

func (fakeCapCountersServerTest) SessionTotal(_ string) float64 { return 0 }
func (fakeCapCountersServerTest) ProjectProfileTierTotal(_, _, _ string, _ time.Duration) float64 {
	return 0
}

type fakeOrchestratorNotifier struct{}

func (fakeOrchestratorNotifier) NotifyINFO(_, _, _ string)     {}
func (fakeOrchestratorNotifier) NotifyWARN(_, _, _ string)     {}
func (fakeOrchestratorNotifier) NotifyCRITICAL(_, _, _ string) {}

func TestServer_PinOverridesAccessorRoundTrip(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})

	if got := srv.PinOverrides(); got != nil {
		t.Fatalf("zero-value PinOverrides() should be nil, got %v", got)
	}

	po := orchestrator.NewPinOverrides(fakePinStoreServerTest{})
	ctx, cancel := context.WithCancel(context.Background())
	done := po.StartTTLSweep(ctx)
	srv.SetPinOverrides(po, cancel, done)

	if got := srv.PinOverrides(); got != po {
		t.Errorf("PinOverrides() round-trip mismatch: got %p, want %p", got, po)
	}

	srv.SetPinOverrides(nil, nil, nil)
	if got := srv.PinOverrides(); got != nil {
		t.Errorf("PinOverrides() after nil SetPinOverrides = %v, want nil", got)
	}

	srv.SetPinOverrides(po, cancel, done)

	// Stop without HTTP server: Stop MUST cancel the sweep ctx and wait
	// on done. Use a short ctx deadline; the goroutine drains in
	// O(milliseconds) since the ticker check loops on ctx.Done.
	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := srv.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// done MUST be closed by now (Stop cancelled the goroutine ctx and
	// waited). A read returns immediately with zero value.
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Error("Stop did not drain the pin-sweep goroutine within 100ms after returning")
	}
}

// TestServer_StopWithNilPinOverridesIsSafe — Stop on a Server that never
// had SetPinOverrides called MUST NOT panic / block. Mirrors the
// audit / cost-maint / recovery-scheduler nil-safety contract.
func TestServer_StopWithNilPinOverridesIsSafe(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{DisableAuditInfra: true})
	// No SetPinOverrides call. Stop MUST be a no-op.
	if err := srv.Stop(context.Background()); err != nil {
		t.Errorf("Stop with nil pin overrides: %v", err)
	}
}

func TestServer_PaygSafetyAccessorRoundTrip(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})

	if got := srv.PaygSafety(); got != nil {
		t.Fatalf("zero-value PaygSafety() should be nil, got %v", got)
	}

	ps := orchestrator.NewPaygSafety(orchestrator.PaygSafetyOptions{
		Counters: fakeCapCountersServerTest{},
		Notifier: fakeOrchestratorNotifier{},
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := ps.WindowResetScheduler(ctx)
	srv.SetPaygSafety(ps, cancel, done)

	if got := srv.PaygSafety(); got != ps {
		t.Errorf("PaygSafety() round-trip mismatch: got %p, want %p", got, ps)
	}

	srv.SetPaygSafety(nil, nil, nil)
	if got := srv.PaygSafety(); got != nil {
		t.Errorf("PaygSafety() after nil SetPaygSafety = %v, want nil", got)
	}

	srv.SetPaygSafety(ps, cancel, done)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := srv.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Error("Stop did not drain the payg-reset goroutine within 100ms after returning")
	}
}

// TestServer_StopWithNilPaygSafetyIsSafe — Stop on a Server that never had
// SetPaygSafety called MUST NOT panic / block. Mirrors the audit /
// cost-maint / recovery-scheduler / pin-sweep nil-safety contract.
func TestServer_StopWithNilPaygSafetyIsSafe(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{DisableAuditInfra: true})
	// No SetPaygSafety call. Stop MUST be a no-op.
	if err := srv.Stop(context.Background()); err != nil {
		t.Errorf("Stop with nil payg safety: %v", err)
	}
}
