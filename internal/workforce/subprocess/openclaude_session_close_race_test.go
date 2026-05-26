// internal/workforce/subprocess/openclaude_session_close_race_test.go
//
// Behavioural sister-test for inv-zen-289 (v0.20.6 fix #2): readLoop
// MUST treat fs.ErrClosed on stdout reads as benign end-of-stream
// during normal shutdown — surfacing it via closeErr falsifies the
// graceful-shutdown contract (callers rely on Close returning nil on
// clean exit).
//
// Race the fix closes: when exec.Cmd.Wait returns, the os/exec
// machinery explicitly closes both stdin and stdout pipe handles. Our
// readLoop may then be mid-read on stdout and observe fs.ErrClosed
// (a.k.a. os.ErrClosed; the OS-level "file already closed" wrap)
// instead of io.EOF. Pre-fix, readLoop stored this as
// `subprocess: readLoop: read |0: file already closed` in closeErr,
// which Close() returned to the caller — producing intermittent
// TestOpenClaudeSessionCloseUnblocksReceive failures under full-suite
// GOMAXPROCS contention (the race window opened only when other lanes
// slowed down the readLoop goroutine enough for the cmd.Wait close to
// land first).
//
// This test forces the race deterministically by closing the
// session's stdout pipe BEFORE calling Close(). readLoop hits
// fs.ErrClosed on its next read; Close() must still return nil.
package subprocess

import (
	"testing"
	"time"
)

func TestOpenClaudeSession_Close_BenignReadLoopErrClosed(t *testing.T) {
	wt := t.TempDir()
	id := ThreadID("tid-benign-errclosed")
	sess, err := newOpenClaudeSessionForTest(t, "happy-path", id, wt)
	if err != nil {
		t.Fatalf("newOpenClaudeSession: %v", err)
	}

	if err := sess.stdout.Close(); err != nil {
		t.Fatalf("pre-close stdout: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	if err := sess.Close(); err != nil {
		t.Fatalf("Close after stdout pre-close: err = %v, want nil; fs.ErrClosed in readLoop must be treated as benign end-of-stream during shutdown (inv-zen-289)", err)
	}
}
