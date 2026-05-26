// SPDX-License-Identifier: MIT
package subprocess

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const openClaudeBinary = "openclaude"

const defaultReadMaxFrameBytes = 16 << 20

type openClaudeOptions struct {
	Binary   string
	ThreadID ThreadID
	Worktree string

	commandFunc func(name string, arg ...string) *exec.Cmd
}

type openClaudeSession struct {
	id       ThreadID
	worktree string
	cmd      *exec.Cmd

	stdin  io.WriteCloser
	stdout io.ReadCloser

	sendCh chan Message
	recvCh chan Message

	closeOnce sync.Once
	closed    chan struct{}
	closeErr  atomic.Value

	killedByClose atomic.Bool

	exitCh chan struct{}

	closeGrace time.Duration

	readMaxFrameBytes int
}

func newOpenClaudeSession(opts openClaudeOptions) (*openClaudeSession, error) {
	if opts.Binary == "" {
		opts.Binary = openClaudeBinary
	}
	if opts.ThreadID.IsZero() {
		return nil, errors.New("subprocess: openClaudeSession requires non-empty ThreadID")
	}
	if opts.Worktree == "" {
		return nil, errors.New("subprocess: openClaudeSession requires non-empty Worktree")
	}
	if opts.commandFunc == nil {
		opts.commandFunc = exec.Command
	}

	args := []string{
		"--stdio",
		"--worktree", opts.Worktree,
		"--thread-id", string(opts.ThreadID),
	}
	cmd := opts.commandFunc(opts.Binary, args...)

	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("subprocess: StdinPipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("subprocess: StdoutPipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("subprocess: Start %s: %w", opts.Binary, err)
	}

	s := &openClaudeSession{
		id:                opts.ThreadID,
		worktree:          opts.Worktree,
		cmd:               cmd,
		stdin:             stdin,
		stdout:            stdout,
		sendCh:            make(chan Message, 8),
		recvCh:            make(chan Message, 16),
		closed:            make(chan struct{}),
		exitCh:            make(chan struct{}),
		closeGrace:        5 * time.Second,
		readMaxFrameBytes: defaultReadMaxFrameBytes,
	}
	go s.readLoop()
	go s.writeLoop()
	go s.waitLoop()
	return s, nil
}

func (s *openClaudeSession) ThreadID() ThreadID { return s.id }

func (s *openClaudeSession) Send(ctx context.Context, msg Message) error {
	if !msg.Kind.IsValid() {
		return fmt.Errorf("subprocess: Send: invalid MessageKind %v", msg.Kind)
	}
	select {
	case <-s.closed:
		return ErrSessionClosed
	default:
	}
	select {
	case s.sendCh <- msg:
		return nil
	case <-s.closed:
		return ErrSessionClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *openClaudeSession) Receive(ctx context.Context) (Message, error) {
	select {
	case msg := <-s.recvCh:
		return msg, nil
	case <-s.closed:

		select {
		case msg := <-s.recvCh:
			return msg, nil
		default:
			return Message{}, ErrSessionClosed
		}
	case <-ctx.Done():
		return Message{}, ctx.Err()
	}
}

func (s *openClaudeSession) Close() error {
	s.closeOnce.Do(func() {
		close(s.closed)
		_ = s.stdin.Close()

		select {
		case <-s.exitCh:
		case <-time.After(s.closeGrace):
			s.killedByClose.Store(true)
			_ = s.killGroup()
			<-s.exitCh
		}
	})
	if v := s.closeErr.Load(); v != nil {
		if err, ok := v.(error); ok {
			return err
		}
	}
	return nil
}

func (s *openClaudeSession) readLoop() {
	r := bufio.NewReaderSize(s.stdout, 1<<16)
	cap := s.readMaxFrameBytes
	if cap <= 0 {
		cap = defaultReadMaxFrameBytes
	}
	var line []byte
	for {
		chunk, err := r.ReadSlice('\n')

		if len(line)+len(chunk) > cap {
			s.closeErr.Store(fmt.Errorf("subprocess: readLoop: frame exceeds %d bytes", cap))
			return
		}

		line = append(line, chunk...)
		if errors.Is(err, bufio.ErrBufferFull) {

			continue
		}
		if len(line) > 0 && (err == nil || err == io.EOF) {

			msg, perr := parseFrame(line, s.id)
			if perr == nil {
				select {
				case s.recvCh <- msg:
				case <-s.closed:
					return
				}
			}

			line = line[:0]
		}
		if err == io.EOF {
			return
		}
		if err != nil && !errors.Is(err, bufio.ErrBufferFull) {

			if !errors.Is(err, fs.ErrClosed) {
				s.closeErr.Store(fmt.Errorf("subprocess: readLoop: %w", err))
			}
			return
		}
	}
}

func (s *openClaudeSession) writeLoop() {
	w := bufio.NewWriterSize(s.stdin, 1<<14)
	for {
		select {
		case <-s.closed:
			_ = w.Flush()
			return
		case msg := <-s.sendCh:
			b, err := encodeFrame(msg)
			if err != nil {

				s.closeErr.Store(fmt.Errorf("subprocess: writeLoop encode: %w", err))
				return
			}
			b = append(b, '\n')
			if _, err := w.Write(b); err != nil {
				s.closeErr.Store(fmt.Errorf("subprocess: write: %w", err))
				return
			}
			if err := w.Flush(); err != nil {
				s.closeErr.Store(fmt.Errorf("subprocess: flush: %w", err))
				return
			}
		}
	}
}

func (s *openClaudeSession) waitLoop() {
	err := s.cmd.Wait()
	if err != nil && !s.killedByClose.Load() {

		s.closeErr.Store(fmt.Errorf("subprocess: child exited: %w", err))
	}
	close(s.exitCh)

	s.closeOnce.Do(func() {
		close(s.closed)
		_ = s.stdin.Close()
	})
}

// getpgid returns the process group id for pid. Test seam allows
// injecting a Getpgid that fails so the fallback paths in
// killGroup/signalGroup are exercisable.
//
// CONCURRENCY WARNING: this is a package-level var. Tests that swap it
// MUST NOT use t.Parallel() — the swap races with concurrent tests in
// the same package that read this var via killGroup/signalGroup. Same
// constraint applies to kill below and randRead/jsonMarshal in their
// respective files.
var getpgid = syscall.Getpgid

// kill sends sig to the process whose id is given (negative pid for
// process-group target, per Kernighan's convention). Test seam.
//
// CONCURRENCY WARNING: package-level var; tests that swap it MUST NOT
// use t.Parallel() (see getpgid above for the rationale).
var kill = syscall.Kill

func (s *openClaudeSession) killGroup() error {
	if s.cmd == nil || s.cmd.Process == nil {
		return nil
	}
	pgid, err := getpgid(s.cmd.Process.Pid)
	if err == nil {
		return kill(-pgid, syscall.SIGKILL)
	}
	return s.cmd.Process.Kill()
}

func (s *openClaudeSession) signalGroup(sig syscall.Signal) error {
	if s.cmd == nil || s.cmd.Process == nil {
		return errors.New("subprocess: no process to signal")
	}
	pgid, err := getpgid(s.cmd.Process.Pid)
	if err == nil {
		return kill(-pgid, sig)
	}
	return s.cmd.Process.Signal(sig)
}

func (s *openClaudeSession) pid() int {
	if s.cmd == nil || s.cmd.Process == nil {
		return 0
	}
	return s.cmd.Process.Pid
}

func parseFrame(line []byte, defaultThread ThreadID) (Message, error) {
	if len(line) == 0 {
		return Message{}, errors.New("subprocess: empty frame")
	}
	var raw struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      any             `json:"id"`
		Method  string          `json:"method"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(line, &raw); err != nil {
		return Message{}, fmt.Errorf("subprocess: parse frame: %w", err)
	}
	msg := Message{ThreadID: defaultThread}
	switch v := raw.ID.(type) {
	case string:
		msg.ID = v
	case float64:
		msg.ID = fmt.Sprintf("%v", v)
	case nil:
		msg.ID = ""
	default:
		msg.ID = fmt.Sprintf("%v", v)
	}
	switch {
	case raw.Error != nil:
		msg.Kind = MessageKindError
		msg.ErrCode = raw.Error.Code
		msg.ErrMsg = raw.Error.Message
	case len(raw.Result) > 0:

		msg.Kind = MessageKindResult
		msg.Payload = raw.Result

		var probe struct {
			ThreadID string `json:"thread_id"`
		}
		if err := json.Unmarshal(raw.Result, &probe); err == nil && probe.ThreadID != "" {
			msg.ThreadID = ThreadID(probe.ThreadID)
		}
	case raw.Method != "":
		if msg.ID == "" {
			msg.Kind = MessageKindNotification
		} else {
			msg.Kind = MessageKindRequest
		}
		msg.Method = raw.Method
		msg.Payload = raw.Params
	default:
		return Message{}, errors.New("subprocess: frame has no method/result/error")
	}
	return msg, nil
}

// NewOpenClaudeSessionForCompliance is a constructor exported solely for
// the tests/compliance/ package, which lives outside this Go package and
// therefore cannot reach the unexported newOpenClaudeSession.
//
// MUST NOT be called from production code: it bypasses Manager bookkeeping
// (registry tracking, eviction, store persistence). The intentional
// boundary is that production paths go through Manager.SpawnEphemeral or
// Manager.AcquirePersistent, which both go through Factory ->
// newOpenClaudeSession.
//
// The cf parameter is the same command-function injection point used by
// in-package tests (commandFunc on openClaudeOptions); it lets compliance
// suites re-exec the test binary as the openclaude_fake helper.
func NewOpenClaudeSessionForCompliance(id ThreadID, worktree string, cf func(name string, arg ...string) *exec.Cmd) (Session, error) {
	sess, err := newOpenClaudeSession(openClaudeOptions{
		Binary:      "openclaude",
		ThreadID:    id,
		Worktree:    worktree,
		commandFunc: cf,
	})
	if err == nil {

		sess.closeGrace = 200 * time.Millisecond
	}
	return sess, err
}

func encodeFrame(msg Message) ([]byte, error) {
	frame := map[string]any{
		"jsonrpc": "2.0",
	}
	switch msg.Kind {
	case MessageKindRequest:
		frame["id"] = msg.ID
		frame["method"] = msg.Method
		if len(msg.Payload) > 0 {
			frame["params"] = json.RawMessage(msg.Payload)
		}
	case MessageKindNotification:
		frame["method"] = msg.Method
		if len(msg.Payload) > 0 {
			frame["params"] = json.RawMessage(msg.Payload)
		}
	case MessageKindResult:
		frame["id"] = msg.ID
		frame["result"] = json.RawMessage(msg.Payload)
	case MessageKindError:
		frame["id"] = msg.ID
		frame["error"] = map[string]any{
			"code":    msg.ErrCode,
			"message": msg.ErrMsg,
		}
	default:
		return nil, fmt.Errorf("subprocess: cannot encode kind %v", msg.Kind)
	}
	return json.Marshal(frame)
}
