// tests/compliance/inv_zen_289_test.go
//
// Compliance gate for invariant (v0.20.6 fix #2): the
// internal/workforce/subprocess openClaudeSession.readLoop MUST treat
// fs.ErrClosed (a.k.a. os.ErrClosed; the OS-level "file already
// closed" wrap) as benign end-of-stream during normal shutdown
// instead of storing it in closeErr.
//
// Why this gate exists: when exec.Cmd.Wait returns after subprocess
// exit, os/exec explicitly closes both stdin and stdout pipe handles.
// readLoop may then be mid-read on stdout and observe fs.ErrClosed
// from the bufio.Reader.ReadSlice call. Pre-fix, readLoop stored
// this as `subprocess: readLoop: read |0: file already closed` in
// closeErr, which Close() returned to its caller — falsifying the
// graceful-shutdown contract (Close MUST return nil on clean exit).
// The bug surfaced as intermittent
// TestOpenClaudeSessionCloseUnblocksReceive failures under full-suite
// GOMAXPROCS contention (the race window opened only when other
// lanes slowed down the readLoop goroutine enough for cmd.Wait to
// land the pipe-close first).
//
// Fix shape: readLoop's `if err != nil && !errors.Is(err, bufio.ErrBufferFull)`
// branch now wraps its `closeErr.Store(...)` in an
// `if !errors.Is(err, fs.ErrClosed)` guard.
//
// Anchor 1 (positive): openclaude_session.go MUST import "io/fs"
// (required by the fs.ErrClosed reference; if the guard is removed,
// `go build` flags the unused import — a strong bite-check signal).
//
// Anchor 2 (positive): openclaude_session.go MUST contain the literal
// `errors.Is(err, fs.ErrClosed)` (proves the guard is in place at the
// readLoop call site).
//
// Sister-test bite check: remove the guard (the `if !errors.Is(err,
// fs.ErrClosed) {... }` wrap) — Anchor 2 fails; if "io/fs" is also
// removed (the natural follow-up cleanup), Anchor 1 fails as well.
// The behavioural sister-test
// `TestOpenClaudeSession_Close_BenignReadLoopErrClosed` in the
// internal subprocess package independently catches the regression
// at test-run time (Close returns non-nil after pre-closing stdout).
//
// invariant (v0.20.6 fix #2).
package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const inv289OpenClaudeSessionPath = "internal/workforce/subprocess/openclaude_session.go"

func TestInvZen289_ImportsIoFs(t *testing.T) {
	abs, err := filepath.Abs(filepath.Join("..", "..", inv289OpenClaudeSessionPath))
	if err != nil {
		t.Fatalf("resolve %s: %v", inv289OpenClaudeSessionPath, err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read %s: %v", abs, err)
	}
	src := string(data)
	required := "\"io/fs\""
	if !strings.Contains(src, required) {
		t.Errorf("inv-zen-289 violated: %s does not import %s. The fs.ErrClosed guard in readLoop requires this import; if the import was removed, the guard cannot exist and the shutdown-race bug recurs.", inv289OpenClaudeSessionPath, required)
	}
}

func TestInvZen289_ReadLoopGuardsFsErrClosed(t *testing.T) {
	abs, err := filepath.Abs(filepath.Join("..", "..", inv289OpenClaudeSessionPath))
	if err != nil {
		t.Fatalf("resolve %s: %v", inv289OpenClaudeSessionPath, err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read %s: %v", abs, err)
	}
	src := string(data)
	required := "errors.Is(err, fs.ErrClosed)"
	if !strings.Contains(src, required) {
		t.Errorf("inv-zen-289 violated: %s does not contain the literal %q. readLoop must guard its closeErr.Store(...) on the fs.ErrClosed branch so the shutdown race (cmd.Wait closes pipes before readLoop drains buffer) is treated as benign end-of-stream instead of being surfaced via Close().", inv289OpenClaudeSessionPath, required)
	}
}
