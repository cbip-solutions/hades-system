package subprocess

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/tests/testharness"
)

func fakeCmdFor(t *testing.T, scenario, threadID, worktree string) *exec.Cmd {
	t.Helper()
	return testharness.BuildFakeCmd("TestHelperOpenClaudeFakeSubprocess", scenario, threadID, worktree)
}

func TestHelperOpenClaudeFakeSubprocess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_OPENCLAUDE_FAKE") != "1" {
		t.Skip("not the helper invocation")
	}

	runFakeFromHelper()
}

func TestOpenClaudeSessionHappyPath(t *testing.T) {
	wt := t.TempDir()
	id := ThreadID("tid-c3-happy")
	sess, err := newOpenClaudeSessionForTest(t, "happy-path", id, wt)
	if err != nil {
		t.Fatalf("newOpenClaudeSession: %v", err)
	}
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := sess.Send(ctx, Message{
		Kind:     MessageKindRequest,
		ID:       "req-1",
		ThreadID: id,
		Method:   "prompt",
		Payload:  json.RawMessage(`{"text":"hi"}`),
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	got, err := sess.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if got.Kind != MessageKindResult {
		t.Errorf("Kind = %v, want result", got.Kind)
	}
	if got.ID != "req-1" {
		t.Errorf("ID = %q, want req-1", got.ID)
	}
	var body map[string]any
	if err := json.Unmarshal(got.Payload, &body); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	if body["thread_id"] != string(id) {
		t.Errorf("payload.thread_id = %v, want %s", body["thread_id"], id)
	}
}

func TestOpenClaudeSessionInteractiveMockMultiFrame(t *testing.T) {
	wt := t.TempDir()
	id := ThreadID("tid-c3-interactive")
	sess, err := newOpenClaudeSessionForTest(t, "interactive-mock", id, wt)
	if err != nil {
		t.Fatalf("newOpenClaudeSession: %v", err)
	}
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := sess.Send(ctx, Message{
		Kind:     MessageKindRequest,
		ID:       "req-1",
		ThreadID: id,
		Method:   "prompt",
		Payload:  json.RawMessage(`{"text":"x"}`),
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	var notifs int
	var sawResult bool
	for i := 0; i < 5; i++ {
		msg, err := sess.Receive(ctx)
		if err != nil {
			t.Fatalf("Receive %d: %v", i, err)
		}
		switch msg.Kind {
		case MessageKindNotification:
			notifs++
		case MessageKindResult:
			sawResult = true
		default:
			t.Errorf("frame %d unexpected kind: %v", i, msg.Kind)
		}
	}
	if notifs != 4 {
		t.Errorf("notifications = %d, want 4", notifs)
	}
	if !sawResult {
		t.Error("did not see final result")
	}
}

func TestOpenClaudeSessionCloseUnblocksReceive(t *testing.T) {
	wt := t.TempDir()
	id := ThreadID("tid-c3-close")
	sess, err := newOpenClaudeSessionForTest(t, "happy-path", id, wt)
	if err != nil {
		t.Fatalf("newOpenClaudeSession: %v", err)
	}

	type res struct {
		err error
	}
	ch := make(chan res, 1)
	go func() {
		_, e := sess.Receive(context.Background())
		ch <- res{e}
	}()
	time.Sleep(50 * time.Millisecond)
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case r := <-ch:
		if !errors.Is(r.err, ErrSessionClosed) {
			t.Errorf("Receive after Close: err = %v, want ErrSessionClosed", r.err)
		}
	case <-time.After(time.Second):
		t.Fatal("Receive did not unblock after Close")
	}
}

func TestOpenClaudeSessionContextCancelOnReceive(t *testing.T) {
	wt := t.TempDir()
	id := ThreadID("tid-c3-cancel")
	sess, err := newOpenClaudeSessionForTest(t, "happy-path", id, wt)
	if err != nil {
		t.Fatalf("newOpenClaudeSession: %v", err)
	}
	defer sess.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = sess.Receive(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Receive ctx err = %v, want DeadlineExceeded", err)
	}
}

func TestOpenClaudeSessionContextCancelOnSend(t *testing.T) {
	wt := t.TempDir()
	id := ThreadID("tid-c3-send-cancel")

	sess, err := newOpenClaudeSessionForTest(t, "hang", id, wt)
	if err != nil {
		t.Fatalf("newOpenClaudeSession: %v", err)
	}
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	hadCtxErr := false
	for i := 0; i < 64; i++ {
		err := sess.Send(ctx, Message{
			Kind:     MessageKindRequest,
			ID:       "x",
			Method:   "prompt",
			ThreadID: id,
			Payload:  json.RawMessage(`{"text":"x"}`),
		})
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				hadCtxErr = true
				break
			}
			t.Fatalf("Send: %v", err)
		}
	}
	if !hadCtxErr {
		t.Skip("flood did not produce ctx.DeadlineExceeded; non-deterministic")
	}
}

func TestOpenClaudeSessionWorktreeArgPropagated(t *testing.T) {
	wt := filepath.Join(t.TempDir(), "wtsubdir")
	if err := os.Mkdir(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	captured := make(chan []string, 1)
	cf := func(name string, arg ...string) *exec.Cmd {
		captured <- append([]string{name}, arg...)
		return fakeCmdFor(t, "happy-path", "tid-arg", wt)
	}
	sess, err := newOpenClaudeSession(openClaudeOptions{
		Binary:      "openclaude",
		ThreadID:    ThreadID("tid-arg"),
		Worktree:    wt,
		commandFunc: cf,
	})
	if err != nil {
		t.Fatalf("newOpenClaudeSession: %v", err)
	}
	defer sess.Close()

	args := <-captured
	wantArgs := []string{"openclaude", "--stdio", "--worktree", wt, "--thread-id", "tid-arg"}
	if len(args) != len(wantArgs) {
		t.Fatalf("argv = %v, want %v", args, wantArgs)
	}
	for i := range wantArgs {
		if args[i] != wantArgs[i] {
			t.Errorf("argv[%d] = %q, want %q", i, args[i], wantArgs[i])
		}
	}
}

func TestNewOpenClaudeSessionRejectsZeroThreadID(t *testing.T) {
	_, err := newOpenClaudeSession(openClaudeOptions{
		Binary:   "openclaude",
		ThreadID: ThreadID(""),
		Worktree: "/tmp",
	})
	if err == nil {
		t.Fatal("zero ThreadID accepted")
	}
}

func TestNewOpenClaudeSessionRejectsEmptyWorktree(t *testing.T) {
	_, err := newOpenClaudeSession(openClaudeOptions{
		Binary:   "openclaude",
		ThreadID: ThreadID("tid"),
		Worktree: "",
	})
	if err == nil {
		t.Fatal("empty Worktree accepted")
	}
}

func TestNewOpenClaudeSessionStartFailure(t *testing.T) {
	cf := func(name string, arg ...string) *exec.Cmd {

		return exec.Command("/no/such/path/zen-fake-bin")
	}
	_, err := newOpenClaudeSession(openClaudeOptions{
		Binary:      "openclaude",
		ThreadID:    ThreadID("tid"),
		Worktree:    t.TempDir(),
		commandFunc: cf,
	})
	if err == nil {
		t.Fatal("Start with bogus binary returned nil err")
	}
}

func TestParseFrameMalformedJSON(t *testing.T) {
	_, err := parseFrame([]byte("not-a-json\n"), ThreadID("t"))
	if err == nil {
		t.Fatal("malformed JSON parsed successfully")
	}
}

func TestParseFrameEmpty(t *testing.T) {
	_, err := parseFrame(nil, ThreadID("t"))
	if err == nil {
		t.Fatal("empty frame parsed successfully")
	}
}

func TestParseFrameRequest(t *testing.T) {
	line := []byte(`{"jsonrpc":"2.0","id":42,"method":"prompt","params":{"text":"x"}}`)
	msg, err := parseFrame(line, ThreadID("t"))
	if err != nil {
		t.Fatalf("parseFrame: %v", err)
	}
	if msg.Kind != MessageKindRequest {
		t.Errorf("Kind = %v, want request", msg.Kind)
	}
	if msg.ID != "42" {
		t.Errorf("ID = %q, want 42 (numeric coerced)", msg.ID)
	}
	if msg.Method != "prompt" {
		t.Errorf("Method = %q, want prompt", msg.Method)
	}
}

func TestParseFrameNotification(t *testing.T) {
	line := []byte(`{"jsonrpc":"2.0","method":"tool_use","params":{"name":"x"}}`)
	msg, err := parseFrame(line, ThreadID("t"))
	if err != nil {
		t.Fatalf("parseFrame: %v", err)
	}
	if msg.Kind != MessageKindNotification {
		t.Errorf("Kind = %v, want notification", msg.Kind)
	}
	if msg.Method != "tool_use" {
		t.Errorf("Method = %q, want tool_use", msg.Method)
	}
}

func TestParseFrameError(t *testing.T) {
	line := []byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32700,"message":"parse error"}}`)
	msg, err := parseFrame(line, ThreadID("t"))
	if err != nil {
		t.Fatalf("parseFrame: %v", err)
	}
	if msg.Kind != MessageKindError {
		t.Errorf("Kind = %v, want error", msg.Kind)
	}
	if msg.ErrCode != -32700 {
		t.Errorf("ErrCode = %d, want -32700", msg.ErrCode)
	}
	if msg.ErrMsg != "parse error" {
		t.Errorf("ErrMsg = %q, want parse error", msg.ErrMsg)
	}
}

func TestParseFrameNoBody(t *testing.T) {
	line := []byte(`{"jsonrpc":"2.0","id":1}`)
	_, err := parseFrame(line, ThreadID("t"))
	if err == nil {
		t.Fatal("no-body frame parsed successfully")
	}
}

func TestParseFrameStringID(t *testing.T) {
	line := []byte(`{"jsonrpc":"2.0","id":"abc","method":"x"}`)
	msg, err := parseFrame(line, ThreadID("t"))
	if err != nil {
		t.Fatalf("parseFrame: %v", err)
	}
	if msg.ID != "abc" {
		t.Errorf("ID = %q, want abc", msg.ID)
	}
}

func TestParseFrameOddID(t *testing.T) {
	line := []byte(`{"jsonrpc":"2.0","id":true,"method":"x"}`)
	msg, err := parseFrame(line, ThreadID("t"))
	if err != nil {
		t.Fatalf("parseFrame: %v", err)
	}
	if msg.ID == "" {
		t.Error("odd ID coerced to empty")
	}
}

func TestParseFrameResultThreadIDOverride(t *testing.T) {
	line := []byte(`{"jsonrpc":"2.0","id":1,"result":{"thread_id":"override","echo":"x"}}`)
	msg, err := parseFrame(line, ThreadID("default"))
	if err != nil {
		t.Fatalf("parseFrame: %v", err)
	}
	if msg.ThreadID != ThreadID("override") {
		t.Errorf("ThreadID = %q, want override", msg.ThreadID)
	}
}

func TestParseFrameResultBadInnerJSON(t *testing.T) {

	line := []byte(`{"jsonrpc":"2.0","id":1,"result":[1,2,3]}`)
	msg, err := parseFrame(line, ThreadID("default"))
	if err != nil {
		t.Fatalf("parseFrame: %v", err)
	}
	if msg.ThreadID != ThreadID("default") {
		t.Errorf("ThreadID = %q, want default", msg.ThreadID)
	}
}

func TestEncodeFrameRequest(t *testing.T) {
	b, err := encodeFrame(Message{
		Kind:    MessageKindRequest,
		ID:      "1",
		Method:  "prompt",
		Payload: json.RawMessage(`{"text":"x"}`),
	})
	if err != nil {
		t.Fatalf("encodeFrame: %v", err)
	}
	if !contains(string(b), `"method":"prompt"`) {
		t.Errorf("missing method: %s", b)
	}
}

func TestEncodeFrameNotification(t *testing.T) {
	b, err := encodeFrame(Message{
		Kind:   MessageKindNotification,
		Method: "tool_use",
	})
	if err != nil {
		t.Fatalf("encodeFrame: %v", err)
	}
	if contains(string(b), `"id"`) {
		t.Errorf("notification should not have id: %s", b)
	}
}

func TestEncodeFrameResult(t *testing.T) {
	b, err := encodeFrame(Message{
		Kind:    MessageKindResult,
		ID:      "1",
		Payload: json.RawMessage(`{"ok":true}`),
	})
	if err != nil {
		t.Fatalf("encodeFrame: %v", err)
	}
	if !contains(string(b), `"result"`) {
		t.Errorf("missing result: %s", b)
	}
}

func TestEncodeFrameError(t *testing.T) {
	b, err := encodeFrame(Message{
		Kind:    MessageKindError,
		ID:      "1",
		ErrCode: -1,
		ErrMsg:  "boom",
	})
	if err != nil {
		t.Fatalf("encodeFrame: %v", err)
	}
	if !contains(string(b), `"error"`) {
		t.Errorf("missing error: %s", b)
	}
}

func TestEncodeFrameUnknownKind(t *testing.T) {
	_, err := encodeFrame(Message{Kind: MessageKind(99)})
	if err == nil {
		t.Fatal("unknown Kind encoded successfully")
	}
}

func TestEncodeFrameRequestEmptyPayload(t *testing.T) {
	b, err := encodeFrame(Message{Kind: MessageKindRequest, ID: "1", Method: "x"})
	if err != nil {
		t.Fatalf("encodeFrame: %v", err)
	}
	if contains(string(b), `"params"`) {
		t.Errorf("empty params should not be present: %s", b)
	}
}

func TestEncodeFrameNotificationEmptyPayload(t *testing.T) {
	b, err := encodeFrame(Message{Kind: MessageKindNotification, Method: "x"})
	if err != nil {
		t.Fatalf("encodeFrame: %v", err)
	}
	if contains(string(b), `"params"`) {
		t.Errorf("empty params should not be present: %s", b)
	}
}

func TestSignalGroupNoProcess(t *testing.T) {
	s := &openClaudeSession{}
	if err := s.signalGroup(0); err == nil {
		t.Error("signalGroup with no process returned nil err")
	}
}

func TestKillGroupNoProcess(t *testing.T) {
	s := &openClaudeSession{}
	if err := s.killGroup(); err != nil {
		t.Errorf("killGroup with no process: %v", err)
	}
}

func TestPidNoProcess(t *testing.T) {
	s := &openClaudeSession{}
	if s.pid() != 0 {
		t.Errorf("pid() = %d, want 0", s.pid())
	}
}

func TestOpenClaudeSessionThreadIDAccessor(t *testing.T) {
	wt := t.TempDir()
	id := ThreadID("tid-accessor")
	sess, err := newOpenClaudeSessionForTest(t, "happy-path", id, wt)
	if err != nil {
		t.Fatalf("newOpenClaudeSession: %v", err)
	}
	defer sess.Close()
	if sess.ThreadID() != id {
		t.Errorf("ThreadID() = %q, want %q", sess.ThreadID(), id)
	}
	if sess.pid() == 0 {
		t.Error("pid() = 0 for live session")
	}
}

func TestOpenClaudeSessionCloseStableAcrossCalls(t *testing.T) {
	wt := t.TempDir()
	id := ThreadID("tid-cs")
	sess, err := newOpenClaudeSessionForTest(t, "crash", id, wt)
	if err != nil {
		t.Fatalf("newOpenClaudeSession: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_ = sess.Send(ctx, Message{
		Kind: MessageKindRequest, ID: "1",
		Method: "prompt", ThreadID: id,
		Payload: json.RawMessage(`{"text":"x"}`),
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-sess.exitCh:
			goto exited
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
	t.Fatal("child did not exit before deadline")
exited:
	first := sess.Close()
	if first == nil {
		t.Fatal("Close after crash: first call returned nil; want non-nil for stability test")
	}
	for i := 0; i < 4; i++ {
		got := sess.Close()
		if got != first {
			t.Errorf("call %d: Close() = %v, want %v (stable across calls)", i, got, first)
		}
	}
}

func TestOpenClaudeSessionCloseAfterChildCrash(t *testing.T) {
	wt := t.TempDir()
	id := ThreadID("tid-crash")
	sess, err := newOpenClaudeSessionForTest(t, "crash", id, wt)
	if err != nil {
		t.Fatalf("newOpenClaudeSession: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_ = sess.Send(ctx, Message{
		Kind: MessageKindRequest, ID: "1",
		Method: "prompt", ThreadID: id,
		Payload: json.RawMessage(`{"text":"x"}`),
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-sess.exitCh:
			goto done
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
done:

	err = sess.Close()
	if err == nil {
		t.Errorf("Close after crash: err = nil, want non-nil (exit code 7)")
	}
}

func TestOpenClaudeSessionSendAfterClose(t *testing.T) {
	wt := t.TempDir()
	id := ThreadID("tid-sac")
	sess, err := newOpenClaudeSessionForTest(t, "happy-path", id, wt)
	if err != nil {
		t.Fatalf("newOpenClaudeSession: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	err = sess.Send(context.Background(), Message{Kind: MessageKindRequest, ID: "x"})
	if !errors.Is(err, ErrSessionClosed) {
		t.Errorf("Send after Close: err = %v, want ErrSessionClosed", err)
	}
}

func TestSignalGroupLiveProcess(t *testing.T) {
	wt := t.TempDir()
	id := ThreadID("tid-sig")
	sess, err := newOpenClaudeSessionForTest(t, "happy-path", id, wt)
	if err != nil {
		t.Fatalf("newOpenClaudeSession: %v", err)
	}
	defer sess.Close()
	if err := sess.signalGroup(syscall.SIGTERM); err != nil {
		t.Errorf("signalGroup SIGTERM: %v", err)
	}
}

func TestKillGroupLiveProcess(t *testing.T) {
	wt := t.TempDir()
	id := ThreadID("tid-kg")
	sess, err := newOpenClaudeSessionForTest(t, "happy-path", id, wt)
	if err != nil {
		t.Fatalf("newOpenClaudeSession: %v", err)
	}
	defer sess.Close()
	sess.killedByClose.Store(true)
	if err := sess.killGroup(); err != nil {
		t.Errorf("killGroup: %v", err)
	}

	select {
	case <-sess.exitCh:
	case <-time.After(2 * time.Second):
		t.Fatal("child did not exit after killGroup")
	}
}

func TestOpenClaudeSessionCloseEscalatesToKill(t *testing.T) {
	wt := t.TempDir()
	id := ThreadID("tid-esc")
	sess, err := newOpenClaudeSessionForTest(t, "hang", id, wt)
	if err != nil {
		t.Fatalf("newOpenClaudeSession: %v", err)
	}
	sess.closeGrace = 50 * time.Millisecond
	start := time.Now()
	if err := sess.Close(); err != nil {
		t.Errorf("Close: %v (suppressed kill must not surface)", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Close took %v, want < 2s", elapsed)
	}
}

func TestSendBlockedThenClosed(t *testing.T) {
	wt := t.TempDir()
	id := ThreadID("tid-sbc")
	sess, err := newOpenClaudeSessionForTest(t, "happy-path", id, wt)
	if err != nil {
		t.Fatalf("newOpenClaudeSession: %v", err)
	}

	_ = sess.stdin.Close()

	time.Sleep(50 * time.Millisecond)

	for i := 0; i < 10; i++ {
		_ = sess.Send(context.Background(), Message{
			Kind: MessageKindRequest, ID: "x",
			Method: "prompt", ThreadID: id,
			Payload: json.RawMessage(`{"text":"x"}`),
		})
	}

	resCh := make(chan error, 1)
	go func() {
		resCh <- sess.Send(context.Background(), Message{
			Kind: MessageKindRequest, ID: "blocked",
			Method: "prompt", ThreadID: id,
			Payload: json.RawMessage(`{"text":"x"}`),
		})
	}()
	time.Sleep(20 * time.Millisecond)
	_ = sess.Close()
	select {
	case err := <-resCh:

		_ = err
	case <-time.After(2 * time.Second):
		t.Fatal("blocked Send did not return after Close")
	}
}

func TestSendCtxCancelOnFullChannel(t *testing.T) {
	wt := t.TempDir()
	id := ThreadID("tid-scfc")
	sess, err := newOpenClaudeSessionForTest(t, "happy-path", id, wt)
	if err != nil {
		t.Fatalf("newOpenClaudeSession: %v", err)
	}
	defer sess.Close()

	_ = sess.stdin.Close()
	time.Sleep(50 * time.Millisecond)

	for i := 0; i < 8; i++ {
		_ = sess.Send(context.Background(), Message{
			Kind: MessageKindRequest, ID: "x",
			Method: "prompt", ThreadID: id,
			Payload: json.RawMessage(`{"text":"x"}`),
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err = sess.Send(ctx, Message{
		Kind: MessageKindRequest, ID: "ctx",
		Method: "prompt", ThreadID: id,
		Payload: json.RawMessage(`{"text":"x"}`),
	})
	if err == nil {
		t.Skip("flooded Send did not block; non-deterministic")
	}

	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, ErrSessionClosed) {
		t.Errorf("Send err = %v, want DeadlineExceeded or ErrSessionClosed", err)
	}
}

func TestReceiveDrainAfterClose(t *testing.T) {
	wt := t.TempDir()
	id := ThreadID("tid-rdac")
	sess, err := newOpenClaudeSessionForTest(t, "happy-path", id, wt)
	if err != nil {
		t.Fatalf("newOpenClaudeSession: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := sess.Send(ctx, Message{
		Kind: MessageKindRequest, ID: "1",
		Method: "prompt", ThreadID: id,
		Payload: json.RawMessage(`{"text":"x"}`),
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if len(sess.recvCh) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	_ = sess.Close()
	got, err := sess.Receive(context.Background())
	if err != nil && !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("Receive: %v", err)
	}
	if err == nil && got.Kind != MessageKindResult {
		t.Errorf("Receive Kind = %v, want result", got.Kind)
	}
}

func TestReadLoopParseErrorContinues(t *testing.T) {

	pr, pw := newPipePair(t)
	s := &openClaudeSession{
		id:         ThreadID("tid-rl"),
		stdout:     pr,
		recvCh:     make(chan Message, 4),
		closed:     make(chan struct{}),
		exitCh:     make(chan struct{}),
		closeGrace: 100 * time.Millisecond,
	}
	done := make(chan struct{})
	go func() { s.readLoop(); close(done) }()

	if _, err := pw.Write([]byte("not-a-json\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := pw.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"text":"ok"}}` + "\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-s.recvCh:
		if msg.Kind != MessageKindResult {
			t.Errorf("Kind = %v, want result", msg.Kind)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive valid frame after malformed skip")
	}
	close(s.closed)

	_ = pw.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("readLoop did not exit on pipe close")
	}
}

func TestReadLoopReadError(t *testing.T) {
	pr, pw := newPipePair(t)
	s := &openClaudeSession{
		id:     ThreadID("tid-rle"),
		stdout: pr,
		recvCh: make(chan Message, 4),
		closed: make(chan struct{}),
		exitCh: make(chan struct{}),
	}
	done := make(chan struct{})
	go func() { s.readLoop(); close(done) }()

	_ = pw.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop did not exit on pipe close")
	}
}

func TestReadLoopClosedDuringSend(t *testing.T) {
	pr, pw := newPipePair(t)
	s := &openClaudeSession{
		id:     ThreadID("tid-rlcd"),
		stdout: pr,
		recvCh: make(chan Message),
		closed: make(chan struct{}),
		exitCh: make(chan struct{}),
	}
	done := make(chan struct{})
	go func() { s.readLoop(); close(done) }()
	if _, err := pw.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"text":"x"}}` + "\n")); err != nil {
		t.Fatal(err)
	}

	time.Sleep(50 * time.Millisecond)
	close(s.closed)
	_ = pw.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop did not exit after closed")
	}
}

func TestWriteLoopEncodeError(t *testing.T) {
	pr, pw := newPipePair(t)
	defer pr.Close()
	defer pw.Close()
	s := &openClaudeSession{
		id:     ThreadID("tid-wle"),
		stdin:  pw,
		sendCh: make(chan Message, 4),
		closed: make(chan struct{}),
		exitCh: make(chan struct{}),
	}
	done := make(chan struct{})
	go func() { s.writeLoop(); close(done) }()

	s.sendCh <- Message{Kind: MessageKind(99), ID: "x"}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("writeLoop did not exit on encode error")
	}
	v := s.closeErr.Load()
	if v == nil {
		t.Fatal("closeErr not stored on encode error (silent drop regression)")
	}
	err, ok := v.(error)
	if !ok {
		t.Fatalf("closeErr type = %T, want error", v)
	}
	if !contains(err.Error(), "encode") {
		t.Errorf("closeErr = %v, want substring 'encode'", err)
	}
}

func TestWriteLoopWriteError(t *testing.T) {
	pr, pw := newPipePair(t)
	s := &openClaudeSession{
		id:     ThreadID("tid-wlwe"),
		stdin:  pw,
		sendCh: make(chan Message, 4),
		closed: make(chan struct{}),
		exitCh: make(chan struct{}),
	}
	done := make(chan struct{})
	go func() { s.writeLoop(); close(done) }()

	_ = pr.Close()
	s.sendCh <- Message{Kind: MessageKindRequest, ID: "1", Method: "x"}

	time.Sleep(50 * time.Millisecond)
	close(s.closed)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("writeLoop did not exit after write error")
	}
}

func TestEncodeFrameNotificationWithPayload(t *testing.T) {
	b, err := encodeFrame(Message{
		Kind:    MessageKindNotification,
		Method:  "tool_use",
		Payload: json.RawMessage(`{"x":1}`),
	})
	if err != nil {
		t.Fatalf("encodeFrame: %v", err)
	}
	if !contains(string(b), `"params"`) {
		t.Errorf("missing params: %s", b)
	}
}

func newPipePair(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	return r, w
}

func TestKillGroupGetpgidFailFallback(t *testing.T) {
	wt := t.TempDir()
	id := ThreadID("tid-kgff")
	sess, err := newOpenClaudeSessionForTest(t, "happy-path", id, wt)
	if err != nil {
		t.Fatalf("newOpenClaudeSession: %v", err)
	}
	defer sess.Close()
	sess.killedByClose.Store(true)
	prev := getpgid
	getpgid = func(_ int) (int, error) { return 0, syscall.ESRCH }
	defer func() { getpgid = prev }()
	if err := sess.killGroup(); err != nil {
		t.Errorf("killGroup fallback: %v", err)
	}
	select {
	case <-sess.exitCh:
	case <-time.After(2 * time.Second):
		t.Fatal("child did not exit after killGroup fallback")
	}
}

func TestSignalGroupGetpgidFailFallback(t *testing.T) {
	wt := t.TempDir()
	id := ThreadID("tid-sgff")
	sess, err := newOpenClaudeSessionForTest(t, "happy-path", id, wt)
	if err != nil {
		t.Fatalf("newOpenClaudeSession: %v", err)
	}
	defer sess.Close()
	prev := getpgid
	getpgid = func(_ int) (int, error) { return 0, syscall.ESRCH }
	defer func() { getpgid = prev }()
	if err := sess.signalGroup(syscall.SIGUSR1); err != nil {
		t.Errorf("signalGroup fallback: %v", err)
	}
}

func TestNewOpenClaudeSessionDefaultBinary(t *testing.T) {
	wt := t.TempDir()
	id := ThreadID("tid-db")
	cf := func(name string, arg ...string) *exec.Cmd {

		if name != openClaudeBinary {
			t.Errorf("default Binary = %q, want %q", name, openClaudeBinary)
		}
		return fakeCmdFor(t, "happy-path", string(id), wt)
	}
	sess, err := newOpenClaudeSession(openClaudeOptions{
		Binary:      "",
		ThreadID:    id,
		Worktree:    wt,
		commandFunc: cf,
	})
	if err != nil {
		t.Fatalf("newOpenClaudeSession: %v", err)
	}
	defer sess.Close()
}

func TestNewOpenClaudeSessionDefaultCommandFunc(t *testing.T) {
	wt := t.TempDir()
	id := ThreadID("tid-dcf")

	_, err := newOpenClaudeSession(openClaudeOptions{
		Binary:   "/no/such/path/zen-fake-bin-default",
		ThreadID: id,
		Worktree: wt,
	})
	if err == nil {
		t.Fatal("Start with bogus binary returned nil err")
	}
}

func TestReadLoopRejectsOversizedFrame(t *testing.T) {
	pr, pw := newPipePair(t)
	s := &openClaudeSession{
		id:     ThreadID("tid-oversized"),
		stdout: pr,
		recvCh: make(chan Message, 4),
		closed: make(chan struct{}),
		exitCh: make(chan struct{}),

		readMaxFrameBytes: 1 << 14,
	}
	done := make(chan struct{})
	go func() { s.readLoop(); close(done) }()

	huge := make([]byte, 80*1024)
	for i := range huge {
		huge[i] = 'x'
	}

	go func() { _, _ = pw.Write(huge); _ = pw.Close() }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop did not exit on oversized frame")
	}
	v := s.closeErr.Load()
	if v == nil {
		t.Fatal("closeErr not stored on oversized frame")
	}
	err, ok := v.(error)
	if !ok {
		t.Fatalf("closeErr type = %T, want error", v)
	}
	if !contains(err.Error(), "exceeds") {
		t.Errorf("closeErr = %v, want substring 'exceeds'", err)
	}
}

func TestReadLoopAcceptsFramesAtMaxBoundary(t *testing.T) {
	const cap = 1024
	pr, pw := newPipePair(t)
	s := &openClaudeSession{
		id:                ThreadID("tid-atmax"),
		stdout:            pr,
		recvCh:            make(chan Message, 4),
		closed:            make(chan struct{}),
		exitCh:            make(chan struct{}),
		readMaxFrameBytes: cap,
	}
	done := make(chan struct{})
	go func() { s.readLoop(); close(done) }()

	padLen := cap - 64
	pad := make([]byte, padLen)
	for i := range pad {
		pad[i] = 'a'
	}
	frame := []byte(`{"jsonrpc":"2.0","id":1,"result":{"text":"`)
	frame = append(frame, pad...)
	frame = append(frame, []byte(`"}}`)...)
	frame = append(frame, '\n')
	if len(frame) > cap {
		t.Fatalf("test bug: frame size %d > cap %d", len(frame), cap)
	}
	if _, err := pw.Write(frame); err != nil {
		t.Fatalf("Write boundary: %v", err)
	}
	select {
	case msg := <-s.recvCh:
		if msg.Kind != MessageKindResult {
			t.Errorf("Kind = %v, want result", msg.Kind)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive boundary frame")
	}
	if v := s.closeErr.Load(); v != nil {
		t.Errorf("closeErr stored unexpectedly: %v", v)
	}
	_ = pw.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("readLoop did not exit on pipe close")
	}
}

func TestNewOpenClaudeSessionDefaultsReadMaxFrameBytes(t *testing.T) {
	wt := t.TempDir()
	id := ThreadID("tid-default-cap")
	sess, err := newOpenClaudeSessionForTest(t, "happy-path", id, wt)
	if err != nil {
		t.Fatalf("newOpenClaudeSession: %v", err)
	}
	defer sess.Close()
	if sess.readMaxFrameBytes != defaultReadMaxFrameBytes {
		t.Errorf("readMaxFrameBytes = %d, want %d (default)",
			sess.readMaxFrameBytes, defaultReadMaxFrameBytes)
	}
}

func TestReadLoopAcceptsFrameLargerThanBufioBuffer(t *testing.T) {
	const cap = 1 << 18
	pr, pw := newPipePair(t)
	s := &openClaudeSession{
		id:                ThreadID("tid-multichunk"),
		stdout:            pr,
		recvCh:            make(chan Message, 4),
		closed:            make(chan struct{}),
		exitCh:            make(chan struct{}),
		readMaxFrameBytes: cap,
	}
	done := make(chan struct{})
	go func() { s.readLoop(); close(done) }()

	const bodyLen = 192 * 1024
	pad := make([]byte, bodyLen)
	for i := range pad {
		pad[i] = 'q'
	}
	frame := []byte(`{"jsonrpc":"2.0","id":1,"result":{"text":"`)
	frame = append(frame, pad...)
	frame = append(frame, []byte(`"}}`)...)
	frame = append(frame, '\n')
	if len(frame) > cap {
		t.Fatalf("test bug: frame size %d > cap %d", len(frame), cap)
	}
	go func() { _, _ = pw.Write(frame) }()

	select {
	case msg := <-s.recvCh:
		if msg.Kind != MessageKindResult {
			t.Errorf("Kind = %v, want result", msg.Kind)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive multi-chunk frame")
	}
	if v := s.closeErr.Load(); v != nil {
		t.Errorf("closeErr stored unexpectedly: %v", v)
	}
	_ = pw.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("readLoop did not exit on pipe close")
	}
}

func TestSendRejectsInvalidMessageKind(t *testing.T) {
	wt := t.TempDir()
	id := ThreadID("tid-bad-kind")
	sess, err := newOpenClaudeSessionForTest(t, "happy-path", id, wt)
	if err != nil {
		t.Fatalf("newOpenClaudeSession: %v", err)
	}
	defer sess.Close()

	err = sess.Send(context.Background(), Message{Kind: MessageKind(99), ID: "x"})
	if err == nil {
		t.Fatal("Send accepted Kind=99; want validation error")
	}
	if !contains(err.Error(), "invalid MessageKind") {
		t.Errorf("Send err = %v, want substring 'invalid MessageKind'", err)
	}

	err = sess.Send(context.Background(), Message{Kind: MessageKind(-1), ID: "x"})
	if err == nil {
		t.Fatal("Send accepted Kind=-1; want validation error")
	}
}

func TestMemSessionSendRejectsInvalidMessageKind(t *testing.T) {
	ms := newMemSession("bad-kind")
	defer ms.Close()
	err := ms.Send(context.Background(), Message{Kind: MessageKind(99), ID: "x"})
	if err == nil {
		t.Fatal("memSession.Send accepted Kind=99")
	}
	if !contains(err.Error(), "invalid MessageKind") {
		t.Errorf("memSession.Send err = %v, want substring 'invalid MessageKind'", err)
	}
}

func TestMessageKindIsValid(t *testing.T) {
	for _, k := range []MessageKind{MessageKindRequest, MessageKindResult, MessageKindError, MessageKindNotification} {
		if !k.IsValid() {
			t.Errorf("%v.IsValid() = false, want true", k)
		}
	}
	for _, k := range []MessageKind{MessageKind(-1), MessageKind(99)} {
		if k.IsValid() {
			t.Errorf("%v.IsValid() = true, want false", k)
		}
	}
}

func TestSendCloseArmInner(t *testing.T) {
	s := &openClaudeSession{
		id:     ThreadID("tid-sca"),
		sendCh: make(chan Message),
		closed: make(chan struct{}),
	}
	resCh := make(chan error, 1)
	go func() {
		resCh <- s.Send(context.Background(), Message{Kind: MessageKindRequest, ID: "x"})
	}()
	time.Sleep(20 * time.Millisecond)
	close(s.closed)
	select {
	case err := <-resCh:
		if !errors.Is(err, ErrSessionClosed) {
			t.Errorf("Send err = %v, want ErrSessionClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Send did not return after close")
	}
}

func TestSendCtxArmInner(t *testing.T) {
	s := &openClaudeSession{
		id:     ThreadID("tid-scai"),
		sendCh: make(chan Message),
		closed: make(chan struct{}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := s.Send(ctx, Message{Kind: MessageKindRequest, ID: "x"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Send err = %v, want DeadlineExceeded", err)
	}
}

func TestReadLoopReadErrorBranch(t *testing.T) {
	r := &errReadCloser{err: errors.New("synthetic read err")}
	s := &openClaudeSession{
		id:     ThreadID("tid-rleb"),
		stdout: r,
		recvCh: make(chan Message, 4),
		closed: make(chan struct{}),
		exitCh: make(chan struct{}),
	}
	done := make(chan struct{})
	go func() { s.readLoop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop did not exit on synthetic read error")
	}
	v := s.closeErr.Load()
	if v == nil {
		t.Fatal("closeErr not stored on read error")
	}
}

func TestWriteLoopWriteErrorBranch(t *testing.T) {
	w := &errWriteCloser{err: errors.New("synthetic write err")}
	s := &openClaudeSession{
		id:     ThreadID("tid-wleb"),
		stdin:  w,
		sendCh: make(chan Message, 4),
		closed: make(chan struct{}),
		exitCh: make(chan struct{}),
	}
	done := make(chan struct{})
	go func() { s.writeLoop(); close(done) }()

	large := make([]byte, 1<<16)
	for i := range large {
		large[i] = 'x'
	}
	s.sendCh <- Message{
		Kind: MessageKindRequest, ID: "1", Method: "x",
		Payload: json.RawMessage(`"` + string(large) + `"`),
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("writeLoop did not exit on synthetic write error")
	}
	if s.closeErr.Load() == nil {
		t.Fatal("closeErr not stored on write error")
	}
}

type errReadCloser struct{ err error }

func (e *errReadCloser) Read(_ []byte) (int, error) { return 0, e.err }
func (e *errReadCloser) Close() error               { return nil }

type errWriteCloser struct{ err error }

func (e *errWriteCloser) Write(_ []byte) (int, error) { return 0, e.err }
func (e *errWriteCloser) Close() error                { return nil }

func TestReceiveDrainAfterCloseInner(t *testing.T) {
	for i := 0; i < 32; i++ {
		s := &openClaudeSession{
			id:     ThreadID("tid-rdaci"),
			recvCh: make(chan Message, 4),
			closed: make(chan struct{}),
		}
		s.recvCh <- Message{Kind: MessageKindResult, ID: "buffered"}
		close(s.closed)
		got, err := s.Receive(context.Background())
		if err != nil {
			t.Fatalf("iter %d: Receive err = %v", i, err)
		}
		if got.ID != "buffered" {
			t.Errorf("iter %d: ID = %q, want buffered", i, got.ID)
		}
	}
}

func TestWriteLoopFlushError(t *testing.T) {
	w := &flakeyWriter{failAfterCall: 0}
	s := &openClaudeSession{
		id:     ThreadID("tid-wfe"),
		stdin:  w,
		sendCh: make(chan Message, 4),
		closed: make(chan struct{}),
		exitCh: make(chan struct{}),
	}
	done := make(chan struct{})
	go func() { s.writeLoop(); close(done) }()

	s.sendCh <- Message{Kind: MessageKindRequest, ID: "1", Method: "x"}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("writeLoop did not exit on flush error")
	}
	if s.closeErr.Load() == nil {
		t.Fatal("closeErr not stored on flush error")
	}
}

type flakeyWriter struct {
	mu            sync.Mutex
	calls         int
	failAfterCall int
}

func (w *flakeyWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	if w.calls > w.failAfterCall {
		return 0, errors.New("flakey writer: synthetic err")
	}
	return len(p), nil
}

func (w *flakeyWriter) Close() error { return nil }

func TestNewOpenClaudeSessionStdinPipeFailure(t *testing.T) {
	wt := t.TempDir()
	id := ThreadID("tid-spf")
	cf := func(name string, arg ...string) *exec.Cmd {
		c := fakeCmdFor(t, "happy-path", string(id), wt)

		_, _ = c.StdinPipe()
		return c
	}
	_, err := newOpenClaudeSession(openClaudeOptions{
		Binary:      "openclaude",
		ThreadID:    id,
		Worktree:    wt,
		commandFunc: cf,
	})
	if err == nil {
		t.Fatal("StdinPipe failure not surfaced")
	}
}

func TestNewOpenClaudeSessionStdoutPipeFailure(t *testing.T) {
	wt := t.TempDir()
	id := ThreadID("tid-spof")
	cf := func(name string, arg ...string) *exec.Cmd {
		c := fakeCmdFor(t, "happy-path", string(id), wt)
		_, _ = c.StdoutPipe()
		return c
	}
	_, err := newOpenClaudeSession(openClaudeOptions{
		Binary:      "openclaude",
		ThreadID:    id,
		Worktree:    wt,
		commandFunc: cf,
	})
	if err == nil {
		t.Fatal("StdoutPipe failure not surfaced")
	}
}

func TestNewOpenClaudeSessionForComplianceRoundTrip(t *testing.T) {
	wt := t.TempDir()
	id := ThreadID("tid-comp")
	cf := func(name string, arg ...string) *exec.Cmd {
		return fakeCmdFor(t, "happy-path", string(id), wt)
	}
	sess, err := NewOpenClaudeSessionForCompliance(id, wt, cf)
	if err != nil {
		t.Fatalf("NewOpenClaudeSessionForCompliance: %v", err)
	}
	defer sess.Close()
	if sess.ThreadID() != id {
		t.Errorf("ThreadID = %q, want %q", sess.ThreadID(), id)
	}
}

func TestNewOpenClaudeSessionForComplianceFailure(t *testing.T) {
	cf := func(name string, arg ...string) *exec.Cmd {
		return exec.Command("/no/such/path/zen-fake-bin-comp")
	}
	_, err := NewOpenClaudeSessionForCompliance(ThreadID("tid"), t.TempDir(), cf)
	if err == nil {
		t.Fatal("Start failure not surfaced")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
