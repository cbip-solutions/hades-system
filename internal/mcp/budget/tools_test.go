package budget

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/mcp/client"
	"github.com/cbip-solutions/hades-system/tests/testharness"
)

func writeFile(path string, data string, perm os.FileMode) error {
	return os.WriteFile(path, []byte(data), perm)
}

func newNullBreakdownFakeDaemon(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/budget/rollup", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		_, _ = w.Write([]byte(`{"total_usd":2.5}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newTestServer(t *testing.T, baseURL string) *Server {
	t.Helper()

	dir := t.TempDir()
	tokenPath := dir + "/auth-token"
	if err := writeFile(tokenPath, "test-token", 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	cfg := client.Config{
		BaseURL:       baseURL,
		AuthTokenPath: tokenPath,
	}
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	bc := client.NewBudgetClient(c)
	return NewServer(bc)
}

func invokeRollup(t *testing.T, srv *Server, axis, value string, since time.Time) (*RollupResponse, error) {
	t.Helper()
	args := map[string]any{"axis": axis, "value": value}
	if !since.IsZero() {
		args["since"] = since.UTC().Format(time.RFC3339)
	}
	out, err := srv.InvokeTool(context.Background(), "rollup", args)
	if err != nil {
		return nil, err
	}
	r, ok := out.(RollupResponse)
	if !ok {
		t.Fatalf("rollup: unexpected type %T", out)
	}
	return &r, nil
}

func invokeCapStatus(t *testing.T, srv *Server, axis, value string) (*CapStatusResponse, error) {
	t.Helper()
	out, err := srv.InvokeTool(context.Background(), "cap_status", map[string]any{"axis": axis, "value": value})
	if err != nil {
		return nil, err
	}
	r, ok := out.(CapStatusResponse)
	if !ok {
		t.Fatalf("cap_status: unexpected type %T", out)
	}
	return &r, nil
}

func invokeTag(t *testing.T, srv *Server, costID string, axisTags map[string]string) (*TagResponse, error) {
	t.Helper()
	tags := make(map[string]any, len(axisTags))
	for k, v := range axisTags {
		tags[k] = v
	}
	out, err := srv.InvokeTool(context.Background(), "tag", map[string]any{
		"cost_id":   costID,
		"axis_tags": tags,
	})
	if err != nil {
		return nil, err
	}
	r, ok := out.(TagResponse)
	if !ok {
		t.Fatalf("tag: unexpected type %T", out)
	}
	return &r, nil
}

func invokeAnomalyCheck(t *testing.T, srv *Server, scope, window string) (*AnomalyCheckResponse, error) {
	t.Helper()
	out, err := srv.InvokeTool(context.Background(), "anomaly_check", map[string]any{"scope": scope, "window": window})
	if err != nil {
		return nil, err
	}
	r, ok := out.(AnomalyCheckResponse)
	if !ok {
		t.Fatalf("anomaly_check: unexpected type %T", out)
	}
	return &r, nil
}

func invokePause(t *testing.T, srv *Server, scope, reason string) (*PauseStateResponse, error) {
	t.Helper()
	out, err := srv.InvokeTool(context.Background(), "pause", map[string]any{"scope": scope, "reason": reason})
	if err != nil {
		return nil, err
	}
	r, ok := out.(PauseStateResponse)
	if !ok {
		t.Fatalf("pause: unexpected type %T", out)
	}
	return &r, nil
}

func invokeResume(t *testing.T, srv *Server, scope string) (*PauseStateResponse, error) {
	t.Helper()
	out, err := srv.InvokeTool(context.Background(), "resume", map[string]any{"scope": scope})
	if err != nil {
		return nil, err
	}
	r, ok := out.(PauseStateResponse)
	if !ok {
		t.Fatalf("resume: unexpected type %T", out)
	}
	return &r, nil
}

func invokeEvents(t *testing.T, srv *Server, since time.Time) (*EventsResponse, error) {
	t.Helper()
	args := map[string]any{}
	if !since.IsZero() {
		args["since"] = since.UTC().Format(time.RFC3339)
	}
	out, err := srv.InvokeTool(context.Background(), "events", args)
	if err != nil {
		return nil, err
	}
	r, ok := out.(EventsResponse)
	if !ok {
		t.Fatalf("events: unexpected type %T", out)
	}
	return &r, nil
}

func TestRollupHappyPath(t *testing.T) {
	cfg := &testharness.BudgetFakeConfig{
		RollupTotalUSD:  4.20,
		RollupBreakdown: map[string]float64{"design": 1.10, "build": 3.10},
	}
	fakeDaemon := testharness.NewBudgetFakeDaemon(t, cfg)
	srv := newTestServer(t, fakeDaemon.URL)

	resp, err := invokeRollup(t, srv, "stage", "design", time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("rollup: unexpected error: %v", err)
	}
	if resp.TotalUSD != 4.20 {
		t.Errorf("TotalUSD = %f, want 4.20", resp.TotalUSD)
	}
	if resp.Breakdown["design"] != 1.10 {
		t.Errorf("Breakdown[design] = %f, want 1.10", resp.Breakdown["design"])
	}
}

func TestRollupDaemonDown(t *testing.T) {
	cfg := &testharness.BudgetFakeConfig{RollupErr: http.StatusInternalServerError}
	fakeDaemon := testharness.NewBudgetFakeDaemon(t, cfg)
	srv := newTestServer(t, fakeDaemon.URL)

	_, err := invokeRollup(t, srv, "project", "internal-platform-x", time.Now())
	if err == nil {
		t.Fatal("rollup: expected error on 5xx daemon response, got nil")
	}
}

func TestRollupDaemonGone(t *testing.T) {

	srv := newTestServer(t, "http://127.0.0.1:0")

	_, err := invokeRollup(t, srv, "project", "x", time.Now())
	if err == nil {
		t.Fatal("rollup: expected error when daemon unreachable, got nil")
	}
}

func TestRollupNilClient(t *testing.T) {
	srv := NewServer(nil)
	_, err := invokeRollup(t, srv, "stage", "design", time.Now())
	if !errors.Is(err, ErrNilClient) {
		t.Fatalf("rollup: want errors.Is(err, ErrNilClient); got err=%v", err)
	}
}

func TestRollupInvalidSinceTimestamp(t *testing.T) {
	cfg := &testharness.BudgetFakeConfig{}
	fakeDaemon := testharness.NewBudgetFakeDaemon(t, cfg)
	srv := newTestServer(t, fakeDaemon.URL)

	_, err := srv.InvokeTool(context.Background(), "rollup", map[string]any{
		"axis":  "stage",
		"value": "design",
		"since": "not-a-timestamp",
	})
	if err == nil {
		t.Fatal("rollup: expected error for invalid since timestamp")
	}
}

func TestRollupNilBreakdown(t *testing.T) {

	fakeDaemon := newNullBreakdownFakeDaemon(t)
	srv := newTestServer(t, fakeDaemon.URL)

	resp, err := invokeRollup(t, srv, "project", "internal-platform-x", time.Time{})
	if err != nil {
		t.Fatalf("rollup nil breakdown: unexpected error: %v", err)
	}
	if resp.Breakdown == nil {
		t.Error("rollup nil breakdown: Breakdown should be non-nil empty map, got nil")
	}
}

func TestCapStatusAllowed(t *testing.T) {
	cfg := &testharness.BudgetFakeConfig{
		CapRemainingUSD: 9.50,
		CapAllowed:      true,
		CapBlockedScope: "",
	}
	fakeDaemon := testharness.NewBudgetFakeDaemon(t, cfg)
	srv := newTestServer(t, fakeDaemon.URL)

	resp, err := invokeCapStatus(t, srv, "stage", "design")
	if err != nil {
		t.Fatalf("cap_status: unexpected error: %v", err)
	}
	if resp.Blocked {
		t.Errorf("cap_status: expected allowed (Blocked=false), got blocked (scope=%q)", resp.BlockedScope)
	}
	if resp.RemainingUSD != 9.50 {
		t.Errorf("RemainingUSD = %f, want 9.50", resp.RemainingUSD)
	}
}

func TestCapStatusBlocked(t *testing.T) {
	cfg := &testharness.BudgetFakeConfig{
		CapRemainingUSD: 0.00,
		CapAllowed:      false,
		CapBlockedScope: "stage",
	}
	fakeDaemon := testharness.NewBudgetFakeDaemon(t, cfg)
	srv := newTestServer(t, fakeDaemon.URL)

	resp, err := invokeCapStatus(t, srv, "stage", "design")
	if err != nil {
		t.Fatalf("cap_status: unexpected error on blocked response: %v", err)
	}
	if !resp.Blocked {
		t.Error("cap_status: expected blocked=true")
	}
	if resp.BlockedScope != "stage" {
		t.Errorf("BlockedScope = %q, want %q", resp.BlockedScope, "stage")
	}
}

func TestCapStatusDaemonDown(t *testing.T) {
	cfg := &testharness.BudgetFakeConfig{CapStatusErr: http.StatusServiceUnavailable}
	fakeDaemon := testharness.NewBudgetFakeDaemon(t, cfg)
	srv := newTestServer(t, fakeDaemon.URL)

	_, err := invokeCapStatus(t, srv, "project", "internal-platform-x")
	if err == nil {
		t.Fatal("cap_status: expected error on 503 daemon response, got nil")
	}
}

func TestCapStatusNilClient(t *testing.T) {
	srv := NewServer(nil)
	_, err := invokeCapStatus(t, srv, "stage", "design")
	if !errors.Is(err, ErrNilClient) {
		t.Fatalf("cap_status: want errors.Is(err, ErrNilClient); got err=%v", err)
	}
}

func TestTagHappyPath(t *testing.T) {
	cfg := &testharness.BudgetFakeConfig{}
	fakeDaemon := testharness.NewBudgetFakeDaemon(t, cfg)
	srv := newTestServer(t, fakeDaemon.URL)

	resp, err := invokeTag(t, srv, "cost-99", map[string]string{
		"project":   "internal-platform-x",
		"stage":     "design",
		"task":      "t-001",
		"operation": "audit_review",
	})
	if err != nil {
		t.Fatalf("tag: unexpected error: %v", err)
	}
	if !resp.OK {
		t.Error("tag: expected ok=true")
	}
}

func TestTagIdempotent(t *testing.T) {
	cfg := &testharness.BudgetFakeConfig{}
	fakeDaemon := testharness.NewBudgetFakeDaemon(t, cfg)
	srv := newTestServer(t, fakeDaemon.URL)

	axisTags := map[string]string{"project": "p1"}
	resp1, err := invokeTag(t, srv, "cost-42", axisTags)
	if err != nil || !resp1.OK {
		t.Fatalf("tag first call: err=%v ok=%v", err, resp1)
	}
	resp2, err := invokeTag(t, srv, "cost-42", axisTags)
	if err != nil || !resp2.OK {
		t.Fatalf("tag second call (idempotent): err=%v ok=%v", err, resp2)
	}
}

func TestTagDaemonDown(t *testing.T) {
	cfg := &testharness.BudgetFakeConfig{TagErr: http.StatusInternalServerError}
	fakeDaemon := testharness.NewBudgetFakeDaemon(t, cfg)
	srv := newTestServer(t, fakeDaemon.URL)

	_, err := invokeTag(t, srv, "cost-1", map[string]string{"project": "x"})
	if err == nil {
		t.Fatal("tag: expected error on 5xx, got nil")
	}
}

func TestTagNilClient(t *testing.T) {
	srv := NewServer(nil)
	_, err := invokeTag(t, srv, "cost-1", map[string]string{})
	if !errors.Is(err, ErrNilClient) {
		t.Fatalf("tag: want errors.Is(err, ErrNilClient); got err=%v", err)
	}
}

func TestAnomalyCheckHappyPath(t *testing.T) {
	cfg := &testharness.BudgetFakeConfig{
		AnomalyZScore:  4.7,
		AnomalyMean:    1.2,
		AnomalyStd:     0.3,
		AnomalySamples: 120,
	}
	fakeDaemon := testharness.NewBudgetFakeDaemon(t, cfg)
	srv := newTestServer(t, fakeDaemon.URL)

	resp, err := invokeAnomalyCheck(t, srv, "stage", "1h")
	if err != nil {
		t.Fatalf("anomaly_check: unexpected error: %v", err)
	}
	if resp.ZScore != 4.7 {
		t.Errorf("ZScore = %f, want 4.7", resp.ZScore)
	}
	if resp.Samples != 120 {
		t.Errorf("Samples = %d, want 120", resp.Samples)
	}
}

func TestAnomalyCheckDaemonDown(t *testing.T) {
	cfg := &testharness.BudgetFakeConfig{AnomalyErr: http.StatusInternalServerError}
	fakeDaemon := testharness.NewBudgetFakeDaemon(t, cfg)
	srv := newTestServer(t, fakeDaemon.URL)

	_, err := invokeAnomalyCheck(t, srv, "project", "")
	if err == nil {
		t.Fatal("anomaly_check: expected error on 5xx, got nil")
	}
}

func TestAnomalyCheckNilClient(t *testing.T) {
	srv := NewServer(nil)
	_, err := invokeAnomalyCheck(t, srv, "project", "")
	if !errors.Is(err, ErrNilClient) {
		t.Fatalf("anomaly_check: want errors.Is(err, ErrNilClient); got err=%v", err)
	}
}

func TestEventsHappyPath(t *testing.T) {
	cfg := &testharness.BudgetFakeConfig{
		Events: []map[string]any{
			testharness.SampleBudgetEvent("cap_hit", "stage"),
			testharness.SampleBudgetEvent("anomaly_triggered", "project"),
		},
	}
	fakeDaemon := testharness.NewBudgetFakeDaemon(t, cfg)
	srv := newTestServer(t, fakeDaemon.URL)

	resp, err := invokeEvents(t, srv, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("events: unexpected error: %v", err)
	}
	if len(resp.Events) != 2 {
		t.Errorf("events count = %d, want 2", len(resp.Events))
	}
	if resp.Events[0].Kind != "cap_hit" {
		t.Errorf("events[0].Kind = %q, want %q", resp.Events[0].Kind, "cap_hit")
	}
}

func TestEventsEmptyListNotNil(t *testing.T) {
	cfg := &testharness.BudgetFakeConfig{Events: []map[string]any{}}
	fakeDaemon := testharness.NewBudgetFakeDaemon(t, cfg)
	srv := newTestServer(t, fakeDaemon.URL)

	resp, err := invokeEvents(t, srv, time.Now())
	if err != nil {
		t.Fatalf("events: unexpected error: %v", err)
	}
	if resp.Events == nil {
		t.Error("events: returned nil slice; want empty non-nil slice")
	}
	if len(resp.Events) != 0 {
		t.Errorf("events: expected 0 events, got %d", len(resp.Events))
	}
}

func TestEventsDaemonDown(t *testing.T) {
	cfg := &testharness.BudgetFakeConfig{EventErr: http.StatusServiceUnavailable}
	fakeDaemon := testharness.NewBudgetFakeDaemon(t, cfg)
	srv := newTestServer(t, fakeDaemon.URL)

	_, err := invokeEvents(t, srv, time.Now())
	if err == nil {
		t.Fatal("events: expected error on 503, got nil")
	}
}

func TestEventsNilClient(t *testing.T) {
	srv := NewServer(nil)
	_, err := invokeEvents(t, srv, time.Now())
	if !errors.Is(err, ErrNilClient) {
		t.Fatalf("events: want errors.Is(err, ErrNilClient); got err=%v", err)
	}
}

func TestEventsInvalidSinceTimestamp(t *testing.T) {
	cfg := &testharness.BudgetFakeConfig{}
	fakeDaemon := testharness.NewBudgetFakeDaemon(t, cfg)
	srv := newTestServer(t, fakeDaemon.URL)

	_, err := srv.InvokeTool(context.Background(), "events", map[string]any{
		"since": "not-a-timestamp",
	})
	if err == nil {
		t.Fatal("events: expected error for invalid since timestamp")
	}
}

func TestPauseHappyPath(t *testing.T) {
	cfg := &testharness.BudgetFakeConfig{
		PauseScope:  "stage",
		PauseActive: true,
		PauseMode:   "descriptive",

		PauseReason: "manual operator halt",
	}
	fakeDaemon := testharness.NewBudgetFakeDaemon(t, cfg)
	srv := newTestServer(t, fakeDaemon.URL)

	resp, err := invokePause(t, srv, "stage", "manual operator halt")
	if err != nil {
		t.Fatalf("pause: unexpected error: %v", err)
	}
	if !resp.Active {
		t.Error("pause: expected active=true after pause call")
	}
	if resp.Scope != "stage" {
		t.Errorf("pause: Scope = %q, want %q", resp.Scope, "stage")
	}
	if resp.Reason != "manual operator halt" {
		t.Errorf("pause: Reason = %q, want 'manual operator halt' (sourced from daemon, not request)", resp.Reason)
	}
}

func TestPauseAlreadyPaused(t *testing.T) {
	cfg := &testharness.BudgetFakeConfig{
		PauseScope:  "project",
		PauseActive: true,
		PauseMode:   "descriptive",
	}
	fakeDaemon := testharness.NewBudgetFakeDaemon(t, cfg)
	srv := newTestServer(t, fakeDaemon.URL)

	resp, err := invokePause(t, srv, "project", "second call")
	if err != nil {
		t.Fatalf("pause (second call): unexpected error: %v", err)
	}
	if !resp.Active {
		t.Error("pause (second call): expected active=true")
	}
}

func TestPauseDaemonDown(t *testing.T) {
	cfg := &testharness.BudgetFakeConfig{PauseErr: http.StatusServiceUnavailable}
	fakeDaemon := testharness.NewBudgetFakeDaemon(t, cfg)
	srv := newTestServer(t, fakeDaemon.URL)

	_, err := invokePause(t, srv, "project", "test")
	if err == nil {
		t.Fatal("pause: expected error on 503, got nil")
	}
}

func TestPauseNilClient(t *testing.T) {
	srv := NewServer(nil)
	_, err := invokePause(t, srv, "project", "test")
	if !errors.Is(err, ErrNilClient) {
		t.Fatalf("pause: want errors.Is(err, ErrNilClient); got err=%v", err)
	}
}

func TestResumeHappyPath(t *testing.T) {
	cfg := &testharness.BudgetFakeConfig{
		PauseScope:  "stage",
		PauseActive: false,
		PauseMode:   "descriptive",
	}
	fakeDaemon := testharness.NewBudgetFakeDaemon(t, cfg)
	srv := newTestServer(t, fakeDaemon.URL)

	resp, err := invokeResume(t, srv, "stage")
	if err != nil {
		t.Fatalf("resume: unexpected error: %v", err)
	}
	if resp.Active {
		t.Error("resume: expected active=false after resume call")
	}
	if resp.Scope != "stage" {
		t.Errorf("resume: Scope = %q, want %q", resp.Scope, "stage")
	}
}

func TestResumeDaemonDown(t *testing.T) {
	cfg := &testharness.BudgetFakeConfig{ResumeErr: http.StatusInternalServerError}
	fakeDaemon := testharness.NewBudgetFakeDaemon(t, cfg)
	srv := newTestServer(t, fakeDaemon.URL)

	_, err := invokeResume(t, srv, "project")
	if err == nil {
		t.Fatal("resume: expected error on 5xx, got nil")
	}
}

func TestResumeNilClient(t *testing.T) {
	srv := NewServer(nil)
	_, err := invokeResume(t, srv, "project")
	if !errors.Is(err, ErrNilClient) {
		t.Fatalf("resume: want errors.Is(err, ErrNilClient); got err=%v", err)
	}
}

func TestIntegrationAllToolsWorkflow(t *testing.T) {
	cfg := &testharness.BudgetFakeConfig{
		RollupTotalUSD:  7.50,
		RollupBreakdown: map[string]float64{"design": 2.50, "build": 5.00},
		CapRemainingUSD: 12.50,
		CapAllowed:      true,
		AnomalyZScore:   1.2,
		AnomalyMean:     0.8,
		AnomalyStd:      0.4,
		AnomalySamples:  60,
		PauseScope:      "stage",
		PauseActive:     false,
		PauseMode:       "descriptive",
		Events: []map[string]any{
			testharness.SampleBudgetEvent("axis_tag", "stage"),
			testharness.SampleBudgetEvent("pre_call_blocked", "stage"),
		},
	}
	fakeDaemon := testharness.NewBudgetFakeDaemon(t, cfg)
	srv := newTestServer(t, fakeDaemon.URL)

	rollupResp, err := invokeRollup(t, srv, "stage", "design", time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("integration/rollup: %v", err)
	}
	if rollupResp.TotalUSD != 7.50 {
		t.Errorf("integration/rollup TotalUSD = %f, want 7.50", rollupResp.TotalUSD)
	}

	capResp, err := invokeCapStatus(t, srv, "stage", "design")
	if err != nil {
		t.Fatalf("integration/cap_status: %v", err)
	}
	if capResp.Blocked {
		t.Errorf("integration/cap_status: expected allowed, got blocked")
	}

	tagResp, err := invokeTag(t, srv, "cost-1001", map[string]string{
		"project": "internal-platform-x", "stage": "design", "task": "t-042", "operation": "synthesize",
	})
	if err != nil {
		t.Fatalf("integration/tag: %v", err)
	}
	if !tagResp.OK {
		t.Error("integration/tag: expected ok=true")
	}

	anomalyResp, err := invokeAnomalyCheck(t, srv, "stage", "30m")
	if err != nil {
		t.Fatalf("integration/anomaly_check: %v", err)
	}
	if anomalyResp.ZScore >= 4.0 {
		t.Errorf("integration/anomaly_check: z-score %f >= 4.0 would trigger auto-pause", anomalyResp.ZScore)
	}

	unlock := testharness.LockBudgetFakeConfig(cfg)
	cfg.PauseActive = true
	unlock()

	pauseResp, err := invokePause(t, srv, "stage", "integration test pause")
	if err != nil {
		t.Fatalf("integration/pause: %v", err)
	}
	if !pauseResp.Active {
		t.Error("integration/pause: expected active=true")
	}

	eventsResp, err := invokeEvents(t, srv, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("integration/events: %v", err)
	}
	if len(eventsResp.Events) != 2 {
		t.Errorf("integration/events: count = %d, want 2", len(eventsResp.Events))
	}

	unlock = testharness.LockBudgetFakeConfig(cfg)
	cfg.PauseActive = false
	unlock()

	resumeResp, err := invokeResume(t, srv, "stage")
	if err != nil {
		t.Fatalf("integration/resume: %v", err)
	}
	if resumeResp.Active {
		t.Error("integration/resume: expected active=false after resume")
	}

	eventsResp2, err := invokeEvents(t, srv, time.Now().Add(-30*time.Minute))
	if err != nil {
		t.Fatalf("integration/events after resume: %v", err)
	}
	if eventsResp2.Events == nil {
		t.Error("integration/events after resume: nil slice returned")
	}

	t.Logf("integration workflow complete: rollup=%.2f cap_allowed=%v tag_ok=%v z=%.2f paused=%v events=%d resumed=%v",
		rollupResp.TotalUSD, !capResp.Blocked, tagResp.OK, anomalyResp.ZScore,
		pauseResp.Active, len(eventsResp.Events), !resumeResp.Active)
}

func TestInvokeToolUnknown(t *testing.T) {
	srv := NewServer(nil)
	_, err := srv.InvokeTool(context.Background(), "nonexistent", nil)
	if err == nil {
		t.Fatal("InvokeTool(unknown): expected error, got nil")
	}
}

func TestReqString_AbsentNilString(t *testing.T) {
	args := map[string]any{
		"explicit_nil": nil,
		"valid":        "hello",
	}

	if got, err := reqString(args, "missing"); err != nil || got != "" {
		t.Errorf("missing: got (%q, %v); want (\"\", nil)", got, err)
	}

	if got, err := reqString(args, "explicit_nil"); err != nil || got != "" {
		t.Errorf("explicit_nil: got (%q, %v); want (\"\", nil)", got, err)
	}

	if got, err := reqString(args, "valid"); err != nil || got != "hello" {
		t.Errorf("valid: got (%q, %v); want (\"hello\", nil)", got, err)
	}

	args["bad"] = 42
	got, err := reqString(args, "bad")
	if err == nil {
		t.Error("bad: expected non-nil error for non-string value")
	}
	if got != "" {
		t.Errorf("bad: got value %q; want \"\"", got)
	}
	if !strings.Contains(err.Error(), "bad") || !strings.Contains(err.Error(), "expected string") {
		t.Errorf("bad: error %q lacks key name or expected-string phrase", err.Error())
	}
}

func TestArgCoercion_NonStringRejected(t *testing.T) {
	cfg := &testharness.BudgetFakeConfig{}
	fakeDaemon := testharness.NewBudgetFakeDaemon(t, cfg)
	srv := newTestServer(t, fakeDaemon.URL)

	cases := []struct {
		name    string
		tool    string
		args    map[string]any
		wantKey string
	}{

		{"rollup_axis_int", "rollup", map[string]any{"axis": 42, "value": "design"}, "axis"},
		{"rollup_value_bool", "rollup", map[string]any{"axis": "stage", "value": true}, "value"},
		{"rollup_since_int", "rollup", map[string]any{"axis": "stage", "value": "design", "since": 1234567890}, "since"},

		{"cap_axis_float", "cap_status", map[string]any{"axis": 1.5, "value": "x"}, "axis"},
		{"cap_value_obj", "cap_status", map[string]any{"axis": "project", "value": map[string]any{}}, "value"},

		{"tag_costid_int", "tag", map[string]any{"cost_id": 99, "axis_tags": map[string]any{"project": "x"}}, "cost_id"},

		{"anomaly_scope_int", "anomaly_check", map[string]any{"scope": 1, "window": "1h"}, "scope"},
		{"anomaly_window_int", "anomaly_check", map[string]any{"scope": "stage", "window": 60}, "window"},

		{"pause_scope_int", "pause", map[string]any{"scope": 7, "reason": "x"}, "scope"},
		{"pause_reason_int", "pause", map[string]any{"scope": "stage", "reason": 42}, "reason"},

		{"resume_scope_int", "resume", map[string]any{"scope": 5}, "scope"},

		{"events_since_int", "events", map[string]any{"since": 1234567890}, "since"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := srv.InvokeTool(context.Background(), tc.tool, tc.args)
			if err == nil {
				t.Fatalf("%s/%s: expected error for non-string %q, got nil",
					tc.tool, tc.name, tc.wantKey)
			}
			msg := err.Error()

			if !strings.Contains(msg, tc.wantKey) {
				t.Errorf("%s: error %q does not mention key %q",
					tc.tool, msg, tc.wantKey)
			}

			if !strings.Contains(msg, "expected string") {
				t.Errorf("%s: error %q lacks 'expected string' phrase", tc.tool, msg)
			}
		})
	}
}

func TestArgCoercion_RequiredFieldsValidated(t *testing.T) {
	cfg := &testharness.BudgetFakeConfig{}
	fakeDaemon := testharness.NewBudgetFakeDaemon(t, cfg)
	srv := newTestServer(t, fakeDaemon.URL)

	cases := []struct {
		name string
		tool string
		args map[string]any
		want string
	}{

		{"rollup_no_axis", "rollup", map[string]any{"value": "design"}, "axis"},
		{"rollup_no_value", "rollup", map[string]any{"axis": "stage"}, "value"},

		{"cap_no_axis", "cap_status", map[string]any{"value": "x"}, "axis"},
		{"cap_no_value", "cap_status", map[string]any{"axis": "stage"}, "value"},

		{"tag_no_costid", "tag", map[string]any{"axis_tags": map[string]any{"project": "x"}}, "cost_id"},

		{"anomaly_no_scope", "anomaly_check", map[string]any{"window": "1h"}, "scope"},

		{"pause_no_scope", "pause", map[string]any{"reason": "x"}, "scope"},
		{"pause_no_reason", "pause", map[string]any{"scope": "stage"}, "reason"},

		{"resume_no_scope", "resume", map[string]any{}, "scope"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := srv.InvokeTool(context.Background(), tc.tool, tc.args)
			if err == nil {
				t.Fatalf("%s: expected required-field error for %q, got nil",
					tc.tool, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s: error %q does not mention required key %q",
					tc.tool, err.Error(), tc.want)
			}
		})
	}
}

func TestEvents_PayloadAndIDPopulated(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/budget/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		_, _ = w.Write([]byte(`[
			{"id":"evt-001","type":"cap_hit","scope":"stage","cost_usd":2.5,"created_at":"2026-04-30T10:00:00Z","payload":{"axis":"stage","value":"design","limit_usd":10.0}},
			{"id":"evt-002","type":"resume","scope":"project","cost_usd":0,"created_at":"2026-04-30T10:01:00Z"}
		]`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	s := newTestServer(t, srv.URL)
	resp, err := invokeEvents(t, s, time.Time{})
	if err != nil {
		t.Fatalf("events: unexpected error: %v", err)
	}
	if len(resp.Events) != 2 {
		t.Fatalf("events: got %d, want 2", len(resp.Events))
	}

	e0 := resp.Events[0]
	if e0.ID != "evt-001" {
		t.Errorf("events[0].ID = %q, want %q", e0.ID, "evt-001")
	}
	if e0.Payload == nil {
		t.Fatal("events[0].Payload: got nil; want non-nil map")
	}
	if axis, ok := e0.Payload["axis"].(string); !ok || axis != "stage" {
		t.Errorf("events[0].Payload[axis] = %v (%T), want \"stage\" (string)", e0.Payload["axis"], e0.Payload["axis"])
	}
	if limit, ok := e0.Payload["limit_usd"].(float64); !ok || limit != 10.0 {
		t.Errorf("events[0].Payload[limit_usd] = %v (%T), want 10.0 (float64)", e0.Payload["limit_usd"], e0.Payload["limit_usd"])
	}

	e1 := resp.Events[1]
	if e1.ID != "evt-002" {
		t.Errorf("events[1].ID = %q, want %q", e1.ID, "evt-002")
	}
	if len(e1.Payload) != 0 {
		t.Errorf("events[1].Payload should be empty/nil when daemon omits it; got %v", e1.Payload)
	}

	hasPayload := false
	for _, e := range resp.Events {
		if e.Payload != nil {
			hasPayload = true
			break
		}
	}
	if !hasPayload {
		t.Error("events: no event has non-nil Payload — handler is dropping the field")
	}
}

func TestPause_ReasonFromDaemonNotRequest(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/budget/pause", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		_, _ = w.Write([]byte(`{"scope":"stage","active":true,"pause_mode":"descriptive","reason":"original-reason-from-daemon"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	s := newTestServer(t, srv.URL)
	resp, err := invokePause(t, s, "stage", "caller-supplied-reason")
	if err != nil {
		t.Fatalf("pause: unexpected error: %v", err)
	}
	if resp.Reason != "original-reason-from-daemon" {
		t.Errorf("Reason = %q, want %q (must come from daemon response, not caller request)",
			resp.Reason, "original-reason-from-daemon")
	}
}

func TestResume_ReasonFromDaemon(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/budget/resume", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		_, _ = w.Write([]byte(`{"scope":"stage","active":false,"pause_mode":"descriptive","reason":"resume-context-from-daemon"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	s := newTestServer(t, srv.URL)
	resp, err := invokeResume(t, s, "stage")
	if err != nil {
		t.Fatalf("resume: unexpected error: %v", err)
	}
	if resp.Reason != "resume-context-from-daemon" {
		t.Errorf("Reason = %q, want %q (must come from daemon response, not be dropped)",
			resp.Reason, "resume-context-from-daemon")
	}
}

func TestHandleTag_NoDuplicateCostID(t *testing.T) {
	var capturedBody []byte
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/budget/record", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		capturedBody = b
		w.WriteHeader(http.StatusCreated)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	fakeDaemon := httptest.NewServer(mux)
	t.Cleanup(fakeDaemon.Close)

	srv := newTestServer(t, fakeDaemon.URL)
	resp, err := invokeTag(t, srv, "cost-555", map[string]string{
		"project": "internal-platform-x",
		"stage":   "design",
	})
	if err != nil {
		t.Fatalf("tag: unexpected error: %v", err)
	}
	if !resp.OK {
		t.Fatal("tag: expected ok=true")
	}
	if len(capturedBody) == 0 {
		t.Fatal("tag: daemon never received request body")
	}

	var body struct {
		CostID   string `json:"cost_id"`
		AxisTags []struct {
			CostID string `json:"cost_id,omitempty"`
			Axis   string `json:"axis"`
			Value  string `json:"value"`
		} `json:"axis_tags"`
	}
	if err := json.Unmarshal(capturedBody, &body); err != nil {
		t.Fatalf("tag: decode captured body: %v (raw=%s)", err, capturedBody)
	}
	if body.CostID != "cost-555" {
		t.Errorf("top-level cost_id = %q, want %q", body.CostID, "cost-555")
	}
	if len(body.AxisTags) != 2 {
		t.Fatalf("axis_tags len = %d, want 2", len(body.AxisTags))
	}
	for i, at := range body.AxisTags {
		if at.CostID != "" {
			t.Errorf("axis_tags[%d].cost_id = %q; expected empty (top-level cost_id is canonical)", i, at.CostID)
		}
		if at.Axis == "" || at.Value == "" {
			t.Errorf("axis_tags[%d]: axis=%q value=%q; both required", i, at.Axis, at.Value)
		}
	}
}
