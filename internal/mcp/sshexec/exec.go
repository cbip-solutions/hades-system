// SPDX-License-Identifier: MIT
// internal/mcp/sshexec/exec.go
//
// Tasks L-5 (basic exec) + L-6 (streaming + truncation) +
// L-7 wiring (interactive detector hook).
//
// Hard rules enforced by this file:
// - golang.org/x/crypto/ssh direct only; NO os/exec import.
// - SSH credentials come only from SSH_AUTH_SOCK; private keys are
// never read from disk.
// - Host keys are verified through known_hosts unless the test-only
// ZEN_SSH_INSECURE_TEST=1 escape is set.
// - Run signature requires ValidationResult with OK=true (compile-check
// anchor for invariant).
// - PTY=false on every session (no interactive shells; sess.RequestPty
// never called).

package sshexec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

type AuthMethod struct {
	signers     []ssh.Signer
	password    string
	useTestPath bool
}

func AgentAuth() (AuthMethod, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return AuthMethod{}, errors.New("SSH_AUTH_SOCK unset; agent required (no ~/.ssh fallback)")
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return AuthMethod{}, fmt.Errorf("dial agent: %w", err)
	}
	ag := agent.NewClient(conn)
	signers, err := ag.Signers()
	if err != nil {
		return AuthMethod{}, fmt.Errorf("agent.Signers: %w", err)
	}
	if len(signers) == 0 {
		return AuthMethod{}, errors.New("agent has no identities")
	}
	return AuthMethod{signers: signers}, nil
}

func AgentAuthForTest() AuthMethod {
	return AuthMethod{useTestPath: true, password: "ignored"}
}

func (a AuthMethod) sshAuth() []ssh.AuthMethod {
	if a.useTestPath {
		return []ssh.AuthMethod{ssh.Password(a.password)}
	}
	return []ssh.AuthMethod{ssh.PublicKeys(a.signers...)}
}

type StreamSink interface {
	Emit(chunk StreamChunk) error
}

type AuditEmitter interface {
	EmitStarted(req ExecRequest) error
	EmitCompleted(req ExecRequest, res ExecResult) error
	EmitDenied(req ExecRequest, reason string) error
	EmitInteractiveBlocked(req ExecRequest, snippet []byte) error
}

type NopAuditEmitter struct{}

func (NopAuditEmitter) EmitStarted(ExecRequest) error { return nil }

func (NopAuditEmitter) EmitCompleted(ExecRequest, ExecResult) error { return nil }

func (NopAuditEmitter) EmitDenied(ExecRequest, string) error { return nil }

func (NopAuditEmitter) EmitInteractiveBlocked(ExecRequest, []byte) error { return nil }

type MemoryStreamSink struct {
	mu     sync.Mutex
	stdout []byte
	stderr []byte
	chunks []StreamChunk
}

func (m *MemoryStreamSink) Emit(c StreamChunk) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data := append([]byte(nil), c.Data...)
	m.chunks = append(m.chunks, StreamChunk{Ordinal: c.Ordinal, Stream: c.Stream, Data: data})
	if c.Stream == StreamStdout {
		m.stdout = append(m.stdout, data...)
	} else {
		m.stderr = append(m.stderr, data...)
	}
	return nil
}

func (m *MemoryStreamSink) StdoutString() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return string(m.stdout)
}

func (m *MemoryStreamSink) StderrString() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return string(m.stderr)
}

func (m *MemoryStreamSink) Chunks() []StreamChunk {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]StreamChunk, len(m.chunks))
	for i, c := range m.chunks {
		data := append([]byte(nil), c.Data...)
		out[i] = StreamChunk{Ordinal: c.Ordinal, Stream: c.Stream, Data: data}
	}
	return out
}

func Run(
	ctx context.Context,
	req ExecRequest,
	vr ValidationResult,
	allow *Allowlist,
	auth AuthMethod,
	sink StreamSink,
	emitter AuditEmitter,
) (ExecResult, error) {
	if !vr.OK {
		_ = emitter.EmitDenied(req, "validation not ok: "+vr.Reason)
		return ExecResult{}, fmt.Errorf("validation not ok: %s", vr.Reason)
	}
	if allow == nil {

		_ = emitter.EmitDenied(req, "nil allowlist")
		return ExecResult{}, errors.New("nil Allowlist")
	}
	if !allow.HostAllowed(req.Host) {
		_ = emitter.EmitDenied(req, "host not in allowlist")
		return ExecResult{}, fmt.Errorf("host not in allowlist: %s", req.Host)
	}

	if err := ValidateCwd(req.Cwd); err != nil {
		_ = emitter.EmitDenied(req, "cwd validation: "+err.Error())
		return ExecResult{}, fmt.Errorf("cwd validation: %w", err)
	}
	if err := emitter.EmitStarted(req); err != nil {
		return ExecResult{}, fmt.Errorf("audit emit started: %w", err)
	}

	cfg := &ssh.ClientConfig{
		User:            sshUserFromEnv(),
		Auth:            auth.sshAuth(),
		HostKeyCallback: hostKeyCallback(),
		Timeout:         15 * time.Second,
	}

	dialCtx, dialCancel := context.WithTimeout(ctx, 15*time.Second)
	defer dialCancel()
	conn, err := dialContext(dialCtx, "tcp", req.Host, cfg)
	if err != nil {
		_ = emitter.EmitDenied(req, "dial: "+err.Error())
		return ExecResult{}, fmt.Errorf("dial %s: %w", req.Host, err)
	}
	defer conn.Close()

	sess, err := conn.NewSession()
	if err != nil {

		_ = emitter.EmitDenied(req, "new session: "+err.Error())
		return ExecResult{}, fmt.Errorf("new session: %w", err)
	}
	defer sess.Close()

	// PTY=false: no shell, no interactive. We deliberately do NOT call
	// sess.RequestPty.
	stdoutPipe, err := sess.StdoutPipe()
	if err != nil {
		return ExecResult{}, fmt.Errorf("stdout pipe: %w", err)
	}
	stderrPipe, err := sess.StderrPipe()
	if err != nil {
		return ExecResult{}, fmt.Errorf("stderr pipe: %w", err)
	}

	commandLine := req.Command
	if req.Cwd != "" {
		// ForceCommand wrapper handles cwd semantics; we do not embed `cd`
		// because ';' is a forbidden char. The wrapper accepts a CWD
		// envelope via SSH env var: pass via $ZEN_CWD; wrapper recognises
		// it.
		//
		// ZEN_CWD_REQUESTED=1 is a sentinel the wrapper checks: if it
		// arrives but ZEN_CWD itself didn't (sshd AcceptEnv missing for
		// ZEN_CWD), the wrapper fails loud instead of silently falling
		// back to $HOME. Both env vars must be in the host's
		// AcceptEnv list — see wrapper header for sshd_config setup.
		_ = sess.Setenv("ZEN_CWD", req.Cwd)
		_ = sess.Setenv("ZEN_CWD_REQUESTED", "1")
	}
	_ = sess.Setenv("ZEN_PROJECT", req.Project)

	start := time.Now()
	if err := sess.Start(commandLine); err != nil {
		return ExecResult{}, fmt.Errorf("session start: %w", err)
	}

	res := ExecResult{ExitReason: ExitReasonNormal}
	det := newDetector()
	wg := sync.WaitGroup{}
	wg.Add(2)
	// copyStream goroutines drain to EOF — they do not honour ctx
	// cancellation so we don't drop in-flight bytes when the session
	// terminates. Truncation is enforced inside copyStream by capacity
	// (still drain remote pipe so it doesn't block) and the detector
	// is fed concurrently.
	go func() {
		defer wg.Done()
		copyStream(sink, det, stdoutPipe, StreamStdout, req.MaxStdout, &res.StdoutBytes, &res.StdoutTruncated)
	}()
	go func() {
		defer wg.Done()
		copyStream(sink, det, stderrPipe, StreamStderr, req.MaxStderr, &res.StderrBytes, &res.StderrTruncated)
	}()

	watchdogCtx, watchdogCancel := context.WithTimeout(ctx, req.Timeout)
	defer watchdogCancel()
	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()

	var execErr error
	select {
	case execErr = <-done:

		streamsDone := make(chan struct{})
		go func() { wg.Wait(); close(streamsDone) }()
		select {
		case <-streamsDone:
		case <-time.After(200 * time.Millisecond):
		}

		select {
		case reason := <-det.Triggered():
			res.InteractiveBlocked = true
			res.BlockedReason = reason
			res.ExitReason = ExitReasonInteractiveBlocked
			execErr = errors.New("interactive prompt blocked: " + reason)
			_ = emitter.EmitInteractiveBlocked(req, det.Snippet())
		default:
		}
	case <-watchdogCtx.Done():
		_ = sess.Signal(ssh.SIGKILL)
		_ = sess.Close()
		execErr = watchdogCtx.Err()
		res.ExitReason = ExitReasonTimeout

		<-done
	case reason := <-det.Triggered():
		_ = sess.Signal(ssh.SIGKILL)
		_ = sess.Close()
		res.InteractiveBlocked = true
		res.BlockedReason = reason
		res.ExitReason = ExitReasonInteractiveBlocked
		execErr = errors.New("interactive prompt blocked: " + reason)
		_ = emitter.EmitInteractiveBlocked(req, det.Snippet())
		<-done
	}

	wg.Wait()
	res.Duration = time.Since(start)
	if exitErr, ok := asExitError(execErr); ok {
		res.ExitCode = exitErr.ExitStatus()

	} else if execErr != nil && !res.InteractiveBlocked {
		res.ExitCode = -1

		if res.ExitReason == ExitReasonNormal {
			res.ExitReason = ExitReasonTransport
		}
	}

	if !res.InteractiveBlocked {
		_ = emitter.EmitCompleted(req, res)
	}

	if execErr != nil && !res.InteractiveBlocked && !isExitError(execErr) {

		return res, execErr
	}
	if res.InteractiveBlocked {
		return res, execErr
	}
	return res, nil
}

func copyStream(
	sink StreamSink,
	det *Detector,
	src io.Reader,
	label StreamLabel,
	cap int64,
	bytesField *int64,
	truncatedField *bool,
) {
	buf := make([]byte, 32*1024)
	var ord int64
	for {
		n, err := src.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			det.Feed(data, label)
			already := atomic.LoadInt64(bytesField)
			remaining := cap - already
			atomic.AddInt64(bytesField, int64(n))
			if remaining <= 0 {
				*truncatedField = true

			} else if int64(n) > remaining {
				out := data[:remaining]
				*truncatedField = true
				ord++
				_ = sink.Emit(StreamChunk{Ordinal: ord, Stream: label, Data: out})
			} else {
				ord++
				_ = sink.Emit(StreamChunk{Ordinal: ord, Stream: label, Data: data})
			}
		}
		if err != nil {
			return
		}
	}
}

func asExitError(err error) (*ssh.ExitError, bool) {
	var e *ssh.ExitError
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

func isExitError(err error) bool { _, ok := asExitError(err); return ok }

func dialContext(ctx context.Context, network, addr string, cfg *ssh.ClientConfig) (*ssh.Client, error) {
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return ssh.NewClient(c, chans, reqs), nil
}

func hostKeyCallback() ssh.HostKeyCallback {
	if os.Getenv("ZEN_SSH_INSECURE_TEST") == "1" {
		return ssh.InsecureIgnoreHostKey()
	}
	paths := knownHostsPaths()
	if len(paths) == 0 {
		return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return errors.New("known_hosts unavailable; set ZEN_SSH_KNOWN_HOSTS or create ~/.ssh/known_hosts")
		}
	}
	cb, err := knownhosts.New(paths...)
	if err == nil {
		return cb
	}
	msg := "known_hosts unavailable at " + strings.Join(paths, ", ") + ": " + err.Error()
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		return errors.New(msg)
	}
}

func knownHostsPaths() []string {
	if raw := os.Getenv("ZEN_SSH_KNOWN_HOSTS"); raw != "" {
		return splitKnownHostsEnv(raw)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	candidates := []string{
		filepath.Join(home, ".ssh", "known_hosts"),
		filepath.Join(home, ".ssh", "known_hosts2"),
	}
	var existing []string
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			existing = append(existing, path)
		}
	}
	return existing
}

func splitKnownHostsEnv(raw string) []string {
	parts := strings.Split(raw, string(os.PathListSeparator))
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			paths = append(paths, part)
		}
	}
	return paths
}

func sshUserFromEnv() string {
	if u := os.Getenv("ZEN_SSH_USER"); u != "" {
		return u
	}
	return os.Getenv("USER")
}
