// Package cli — day_test.go (Plan 7 Phase F Task F-10).
//
// Tests for the F-10 zen day dispatcher:
//
//   - runDay flag precedence (check-pending > eod > default morning).
//   - Force flag propagation through the selected path.
//   - Error propagation + classification (409 → recoverable + force
//     hint; 503 → recoverable + daemon-not-ready hint; opaque → bare).
//   - NewDayCmd integration via httptest.Server (full wire path:
//     cobra → newClientFromCmd → typed Client → daemon → BriefDoc →
//     Render → stdout).
//   - --include-bypass legacy no-op emits the deprecation warning.
//
// The runDay-level tests use a fakeZenDayClient so each flag
// combination is exercised without standing up a real httptest server.
// The integration test pins the wire path via TestOnlyClientFactory +
// httptest.Server stubbing /v1/zen-day/*.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/zenday"
)

type fakeZenDayClient struct {
	mu sync.Mutex

	morningDoc   zenday.BriefDoc
	morningErr   error
	morningForce []bool

	eodDoc   zenday.BriefDoc
	eodErr   error
	eodForce []bool

	cpDoc   zenday.BriefDoc
	cpErr   error
	cpCalls int
}

func (f *fakeZenDayClient) GenerateMorning(_ context.Context, force bool) (zenday.BriefDoc, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.morningForce = append(f.morningForce, force)
	return f.morningDoc, f.morningErr
}

func (f *fakeZenDayClient) GenerateEOD(_ context.Context, force bool) (zenday.BriefDoc, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.eodForce = append(f.eodForce, force)
	return f.eodDoc, f.eodErr
}

func (f *fakeZenDayClient) CheckPending(_ context.Context) (zenday.BriefDoc, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cpCalls++
	return f.cpDoc, f.cpErr
}

func canonicalMorning() zenday.BriefDoc {
	return zenday.BriefDoc{
		Date: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Type: zenday.BriefTypeMorning,
		Items: []zenday.BriefItem{{
			Rank:      zenday.RankOperatorGate,
			Project:   "internal-platform-x",
			Message:   "autonomous-mode pause-for-confirmation pending",
			Action:    "zen autonomy ack",
			CreatedAt: time.Date(2026, 5, 1, 7, 30, 0, 0, time.UTC),
		}},
	}
}

func canonicalEOD() zenday.BriefDoc {
	return zenday.BriefDoc{
		Date:         time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Type:         zenday.BriefTypeEOD,
		CostWatchUSD: 1.24,
	}
}

func canonicalCheckPending() zenday.BriefDoc {
	return zenday.BriefDoc{
		Date:                time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Type:                zenday.BriefTypeCheckPending,
		NextScheduledAt:     time.Date(2026, 5, 2, 8, 0, 0, 0, time.UTC),
		PendingActionNeeded: 2,
		PendingUrgent:       1,
	}
}

func TestRunDay_DefaultDispatchesToMorning(t *testing.T) {
	f := &fakeZenDayClient{morningDoc: canonicalMorning()}
	var buf bytes.Buffer

	if err := runDay(t.Context(), f, &buf, false, false, false); err != nil {
		t.Fatalf("runDay err = %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.morningForce) != 1 {
		t.Errorf("expected 1 morning call, got %d", len(f.morningForce))
	}
	if len(f.eodForce) != 0 {
		t.Errorf("eod must not be invoked: %v", f.eodForce)
	}
	if f.cpCalls != 0 {
		t.Errorf("check-pending must not be invoked: %d calls", f.cpCalls)
	}
	if !strings.Contains(buf.String(), "morning brief") {
		t.Errorf("output missing morning brief heading: %q", buf.String())
	}
}

func TestRunDay_EODFlagDispatchesToEOD(t *testing.T) {
	f := &fakeZenDayClient{eodDoc: canonicalEOD()}
	var buf bytes.Buffer

	if err := runDay(t.Context(), f, &buf, false, true, false); err != nil {
		t.Fatalf("runDay err = %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.eodForce) != 1 {
		t.Errorf("expected 1 eod call, got %d", len(f.eodForce))
	}
	if len(f.morningForce) != 0 {
		t.Errorf("morning must not be invoked: %v", f.morningForce)
	}
	if !strings.Contains(buf.String(), "EOD digest") {
		t.Errorf("output missing EOD heading: %q", buf.String())
	}
}

func TestRunDay_CheckPendingFlagDispatchesToCheckPending(t *testing.T) {
	f := &fakeZenDayClient{cpDoc: canonicalCheckPending()}
	var buf bytes.Buffer

	if err := runDay(t.Context(), f, &buf, false, false, true); err != nil {
		t.Fatalf("runDay err = %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cpCalls != 1 {
		t.Errorf("expected 1 check-pending call, got %d", f.cpCalls)
	}
	if !strings.Contains(buf.String(), "Next morning brief") {
		t.Errorf("output missing check-pending heading: %q", buf.String())
	}
}

func TestRunDay_CheckPendingPrecedesEOD(t *testing.T) {
	f := &fakeZenDayClient{cpDoc: canonicalCheckPending()}
	var buf bytes.Buffer

	if err := runDay(t.Context(), f, &buf, false, true, true); err != nil {
		t.Fatalf("runDay err = %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cpCalls != 1 {
		t.Errorf("expected 1 check-pending call, got %d", f.cpCalls)
	}
	if len(f.eodForce) != 0 {
		t.Errorf("eod must not be invoked when check-pending also set: %v", f.eodForce)
	}
}

func TestRunDay_CheckPendingPrecedesForce(t *testing.T) {
	f := &fakeZenDayClient{cpDoc: canonicalCheckPending()}
	var buf bytes.Buffer

	if err := runDay(t.Context(), f, &buf, true, false, true); err != nil {
		t.Fatalf("runDay err = %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cpCalls != 1 {
		t.Errorf("expected 1 check-pending call, got %d", f.cpCalls)
	}

}

func TestRunDay_ForceFlagPropagatesToMorning(t *testing.T) {
	f := &fakeZenDayClient{morningDoc: canonicalMorning()}
	var buf bytes.Buffer

	if err := runDay(t.Context(), f, &buf, true, false, false); err != nil {
		t.Fatalf("runDay err = %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.morningForce) != 1 || !f.morningForce[0] {
		t.Errorf("force flag did not propagate to morning: %v", f.morningForce)
	}
}

func TestRunDay_ForceFlagPropagatesToEOD(t *testing.T) {
	f := &fakeZenDayClient{eodDoc: canonicalEOD()}
	var buf bytes.Buffer

	if err := runDay(t.Context(), f, &buf, true, true, false); err != nil {
		t.Fatalf("runDay err = %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.eodForce) != 1 || !f.eodForce[0] {
		t.Errorf("force flag did not propagate to eod: %v", f.eodForce)
	}
}

func TestRunDay_409MapsToRecoverableWithForceHint(t *testing.T) {
	herr := &client.HTTPError{Method: "POST", Path: "/v1/zen-day/morning", Status: http.StatusConflict, RawBody: []byte("today's brief already generated")}
	f := &fakeZenDayClient{morningErr: fmt.Errorf("POST /v1/zen-day/morning: 409: %w", herr)}
	var buf bytes.Buffer

	err := runDay(t.Context(), f, &buf, false, false, false)
	if err == nil {
		t.Fatalf("runDay err = nil, want recoverable")
	}
	if !IsRecoverable(err) {
		t.Errorf("err must be recoverable for 409: %v", err)
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("err must hint at --force: %v", err)
	}
}

func TestRunDay_503MapsToRecoverableWithDaemonHint(t *testing.T) {
	herr := &client.HTTPError{Method: "POST", Path: "/v1/zen-day/morning", Status: http.StatusServiceUnavailable, RawBody: []byte("zen day generator not configured")}
	f := &fakeZenDayClient{morningErr: fmt.Errorf("POST /v1/zen-day/morning: 503: %w", herr)}
	var buf bytes.Buffer

	err := runDay(t.Context(), f, &buf, false, false, false)
	if err == nil {
		t.Fatalf("runDay err = nil, want recoverable")
	}
	if !IsRecoverable(err) {
		t.Errorf("err must be recoverable for 503: %v", err)
	}
	if !strings.Contains(err.Error(), "zen daemon start") {
		t.Errorf("err must hint at `zen daemon start`: %v", err)
	}
}

func TestRunDay_500IsBareError(t *testing.T) {
	herr := &client.HTTPError{Method: "POST", Path: "/v1/zen-day/morning", Status: http.StatusInternalServerError, RawBody: []byte("disk full")}
	f := &fakeZenDayClient{morningErr: fmt.Errorf("POST /v1/zen-day/morning: 500: %w", herr)}
	var buf bytes.Buffer

	err := runDay(t.Context(), f, &buf, false, false, false)
	if err == nil {
		t.Fatalf("runDay err = nil, want bare error")
	}
	if IsRecoverable(err) {
		t.Errorf("500 must not be recoverable: %v", err)
	}
}

func TestRunDay_TransportErrorIsBare(t *testing.T) {
	f := &fakeZenDayClient{morningErr: errors.New("dial unix: no such file")}
	var buf bytes.Buffer

	err := runDay(t.Context(), f, &buf, false, false, false)
	if err == nil {
		t.Fatalf("runDay err = nil, want bare error")
	}
	if IsRecoverable(err) {
		t.Errorf("dial error must not be recoverable: %v", err)
	}
}

func TestClassifyDayError_NilPassthrough(t *testing.T) {
	if got := classifyDayError(nil, "morning"); got != nil {
		t.Errorf("classifyDayError(nil) = %v, want nil", got)
	}
}

func TestDayPathFor(t *testing.T) {
	cases := []struct {
		eod, checkPending bool
		want              string
	}{
		{false, false, "morning"},
		{true, false, "eod"},
		{false, true, "check-pending"},
		{true, true, "check-pending"},
	}
	for _, c := range cases {
		if got := dayPathFor(c.eod, c.checkPending); got != c.want {
			t.Errorf("dayPathFor(eod=%v, check=%v) = %q, want %q", c.eod, c.checkPending, got, c.want)
		}
	}
}

func TestRunDay_EODConflictMessageReferencesEOD(t *testing.T) {
	herr := &client.HTTPError{Method: "POST", Path: "/v1/zen-day/eod", Status: http.StatusConflict, RawBody: []byte("today's eod already generated")}
	f := &fakeZenDayClient{eodErr: fmt.Errorf("POST /v1/zen-day/eod: 409: %w", herr)}
	var buf bytes.Buffer

	err := runDay(t.Context(), f, &buf, false, true, false)
	if err == nil {
		t.Fatalf("err = nil, want recoverable")
	}
	if !strings.Contains(err.Error(), "eod") {
		t.Errorf("err message must reference 'eod': %v", err)
	}
}

type fakeDayDaemon struct {
	srv          *httptest.Server
	statusByPath map[string]int
	bodyByPath   map[string]zenday.BriefDoc
	mu           sync.Mutex
	gotForce     map[string]bool
}

func newFakeDayDaemon(t *testing.T) *fakeDayDaemon {
	t.Helper()
	d := &fakeDayDaemon{
		statusByPath: make(map[string]int),
		bodyByPath:   make(map[string]zenday.BriefDoc),
		gotForce:     make(map[string]bool),
	}
	mux := http.NewServeMux()
	handler := func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		var body struct {
			Force bool `json:"force,omitempty"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		d.mu.Lock()
		d.gotForce[path] = body.Force
		status := d.statusByPath[path]
		respDoc := d.bodyByPath[path]
		d.mu.Unlock()
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		if status >= 400 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(http.StatusText(status)))
			return
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(respDoc)
	}
	mux.HandleFunc("/v1/zen-day/morning", handler)
	mux.HandleFunc("/v1/zen-day/eod", handler)
	mux.HandleFunc("/v1/zen-day/check-pending", handler)
	d.srv = httptest.NewServer(mux)
	t.Cleanup(d.srv.Close)
	return d
}

// invokeDayCmd executes NewDayCmd against the fake daemon via
// TestOnlyClientFactory + the cobra ExecuteContext path. Restores the
// factory on cleanup so test bodies do not leak the override.
func invokeDayCmd(t *testing.T, fake *fakeDayDaemon, args ...string) (string, string, error) {
	t.Helper()
	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(_ string) *client.Client {
		return client.NewWithBaseURL(fake.srv.URL)
	}
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	root := NewRootCmd()
	root.SetArgs(append([]string{"day"}, args...))
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	err := root.ExecuteContext(t.Context())
	return stdout.String(), stderr.String(), err
}

func TestNewDayCmd_DefaultMorningIntegration(t *testing.T) {
	fake := newFakeDayDaemon(t)
	fake.bodyByPath["/v1/zen-day/morning"] = canonicalMorning()

	stdout, _, err := invokeDayCmd(t, fake)
	if err != nil {
		t.Fatalf("execute err = %v", err)
	}
	if !strings.Contains(stdout, "morning brief") {
		t.Errorf("stdout missing morning brief: %q", stdout)
	}
	if !strings.Contains(stdout, "internal-platform-x") {
		t.Errorf("stdout missing item project: %q", stdout)
	}
	if fake.gotForce["/v1/zen-day/morning"] {
		t.Errorf("force should be false by default; got true on wire")
	}
}

func TestNewDayCmd_ForceFlagReachesDaemon(t *testing.T) {
	fake := newFakeDayDaemon(t)
	fake.bodyByPath["/v1/zen-day/morning"] = canonicalMorning()

	if _, _, err := invokeDayCmd(t, fake, "--force"); err != nil {
		t.Fatalf("execute err = %v", err)
	}
	if !fake.gotForce["/v1/zen-day/morning"] {
		t.Errorf("--force did not propagate to daemon")
	}
}

func TestNewDayCmd_EODFlagSelectsEODRoute(t *testing.T) {
	fake := newFakeDayDaemon(t)
	fake.bodyByPath["/v1/zen-day/eod"] = canonicalEOD()

	stdout, _, err := invokeDayCmd(t, fake, "--eod")
	if err != nil {
		t.Fatalf("execute err = %v", err)
	}
	if !strings.Contains(stdout, "EOD digest") {
		t.Errorf("stdout missing EOD heading: %q", stdout)
	}
}

func TestNewDayCmd_CheckPendingFlagSelectsCheckPendingRoute(t *testing.T) {
	fake := newFakeDayDaemon(t)
	fake.bodyByPath["/v1/zen-day/check-pending"] = canonicalCheckPending()

	stdout, _, err := invokeDayCmd(t, fake, "--check-pending")
	if err != nil {
		t.Fatalf("execute err = %v", err)
	}
	if !strings.Contains(stdout, "Next morning brief") {
		t.Errorf("stdout missing check-pending heading: %q", stdout)
	}
	if !strings.Contains(stdout, "2 action-needed") {
		t.Errorf("stdout missing pending counts: %q", stdout)
	}
}

func TestNewDayCmd_409SurfacesRecoverableForceHint(t *testing.T) {
	fake := newFakeDayDaemon(t)
	fake.statusByPath["/v1/zen-day/morning"] = http.StatusConflict

	_, _, err := invokeDayCmd(t, fake)
	if err == nil {
		t.Fatalf("err = nil, want recoverable")
	}
	if !IsRecoverable(err) {
		t.Errorf("err not recoverable: %v", err)
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("err missing --force hint: %v", err)
	}
}

func TestNewDayCmd_503SurfacesRecoverableDaemonHint(t *testing.T) {
	fake := newFakeDayDaemon(t)
	fake.statusByPath["/v1/zen-day/morning"] = http.StatusServiceUnavailable

	_, _, err := invokeDayCmd(t, fake)
	if err == nil {
		t.Fatalf("err = nil, want recoverable")
	}
	if !IsRecoverable(err) {
		t.Errorf("err not recoverable: %v", err)
	}
	if !strings.Contains(err.Error(), "zen daemon start") {
		t.Errorf("err missing daemon start hint: %v", err)
	}
}

func TestNewDayCmd_IncludeBypassEmitsDeprecationWarning(t *testing.T) {
	fake := newFakeDayDaemon(t)
	fake.bodyByPath["/v1/zen-day/morning"] = canonicalMorning()

	_, stderr, err := invokeDayCmd(t, fake, "--include-bypass")
	if err != nil {
		t.Fatalf("execute err = %v", err)
	}
	if !strings.Contains(stderr, "legacy Plan 2") {
		t.Errorf("--include-bypass did not emit deprecation warning: %q", stderr)
	}
}

func TestNewDayCmd_FlagsRegistered(t *testing.T) {
	cmd := NewDayCmd()
	for _, name := range []string{"force", "eod", "check-pending", "include-bypass"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag --%s not registered", name)
		}
	}
}
