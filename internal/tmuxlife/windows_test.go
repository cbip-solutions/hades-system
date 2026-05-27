package tmuxlife

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// TestWindowNameValues asserts each constant string matches the spec
// (Q5 D + §6.3 operator UX). Stable identifiers — DO NOT rename without
// migrating the daemon.db tmux_session_state schema and operator-facing
// `zen attach --window <name>` flag value set.
func TestWindowNameValues(t *testing.T) {
	cases := []struct {
		got, want string
	}{
		{string(WindowOrch), "orch"},
		{string(WindowLeads), "leads"},
		{string(WindowWorkers), "workers"},
		{string(WindowHRA), "hra"},
		{string(WindowLogs), "logs"},
		{string(WindowScratch), "scratch"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
}

// TestDaemonOwnedExcludesScratch is load-bearing for invariant (snapshot
// privacy): scratch MUST be partitioned to operator-owned. If a future
// edit accidentally moved scratch into DaemonOwnedWindows, snapshot save
// would include scratch contents — privacy violation.
func TestDaemonOwnedExcludesScratch(t *testing.T) {
	for _, w := range DaemonOwnedWindows {
		if w == WindowScratch {
			t.Fatalf("DaemonOwnedWindows includes WindowScratch; inv-zen-118 violated")
		}
	}
}

func TestOperatorOwnedIsExactlyScratch(t *testing.T) {
	want := []WindowName{WindowScratch}
	if !reflect.DeepEqual(OperatorOwnedWindows, want) {
		t.Errorf("OperatorOwnedWindows = %v, want %v", OperatorOwnedWindows, want)
	}
}

func TestDaemonOwnedHasFive(t *testing.T) {
	if len(DaemonOwnedWindows) != 5 {
		t.Errorf("DaemonOwnedWindows len = %d, want 5", len(DaemonOwnedWindows))
	}
	expectedSet := map[WindowName]bool{
		WindowOrch: true, WindowLeads: true, WindowWorkers: true,
		WindowHRA: true, WindowLogs: true,
	}
	for _, w := range DaemonOwnedWindows {
		if !expectedSet[w] {
			t.Errorf("unexpected window %q in DaemonOwnedWindows", w)
		}
		delete(expectedSet, w)
	}
	if len(expectedSet) != 0 {
		t.Errorf("missing windows in DaemonOwnedWindows: %v", expectedSet)
	}
}

func TestAllWindowsHasSix(t *testing.T) {
	if len(AllWindows) != 6 {
		t.Errorf("AllWindows len = %d, want 6", len(AllWindows))
	}
}

func TestAllWindowsCanonicalOrder(t *testing.T) {
	want := []WindowName{
		WindowOrch, WindowLeads, WindowWorkers, WindowHRA, WindowLogs, WindowScratch,
	}
	if !reflect.DeepEqual(AllWindows, want) {
		t.Errorf("AllWindows = %v, want %v", AllWindows, want)
	}
}

func TestWindowNameValid(t *testing.T) {
	known := []string{"orch", "leads", "workers", "hra", "logs", "scratch"}
	for _, n := range known {
		if !IsValidWindowName(WindowName(n)) {
			t.Errorf("IsValidWindowName(%q) = false, want true", n)
		}
	}
	unknown := []string{"", "ORCH", "main", "tmp", "scratchpad"}
	for _, n := range unknown {
		if IsValidWindowName(WindowName(n)) {
			t.Errorf("IsValidWindowName(%q) = true, want false", n)
		}
	}
}

func TestCreateWindowsExecOrder(t *testing.T) {
	st := newFakeSessionStore()
	exec := &fakeExecutor{}
	exec.responses = map[string]execResp{}

	expected := []string{
		"rename-window-zen-foo-deadbeef-0-orch",
		"new-window-zen-foo-deadbeef-leads",
		"new-window-zen-foo-deadbeef-workers",
		"new-window-zen-foo-deadbeef-hra",
		"new-window-zen-foo-deadbeef-logs",
		"new-window-zen-foo-deadbeef-scratch",
	}
	for _, op := range expected {
		exec.responses[op] = execResp{out: nil, err: nil}
	}
	m := New(st)
	m.exec = exec.Exec

	if err := m.CreateWindows(context.Background(), "zen-foo-deadbeef"); err != nil {
		t.Fatalf("CreateWindows: %v", err)
	}
	if !reflect.DeepEqual(exec.calls, expected) {
		t.Errorf("call order:\n got: %v\nwant: %v", exec.calls, expected)
	}
}

func TestCreateWindowsRenameFailureBubbles(t *testing.T) {
	st := newFakeSessionStore()
	exec := &fakeExecutor{}
	exec.responses = map[string]execResp{
		"rename-window-zen-bar-12345678-0-orch": {err: errors.New("tmux: no current window")},
	}
	m := New(st)
	m.exec = exec.Exec

	err := m.CreateWindows(context.Background(), "zen-bar-12345678")
	if err == nil {
		t.Fatalf("expected error; got nil")
	}
	if !strings.Contains(err.Error(), "rename-window") {
		t.Errorf("err = %v; missing rename-window context", err)
	}
}

func TestCreateWindowsNewWindowFailureBubbles(t *testing.T) {
	st := newFakeSessionStore()
	exec := &fakeExecutor{}
	exec.responses = map[string]execResp{
		"rename-window-zen-mid-cafebabe-0-orch": {},
		"new-window-zen-mid-cafebabe-leads":     {},
		"new-window-zen-mid-cafebabe-workers":   {err: errors.New("tmux: cannot create window")},
	}
	m := New(st)
	m.exec = exec.Exec

	err := m.CreateWindows(context.Background(), "zen-mid-cafebabe")
	if err == nil {
		t.Fatalf("expected error; got nil")
	}
	if !strings.Contains(err.Error(), "new-window") {
		t.Errorf("err = %v; missing new-window context", err)
	}
	if !strings.Contains(err.Error(), "workers") {
		t.Errorf("err = %v; missing failed-window name (workers)", err)
	}
	// Subsequent windows (hra/logs/scratch) MUST NOT have been attempted —
	// a failure must short-circuit so the operator does not get a partial
	// drift state masking the underlying failure.
	for _, banned := range []string{
		"new-window-zen-mid-cafebabe-hra",
		"new-window-zen-mid-cafebabe-logs",
		"new-window-zen-mid-cafebabe-scratch",
	} {
		for _, op := range exec.calls {
			if op == banned {
				t.Errorf("CreateWindows continued past failure: saw %q", banned)
			}
		}
	}
}

func TestCreateWindowsScratchLast(t *testing.T) {
	st := newFakeSessionStore()
	exec := &fakeExecutor{}
	exec.responses = map[string]execResp{}
	for _, op := range []string{
		"rename-window-zen-x-aaaaaaaa-0-orch",
		"new-window-zen-x-aaaaaaaa-leads",
		"new-window-zen-x-aaaaaaaa-workers",
		"new-window-zen-x-aaaaaaaa-hra",
		"new-window-zen-x-aaaaaaaa-logs",
		"new-window-zen-x-aaaaaaaa-scratch",
	} {
		exec.responses[op] = execResp{}
	}
	m := New(st)
	m.exec = exec.Exec
	if err := m.CreateWindows(context.Background(), "zen-x-aaaaaaaa"); err != nil {
		t.Fatalf("CreateWindows: %v", err)
	}
	last := exec.calls[len(exec.calls)-1]
	if !strings.HasSuffix(last, "-scratch") {
		t.Errorf("last call = %q; expected scratch window suffix", last)
	}
}

func TestCreateWindowsContextCanceled(t *testing.T) {
	st := newFakeSessionStore()
	exec := &fakeExecutor{honorCtx: true}
	m := New(st)
	m.exec = exec.Exec

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := m.CreateWindows(ctx, "zen-cancel-99887766")
	if err == nil {
		t.Fatalf("CreateWindows succeeded on canceled ctx; expected error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v; want errors.Is context.Canceled", err)
	}

	if len(exec.calls) != 0 {
		t.Errorf("calls = %v; want 0 (cancellation surfaced before any tmux invocation)", exec.calls)
	}
}
