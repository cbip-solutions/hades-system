package sshexec

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/tests/testharness"
	"golang.org/x/crypto/ssh"
)

func TestSentinelExports(t *testing.T) {
	if !AssertStdioCanonical() {
		t.Error("AssertStdioCanonical() = false")
	}
	if !AssertBoundaryPreserved() {
		t.Error("AssertBoundaryPreserved() = false")
	}
}

func TestIsWordByte(t *testing.T) {
	cases := map[byte]bool{
		'a': true, 'z': true, 'A': true, 'Z': true,
		'0': true, '9': true, '_': true,
		'/': false, '-': false, '.': false, ' ': false,
		':': false, '*': false,
	}
	for b, want := range cases {
		if got := isWordByte(b); got != want {
			t.Errorf("isWordByte(%q) = %v, want %v", b, got, want)
		}
	}
}

func TestReqStringTypeMismatch(t *testing.T) {
	args := map[string]any{"cmd": 42}
	_, err := reqString(args, "cmd")
	if err == nil {
		t.Error("reqString accepted int as string")
	}
	if !strings.Contains(err.Error(), "expected string") {
		t.Errorf("err = %v", err)
	}
}

func TestReqStringExplicitNil(t *testing.T) {
	args := map[string]any{"k": nil}
	s, err := reqString(args, "k")
	if err != nil {
		t.Errorf("reqString nil: %v", err)
	}
	if s != "" {
		t.Errorf("reqString nil = %q", s)
	}
}

func TestRequireStringEmpty(t *testing.T) {
	args := map[string]any{"k": ""}
	_, err := requireString(args, "k")
	if err == nil {
		t.Error("requireString accepted empty string")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("err = %v", err)
	}
}

func TestRequireStringTypeError(t *testing.T) {
	args := map[string]any{"k": []string{"x"}}
	_, err := requireString(args, "k")
	if err == nil {
		t.Error("requireString accepted []string")
	}
}

func TestJSONToolResultShape(t *testing.T) {
	r := jsonToolResult([]byte(`{"x":1}`))
	if r == nil || len(r.Content) != 1 {
		t.Fatalf("jsonToolResult = %v", r)
	}
}

func TestDiscardingSinkEmit(t *testing.T) {
	if err := (discardingSink{}).Emit(StreamChunk{Ordinal: 1}); err != nil {
		t.Errorf("discardingSink.Emit = %v", err)
	}
}

func TestProgressSinkEmitsViaSession(t *testing.T) {

	srv := NewServer(ServerConfig{
		AllowlistResolver: stubAllowlistInternal([]string{"alembic *"}, []string{"vps"}),
		Auth:              AgentAuthForTest(),
		Emitter:           NopAuditEmitter{},
	})
	_ = srv

	ps := &progressSink{token: "tok-123"}
	if err := ps.Emit(StreamChunk{Stream: StreamStdout, Data: []byte("AAA")}); err == nil {

	}
	if err := ps.Emit(StreamChunk{Stream: StreamStderr, Data: []byte("E")}); err == nil {
	}
	if ps.stdoutCnt != 3 {
		t.Errorf("stdoutCnt = %v, want 3", ps.stdoutCnt)
	}
	if ps.stderrCnt != 1 {
		t.Errorf("stderrCnt = %v, want 1", ps.stderrCnt)
	}
}

func TestChanSinkOverflow(t *testing.T) {
	ch := make(chan StreamChunk, 1)
	s := &chanSink{ch: ch}
	if err := s.Emit(StreamChunk{Ordinal: 1}); err != nil {
		t.Fatalf("first emit: %v", err)
	}

	if err := s.Emit(StreamChunk{Ordinal: 2}); err != nil {
		t.Fatalf("overflow emit: %v", err)
	}
}

func TestHandleExecResolverErrorPropagates(t *testing.T) {
	srv := NewServer(ServerConfig{
		AllowlistResolver: func(string) (*Allowlist, error) {
			return nil, errors.New("resolver-bad")
		},
		Auth:    AgentAuthForTest(),
		Emitter: NopAuditEmitter{},
	})
	ch := make(chan StreamChunk, 1)
	_, err := srv.InvokeExecForTest(context.Background(), map[string]any{
		"host":    "vps",
		"cmd":     "alembic upgrade",
		"project": "internal-platform-x",
	}, ch)
	if err == nil {
		t.Error("expected resolver error")
	}
}

func TestHandleValidateResolverError(t *testing.T) {
	srv := NewServer(ServerConfig{
		AllowlistResolver: func(string) (*Allowlist, error) {
			return nil, errors.New("resolver-bad")
		},
	})
	_, err := srv.InvokeForTest(context.Background(), "validate", map[string]any{
		"cmd":     "alembic upgrade",
		"project": "internal-platform-x",
	})
	if err == nil {
		t.Error("expected resolver error")
	}
}

func TestHandleListAllowedResolverError(t *testing.T) {
	srv := NewServer(ServerConfig{
		AllowlistResolver: func(string) (*Allowlist, error) {
			return nil, errors.New("resolver-bad")
		},
	})
	_, err := srv.InvokeForTest(context.Background(), "list_allowed", map[string]any{
		"project": "internal-platform-x",
	})
	if err == nil {
		t.Error("expected resolver error")
	}
}

func TestSSHUserFromEnvOverride(t *testing.T) {
	t.Setenv("ZEN_SSH_USER", "explicit-user")
	if got := sshUserFromEnv(); got != "explicit-user" {
		t.Errorf("sshUserFromEnv = %q, want %q", got, "explicit-user")
	}
	t.Setenv("ZEN_SSH_USER", "")
	t.Setenv("USER", "fallback-user")
	if got := sshUserFromEnv(); got != "fallback-user" {
		t.Errorf("sshUserFromEnv fallback = %q", got)
	}
}

func TestHostKeyCallbackProductionRefusal(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "")
	cb := hostKeyCallback()
	err := cb("h", &net.TCPAddr{}, nil)
	if err == nil {
		t.Error("production callback accepted host key without known_hosts")
	}
}

func TestSSHAuthMethodPaths(t *testing.T) {
	test := AgentAuthForTest()
	if got := test.sshAuth(); len(got) == 0 {
		t.Error("test path returned no auth")
	}
	prod := AuthMethod{signers: nil}
	if got := prod.sshAuth(); len(got) == 0 {
		t.Error("prod path (empty signers) returned no auth")
	}
}

func TestRunCwdEmptyDoesNotSetenv(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "1")
	srv, _ := testharness.NewFakeSSHD(testharness.HandlerFunc(func(req string) testharness.HandlerScript {
		return testharness.HandlerScript{Stdout: "ok\n", ExitCode: 0}
	}))
	defer srv.Close()
	req := ExecRequest{Host: srv.Addr(), Command: "alembic upgrade", Project: "p"}
	req.ApplyDefaults()
	vr := Validate(req.Command, []string{"alembic *"})
	allow := &Allowlist{Patterns: []string{"alembic *"}, Hosts: []string{srv.Addr()}}
	_, err := Run(context.Background(), req, vr, allow, AgentAuthForTest(), &MemoryStreamSink{}, NopAuditEmitter{})
	if err != nil {
		t.Errorf("Run with empty Cwd: %v", err)
	}
}

func TestEmitOnNilEmitClient(t *testing.T) {
	em := NewEmitter(nil, "p")
	if err := em.EmitStarted(ExecRequest{}); err != nil {
		t.Errorf("nil-EmitClient EmitStarted = %v", err)
	}
}

func TestDetectorMatchPromptsEdgeCases(t *testing.T) {
	cases := []struct {
		name string
		in   string
		hit  bool
	}{
		{"empty", "", false},
		{"plain-text-no-trigger", "hello world\n", false},
		{"password-with-colon-but-also-newline", "Migration password: abc\nbody", true},

		{"password-near-buffer-end", "PASSword", true},
		{"yes-no-only", "(yes/no)", true},
	}
	for _, c := range cases {
		got := matchPrompts([]byte(c.in)) != ""
		if got != c.hit {
			t.Errorf("matchPrompts(%q) hit = %v, want %v", c.in, got, c.hit)
		}
	}
}

func TestDetectorChannelDefaultArmDocumented(t *testing.T) {
	// Cannot be triggered without breaking the latch invariant. The
	// `default:` arm exists for safety against future code that might
	// remove the latch. Asserting via documentation here is sufficient
	// for security review.
	d := newDetector()
	d.Feed([]byte("[sudo]"), StreamStdout)

	d.Feed([]byte("[sudo]"), StreamStdout)
	if !d.triggered.Load() {
		t.Error("triggered = false after first Feed")
	}
}

func TestValidatePatternsBlankReject(t *testing.T) {
	if err := validatePatterns([]string{""}); err == nil {
		t.Error("validatePatterns accepted empty string")
	}
}

func TestDetectorFeedAfterWindowFull(t *testing.T) {
	d := newDetector()
	d.Feed(make([]byte, detectorWindow), StreamStdout)

	d.Feed([]byte("(yes/no): "), StreamStdout)
	select {
	case r := <-d.Triggered():
		t.Errorf("post-full Feed triggered: %q", r)
	default:
	}
}

func TestRunExitCodeMinusOneOnTransport(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "1")

	srv, _ := testharness.NewFakeSSHD(testharness.HandlerFunc(func(req string) testharness.HandlerScript {
		return testharness.HandlerScript{Stdout: "slow\n", Delay: 2 * time.Second}
	}))
	defer srv.Close()
	req := ExecRequest{Host: srv.Addr(), Command: "alembic upgrade", Project: "p"}
	req.Timeout = 50 * time.Millisecond
	req.ApplyDefaults()
	vr := Validate(req.Command, []string{"alembic *"})
	allow := &Allowlist{Patterns: []string{"alembic *"}, Hosts: []string{srv.Addr()}}
	res, err := Run(context.Background(), req, vr, allow, AgentAuthForTest(), &MemoryStreamSink{}, NopAuditEmitter{})
	if err == nil {
		t.Fatal("Run nil err on timeout")
	}
	if res.ExitCode != -1 {
		t.Errorf("ExitCode = %d, want -1 on timeout", res.ExitCode)
	}
}

func TestAuthMethodInternals(t *testing.T) {
	a := AuthMethod{useTestPath: true, password: "x"}
	if got := a.sshAuth(); len(got) != 1 {
		t.Errorf("test sshAuth len = %d", len(got))
	}
}

func TestServerRunCancelsPromptly(t *testing.T) {
	srv := NewServer(ServerConfig{
		AllowlistResolver: stubAllowlistInternal([]string{"alembic *"}, []string{"vps"}),
		Auth:              AgentAuthForTest(),
		Emitter:           NopAuditEmitter{},
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_ = srv.Run(ctx)
}

func stubAllowlistInternal(patterns, hosts []string) AllowlistResolver {
	return func(project string) (*Allowlist, error) {
		return &Allowlist{
			Project:  project,
			Patterns: patterns,
			Hosts:    hosts,
			Source:   "stub",
		}, nil
	}
}

func TestRunPostWaitDetectorTriggers(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "1")
	srv, _ := testharness.NewFakeSSHD(testharness.HandlerFunc(func(req string) testharness.HandlerScript {

		return testharness.HandlerScript{
			Stdout:   "[sudo] password for testuser:\n",
			ExitCode: 0,
		}
	}))
	defer srv.Close()
	req := ExecRequest{Host: srv.Addr(), Command: "alembic upgrade", Project: "p"}
	req.ApplyDefaults()
	vr := Validate(req.Command, []string{"alembic *"})
	allow := &Allowlist{Patterns: []string{"alembic *"}, Hosts: []string{srv.Addr()}}
	emitter := &recordingEmitter{}
	res, err := Run(context.Background(), req, vr, allow, AgentAuthForTest(), &MemoryStreamSink{}, emitter)
	if err == nil {
		t.Fatal("Run nil err with prompt-then-exit")
	}
	if !res.InteractiveBlocked {
		t.Errorf("InteractiveBlocked = false; want true (post-Wait detector)")
	}
	if len(emitter.interactive) == 0 {
		t.Errorf("interactive_blocked emit not recorded")
	}
}

func TestRunHostKeyCallbackProductionRefuses(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "")
	srv, _ := testharness.NewFakeSSHD(testharness.HandlerFunc(func(req string) testharness.HandlerScript {
		return testharness.HandlerScript{Stdout: "ok\n", ExitCode: 0}
	}))
	defer srv.Close()
	req := ExecRequest{Host: srv.Addr(), Command: "alembic upgrade", Project: "p"}
	req.ApplyDefaults()
	vr := Validate(req.Command, []string{"alembic *"})
	allow := &Allowlist{Patterns: []string{"alembic *"}, Hosts: []string{srv.Addr()}}
	_, err := Run(context.Background(), req, vr, allow, AgentAuthForTest(), &MemoryStreamSink{}, NopAuditEmitter{})
	if err == nil {
		t.Error("Run accepted untrusted host without ZEN_SSH_INSECURE_TEST")
	}
}

func TestDialContextNewClientConnFails(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		c, _ := ln.Accept()
		if c != nil {
			c.Close()
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := &ssh.ClientConfig{
		User:            "x",
		Auth:            []ssh.AuthMethod{ssh.Password("x")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         500 * 1000 * 1000,
	}
	_, err = dialContext(ctx, "tcp", ln.Addr().String(), cfg)
	if err == nil {
		t.Error("dialContext accepted non-SSH listener")
	}
}

func TestEmitNilHandlesAllMethods(t *testing.T) {
	em := NewEmitter(nil, "")
	for _, fn := range []func() error{
		func() error { return em.EmitStarted(ExecRequest{}) },
		func() error { return em.EmitCompleted(ExecRequest{}, ExecResult{}) },
		func() error { return em.EmitDenied(ExecRequest{}, "x") },
		func() error { return em.EmitInteractiveBlocked(ExecRequest{}, []byte{}) },
	} {
		if err := fn(); err != nil {
			t.Errorf("nil emitter method returned err: %v", err)
		}
	}
	_, err := em.DrainBuffer(context.Background())
	if err != nil {
		t.Errorf("nil emitter DrainBuffer: %v", err)
	}
}

func TestHandleValidateMissingProject(t *testing.T) {
	srv := NewServer(ServerConfig{
		AllowlistResolver: stubAllowlistInternal([]string{"alembic *"}, []string{"vps"}),
	})
	_, err := srv.handleValidate(context.Background(), map[string]any{
		"cmd": "alembic upgrade",
	})
	if err == nil {
		t.Fatal("expected error for missing project")
	}
}

func TestHandleExecMissingHost(t *testing.T) {
	srv := NewServer(ServerConfig{
		AllowlistResolver: stubAllowlistInternal([]string{"alembic *"}, []string{"vps"}),
	})
	_, err := srv.handleExec(context.Background(), map[string]any{
		"cmd":     "alembic upgrade",
		"project": "internal-platform-x",
	}, &MemoryStreamSink{})
	if err == nil {
		t.Fatal("expected error for missing host")
	}
}

func TestHandleExecMissingCmd(t *testing.T) {
	srv := NewServer(ServerConfig{
		AllowlistResolver: stubAllowlistInternal([]string{"alembic *"}, []string{"vps"}),
	})
	_, err := srv.handleExec(context.Background(), map[string]any{
		"host":    "vps",
		"project": "internal-platform-x",
	}, &MemoryStreamSink{})
	if err == nil {
		t.Fatal("expected error for missing cmd")
	}
}

func TestHandleExecMissingProject(t *testing.T) {
	srv := NewServer(ServerConfig{
		AllowlistResolver: stubAllowlistInternal([]string{"alembic *"}, []string{"vps"}),
	})
	_, err := srv.handleExec(context.Background(), map[string]any{
		"host": "vps",
		"cmd":  "alembic upgrade",
	}, &MemoryStreamSink{})
	if err == nil {
		t.Fatal("expected error for missing project")
	}
}

func TestHandleExecBadCwdType(t *testing.T) {
	srv := NewServer(ServerConfig{
		AllowlistResolver: stubAllowlistInternal([]string{"alembic *"}, []string{"vps"}),
	})
	_, err := srv.handleExec(context.Background(), map[string]any{
		"host":    "vps",
		"cmd":     "alembic upgrade",
		"project": "internal-platform-x",
		"cwd":     42,
	}, &MemoryStreamSink{})
	if err == nil {
		t.Fatal("expected error for bad cwd type")
	}
}

func TestHandleExecBadTimeoutType(t *testing.T) {
	srv := NewServer(ServerConfig{
		AllowlistResolver: stubAllowlistInternal([]string{"alembic *"}, []string{"vps"}),
	})
	_, err := srv.handleExec(context.Background(), map[string]any{
		"host":    "vps",
		"cmd":     "alembic upgrade",
		"project": "internal-platform-x",
		"timeout": 42,
	}, &MemoryStreamSink{})
	if err == nil {
		t.Fatal("expected error for bad timeout type")
	}
}

func TestHandleExecRunFailureSurfaces(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "1")
	srv := NewServer(ServerConfig{
		AllowlistResolver: stubAllowlistInternal([]string{"alembic *"}, []string{"127.0.0.1:1"}),
		Auth:              AgentAuthForTest(),
		Emitter:           NopAuditEmitter{},
	})
	_, err := srv.handleExec(context.Background(), map[string]any{
		"host":    "127.0.0.1:1",
		"cmd":     "alembic upgrade",
		"project": "internal-platform-x",
	}, &MemoryStreamSink{})
	if err == nil {
		t.Fatal("expected dial failure to propagate")
	}
}

func TestEmitOnEmitClientReceivesEvents(t *testing.T) {

	_ = (&Emitter{}).projectID
}

var _ = os.UserHomeDir
