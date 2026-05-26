package testharness

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestOpenClaudeFakeHappyPath(t *testing.T) {
	cmd := helperFakeCmd(t, "happy-path", "thread-1", t.TempDir())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()

	req := `{"jsonrpc":"2.0","id":1,"method":"prompt","params":{"text":"hello"}}` + "\n"
	if _, err := io.WriteString(stdin, req); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	r := bufio.NewReader(stdout)
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &resp); err != nil {
		t.Fatalf("unmarshal: %v: line=%q", err, line)
	}
	if resp["jsonrpc"] != "2.0" || resp["id"] == nil || resp["result"] == nil {
		t.Fatalf("unexpected response shape: %v", resp)
	}
}

func TestOpenClaudeFakeCrashScenario(t *testing.T) {
	cmd := helperFakeCmd(t, "crash", "thread-2", t.TempDir())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(stdin, `{"jsonrpc":"2.0","id":1,"method":"prompt","params":{"text":"x"}}`+"\n"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("crash scenario exited 0; want non-zero")
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("crash scenario hung")
	}
}

func TestOpenClaudeFakeHangScenario(t *testing.T) {
	cmd := helperFakeCmd(t, "hang", "thread-3", t.TempDir())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()

	r := bufio.NewReader(stdout)
	readyLine, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read ready line: %v", err)
	}
	if !strings.Contains(readyLine, `"ready"`) {
		t.Errorf("expected ready notification, got: %q", readyLine)
	}

	if _, err := io.WriteString(stdin, `{"jsonrpc":"2.0","id":1,"method":"prompt","params":{"text":"x"}}`+"\n"); err != nil {
		t.Fatal(err)
	}
	type readRes struct {
		line string
		err  error
	}
	ch := make(chan readRes, 1)
	go func() {
		l, e := r.ReadString('\n')
		ch <- readRes{l, e}
	}()
	select {
	case r := <-ch:
		t.Fatalf("hang scenario produced output: line=%q err=%v", r.line, r.err)
	case <-time.After(300 * time.Millisecond):

	}
}

func TestOpenClaudeFakeInteractiveMock(t *testing.T) {
	cmd := helperFakeCmd(t, "interactive-mock", "thread-4", t.TempDir())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()
	if _, err := io.WriteString(stdin, `{"jsonrpc":"2.0","id":1,"method":"prompt","params":{"text":"x"}}`+"\n"); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(stdout)
	var saw string
	for i := 0; i < 4; i++ {
		l, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		saw += l
	}
	if !strings.Contains(saw, `"method":"tool_use"`) {
		t.Fatalf("interactive-mock did not emit tool_use; saw: %s", saw)
	}
}

func TestOpenClaudeFakeUnknownScenarioExitsTwo(t *testing.T) {
	cmd := helperFakeCmd(t, "no-such-scenario", "thread-x", t.TempDir())
	if err := cmd.Run(); err == nil {
		t.Fatal("unknown scenario exited 0; want non-zero")
	}
}

func TestOpenClaudeFakeBadWorktreeExitsTwo(t *testing.T) {
	cmd := helperFakeCmd(t, "happy-path", "thread-y", "/nonexistent/path/zen-fake-test")
	if err := cmd.Run(); err == nil {
		t.Fatal("missing worktree exited 0; want non-zero")
	}
}

func TestOpenClaudeFakeHappyPathParseError(t *testing.T) {
	cmd := helperFakeCmd(t, "happy-path", "thread-bad", t.TempDir())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()

	if _, err := io.WriteString(stdin, "not-a-json\n"); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(stdout)
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if !strings.Contains(line, `"error"`) || !strings.Contains(line, `parse error`) {
		t.Fatalf("expected parse error frame, got: %s", line)
	}
}

func TestOpenClaudeFakeInteractiveParseError(t *testing.T) {
	cmd := helperFakeCmd(t, "interactive-mock", "thread-im-bad", t.TempDir())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()

	if _, err := io.WriteString(stdin, "not-a-json\n"); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(stdout)
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if !strings.Contains(line, `"error"`) {
		t.Fatalf("expected error frame on malformed input, got: %s", line)
	}
}

func TestFakeScenariosListed(t *testing.T) {
	if len(FakeScenarios) == 0 {
		t.Fatal("FakeScenarios empty")
	}
	want := map[string]bool{"happy-path": true, "hang": true, "crash": true, "interactive-mock": true}
	got := make(map[string]bool)
	for _, s := range FakeScenarios {
		got[s] = true
	}
	for k := range want {
		if !got[k] {
			t.Errorf("FakeScenarios missing %q", k)
		}
	}
	for k := range got {
		if !want[k] {
			t.Errorf("FakeScenarios has unexpected scenario %q", k)
		}
	}
}

func helperFakeCmd(t *testing.T, scenario, threadID, worktree string) *exec.Cmd {
	t.Helper()
	return BuildFakeCmd("TestHelperOpenClaudeFake", scenario, threadID, worktree)
}

func TestHelperOpenClaudeFake(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_OPENCLAUDE_FAKE") != "1" {
		t.Skip("not the helper invocation")
	}
	RunOpenClaudeFake()
}

func TestRunHappyPathDirect(t *testing.T) {
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"prompt","params":{"text":"hello"}}` + "\n")
	var out bytesBuffer
	runHappyPath(&in2{r: in}, &out, "tid-x")
	got := out.String()
	if !strings.Contains(got, `"result"`) {
		t.Errorf("missing result frame: %q", got)
	}
	if !strings.Contains(got, "tid-x") {
		t.Errorf("missing thread_id: %q", got)
	}
}

func TestRunHappyPathParseErrorDirect(t *testing.T) {
	in := strings.NewReader("not-a-json\n")
	var out bytesBuffer
	runHappyPath(&in2{r: in}, &out, "tid-x")
	got := out.String()
	if !strings.Contains(got, `parse error`) {
		t.Errorf("missing parse error frame: %q", got)
	}
}

func TestRunHappyPathReadErrDirect(t *testing.T) {
	r := &errReader{err: io.ErrUnexpectedEOF}
	var out bytesBuffer
	runHappyPath(r, &out, "tid-x")

	if out.Len() != 0 {
		t.Errorf("expected no output on read error, got: %q", out.String())
	}
}

func TestRunCrashDirect(t *testing.T) {
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"prompt"}` + "\n")
	var out bytesBuffer
	runCrash(in, &out, "tid")
	if out.Len() != 0 {
		t.Errorf("crash should not write output, got: %q", out.String())
	}
}

func TestRunInteractiveMockDirect(t *testing.T) {
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"prompt"}` + "\n")
	var out bytesBuffer
	runInteractiveMock(&in2{r: in}, &out, "tid-im")
	got := out.String()
	if strings.Count(got, `"method":"tool_use"`) != 4 {
		t.Errorf("expected 4 tool_use, got: %q", got)
	}
	if !strings.Contains(got, "interactive-mock done") {
		t.Errorf("missing final result: %q", got)
	}
}

func TestRunInteractiveMockParseErrorDirect(t *testing.T) {
	in := strings.NewReader("not-a-json\n")
	var out bytesBuffer
	runInteractiveMock(&in2{r: in}, &out, "tid-x")
	if !strings.Contains(out.String(), `"error"`) {
		t.Errorf("expected error frame on malformed JSON, got: %q", out.String())
	}
}

func TestRunInteractiveMockReadErrDirect(t *testing.T) {
	r := &errReader{err: io.ErrUnexpectedEOF}
	var out bytesBuffer
	runInteractiveMock(r, &out, "tid-x")
	if out.Len() != 0 {
		t.Errorf("expected no output on read error, got: %q", out.String())
	}
}

func TestRunHangDirect(t *testing.T) {
	prev := hangSleep
	hangSleep = 5 * time.Millisecond
	defer func() { hangSleep = prev }()

	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"prompt"}` + "\n")
	var out bytesBuffer
	done := make(chan struct{})
	go func() {
		runHang(in, &out)
		close(done)
	}()
	select {
	case <-done:

	case <-time.After(30 * time.Second):
		t.Fatal("runHang did not return with hangSleep override")
	}

	if !strings.Contains(out.String(), `"ready"`) {
		t.Errorf("missing ready frame: %q", out.String())
	}
}

func TestRunHangReadErrDirect(t *testing.T) {
	prev := hangSleep
	hangSleep = 5 * time.Millisecond
	defer func() { hangSleep = prev }()

	r := &errReader{err: io.ErrUnexpectedEOF}
	var out bytesBuffer
	done := make(chan struct{})
	go func() {
		runHang(r, &out)
		close(done)
	}()
	select {
	case <-done:

	case <-time.After(30 * time.Second):
		t.Fatal("runHang did not return on read error")
	}
}

func TestRunHangNilOut(t *testing.T) {
	prev := hangSleep
	hangSleep = 5 * time.Millisecond
	defer func() { hangSleep = prev }()
	r := strings.NewReader("")
	done := make(chan struct{})
	go func() {
		runHang(r, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("runHang did not return with nil out")
	}
}

type in2 struct{ r io.Reader }

func (i *in2) Read(p []byte) (int, error) { return i.r.Read(p) }

type bytesBuffer struct{ data []byte }

func (b *bytesBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}
func (b *bytesBuffer) String() string { return string(b.data) }
func (b *bytesBuffer) Len() int       { return len(b.data) }

type errReader struct{ err error }

func (e *errReader) Read(_ []byte) (int, error) { return 0, e.err }

type recordExit struct{ codes []int }

func (r *recordExit) call(code int) { r.codes = append(r.codes, code) }

func withRecordedExit(t *testing.T) *recordExit {
	t.Helper()
	prev := exitFunc
	rec := &recordExit{}
	exitFunc = rec.call
	t.Cleanup(func() { exitFunc = prev })
	return rec
}

func TestRunFakeWithIOHappyPath(t *testing.T) {
	rec := withRecordedExit(t)
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"prompt"}` + "\n")
	var out, errw bytesBuffer
	runFakeWithIO("happy-path", "tid-h", t.TempDir(), in, &out, &errw)
	if len(rec.codes) != 1 || rec.codes[0] != 0 {
		t.Errorf("exit codes = %v, want [0]", rec.codes)
	}
	if !strings.Contains(out.String(), `"result"`) {
		t.Errorf("missing result frame: %q", out.String())
	}
}

func TestRunFakeWithIOHang(t *testing.T) {
	prev := hangSleep
	hangSleep = 5 * time.Millisecond
	defer func() { hangSleep = prev }()
	rec := withRecordedExit(t)
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"prompt"}` + "\n")
	var out, errw bytesBuffer
	runFakeWithIO("hang", "tid-h", t.TempDir(), in, &out, &errw)
	if len(rec.codes) != 1 || rec.codes[0] != 0 {
		t.Errorf("exit codes = %v, want [0]", rec.codes)
	}
}

func TestRunFakeWithIOCrash(t *testing.T) {
	rec := withRecordedExit(t)
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"prompt"}` + "\n")
	var out, errw bytesBuffer
	runFakeWithIO("crash", "tid-c", t.TempDir(), in, &out, &errw)
	if len(rec.codes) != 1 || rec.codes[0] != 7 {
		t.Errorf("exit codes = %v, want [7]", rec.codes)
	}
}

func TestRunFakeWithIOInteractiveMock(t *testing.T) {
	rec := withRecordedExit(t)
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"prompt"}` + "\n")
	var out, errw bytesBuffer
	runFakeWithIO("interactive-mock", "tid-im", t.TempDir(), in, &out, &errw)
	if len(rec.codes) != 1 || rec.codes[0] != 0 {
		t.Errorf("exit codes = %v, want [0]", rec.codes)
	}
}

func TestRunFakeWithIOUnknownScenario(t *testing.T) {
	rec := withRecordedExit(t)
	in := strings.NewReader("")
	var out, errw bytesBuffer
	runFakeWithIO("nope", "tid", t.TempDir(), in, &out, &errw)
	if len(rec.codes) != 1 || rec.codes[0] != 2 {
		t.Errorf("exit codes = %v, want [2]", rec.codes)
	}
	if !strings.Contains(errw.String(), "unknown scenario") {
		t.Errorf("missing stderr message: %q", errw.String())
	}
}

func TestRunFakeWithIOMissingWorktree(t *testing.T) {
	rec := withRecordedExit(t)
	in := strings.NewReader("")
	var out, errw bytesBuffer
	runFakeWithIO("happy-path", "tid", "/no/such/path/zen-fake", in, &out, &errw)
	if len(rec.codes) != 1 || rec.codes[0] != 2 {
		t.Errorf("exit codes = %v, want [2]", rec.codes)
	}
	if !strings.Contains(errw.String(), "worktree stat") {
		t.Errorf("missing stderr message: %q", errw.String())
	}
}

func TestRunOpenClaudeFakePassThrough(t *testing.T) {
	rec := withRecordedExit(t)
	t.Setenv("ZEN_FAKE_OPENCLAUDE_SCENARIO", "nope-via-env")
	t.Setenv("ZEN_FAKE_OPENCLAUDE_THREAD_ID", "tid-env")
	t.Setenv("ZEN_FAKE_OPENCLAUDE_WORKTREE", "")
	RunOpenClaudeFake()
	if len(rec.codes) != 1 || rec.codes[0] != 2 {
		t.Errorf("exit codes = %v, want [2]", rec.codes)
	}
}
