package sshexec

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/tests/testharness"
)

func TestDetectorPasswordPrompt(t *testing.T) {
	d := newDetector()
	d.Feed([]byte("[sudo] password for testuser: "), StreamStdout)
	select {
	case reason := <-d.Triggered():
		if !strings.Contains(reason, "sudo") && !strings.Contains(reason, "password") {
			t.Errorf("trigger reason = %q, want sudo/password", reason)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("Detector did not trigger on [sudo] prompt")
	}
}

func TestDetectorAreYouSurePrompt(t *testing.T) {
	d := newDetector()
	d.Feed([]byte("Are you sure you want to continue? (yes/no): "), StreamStdout)
	select {
	case reason := <-d.Triggered():
		if !strings.Contains(reason, "yes/no") && !strings.Contains(reason, "are you sure") {
			t.Errorf("trigger reason = %q", reason)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("Detector did not trigger on yes/no prompt")
	}
}

func TestDetectorTIOCSTI(t *testing.T) {
	d := newDetector()
	d.Feed([]byte{0xfd, 0x18, 'l', 's', '\n'}, StreamStderr)
	select {
	case reason := <-d.Triggered():
		if !strings.Contains(reason, "TIOCSTI") {
			t.Errorf("trigger reason = %q, want contains TIOCSTI", reason)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("Detector did not trigger on TIOCSTI bytes")
	}
}

func TestDetectorContinuationPrompt(t *testing.T) {
	d := newDetector()
	d.Feed([]byte("> "), StreamStdout)
	select {
	case reason := <-d.Triggered():
		if !strings.Contains(reason, "continuation") {
			t.Errorf("reason = %q", reason)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("Detector did not trigger on '> ' prompt")
	}
}

func TestDetectorMidlineContinuationPrompt(t *testing.T) {
	d := newDetector()
	d.Feed([]byte("hello\n> "), StreamStdout)
	select {
	case <-d.Triggered():
	case <-time.After(50 * time.Millisecond):
		t.Fatal("Detector did not trigger on mid-line '> ' prompt")
	}
}

func TestDetectorPasswordKeywordWithoutColon(t *testing.T) {
	d := newDetector()
	d.Feed([]byte("password is required for sudo"), StreamStdout)
	select {
	case r := <-d.Triggered():
		// Either keyword or prompt is acceptable; the security goal is
		// SIGKILL on this content.
		if !strings.Contains(r, "password") {
			t.Errorf("reason = %q", r)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("Detector did not trigger on password keyword")
	}
}

func TestDetectorBenignContent(t *testing.T) {
	d := newDetector()
	d.Feed([]byte("Migration applied: 0001_init -> 0002_users\n"), StreamStdout)
	select {
	case r := <-d.Triggered():
		t.Fatalf("Detector falsely triggered on benign output: %q", r)
	case <-time.After(20 * time.Millisecond):
	}
}

// TestDetectorInvariant1024Window bytes after position 1024 do NOT
// trigger; rationale = §3.5 Flow 5 first-1024-bytes pattern.
func TestDetectorInvariant1024Window(t *testing.T) {
	d := newDetector()
	prefix := bytes.Repeat([]byte("x"), 1100)
	d.Feed(prefix, StreamStdout)
	d.Feed([]byte("[sudo] password for testuser: "), StreamStdout)
	select {
	case reason := <-d.Triggered():
		t.Fatalf("Detector triggered after window closed: %q", reason)
	case <-time.After(20 * time.Millisecond):

	}
}

func TestDetectorIdempotent(t *testing.T) {
	d := newDetector()
	d.Feed([]byte("[sudo] password for testuser: "), StreamStdout)
	<-d.Triggered()

	for i := 0; i < 10; i++ {
		d.Feed([]byte("more (yes/no): "), StreamStdout)
	}
	select {
	case reason := <-d.Triggered():
		t.Fatalf("Detector re-triggered after latch: %q", reason)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestDetectorSnippet(t *testing.T) {
	d := newDetector()
	d.Feed([]byte("[sudo] password: hello"), StreamStdout)
	<-d.Triggered()
	snip := d.Snippet()
	if !bytes.Contains(snip, []byte("[sudo]")) {
		t.Errorf("Snippet = %q, want contains [sudo]", snip)
	}
	if len(snip) > detectorWindow {
		t.Errorf("Snippet len = %d, want <=%d", len(snip), detectorWindow)
	}
}

func TestDetectorSnippetNilBeforeTrigger(t *testing.T) {
	d := newDetector()
	if got := d.Snippet(); got != nil {
		t.Errorf("Snippet pre-trigger = %v, want nil", got)
	}
}

func TestDetectorTriggerLatency(t *testing.T) {
	d := newDetector()
	start := time.Now()
	d.Feed([]byte("[sudo] password: "), StreamStdout)
	select {
	case <-d.Triggered():
		if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
			t.Errorf("trigger latency = %v, want <100ms", elapsed)
		}
	case <-time.After(150 * time.Millisecond):
		t.Fatal("trigger latency >150ms")
	}
}

func TestDetectorFeedZeroByteIsHandled(t *testing.T) {
	d := newDetector()
	d.Feed([]byte{}, StreamStdout)
	select {
	case <-d.Triggered():
		t.Fatal("Detector triggered on empty payload")
	case <-time.After(10 * time.Millisecond):
	}
}

func TestRunBlocksOnInteractivePrompt(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "1")
	srv, _ := testharness.NewFakeSSHD(testharness.HandlerFunc(func(req string) testharness.HandlerScript {
		return testharness.HandlerScript{
			Stdout:   "[sudo] password for testuser: ",
			Delay:    20 * time.Millisecond,
			ExitCode: 0,
		}
	}))
	defer srv.Close()
	req := ExecRequest{Host: srv.Addr(), Command: "alembic upgrade", Project: "p"}
	req.ApplyDefaults()
	vr := Validate(req.Command, []string{"alembic *"})
	allow := &Allowlist{Patterns: []string{"alembic *"}, Hosts: []string{srv.Addr()}}
	sink := &MemoryStreamSink{}
	emitter := &recordingEmitter{}
	start := time.Now()
	res, err := Run(context.Background(), req, vr, allow, AgentAuthForTest(), sink, emitter)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("Run returned nil err on interactive prompt; want error")
	}
	if !res.InteractiveBlocked {
		t.Fatal("InteractiveBlocked = false; want true")
	}
	if elapsed > 1500*time.Millisecond {
		t.Errorf("kill latency = %v, want <1500ms (incl. dial)", elapsed)
	}
	if len(emitter.interactive) == 0 {
		t.Errorf("audit interactive_blocked not emitted")
	}
}

type recordingEmitter struct {
	started     []ExecRequest
	completed   []ExecResult
	denied      []string
	interactive [][]byte
}

func (r *recordingEmitter) EmitStarted(req ExecRequest) error {
	r.started = append(r.started, req)
	return nil
}
func (r *recordingEmitter) EmitCompleted(req ExecRequest, res ExecResult) error {
	r.completed = append(r.completed, res)
	return nil
}
func (r *recordingEmitter) EmitDenied(req ExecRequest, reason string) error {
	r.denied = append(r.denied, reason)
	return nil
}
func (r *recordingEmitter) EmitInteractiveBlocked(req ExecRequest, snippet []byte) error {
	r.interactive = append(r.interactive, append([]byte(nil), snippet...))
	return nil
}
