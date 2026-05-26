package cli

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

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/inbox"
)

type fakeQuietClient struct {
	cfg       inbox.QuietConfig
	pauseSet  *time.Time
	cancelled bool
	err       error
}

func (f *fakeQuietClient) Get(_ context.Context) (inbox.QuietConfig, error) {
	if f.err != nil {
		return inbox.QuietConfig{}, f.err
	}
	return f.cfg, nil
}

func (f *fakeQuietClient) SetUrgentPause(_ context.Context, until time.Time) error {
	if f.err != nil {
		return f.err
	}
	t := until
	f.pauseSet = &t
	return nil
}

func (f *fakeQuietClient) CancelUrgentPause(_ context.Context) error {
	if f.err != nil {
		return f.err
	}
	f.cancelled = true
	f.pauseSet = nil
	return nil
}

func TestQuietListBasic(t *testing.T) {
	c := &fakeQuietClient{cfg: inbox.QuietConfig{
		Default: inbox.QuietHours{
			Start:           21 * time.Hour,
			End:             9 * time.Hour,
			WeekendExtended: true,
			UrgentBypass:    true,
		},
	}}
	var buf bytes.Buffer
	if err := RunQuietList(context.Background(), c, &buf, time.Now()); err != nil {
		t.Fatalf("RunQuietList: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "21:00") || !strings.Contains(out, "09:00") {
		t.Errorf("output missing time window: %s", out)
	}
	if !strings.Contains(out, "Urgent severity bypass: enabled") {
		t.Errorf("output missing urgent bypass status: %s", out)
	}
	if !strings.Contains(out, "Override (active): none") {
		t.Errorf("output should report no active override: %s", out)
	}
	if !strings.Contains(out, "weekdays + extended weekends") {
		t.Errorf("output should report weekend-extended note: %s", out)
	}
}

func TestQuietListShowsActivePause(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	until := now.Add(30 * time.Minute)
	c := &fakeQuietClient{cfg: inbox.QuietConfig{
		Default:          inbox.QuietHours{Start: 21 * time.Hour, End: 9 * time.Hour, UrgentBypass: true},
		UrgentPauseUntil: &until,
	}}
	var buf bytes.Buffer
	if err := RunQuietList(context.Background(), c, &buf, now); err != nil {
		t.Fatalf("RunQuietList: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Urgent pause active") {
		t.Errorf("output should report active pause: %s", out)
	}
	if strings.Contains(out, "Override (active): none") {
		t.Errorf("output should not report 'no override' when paused: %s", out)
	}
}

func TestQuietListExpiredPauseRendersNoOverride(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	expired := now.Add(-30 * time.Minute)
	c := &fakeQuietClient{cfg: inbox.QuietConfig{
		Default:          inbox.QuietHours{Start: 21 * time.Hour, End: 9 * time.Hour, UrgentBypass: true},
		UrgentPauseUntil: &expired,
	}}
	var buf bytes.Buffer
	if err := RunQuietList(context.Background(), c, &buf, now); err != nil {
		t.Fatalf("RunQuietList: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Override (active): none") {
		t.Errorf("expired pause should render as no-override: %s", out)
	}
	if strings.Contains(out, "Urgent pause active") {
		t.Errorf("expired pause must not render as active: %s", out)
	}
}

func TestQuietListWeekendNotExtendedRendering(t *testing.T) {
	c := &fakeQuietClient{cfg: inbox.QuietConfig{
		Default: inbox.QuietHours{
			Start: 22 * time.Hour, End: 8 * time.Hour,
			WeekendExtended: false, UrgentBypass: true,
		},
	}}
	var buf bytes.Buffer
	if err := RunQuietList(context.Background(), c, &buf, time.Now()); err != nil {
		t.Fatalf("RunQuietList: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "extended weekends") {
		t.Errorf("output should NOT mention extended weekends: %s", out)
	}
	if !strings.Contains(out, "weekdays only") {
		t.Errorf("output should mention 'weekdays only' note: %s", out)
	}
}

func TestQuietListUrgentBypassDisabledRendering(t *testing.T) {
	c := &fakeQuietClient{cfg: inbox.QuietConfig{
		Default: inbox.QuietHours{
			Start: 21 * time.Hour, End: 9 * time.Hour, UrgentBypass: false,
		},
	}}
	var buf bytes.Buffer
	if err := RunQuietList(context.Background(), c, &buf, time.Now()); err != nil {
		t.Fatalf("RunQuietList: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "disabled") {
		t.Errorf("output should report urgent-bypass disabled: %s", out)
	}
}

func TestQuietUrgentPauseSets(t *testing.T) {
	c := &fakeQuietClient{}
	var buf bytes.Buffer
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	if err := RunQuietPause(context.Background(), c, "30m", now, &buf); err != nil {
		t.Fatalf("RunQuietPause: %v", err)
	}
	if c.pauseSet == nil {
		t.Fatal("pause was not set")
	}
	want := now.Add(30 * time.Minute)
	if !c.pauseSet.Equal(want) {
		t.Errorf("pause until = %v, want %v", c.pauseSet, want)
	}
	if !strings.Contains(buf.String(), "Urgent bypass paused") {
		t.Errorf("output missing pause confirmation: %s", buf.String())
	}
}

func TestQuietPauseRejectsInvalidDuration(t *testing.T) {
	c := &fakeQuietClient{}
	var buf bytes.Buffer
	err := RunQuietPause(context.Background(), c, "not-a-duration", time.Now(), &buf)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !IsRecoverable(err) {
		t.Errorf("invalid --urgent-pause duration should be recoverable: %v", err)
	}
}

func TestQuietPauseRejectsZeroDuration(t *testing.T) {
	c := &fakeQuietClient{}
	var buf bytes.Buffer
	err := RunQuietPause(context.Background(), c, "0s", time.Now(), &buf)
	if err == nil {
		t.Fatal("expected error on zero duration")
	}
	if !IsRecoverable(err) {
		t.Errorf("zero duration should be recoverable: %v", err)
	}
}

func TestQuietPauseRejectsNegativeDuration(t *testing.T) {
	c := &fakeQuietClient{}
	var buf bytes.Buffer
	err := RunQuietPause(context.Background(), c, "-30m", time.Now(), &buf)
	if err == nil {
		t.Fatal("expected error on negative duration")
	}
	if !IsRecoverable(err) {
		t.Errorf("negative duration should be recoverable: %v", err)
	}
}

func TestQuietPauseBackendError(t *testing.T) {
	c := &fakeQuietClient{err: errors.New("backend down")}
	var buf bytes.Buffer
	err := RunQuietPause(context.Background(), c, "30m", time.Now(), &buf)
	if err == nil {
		t.Fatal("expected backend error to propagate")
	}
}

func TestQuietCancel(t *testing.T) {
	c := &fakeQuietClient{}
	var buf bytes.Buffer
	if err := RunQuietCancel(context.Background(), c, &buf); err != nil {
		t.Fatalf("RunQuietCancel: %v", err)
	}
	if !c.cancelled {
		t.Error("Cancel was not invoked")
	}
	if !strings.Contains(buf.String(), "cancelled") {
		t.Errorf("output missing cancel confirmation: %s", buf.String())
	}
}

func TestQuietCancelBackendError(t *testing.T) {
	c := &fakeQuietClient{err: errors.New("backend down")}
	var buf bytes.Buffer
	err := RunQuietCancel(context.Background(), c, &buf)
	if err == nil {
		t.Fatal("expected backend error to propagate")
	}
}

func TestQuietClientErrorPropagates(t *testing.T) {
	c := &fakeQuietClient{err: errors.New("backend down")}
	var buf bytes.Buffer
	if err := RunQuietList(context.Background(), c, &buf, time.Now()); err == nil {
		t.Fatal("expected backend error")
	}
}

func TestFormatHHMM(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "00:00"},
		{9 * time.Hour, "09:00"},
		{21 * time.Hour, "21:00"},
		{22*time.Hour + 30*time.Minute, "22:30"},
		{23*time.Hour + 59*time.Minute, "23:59"},
	}
	for _, c := range cases {
		if got := formatHHMM(c.d); got != c.want {
			t.Errorf("formatHHMM(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func newQuietCmdForTest(c QuietClient) *cobra.Command {
	return NewQuietCmd(func(_ *cobra.Command) QuietClient { return c })
}

func TestQuietCmdNoFlagsDefaultsToList(t *testing.T) {
	c := &fakeQuietClient{cfg: inbox.QuietConfig{
		Default: inbox.QuietHours{Start: 21 * time.Hour, End: 9 * time.Hour, UrgentBypass: true},
	}}
	cmd := newQuietCmdForTest(c)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "Quiet hours") {
		t.Errorf("default action should list: %s", buf.String())
	}
}

func TestQuietCmdListFlag(t *testing.T) {
	c := &fakeQuietClient{cfg: inbox.QuietConfig{
		Default: inbox.QuietHours{Start: 21 * time.Hour, End: 9 * time.Hour, UrgentBypass: true},
	}}
	cmd := newQuietCmdForTest(c)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "Quiet hours") {
		t.Errorf("--list should render quiet config: %s", buf.String())
	}
}

func TestQuietCmdUrgentPauseFlag(t *testing.T) {
	c := &fakeQuietClient{}
	cmd := newQuietCmdForTest(c)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--urgent-pause", "30m"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if c.pauseSet == nil {
		t.Error("--urgent-pause did not invoke SetUrgentPause")
	}
}

func TestQuietCmdCancelFlag(t *testing.T) {
	c := &fakeQuietClient{}
	cmd := newQuietCmdForTest(c)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--cancel"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !c.cancelled {
		t.Error("--cancel did not invoke CancelUrgentPause")
	}
}

func TestQuietCmdMutuallyExclusiveFlags(t *testing.T) {
	c := &fakeQuietClient{}
	cmd := newQuietCmdForTest(c)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--list", "--cancel"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error on conflicting flags")
	}
	if !IsRecoverable(err) {
		t.Errorf("conflicting flags should be recoverable: %v", err)
	}
}

func TestQuietCmdMutuallyExclusiveListAndPause(t *testing.T) {
	c := &fakeQuietClient{}
	cmd := newQuietCmdForTest(c)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--list", "--urgent-pause", "30m"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error on conflicting flags")
	}
	if !IsRecoverable(err) {
		t.Errorf("conflicting flags should be recoverable: %v", err)
	}
}

func TestQuietCmdMutuallyExclusivePauseAndCancel(t *testing.T) {
	c := &fakeQuietClient{}
	cmd := newQuietCmdForTest(c)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--urgent-pause", "30m", "--cancel"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error on conflicting flags")
	}
	if !IsRecoverable(err) {
		t.Errorf("conflicting flags should be recoverable: %v", err)
	}
}

func resetQuietClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(_ string) *client.Client { return client.NewWithBaseURL(srv.URL) }
	t.Cleanup(func() { TestOnlyClientFactory = prev })
}

func TestQuietCmdHTTPListHappy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(client.QuietGetResponse{
			Default: client.QuietHoursWire{
				StartSec:        int64((21 * time.Hour).Seconds()),
				EndSec:          int64((9 * time.Hour).Seconds()),
				WeekendExtended: true,
				UrgentBypass:    true,
			},
			PerProject: map[string]client.QuietHoursWire{},
		})
	}))
	defer srv.Close()
	resetQuietClient(t, srv)

	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"quiet"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "21:00") {
		t.Errorf("missing 21:00 in output: %s", buf.String())
	}
}

func TestQuietCmdHTTPUrgentPauseHappy(t *testing.T) {
	var capturedReq client.QuietPauseRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedReq)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "until": capturedReq.Until})
	}))
	defer srv.Close()
	resetQuietClient(t, srv)

	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"quiet", "--urgent-pause", "30m"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if capturedReq.Until.IsZero() {
		t.Error("Until was not propagated to daemon")
	}
}

func TestQuietCmdHTTPCancelHappy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/quiet/cancel" {
			t.Errorf("path = %s, want /v1/quiet/cancel", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()
	resetQuietClient(t, srv)

	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"quiet", "--cancel"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "cancelled") {
		t.Errorf("missing cancellation message: %s", buf.String())
	}
}

func TestQuietCmdHTTPList503Unrecoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "quiet store not configured", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	resetQuietClient(t, srv)

	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"quiet"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected 503 to surface as error")
	}
	if IsRecoverable(err) {
		t.Errorf("503 should be unrecoverable: %v", err)
	}
}

func TestQuietCmdHTTPListActivePauseRendered(t *testing.T) {
	until := time.Now().UTC().Add(30 * time.Minute)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(client.QuietGetResponse{
			Default: client.QuietHoursWire{
				StartSec:        int64((21 * time.Hour).Seconds()),
				EndSec:          int64((9 * time.Hour).Seconds()),
				WeekendExtended: true,
				UrgentBypass:    true,
			},
			PerProject:       map[string]client.QuietHoursWire{},
			UrgentPauseUntil: &until,
		})
	}))
	defer srv.Close()
	resetQuietClient(t, srv)

	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"quiet"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "Urgent pause active") {
		t.Errorf("missing active pause line: %s", buf.String())
	}
}

func TestQuietCmdHTTPListWithPerProjectOverrides(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(client.QuietGetResponse{
			Default: client.QuietHoursWire{
				StartSec:        int64((21 * time.Hour).Seconds()),
				EndSec:          int64((9 * time.Hour).Seconds()),
				WeekendExtended: true,
				UrgentBypass:    true,
			},
			PerProject: map[string]client.QuietHoursWire{
				"project-a": {
					StartSec:        int64((22 * time.Hour).Seconds()),
					EndSec:          int64((6 * time.Hour).Seconds()),
					WeekendExtended: false,
					UrgentBypass:    true,
				},
			},
		})
	}))
	defer srv.Close()
	resetQuietClient(t, srv)

	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"quiet"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(buf.String(), "21:00") {
		t.Errorf("missing 21:00 in output: %s", buf.String())
	}
}

func TestQuietCmdHTTPPause422Recoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "until must be in the future", http.StatusUnprocessableEntity)
	}))
	defer srv.Close()
	resetQuietClient(t, srv)

	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"quiet", "--urgent-pause", "30m"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected 422 to surface as error")
	}
	if !IsRecoverable(err) {
		t.Errorf("422 should be recoverable: %v", err)
	}
}

func TestClassifyQuietErrorBranches(t *testing.T) {
	if got := classifyQuietError(nil, "x"); got != nil {
		t.Errorf("nil err: got %v, want nil", got)
	}
	rec := recoverable("operator typo")
	if got := classifyQuietError(rec, "x"); !errors.Is(got, ErrRecoverable) {
		t.Errorf("pre-recoverable not pass-through: %v", got)
	}
	bare := errors.New("transport reset")
	got := classifyQuietError(bare, "list")
	if got == nil {
		t.Fatal("opaque err: nil result")
	}
	if IsRecoverable(got) {
		t.Errorf("opaque err wrongly marked recoverable: %v", got)
	}
}
