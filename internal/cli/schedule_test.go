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
)

type fakeScheduleClient struct {
	createResp     *client.CreateRoutineResponse
	createTaskResp *client.CreateTaskResponse
	createLoopResp *client.CreateLoopResponse
	listRows       []client.RoutineRow
	runResp        *client.RunRoutineResponse
	historyRows    []client.HistoryRow
	queueRows      []client.QueueRow

	createErr     error
	createTaskErr error
	createLoopErr error
	listErr       error
	deleteErr     error
	runErr        error
	historyErr    error
	queueErr      error

	lastCreate     client.CreateRoutineRequest
	lastCreateTask client.CreateTaskRequest
	lastCreateLoop client.CreateLoopRequest
	lastListAlias  string
	lastDeleteID   string
	lastRunID      string
	lastHistoryID  string
	lastHistFrom   time.Time
	lastHistTo     time.Time
}

func (f *fakeScheduleClient) ScheduleCreate(_ context.Context, req client.CreateRoutineRequest) (*client.CreateRoutineResponse, error) {
	f.lastCreate = req
	if f.createErr != nil {
		return nil, f.createErr
	}
	if f.createResp == nil {
		return &client.CreateRoutineResponse{ID: "fake-id", Tier: "routine", NextRunAt: time.Date(2026, 5, 8, 8, 0, 0, 0, time.UTC)}, nil
	}
	return f.createResp, nil
}

func (f *fakeScheduleClient) ScheduleCreateTask(_ context.Context, req client.CreateTaskRequest) (*client.CreateTaskResponse, error) {
	f.lastCreateTask = req
	if f.createTaskErr != nil {
		return nil, f.createTaskErr
	}
	if f.createTaskResp == nil {
		return &client.CreateTaskResponse{ID: "fake-task", Tier: "task", NextRunAt: time.Date(2026, 5, 7, 13, 30, 0, 0, time.UTC)}, nil
	}
	return f.createTaskResp, nil
}

func (f *fakeScheduleClient) ScheduleCreateLoop(_ context.Context, req client.CreateLoopRequest) (*client.CreateLoopResponse, error) {
	f.lastCreateLoop = req
	if f.createLoopErr != nil {
		return nil, f.createLoopErr
	}
	if f.createLoopResp == nil {
		return &client.CreateLoopResponse{ID: "fake-loop", Tier: "loop", SessionID: "zen-internal-platform-x-deadbeef"}, nil
	}
	return f.createLoopResp, nil
}

func (f *fakeScheduleClient) ScheduleList(_ context.Context, alias string) ([]client.RoutineRow, error) {
	f.lastListAlias = alias
	return f.listRows, f.listErr
}

func (f *fakeScheduleClient) ScheduleDelete(_ context.Context, id string) error {
	f.lastDeleteID = id
	return f.deleteErr
}

func (f *fakeScheduleClient) ScheduleRun(_ context.Context, id string) (*client.RunRoutineResponse, error) {
	f.lastRunID = id
	if f.runErr != nil {
		return nil, f.runErr
	}
	if f.runResp == nil {
		return &client.RunRoutineResponse{Outcome: "success", DurationMs: 100, CostUSD: 0.001}, nil
	}
	return f.runResp, nil
}

func (f *fakeScheduleClient) ScheduleHistory(_ context.Context, id string, from, to time.Time) ([]client.HistoryRow, error) {
	f.lastHistoryID = id
	f.lastHistFrom = from
	f.lastHistTo = to
	return f.historyRows, f.historyErr
}

func (f *fakeScheduleClient) ScheduleQueue(_ context.Context) ([]client.QueueRow, error) {
	return f.queueRows, f.queueErr
}

func newScheduleCmdForTest(c ScheduleClient) *cobra.Command {
	return NewScheduleCmd(func(_ *cobra.Command) ScheduleClient { return c })
}

func resetScheduleClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(_ string) *client.Client { return client.NewWithBaseURL(srv.URL) }
	t.Cleanup(func() { TestOnlyClientFactory = prev })
}

func TestScheduleCmdHasAllSubcommands(t *testing.T) {
	root := newScheduleCmdForTest(&fakeScheduleClient{})
	got := map[string]bool{}
	for _, sc := range root.Commands() {
		got[sc.Name()] = true
	}
	for _, want := range []string{"routine", "task", "loop", "history", "queue"} {
		if !got[want] {
			t.Errorf("subcommand %q missing", want)
		}
	}
}

func TestScheduleRoutineSubcmdHasCreateListDeleteRun(t *testing.T) {
	root := newScheduleCmdForTest(&fakeScheduleClient{})
	var routine *cobra.Command
	for _, sc := range root.Commands() {
		if sc.Name() == "routine" {
			routine = sc
			break
		}
	}
	if routine == nil {
		t.Fatal("routine subcommand missing")
	}
	got := map[string]bool{}
	for _, sc := range routine.Commands() {
		got[sc.Name()] = true
	}
	for _, want := range []string{"create", "list", "delete", "run"} {
		if !got[want] {
			t.Errorf("routine.%s missing", want)
		}
	}
}

func TestScheduleRoutineCreate_PrintsBearerOnce(t *testing.T) {
	c := &fakeScheduleClient{
		createResp: &client.CreateRoutineResponse{
			ID:             "abc-123",
			Tier:           "routine",
			NextRunAt:      time.Date(2026, 5, 8, 8, 0, 0, 0, time.UTC),
			RawBearerToken: "tok-secret-43",
		},
	}
	root := newScheduleCmdForTest(c)
	root.SetArgs([]string{
		"routine", "create",
		"--project", "internal-platform-x",
		"--action", "morning-brief",
		"--trigger", "http",
	})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&bytes.Buffer{})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Bearer token (save NOW; not shown again):") {
		t.Errorf("missing bearer banner; out=%q", out)
	}
	if !strings.Contains(out, "tok-secret-43") {
		t.Errorf("token not surfaced; out=%q", out)
	}
	if !strings.Contains(out, "abc-123") {
		t.Errorf("routine id not surfaced; out=%q", out)
	}
	if c.lastCreate.Trigger != "http" || c.lastCreate.ProjectAlias != "internal-platform-x" {
		t.Errorf("last create req=%+v", c.lastCreate)
	}
}

func TestScheduleRoutineCreate_CronNoBearer(t *testing.T) {
	c := &fakeScheduleClient{
		createResp: &client.CreateRoutineResponse{
			ID: "x", Tier: "routine", NextRunAt: time.Date(2026, 5, 8, 8, 0, 0, 0, time.UTC),
		},
	}
	root := newScheduleCmdForTest(c)
	root.SetArgs([]string{
		"routine", "create",
		"--project", "internal-platform-x",
		"--action", "morning-brief",
		"--trigger", "cron",
		"--cron", "0 8 * * 1-5",
	})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&bytes.Buffer{})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(buf.String(), "Bearer token") {
		t.Errorf("cron trigger should NOT print bearer banner: %s", buf.String())
	}
	if c.lastCreate.CronExpr != "0 8 * * 1-5" {
		t.Errorf("CronExpr not propagated: %+v", c.lastCreate)
	}
}

func TestScheduleRoutineCreate_MissingProjectRecoverable(t *testing.T) {
	c := &fakeScheduleClient{}
	root := newScheduleCmdForTest(c)
	root.SetArgs([]string{
		"routine", "create",
		"--action", "morning-brief",
		"--trigger", "http",
	})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error on missing --project")
	}
	if !IsRecoverable(err) {
		t.Errorf("missing --project should be recoverable: %v", err)
	}
}

func TestScheduleRoutineCreate_MissingActionRecoverable(t *testing.T) {
	c := &fakeScheduleClient{}
	root := newScheduleCmdForTest(c)
	root.SetArgs([]string{
		"routine", "create",
		"--project", "internal-platform-x",
		"--trigger", "http",
	})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error on missing --action")
	}
	if !IsRecoverable(err) {
		t.Errorf("missing --action should be recoverable: %v", err)
	}
}

func TestScheduleRoutineCreate_CronMissingExprRecoverable(t *testing.T) {
	c := &fakeScheduleClient{}
	root := newScheduleCmdForTest(c)
	root.SetArgs([]string{
		"routine", "create",
		"--project", "internal-platform-x",
		"--action", "x",
		"--trigger", "cron",
	})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error on missing --cron when trigger=cron")
	}
	if !IsRecoverable(err) {
		t.Errorf("missing --cron should be recoverable: %v", err)
	}
}

func TestScheduleRoutineCreate_GitPollMissingRepoRecoverable(t *testing.T) {
	c := &fakeScheduleClient{}
	root := newScheduleCmdForTest(c)
	root.SetArgs([]string{
		"routine", "create",
		"--project", "internal-platform-x",
		"--action", "x",
		"--trigger", "git-poll",
	})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error on missing --repo when trigger=git-poll")
	}
	if !IsRecoverable(err) {
		t.Errorf("missing --repo should be recoverable: %v", err)
	}
}

func TestScheduleRoutineList_RendersAllColumns(t *testing.T) {
	c := &fakeScheduleClient{
		listRows: []client.RoutineRow{
			{ID: "abcdefgh-12345", ProjectAlias: "internal-platform-x", Action: "morning-brief", Tier: "routine", Status: "enabled", NextRunAt: time.Date(2026, 5, 8, 8, 0, 0, 0, time.UTC)},
			{ID: "ijklmnop-67890", ProjectAlias: "nexus", Action: "weekly-review", Tier: "routine", Status: "enabled"},
		},
	}
	root := newScheduleCmdForTest(c)
	root.SetArgs([]string{"routine", "list", "--all"})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&bytes.Buffer{})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"ID-SHORT", "PROJECT", "ACTION", "TIER", "STATUS", "NEXT", "internal-platform-x", "nexus", "morning-brief", "weekly-review", "abcdefgh", "ijklmnop"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
	if c.lastListAlias != "" {
		t.Errorf("--all should pass empty alias; got %q", c.lastListAlias)
	}
}

func TestScheduleRoutineList_ByProject(t *testing.T) {
	c := &fakeScheduleClient{listRows: []client.RoutineRow{}}
	root := newScheduleCmdForTest(c)
	root.SetArgs([]string{"routine", "list", "--project", "internal-platform-x"})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&bytes.Buffer{})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if c.lastListAlias != "internal-platform-x" {
		t.Errorf("alias filter not propagated; got %q", c.lastListAlias)
	}
}

func TestScheduleRoutineList_EmptyState(t *testing.T) {
	c := &fakeScheduleClient{listRows: nil}
	root := newScheduleCmdForTest(c)
	root.SetArgs([]string{"routine", "list", "--all"})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&bytes.Buffer{})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "no schedules") {
		t.Errorf("expected empty-state message; got %q", buf.String())
	}
}

func TestScheduleRoutineList_NoFilterRecoverable(t *testing.T) {
	c := &fakeScheduleClient{}
	root := newScheduleCmdForTest(c)
	root.SetArgs([]string{"routine", "list"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error: --project or --all required")
	}
	if !IsRecoverable(err) {
		t.Errorf("missing filter should be recoverable: %v", err)
	}
}

func TestScheduleRoutineDelete_OK(t *testing.T) {
	c := &fakeScheduleClient{}
	root := newScheduleCmdForTest(c)
	root.SetArgs([]string{"routine", "delete", "abc-123"})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&bytes.Buffer{})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if c.lastDeleteID != "abc-123" {
		t.Errorf("id not propagated: %q", c.lastDeleteID)
	}
	if !strings.Contains(buf.String(), "deleted") {
		t.Errorf("expected 'deleted'; got %q", buf.String())
	}
}

func TestScheduleRoutineDelete_404Recoverable(t *testing.T) {
	c := &fakeScheduleClient{deleteErr: &client.HTTPError{Status: http.StatusNotFound}}
	root := newScheduleCmdForTest(c)
	root.SetArgs([]string{"routine", "delete", "missing"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected 404 error")
	}
	if !IsRecoverable(err) {
		t.Errorf("404 should be recoverable: %v", err)
	}
}

func TestScheduleRoutineDelete_500Unrecoverable(t *testing.T) {
	c := &fakeScheduleClient{deleteErr: &client.HTTPError{Status: http.StatusInternalServerError}}
	root := newScheduleCmdForTest(c)
	root.SetArgs([]string{"routine", "delete", "abc"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected 500 error")
	}
	if IsRecoverable(err) {
		t.Errorf("500 should NOT be recoverable: %v", err)
	}
}

func TestScheduleRoutineRun_OKRendersOutcome(t *testing.T) {
	c := &fakeScheduleClient{
		runResp: &client.RunRoutineResponse{Outcome: "success", CostUSD: 0.0123, DurationMs: 4567},
	}
	root := newScheduleCmdForTest(c)
	root.SetArgs([]string{"routine", "run", "abc-123"})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&bytes.Buffer{})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"abc-123", "success", "0.0123", "4567"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}

func TestScheduleRoutineRun_503Unrecoverable(t *testing.T) {
	c := &fakeScheduleClient{runErr: &client.HTTPError{Status: http.StatusServiceUnavailable}}
	root := newScheduleCmdForTest(c)
	root.SetArgs([]string{"routine", "run", "abc"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected 503 error (Phase I gap)")
	}
	if IsRecoverable(err) {
		t.Errorf("503 should NOT be recoverable: %v", err)
	}
}

func TestScheduleTask_OKPropagatesIn(t *testing.T) {
	c := &fakeScheduleClient{}
	root := newScheduleCmdForTest(c)
	root.SetArgs([]string{
		"task",
		"--project", "internal-platform-x",
		"--in", "30m",
		"send", "report",
	})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&bytes.Buffer{})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if c.lastCreateTask.In != 30*time.Minute {
		t.Errorf("In = %v, want 30m", c.lastCreateTask.In)
	}
	if c.lastCreateTask.Action != "send report" {
		t.Errorf("Action = %q, want 'send report' (multi-word join)", c.lastCreateTask.Action)
	}
	if !strings.Contains(buf.String(), "Task fake-task scheduled") {
		t.Errorf("output missing task ID: %q", buf.String())
	}
}

func TestScheduleTask_MissingInRecoverable(t *testing.T) {
	c := &fakeScheduleClient{}
	root := newScheduleCmdForTest(c)
	root.SetArgs([]string{"task", "--project", "internal-platform-x", "send"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error: --in required")
	}
	if !IsRecoverable(err) {
		t.Errorf("missing --in should be recoverable: %v", err)
	}
}

func TestScheduleTask_MissingProjectRecoverable(t *testing.T) {
	c := &fakeScheduleClient{}
	root := newScheduleCmdForTest(c)
	root.SetArgs([]string{"task", "--in", "30m", "send"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error: --project required")
	}
	if !IsRecoverable(err) {
		t.Errorf("missing --project should be recoverable: %v", err)
	}
}

func TestScheduleLoop_OKPropagatesInterval(t *testing.T) {
	c := &fakeScheduleClient{}
	root := newScheduleCmdForTest(c)
	root.SetArgs([]string{
		"loop",
		"--project", "internal-platform-x",
		"--interval", "10m",
		"watch", "inbox",
	})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&bytes.Buffer{})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if c.lastCreateLoop.Interval != 10*time.Minute {
		t.Errorf("Interval = %v, want 10m", c.lastCreateLoop.Interval)
	}
	if c.lastCreateLoop.Action != "watch inbox" {
		t.Errorf("Action = %q, want 'watch inbox'", c.lastCreateLoop.Action)
	}
	if !strings.Contains(buf.String(), "Loop fake-loop") {
		t.Errorf("output missing loop ID: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "zen-internal-platform-x-deadbeef") {
		t.Errorf("output missing session id: %q", buf.String())
	}
}

func TestScheduleLoop_SubMinuteIntervalRecoverable(t *testing.T) {
	c := &fakeScheduleClient{}
	root := newScheduleCmdForTest(c)
	root.SetArgs([]string{
		"loop",
		"--project", "internal-platform-x",
		"--interval", "30s",
		"watch",
	})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error: --interval below 1min")
	}
	if !IsRecoverable(err) {
		t.Errorf("sub-1min interval should be recoverable: %v", err)
	}
}

func TestScheduleLoop_UnboundSessionRendersUnboundLabel(t *testing.T) {
	c := &fakeScheduleClient{
		createLoopResp: &client.CreateLoopResponse{ID: "x", Tier: "loop", SessionID: ""},
	}
	root := newScheduleCmdForTest(c)
	root.SetArgs([]string{
		"loop",
		"--project", "internal-platform-x",
		"--interval", "5m",
		"watch",
	})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&bytes.Buffer{})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "(unbound)") {
		t.Errorf("empty session_id should render as '(unbound)'; got %q", buf.String())
	}
}

func TestScheduleHistory_RendersRows(t *testing.T) {
	c := &fakeScheduleClient{
		historyRows: []client.HistoryRow{
			{ScheduleID: "abc", FiredAt: time.Date(2026, 5, 5, 8, 0, 0, 0, time.UTC), Outcome: 0, CostUSD: 0.012, DurationMs: 4567, Reason: ""},
			{ScheduleID: "abc", FiredAt: time.Date(2026, 5, 6, 8, 0, 0, 0, time.UTC), Outcome: 1, CostUSD: 0, DurationMs: 100, Reason: "dispatch failed"},
		},
	}
	root := newScheduleCmdForTest(c)
	root.SetArgs([]string{"history", "--id", "abc"})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&bytes.Buffer{})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"FIRED-AT", "OUTCOME", "COST", "success", "failed", "dispatch failed"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
}

func TestScheduleHistory_MissingIDRecoverable(t *testing.T) {
	c := &fakeScheduleClient{}
	root := newScheduleCmdForTest(c)
	root.SetArgs([]string{"history"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error: --id required")
	}
	if !IsRecoverable(err) {
		t.Errorf("missing --id should be recoverable: %v", err)
	}
}

func TestScheduleHistory_404Recoverable(t *testing.T) {
	c := &fakeScheduleClient{historyErr: &client.HTTPError{Status: http.StatusNotFound}}
	root := newScheduleCmdForTest(c)
	root.SetArgs([]string{"history", "--id", "missing"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected 404")
	}
	if !IsRecoverable(err) {
		t.Errorf("404 should be recoverable: %v", err)
	}
}

func TestScheduleHistory_EmptyRowsRendersMessage(t *testing.T) {
	c := &fakeScheduleClient{historyRows: nil}
	root := newScheduleCmdForTest(c)
	root.SetArgs([]string{"history", "--id", "abc"})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&bytes.Buffer{})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "no history rows") {
		t.Errorf("expected empty-state; got %q", buf.String())
	}
}

func TestScheduleHistory_PropagatesSinceWindow(t *testing.T) {
	c := &fakeScheduleClient{}
	root := newScheduleCmdForTest(c)
	root.SetArgs([]string{"history", "--id", "abc", "--since", "1h"})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&bytes.Buffer{})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	gap := c.lastHistTo.Sub(c.lastHistFrom)
	if gap < 50*time.Minute || gap > 70*time.Minute {
		t.Errorf("--since 1h should produce ~1h window; got %v", gap)
	}
}

func TestScheduleQueue_RendersRows(t *testing.T) {
	c := &fakeScheduleClient{
		queueRows: []client.QueueRow{
			{ID: "a", ProjectAlias: "internal-platform-x", Action: "morning-brief", NextRunAt: time.Now().UTC().Add(1 * time.Hour)},
			{ID: "b", ProjectAlias: "nexus", Action: "report", NextRunAt: time.Now().UTC().Add(2 * time.Hour)},
		},
	}
	root := newScheduleCmdForTest(c)
	root.SetArgs([]string{"queue"})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&bytes.Buffer{})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"NEXT-RUN-AT", "PROJECT", "ACTION", "IN", "internal-platform-x", "nexus"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}

func TestScheduleQueue_EmptyState(t *testing.T) {
	c := &fakeScheduleClient{queueRows: nil}
	root := newScheduleCmdForTest(c)
	root.SetArgs([]string{"queue"})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&bytes.Buffer{})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "no scheduled fires") {
		t.Errorf("expected empty-state; got %q", buf.String())
	}
}

func TestClassifyScheduleError_PropagatesRecoverable(t *testing.T) {
	in := recoverable("boom")
	out := classifyScheduleError(in)
	if !errors.Is(out, ErrRecoverable) {
		t.Errorf("recoverable in must propagate; got %v", out)
	}
}

func TestClassifyScheduleError_404MapsToRecoverable(t *testing.T) {
	in := &client.HTTPError{Status: http.StatusNotFound}
	out := classifyScheduleError(in)
	if !IsRecoverable(out) {
		t.Errorf("404 should map to recoverable: %v", out)
	}
}

func TestClassifyScheduleError_422MapsToRecoverable(t *testing.T) {
	in := &client.HTTPError{Status: http.StatusUnprocessableEntity}
	out := classifyScheduleError(in)
	if !IsRecoverable(out) {
		t.Errorf("422 should map to recoverable: %v", out)
	}
}

func TestClassifyScheduleError_500BareThrough(t *testing.T) {
	in := &client.HTTPError{Status: http.StatusInternalServerError}
	out := classifyScheduleError(in)
	if IsRecoverable(out) {
		t.Errorf("500 should NOT be recoverable: %v", out)
	}
	if out == nil {
		t.Error("err should propagate")
	}
}

func TestClassifyScheduleError_NilPropagates(t *testing.T) {
	if classifyScheduleError(nil) != nil {
		t.Error("nil in → nil out")
	}
}

func TestScheduleOutcomeStr(t *testing.T) {
	cases := map[int]string{
		0:  "success",
		1:  "failed",
		2:  "skipped",
		3:  "rate-limited",
		99: "outcome(99)",
	}
	for in, want := range cases {
		if got := scheduleOutcomeStr(in); got != want {
			t.Errorf("scheduleOutcomeStr(%d)=%q, want %q", in, got, want)
		}
	}
}

func TestTruncateScheduleReason(t *testing.T) {
	if truncateScheduleReason("short", 10) != "short" {
		t.Error("short should pass through")
	}
	if got := truncateScheduleReason("aaaaaaaaaaaaaaa", 10); got != "aaaaaaa..." {
		t.Errorf("got %q, want 'aaaaaaa...'", got)
	}

	if got := truncateScheduleReason("aaaa", 2); got != "aa" {
		t.Errorf("n<3 path: got %q, want 'aa'", got)
	}
}

func TestScheduleProdHTTPRoundTripList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"schedules": [
				{"id":"abc","project_alias":"internal-platform-x","action":"morning-brief","tier":"routine","status":"enabled","next_run_at":"2026-05-08T08:00:00Z"}
			]
		}`))
	}))
	defer srv.Close()
	resetScheduleClient(t, srv)

	cmd := NewScheduleCmdProd()
	cmd.SetArgs([]string{"routine", "list", "--all"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "internal-platform-x") {
		t.Errorf("output missing internal-platform-x: %q", buf.String())
	}
}

func TestScheduleProdHTTPRoundTripCreateRoutine(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"id":          "01HZ-1",
			"tier":        "routine",
			"next_run_at": time.Date(2026, 5, 8, 8, 0, 0, 0, time.UTC).Format(time.RFC3339),
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	resetScheduleClient(t, srv)

	cmd := NewScheduleCmdProd()
	cmd.SetArgs([]string{"routine", "create",
		"--project", "internal-platform-x", "--action", "morning",
		"--trigger", "cron", "--cron", "0 8 * * 1-5",
	})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/schedules" {
		t.Errorf("path = %q", gotPath)
	}
	if !strings.Contains(buf.String(), "01HZ-1") {
		t.Errorf("output missing id: %q", buf.String())
	}
}

func TestScheduleProdHTTPRoundTripDelete404Recoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "schedule not found", http.StatusNotFound)
	}))
	defer srv.Close()
	resetScheduleClient(t, srv)

	cmd := NewScheduleCmdProd()
	cmd.SetArgs([]string{"routine", "delete", "missing"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected 404")
	}
	if !IsRecoverable(err) {
		t.Errorf("404 should be recoverable: %v", err)
	}
}

func TestScheduleProdHTTPRoundTripCreateTask(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"task-01","tier":"task","next_run_at":"2026-05-07T13:30:00Z"}`))
	}))
	defer srv.Close()
	resetScheduleClient(t, srv)

	cmd := NewScheduleCmdProd()
	cmd.SetArgs([]string{
		"task", "--project", "internal-platform-x", "--in", "30m", "send", "report",
	})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "task-01") {
		t.Errorf("output missing task id: %q", buf.String())
	}
}

func TestScheduleProdHTTPRoundTripCreateLoop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"loop-01","tier":"loop","session_id":"zen-internal-platform-x-deadbeef"}`))
	}))
	defer srv.Close()
	resetScheduleClient(t, srv)

	cmd := NewScheduleCmdProd()
	cmd.SetArgs([]string{
		"loop", "--project", "internal-platform-x", "--interval", "5m", "watch",
	})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "loop-01") {
		t.Errorf("output missing loop id: %q", buf.String())
	}
}

func TestScheduleProdHTTPRoundTripRun503(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "phase I gap", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	resetScheduleClient(t, srv)

	cmd := NewScheduleCmdProd()
	cmd.SetArgs([]string{"routine", "run", "abc"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected 503")
	}
	if IsRecoverable(err) {
		t.Errorf("503 should NOT be recoverable: %v", err)
	}
}

func TestScheduleProdHTTPRoundTripHistory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"rows": [
				{"schedule_id":"abc","fired_at":"2026-05-05T08:00:00Z","outcome":0,"duration_ms":4000,"cost_usd":0.012}
			]
		}`))
	}))
	defer srv.Close()
	resetScheduleClient(t, srv)

	cmd := NewScheduleCmdProd()
	cmd.SetArgs([]string{"history", "--id", "abc", "--since", "1h"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "success") {
		t.Errorf("output missing success row: %q", buf.String())
	}
}

func TestScheduleProdHTTPRoundTripQueue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"rows": [
				{"id":"a","project_alias":"internal-platform-x","action":"morning-brief","next_run_at":"2026-05-08T08:00:00Z"}
			]
		}`))
	}))
	defer srv.Close()
	resetScheduleClient(t, srv)

	cmd := NewScheduleCmdProd()
	cmd.SetArgs([]string{"queue"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "internal-platform-x") {
		t.Errorf("queue output missing internal-platform-x: %q", buf.String())
	}
}

func TestRootHasScheduleCmd(t *testing.T) {
	root := NewRootCmd()
	c := findCobraChild(root.Commands(), "schedule")
	if c == nil {
		t.Fatal("`schedule` not registered on root (D-13)")
	}
	if findCobraChild(c.Commands(), "routine") == nil {
		t.Error("`schedule routine` subcommand missing")
	}
	if findCobraChild(c.Commands(), "task") == nil {
		t.Error("`schedule task` subcommand missing")
	}
	if findCobraChild(c.Commands(), "loop") == nil {
		t.Error("`schedule loop` subcommand missing")
	}
	if findCobraChild(c.Commands(), "history") == nil {
		t.Error("`schedule history` subcommand missing")
	}
	if findCobraChild(c.Commands(), "queue") == nil {
		t.Error("`schedule queue` subcommand missing")
	}
}
