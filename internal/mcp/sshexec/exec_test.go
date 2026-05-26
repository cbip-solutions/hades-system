package sshexec_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/mcp/sshexec"
	"github.com/cbip-solutions/hades-system/tests/testharness"
)

func TestRunHappyPath(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "1")
	srv, err := testharness.NewFakeSSHD(testharness.HandlerFunc(func(req string) testharness.HandlerScript {
		return testharness.HandlerScript{Stdout: "alembic ok\n", ExitCode: 0}
	}))
	if err != nil {
		t.Fatalf("NewFakeSSHD: %v", err)
	}
	defer srv.Close()

	req := sshexec.ExecRequest{
		Host:    srv.Addr(),
		Command: "alembic upgrade head",
		Project: "internal-platform-x",
	}
	req.ApplyDefaults()
	vr := sshexec.Validate(req.Command, []string{"alembic *"})
	if !vr.OK {
		t.Fatalf("validate setup wrong: %s", vr.Reason)
	}
	allow := &sshexec.Allowlist{
		Project:  "internal-platform-x",
		Patterns: []string{"alembic *"},
		Hosts:    []string{srv.Addr()},
		Source:   "test",
	}
	collector := &sshexec.MemoryStreamSink{}
	auth := sshexec.AgentAuthForTest()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := sshexec.Run(ctx, req, vr, allow, auth, collector, sshexec.NopAuditEmitter{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	if !strings.Contains(collector.StdoutString(), "alembic ok") {
		t.Errorf("stdout = %q, want contains %q", collector.StdoutString(), "alembic ok")
	}
}

func TestRunRefusesWhenValidationNotOK(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "1")
	req := sshexec.ExecRequest{Host: "ignored", Command: "x", Project: "p"}
	req.ApplyDefaults()
	vr := sshexec.Refuse("test forced refuse")
	allow := &sshexec.Allowlist{Patterns: []string{"x"}, Hosts: []string{"ignored"}}
	res, err := sshexec.Run(context.Background(), req, vr, allow, sshexec.AgentAuthForTest(), &sshexec.MemoryStreamSink{}, sshexec.NopAuditEmitter{})
	if err == nil {
		t.Fatalf("Run accepted unvalidated request: %+v", res)
	}
	if !strings.Contains(err.Error(), "validation not ok") {
		t.Errorf("err = %v, want contains 'validation not ok'", err)
	}
}

func TestRunRefusesUnauthorizedHost(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "1")
	srv, _ := testharness.NewFakeSSHD(testharness.HandlerFunc(func(req string) testharness.HandlerScript {
		return testharness.HandlerScript{Stdout: "ok\n"}
	}))
	defer srv.Close()
	req := sshexec.ExecRequest{Host: srv.Addr(), Command: "alembic upgrade", Project: "p"}
	req.ApplyDefaults()
	vr := sshexec.Validate(req.Command, []string{"alembic *"})
	allow := &sshexec.Allowlist{Patterns: []string{"alembic *"}, Hosts: []string{"only-this-other-host"}}
	_, err := sshexec.Run(context.Background(), req, vr, allow, sshexec.AgentAuthForTest(), &sshexec.MemoryStreamSink{}, sshexec.NopAuditEmitter{})
	if err == nil || !strings.Contains(err.Error(), "host not in allowlist") {
		t.Fatalf("err = %v, want 'host not in allowlist'", err)
	}
}

func TestRunNilAllowlistRejected(t *testing.T) {
	req := sshexec.ExecRequest{Host: "h", Command: "alembic upgrade", Project: "p"}
	req.ApplyDefaults()
	vr := sshexec.Validate(req.Command, []string{"alembic *"})
	_, err := sshexec.Run(context.Background(), req, vr, nil, sshexec.AgentAuthForTest(), &sshexec.MemoryStreamSink{}, sshexec.NopAuditEmitter{})
	if err == nil {
		t.Fatal("nil Allowlist accepted")
	}
}

func TestEmitDeniedOnNilAllowlist(t *testing.T) {
	req := sshexec.ExecRequest{Host: "h", Command: "alembic upgrade", Project: "p"}
	req.ApplyDefaults()
	vr := sshexec.Validate(req.Command, []string{"alembic *"})
	em := &recordingEmitterShared{}
	_, err := sshexec.Run(context.Background(), req, vr, nil, sshexec.AgentAuthForTest(), &sshexec.MemoryStreamSink{}, em)
	if err == nil {
		t.Fatal("nil Allowlist accepted")
	}
	if len(em.denied) == 0 {
		t.Errorf("EmitDenied not called on nil-Allowlist path; want at least 1 entry")
	}
	if len(em.denied) > 0 && !strings.Contains(em.denied[0], "allowlist") {
		t.Errorf("denied reason = %q, want contains 'allowlist'", em.denied[0])
	}
}

func TestEmitDeniedOnNewSessionFailure(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "1")
	srv, _ := testharness.NewFakeSSHD(testharness.HandlerFunc(func(req string) testharness.HandlerScript {
		return testharness.HandlerScript{Stdout: "ok\n"}
	}))
	addr := srv.Addr()
	srv.Close()
	req := sshexec.ExecRequest{Host: addr, Command: "alembic upgrade", Project: "p"}
	req.ApplyDefaults()
	vr := sshexec.Validate(req.Command, []string{"alembic *"})
	allow := &sshexec.Allowlist{Patterns: []string{"alembic *"}, Hosts: []string{addr}}
	em := &recordingEmitterShared{}
	_, err := sshexec.Run(context.Background(), req, vr, allow, sshexec.AgentAuthForTest(), &sshexec.MemoryStreamSink{}, em)
	if err == nil {
		t.Fatal("Run returned nil on closed-listener path")
	}

	if len(em.denied) == 0 {
		t.Errorf("EmitDenied not called on dial/NewSession failure path; want at least 1 entry")
	}
}

type recordingEmitterShared struct {
	sshexec.NopAuditEmitter
	started     int
	completed   int
	denied      []string
	interactive [][]byte
}

func (r *recordingEmitterShared) EmitStarted(sshexec.ExecRequest) error {
	r.started++
	return nil
}
func (r *recordingEmitterShared) EmitCompleted(sshexec.ExecRequest, sshexec.ExecResult) error {
	r.completed++
	return nil
}
func (r *recordingEmitterShared) EmitDenied(_ sshexec.ExecRequest, reason string) error {
	r.denied = append(r.denied, reason)
	return nil
}
func (r *recordingEmitterShared) EmitInteractiveBlocked(_ sshexec.ExecRequest, snippet []byte) error {
	r.interactive = append(r.interactive, append([]byte(nil), snippet...))
	return nil
}

func TestRunExitReasonTimeout(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "1")
	srv, _ := testharness.NewFakeSSHD(testharness.HandlerFunc(func(req string) testharness.HandlerScript {
		return testharness.HandlerScript{Stdout: "slow\n", Delay: 2 * time.Second}
	}))
	defer srv.Close()
	req := sshexec.ExecRequest{Host: srv.Addr(), Command: "alembic upgrade", Project: "p"}
	req.Timeout = 100 * time.Millisecond
	req.ApplyDefaults()
	vr := sshexec.Validate(req.Command, []string{"alembic *"})
	allow := &sshexec.Allowlist{Patterns: []string{"alembic *"}, Hosts: []string{srv.Addr()}}
	res, err := sshexec.Run(context.Background(), req, vr, allow, sshexec.AgentAuthForTest(), &sshexec.MemoryStreamSink{}, sshexec.NopAuditEmitter{})
	if err == nil {
		t.Fatal("Run nil err on timeout")
	}
	if res.ExitReason != sshexec.ExitReasonTimeout {
		t.Errorf("ExitReason = %q, want %q", res.ExitReason, sshexec.ExitReasonTimeout)
	}
	if res.ExitCode != -1 {
		t.Errorf("ExitCode = %d, want -1 on timeout", res.ExitCode)
	}
}

func TestRunExitReasonNormalOnNonZero(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "1")
	srv, _ := testharness.NewFakeSSHD(testharness.HandlerFunc(func(req string) testharness.HandlerScript {
		return testharness.HandlerScript{Stdout: "fail\n", ExitCode: 7}
	}))
	defer srv.Close()
	req := sshexec.ExecRequest{Host: srv.Addr(), Command: "alembic upgrade", Project: "p"}
	req.ApplyDefaults()
	vr := sshexec.Validate(req.Command, []string{"alembic *"})
	allow := &sshexec.Allowlist{Patterns: []string{"alembic *"}, Hosts: []string{srv.Addr()}}
	res, _ := sshexec.Run(context.Background(), req, vr, allow, sshexec.AgentAuthForTest(), &sshexec.MemoryStreamSink{}, sshexec.NopAuditEmitter{})
	if res.ExitCode != 7 {
		t.Errorf("ExitCode = %d, want 7", res.ExitCode)
	}
	if res.ExitReason != sshexec.ExitReasonNormal {
		t.Errorf("ExitReason = %q, want %q", res.ExitReason, sshexec.ExitReasonNormal)
	}
}

func TestRunExitReasonInteractiveBlocked(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "1")
	srv, _ := testharness.NewFakeSSHD(testharness.HandlerFunc(func(req string) testharness.HandlerScript {
		return testharness.HandlerScript{
			Stdout:   "[sudo] password for testuser:\n",
			ExitCode: 0,
		}
	}))
	defer srv.Close()
	req := sshexec.ExecRequest{Host: srv.Addr(), Command: "alembic upgrade", Project: "p"}
	req.ApplyDefaults()
	vr := sshexec.Validate(req.Command, []string{"alembic *"})
	allow := &sshexec.Allowlist{Patterns: []string{"alembic *"}, Hosts: []string{srv.Addr()}}
	res, _ := sshexec.Run(context.Background(), req, vr, allow, sshexec.AgentAuthForTest(), &sshexec.MemoryStreamSink{}, sshexec.NopAuditEmitter{})
	if !res.InteractiveBlocked {
		t.Fatal("InteractiveBlocked = false")
	}
	if res.ExitReason != sshexec.ExitReasonInteractiveBlocked {
		t.Errorf("ExitReason = %q, want %q", res.ExitReason, sshexec.ExitReasonInteractiveBlocked)
	}
}

func TestRunTimeoutKills(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "1")
	srv, _ := testharness.NewFakeSSHD(testharness.HandlerFunc(func(req string) testharness.HandlerScript {
		return testharness.HandlerScript{Stdout: "slow\n", Delay: 2 * time.Second}
	}))
	defer srv.Close()
	req := sshexec.ExecRequest{Host: srv.Addr(), Command: "alembic upgrade", Project: "p"}
	req.Timeout = 100 * time.Millisecond
	req.ApplyDefaults()
	vr := sshexec.Validate(req.Command, []string{"alembic *"})
	allow := &sshexec.Allowlist{Patterns: []string{"alembic *"}, Hosts: []string{srv.Addr()}}

	start := time.Now()
	_, err := sshexec.Run(context.Background(), req, vr, allow, sshexec.AgentAuthForTest(), &sshexec.MemoryStreamSink{}, sshexec.NopAuditEmitter{})
	elapsed := time.Since(start)
	if elapsed > 800*time.Millisecond {
		t.Errorf("Run took %v after 100ms timeout (want <800ms incl. dial)", elapsed)
	}
	if err == nil {
		t.Fatal("Run returned nil err on timeout")
	}
}

func TestRunDialFailureSurfaces(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "1")
	req := sshexec.ExecRequest{

		Host:    "127.0.0.1:1",
		Command: "alembic upgrade",
		Project: "p",
	}
	req.ApplyDefaults()
	vr := sshexec.Validate(req.Command, []string{"alembic *"})
	allow := &sshexec.Allowlist{Patterns: []string{"alembic *"}, Hosts: []string{req.Host}}
	_, err := sshexec.Run(context.Background(), req, vr, allow, sshexec.AgentAuthForTest(), &sshexec.MemoryStreamSink{}, sshexec.NopAuditEmitter{})
	if err == nil {
		t.Fatal("Run returned nil on dial failure")
	}
}

func TestRunNonZeroExitCode(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "1")
	srv, _ := testharness.NewFakeSSHD(testharness.HandlerFunc(func(req string) testharness.HandlerScript {
		return testharness.HandlerScript{Stdout: "fail\n", ExitCode: 42}
	}))
	defer srv.Close()
	req := sshexec.ExecRequest{Host: srv.Addr(), Command: "alembic upgrade", Project: "p"}
	req.ApplyDefaults()
	vr := sshexec.Validate(req.Command, []string{"alembic *"})
	allow := &sshexec.Allowlist{Patterns: []string{"alembic *"}, Hosts: []string{srv.Addr()}}
	res, _ := sshexec.Run(context.Background(), req, vr, allow, sshexec.AgentAuthForTest(), &sshexec.MemoryStreamSink{}, sshexec.NopAuditEmitter{})
	if res.ExitCode != 42 {
		t.Errorf("ExitCode = %d, want 42", res.ExitCode)
	}
}

func TestValidateCwdAcceptsCleanPaths(t *testing.T) {
	cases := []string{
		"/tmp",
		"/home/user/projects/internal-platform-x",
		"/var/lib/zen-swarm/data",
		"projects/relative/path",
		"",
		".",
		"..",
		"/path-with-dash/and_underscore",
	}
	for _, p := range cases {
		if err := sshexec.ValidateCwd(p); err != nil {
			t.Errorf("ValidateCwd(%q) = %v, want nil", p, err)
		}
	}
}

func TestValidateCwdRejectsForbidden(t *testing.T) {
	cases := []struct {
		cwd  string
		want string
	}{
		{"/tmp; rm -rf /", "forbidden character"},
		{"/tmp$(whoami)", "forbidden character"},
		{"/tmp`whoami`", "forbidden character"},
		{"/tmp|tee", "forbidden character"},
		{"/tmp>output", "forbidden character"},
		{"/tmp\x00", "null byte"},
		{"/tmp/path\x00rest", "null byte"},
		{" /tmp", "leading whitespace"},
		{"\t/tmp", "leading whitespace"},
		{"\n/tmp", "leading whitespace"},
		{"/tmp&", "forbidden character"},
		{"/tmp[a]", "forbidden character"},
		{"/tmp{a}", "forbidden character"},
		{"/tmp~", "forbidden character"},
		{"/tmp*", "forbidden character"},
	}
	for _, c := range cases {
		err := sshexec.ValidateCwd(c.cwd)
		if err == nil {
			t.Errorf("ValidateCwd(%q) = nil; want error", c.cwd)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("ValidateCwd(%q).err = %v, want contains %q", c.cwd, err, c.want)
		}
	}
}

func TestRunRejectsBadCwd(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "1")
	req := sshexec.ExecRequest{Host: "127.0.0.1:1", Command: "alembic upgrade", Cwd: "/tmp; rm -rf /", Project: "p"}
	req.ApplyDefaults()
	vr := sshexec.Validate(req.Command, []string{"alembic *"})
	allow := &sshexec.Allowlist{Patterns: []string{"alembic *"}, Hosts: []string{"127.0.0.1:1"}}
	em := &recordingEmitterShared{}
	_, err := sshexec.Run(context.Background(), req, vr, allow, sshexec.AgentAuthForTest(), &sshexec.MemoryStreamSink{}, em)
	if err == nil {
		t.Fatal("Run accepted forbidden-char Cwd")
	}
	if !strings.Contains(err.Error(), "cwd") {
		t.Errorf("err = %v, want contains 'cwd'", err)
	}

	if len(em.denied) == 0 {
		t.Errorf("EmitDenied not called for bad Cwd")
	}
}

func TestRunCwdSetenv(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "1")
	srv, _ := testharness.NewFakeSSHD(testharness.HandlerFunc(func(req string) testharness.HandlerScript {
		return testharness.HandlerScript{Stdout: "ok\n", ExitCode: 0}
	}))
	defer srv.Close()
	req := sshexec.ExecRequest{Host: srv.Addr(), Command: "alembic upgrade", Cwd: "/tmp", Project: "p"}
	req.ApplyDefaults()
	vr := sshexec.Validate(req.Command, []string{"alembic *"})
	allow := &sshexec.Allowlist{Patterns: []string{"alembic *"}, Hosts: []string{srv.Addr()}}
	res, err := sshexec.Run(context.Background(), req, vr, allow, sshexec.AgentAuthForTest(), &sshexec.MemoryStreamSink{}, sshexec.NopAuditEmitter{})
	if err != nil {
		t.Fatalf("Run with Cwd: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d", res.ExitCode)
	}
}

func TestRunEmitterStartedFailureBubbles(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "1")
	req := sshexec.ExecRequest{Host: "h", Command: "alembic upgrade", Project: "p"}
	req.ApplyDefaults()
	vr := sshexec.Validate(req.Command, []string{"alembic *"})
	allow := &sshexec.Allowlist{Patterns: []string{"alembic *"}, Hosts: []string{"h"}}
	em := &failingStartEmitter{}
	_, err := sshexec.Run(context.Background(), req, vr, allow, sshexec.AgentAuthForTest(), &sshexec.MemoryStreamSink{}, em)
	if err == nil {
		t.Fatal("Run accepted EmitStarted failure")
	}
}

type failingStartEmitter struct{ sshexec.NopAuditEmitter }

func (f *failingStartEmitter) EmitStarted(sshexec.ExecRequest) error {
	return errors.New("emitter dead")
}

func TestStreamingOrdinalMonotonic(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "1")
	srv, _ := testharness.NewFakeSSHD(testharness.HandlerFunc(func(req string) testharness.HandlerScript {
		return testharness.HandlerScript{
			Stdout:   "AAAAA\nBBBBB\nCCCCC\nDDDDD\nEEEEE\n",
			ExitCode: 0,
		}
	}))
	defer srv.Close()
	req := sshexec.ExecRequest{Host: srv.Addr(), Command: "alembic upgrade", Project: "p"}
	req.ApplyDefaults()
	vr := sshexec.Validate(req.Command, []string{"alembic *"})
	allow := &sshexec.Allowlist{Patterns: []string{"alembic *"}, Hosts: []string{srv.Addr()}}
	sink := &sshexec.MemoryStreamSink{}
	_, err := sshexec.Run(context.Background(), req, vr, allow, sshexec.AgentAuthForTest(), sink, sshexec.NopAuditEmitter{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	chunks := sink.Chunks()
	var lastStdout int64
	for _, c := range chunks {
		if c.Stream == sshexec.StreamStdout {
			if c.Ordinal <= lastStdout {
				t.Errorf("ordinal not monotonic: prev=%d cur=%d", lastStdout, c.Ordinal)
			}
			lastStdout = c.Ordinal
		}
	}
	if !strings.Contains(sink.StdoutString(), "AAAAA") || !strings.Contains(sink.StdoutString(), "EEEEE") {
		t.Errorf("missing segments in stdout: %q", sink.StdoutString())
	}
}

func TestTruncationStdout(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "1")
	big := strings.Repeat("X", 64*1024)
	srv, _ := testharness.NewFakeSSHD(testharness.HandlerFunc(func(req string) testharness.HandlerScript {
		return testharness.HandlerScript{Stdout: big, ExitCode: 0}
	}))
	defer srv.Close()
	req := sshexec.ExecRequest{
		Host:      srv.Addr(),
		Command:   "alembic upgrade",
		Project:   "p",
		MaxStdout: 8 * 1024,
		MaxStderr: 1024,
	}
	req.ApplyDefaults()
	vr := sshexec.Validate(req.Command, []string{"alembic *"})
	allow := &sshexec.Allowlist{Patterns: []string{"alembic *"}, Hosts: []string{srv.Addr()}}
	sink := &sshexec.MemoryStreamSink{}
	res, err := sshexec.Run(context.Background(), req, vr, allow, sshexec.AgentAuthForTest(), sink, sshexec.NopAuditEmitter{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.StdoutTruncated {
		t.Errorf("StdoutTruncated = false; want true (bytes=%d cap=%d)", res.StdoutBytes, req.MaxStdout)
	}
	if int64(len(sink.StdoutString())) > req.MaxStdout {
		t.Errorf("delivered stdout (%d) exceeds cap (%d)", len(sink.StdoutString()), req.MaxStdout)
	}
	if res.StdoutBytes < int64(len(big)) {
		t.Errorf("StdoutBytes = %d, want >=%d (full count even after truncation)", res.StdoutBytes, len(big))
	}
}

func TestTruncationStderr(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "1")
	bigErr := strings.Repeat("E", 16*1024)
	srv, _ := testharness.NewFakeSSHD(testharness.HandlerFunc(func(req string) testharness.HandlerScript {
		return testharness.HandlerScript{Stderr: bigErr, ExitCode: 1}
	}))
	defer srv.Close()
	req := sshexec.ExecRequest{
		Host:      srv.Addr(),
		Command:   "alembic upgrade",
		Project:   "p",
		MaxStdout: 1024,
		MaxStderr: 4 * 1024,
	}
	req.ApplyDefaults()
	vr := sshexec.Validate(req.Command, []string{"alembic *"})
	allow := &sshexec.Allowlist{Patterns: []string{"alembic *"}, Hosts: []string{srv.Addr()}}
	sink := &sshexec.MemoryStreamSink{}
	res, _ := sshexec.Run(context.Background(), req, vr, allow, sshexec.AgentAuthForTest(), sink, sshexec.NopAuditEmitter{})
	if !res.StderrTruncated {
		t.Errorf("StderrTruncated = false; want true")
	}
	if int64(len(sink.StderrString())) > req.MaxStderr {
		t.Errorf("delivered stderr (%d) exceeds cap (%d)", len(sink.StderrString()), req.MaxStderr)
	}
}

type failingSink struct{ count int }

func (f *failingSink) Emit(c sshexec.StreamChunk) error {
	f.count++
	return errors.New("boom")
}
func TestStdoutBytesCountsEvenWhenSinkFails(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "1")
	srv, _ := testharness.NewFakeSSHD(testharness.HandlerFunc(func(req string) testharness.HandlerScript {
		return testharness.HandlerScript{Stdout: "hello\nhello\n", ExitCode: 0}
	}))
	defer srv.Close()
	req := sshexec.ExecRequest{Host: srv.Addr(), Command: "alembic upgrade", Project: "p"}
	req.ApplyDefaults()
	vr := sshexec.Validate(req.Command, []string{"alembic *"})
	allow := &sshexec.Allowlist{Patterns: []string{"alembic *"}, Hosts: []string{srv.Addr()}}
	sink := &failingSink{}
	res, _ := sshexec.Run(context.Background(), req, vr, allow, sshexec.AgentAuthForTest(), sink, sshexec.NopAuditEmitter{})
	if res.StdoutBytes == 0 {
		t.Errorf("StdoutBytes=0 with failing sink; expected non-zero (sink failures must not zero accounting)")
	}
}

func TestAgentAuthFailsWithoutSocket(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	_, err := sshexec.AgentAuth()
	if err == nil {
		t.Fatal("AgentAuth returned nil err with empty SSH_AUTH_SOCK")
	}
	if !strings.Contains(err.Error(), "SSH_AUTH_SOCK") {
		t.Errorf("err = %v, want SSH_AUTH_SOCK substring", err)
	}
}

func TestAgentAuthBadSocketDial(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "/nonexistent-socket-xyz")
	_, err := sshexec.AgentAuth()
	if err == nil {
		t.Fatal("AgentAuth accepted unreachable socket")
	}
	if !strings.Contains(err.Error(), "dial agent") {
		t.Errorf("err = %v", err)
	}
}

func TestNopAuditEmitterMethodsReturnNil(t *testing.T) {
	em := sshexec.NopAuditEmitter{}
	if err := em.EmitStarted(sshexec.ExecRequest{}); err != nil {
		t.Errorf("EmitStarted: %v", err)
	}
	if err := em.EmitCompleted(sshexec.ExecRequest{}, sshexec.ExecResult{}); err != nil {
		t.Errorf("EmitCompleted: %v", err)
	}
	if err := em.EmitDenied(sshexec.ExecRequest{}, "x"); err != nil {
		t.Errorf("EmitDenied: %v", err)
	}
	if err := em.EmitInteractiveBlocked(sshexec.ExecRequest{}, []byte("x")); err != nil {
		t.Errorf("EmitInteractiveBlocked: %v", err)
	}
}

func TestMemoryStreamSinkChunksImmutable(t *testing.T) {
	sink := &sshexec.MemoryStreamSink{}
	if err := sink.Emit(sshexec.StreamChunk{Ordinal: 1, Stream: sshexec.StreamStdout, Data: []byte("a")}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	c := sink.Chunks()
	if len(c) != 1 {
		t.Fatalf("Chunks len = %d", len(c))
	}
	c[0].Data = []byte("MUTATED")
	if string(sink.Chunks()[0].Data) == "MUTATED" {
		t.Errorf("Chunks() returned a live alias; want defensive copy")
	}
}
