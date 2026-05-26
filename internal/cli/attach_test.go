package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/client"
)

type fakeAttachClient struct {
	calls       []attachCall
	attachErr   error
	tmuxExecCmd string
}

type attachCall struct {
	alias  string
	window string
}

func (f *fakeAttachClient) AttachSession(_ context.Context, alias, window string) (string, error) {
	f.calls = append(f.calls, attachCall{alias, window})
	if f.attachErr != nil {
		return "", f.attachErr
	}
	return f.tmuxExecCmd, nil
}

func newAttachCmdForTest(c AttachClient) *cobra.Command {
	return NewAttachCmd(func(_ *cobra.Command) AttachClient { return c })
}

func resetSessionsClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(_ string) *client.Client { return client.NewWithBaseURL(srv.URL) }
	t.Cleanup(func() { TestOnlyClientFactory = prev })
}

func TestAttachWithDefaultWindow(t *testing.T) {
	c := &fakeAttachClient{tmuxExecCmd: "tmux -S /tmp/zen-swarm.sock attach -t zen-internal-platform-x-deadbeef:orch"}
	cmd := newAttachCmdForTest(c)
	cmd.SetArgs([]string{"internal-platform-x"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if len(c.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(c.calls))
	}
	if c.calls[0].alias != "internal-platform-x" || c.calls[0].window != "orch" {
		t.Errorf("call = %+v, want {internal-platform-x, orch}", c.calls[0])
	}
}

func TestAttachWithExplicitWindow(t *testing.T) {
	for _, w := range []string{"orch", "leads", "workers", "hra", "logs", "scratch"} {
		t.Run(w, func(t *testing.T) {
			c := &fakeAttachClient{tmuxExecCmd: "tmux -S /tmp/zen-swarm.sock attach -t zen-internal-platform-x-deadbeef:" + w}
			cmd := newAttachCmdForTest(c)
			cmd.SetArgs([]string{"internal-platform-x", "--window", w})
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute err: %v", err)
			}
			if len(c.calls) != 1 || c.calls[0].window != w {
				t.Errorf("calls=%+v, want one call with window=%q", c.calls, w)
			}
		})
	}
}

func TestAttachInvalidWindow(t *testing.T) {
	c := &fakeAttachClient{}
	cmd := newAttachCmdForTest(c)
	cmd.SetArgs([]string{"internal-platform-x", "--window", "bogus"})
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	cmd.SetOut(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected pre-flight error on bogus window; got nil")
	}
	if !IsRecoverable(err) {
		t.Errorf("invalid-window err not recoverable: %v", err)
	}
	if len(c.calls) != 0 {
		t.Errorf("daemon called despite pre-flight reject: %d calls", len(c.calls))
	}
}

func TestAttachMissingAlias(t *testing.T) {
	c := &fakeAttachClient{}
	cmd := newAttachCmdForTest(c)
	cmd.SetArgs([]string{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Error("expected usage error on missing alias")
	}
}

func TestAttachTooManyArgs(t *testing.T) {
	c := &fakeAttachClient{}
	cmd := newAttachCmdForTest(c)
	cmd.SetArgs([]string{"internal-platform-x", "extra"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Error("expected error on extra positional arg")
	}
}

func TestAttachClientError(t *testing.T) {
	c := &fakeAttachClient{attachErr: context.DeadlineExceeded}
	cmd := newAttachCmdForTest(c)
	cmd.SetArgs([]string{"internal-platform-x"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error on daemon timeout")
	}
	if IsRecoverable(err) {
		t.Errorf("transport timeout wrongly marked recoverable: %v", err)
	}
}

func TestAttachIsRecoverableSentinelPropagates(t *testing.T) {
	c := &fakeAttachClient{attachErr: errors.New("alias not found")}

	c.attachErr = recoverableWrap(c.attachErr, "alias not found")
	cmd := newAttachCmdForTest(c)
	cmd.SetArgs([]string{"internal-platform-x"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsRecoverable(err) {
		t.Errorf("recoverable sentinel lost across cobra: %v", err)
	}
}

func TestAttachEmptyTmuxCmdRejected(t *testing.T) {
	c := &fakeAttachClient{tmuxExecCmd: ""}
	cmd := newAttachCmdForTest(c)
	cmd.SetArgs([]string{"internal-platform-x"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error on empty tmux command from daemon")
	}
}

func TestAttachSingleFieldTmuxCmdRejected(t *testing.T) {
	c := &fakeAttachClient{tmuxExecCmd: "tmux"}
	cmd := newAttachCmdForTest(c)
	cmd.SetArgs([]string{"internal-platform-x"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error on single-token tmux command from daemon")
	}
}

// TestAttachTestModeEcho — when running under `go test`, the cobra RunE
// MUST NOT actually exec tmux (would replace the test process); instead
// it echoes the daemon-returned command to stdout. isTestMode() detects
// the .test binary suffix.
func TestAttachTestModeEcho(t *testing.T) {
	wantCmd := "tmux -S /tmp/zen-swarm.sock attach -t zen-internal-platform-x-deadbeef:orch"
	c := &fakeAttachClient{tmuxExecCmd: wantCmd}
	cmd := newAttachCmdForTest(c)
	cmd.SetArgs([]string{"internal-platform-x"})
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), wantCmd) {
		t.Errorf("test-mode echo missing %q; got %q", wantCmd, out.String())
	}
}

// TestAttachCmdSendsHTTPBody — the production attach RunE MUST reach
// POST /v1/sessions/{alias}/attach with the window field in the body.
func TestAttachCmdSendsHTTPBody(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tmux_cmd":"tmux -S /tmp/zen-swarm.sock attach -t zen-internal-platform-x-deadbeef:orch"}`))
	}))
	defer srv.Close()
	resetSessionsClient(t, srv)

	cmd := NewAttachCmdProd()
	cmd.SetArgs([]string{"internal-platform-x"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method=%q want POST", gotMethod)
	}
	if gotPath != "/v1/sessions/internal-platform-x/attach" {
		t.Errorf("path=%q want /v1/sessions/internal-platform-x/attach", gotPath)
	}
	if gotBody["window"] != "orch" {
		t.Errorf("body.window=%v want orch", gotBody["window"])
	}
}

func TestAttachCmdHTTPErrorMaps404Recoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("alias not found"))
	}))
	defer srv.Close()
	resetSessionsClient(t, srv)

	cmd := NewAttachCmdProd()
	cmd.SetArgs([]string{"missing"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected 404 error")
	}
	if !IsRecoverable(err) {
		t.Errorf("404 not recoverable: %v", err)
	}
}

func TestAttachCmdHTTPError503Unrecoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("phase I pending"))
	}))
	defer srv.Close()
	resetSessionsClient(t, srv)

	cmd := NewAttachCmdProd()
	cmd.SetArgs([]string{"internal-platform-x"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected 503 error")
	}
	if IsRecoverable(err) {
		t.Errorf("503 wrongly marked recoverable: %v", err)
	}
}

func TestAttachCmdHTTPError500Unrecoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()
	resetSessionsClient(t, srv)

	cmd := NewAttachCmdProd()
	cmd.SetArgs([]string{"internal-platform-x"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected 500 error")
	}
	if IsRecoverable(err) {
		t.Errorf("500 wrongly marked recoverable: %v", err)
	}
}

func TestAttachCmdRendersTmuxCmdInTestMode(t *testing.T) {
	want := "tmux -S /tmp/zen-swarm.sock attach -t zen-internal-platform-x-deadbeef:logs"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tmux_cmd":"` + want + `"}`))
	}))
	defer srv.Close()
	resetSessionsClient(t, srv)

	cmd := NewAttachCmdProd()
	cmd.SetArgs([]string{"internal-platform-x", "--window", "logs"})
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), want) {
		t.Errorf("output missing daemon-returned tmux cmd; got %q", out.String())
	}
}

// TestIsTestModeReturnsTrueUnderGoTest documents the runtime check
// used by cobra RunE to bypass syscall.Exec. The function MUST return
// true under any go test invocation.
func TestIsTestModeReturnsTrueUnderGoTest(t *testing.T) {
	if !isTestMode() {
		t.Error("isTestMode() = false during go test; expected true (binary path should match .test suffix)")
	}
}

func TestIsTestModeWithEmptyArgs0(t *testing.T) {
	prev := testModeArg0
	testModeArg0 = func() string { return "" }
	t.Cleanup(func() { testModeArg0 = prev })
	if isTestMode() {
		t.Error("isTestMode() with empty arg0 = true; expected false")
	}
}

func TestTestModeArg0OriginalClosureBranches(t *testing.T) {
	prev := testModeArg0
	savedArgs := os.Args
	t.Cleanup(func() {
		os.Args = savedArgs
		testModeArg0 = prev
	})

	if got := prev(); got == "" {
		t.Errorf("prev() with populated os.Args = %q; expected non-empty", got)
	}

	os.Args = []string{}
	if got := prev(); got != "" {
		t.Errorf("prev() with empty os.Args = %q; expected empty string", got)
	}
}

func TestIsTestModeWithProductionLikeBinaryName(t *testing.T) {
	prev := testModeArg0
	testModeArg0 = func() string { return "/usr/local/bin/zen" }
	t.Cleanup(func() { testModeArg0 = prev })
	if isTestMode() {
		t.Error("isTestMode() with prod binary path = true; expected false")
	}
}

// TestExecAttachNonTestModeInvocation — the cobra RunE production path
// (when isTestMode() is false) MUST hit execAttach with the daemon-
// rendered fields. We override testModeArg0 to flip the runtime out of
// test mode, then stub execAttach to capture the invocation without
// actually exec-ing tmux.
func TestExecAttachNonTestModeInvocation(t *testing.T) {

	prevArg0 := testModeArg0
	testModeArg0 = func() string { return "/usr/local/bin/zen" }
	t.Cleanup(func() { testModeArg0 = prevArg0 })

	var gotFields []string
	prevExec := execAttach
	execAttach = func(fields []string) error {
		gotFields = fields
		return nil
	}
	t.Cleanup(func() { execAttach = prevExec })

	want := "tmux -S /tmp/zen-swarm.sock attach -t zen-internal-platform-x-deadbeef:orch"
	c := &fakeAttachClient{tmuxExecCmd: want}
	cmd := newAttachCmdForTest(c)
	cmd.SetArgs([]string{"internal-platform-x"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(gotFields) < 2 || gotFields[0] != "tmux" {
		t.Errorf("execAttach fields = %v; want [tmux ...]", gotFields)
	}
	if gotFields[len(gotFields)-1] != "zen-internal-platform-x-deadbeef:orch" {
		t.Errorf("execAttach last field = %q; want target zen-internal-platform-x-deadbeef:orch",
			gotFields[len(gotFields)-1])
	}
}

func TestExecAttachPropagatesError(t *testing.T) {
	prevArg0 := testModeArg0
	testModeArg0 = func() string { return "/usr/local/bin/zen" }
	t.Cleanup(func() { testModeArg0 = prevArg0 })

	prevExec := execAttach
	execAttach = func(_ []string) error {
		return errors.New("tmux exited 1")
	}
	t.Cleanup(func() { execAttach = prevExec })

	c := &fakeAttachClient{tmuxExecCmd: "tmux -S /tmp/zen-swarm.sock attach -t zen-x-aabbccdd:orch"}
	cmd := newAttachCmdForTest(c)
	cmd.SetArgs([]string{"x"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error from execAttach failure")
	}
}

func TestExecAttachOriginalClosureRunsSubprocess(t *testing.T) {
	prev := execAttach
	t.Cleanup(func() { execAttach = prev })

	candidates := []string{"/usr/bin/true", "/bin/true"}
	var truePath string
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			truePath = p
			break
		}
	}
	if truePath == "" {
		t.Skip("no /usr/bin/true or /bin/true; skipping subprocess exec smoke")
	}

	if err := prev([]string{truePath}); err != nil {
		t.Errorf("prev([true]) = %v; want nil", err)
	}
}

var _ = time.Millisecond
