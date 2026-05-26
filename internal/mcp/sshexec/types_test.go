package sshexec

import (
	"testing"
	"time"
)

func TestExecRequestZeroValueDefaults(t *testing.T) {
	var r ExecRequest
	if r.Timeout != 0 {
		t.Errorf("zero Timeout = %v, want 0", r.Timeout)
	}
	r.ApplyDefaults()
	if r.Timeout != 60*time.Second {
		t.Errorf("ApplyDefaults Timeout = %v, want 60s", r.Timeout)
	}
	if r.MaxStdout != 10*1024*1024 {
		t.Errorf("ApplyDefaults MaxStdout = %d, want 10 MiB", r.MaxStdout)
	}
	if r.MaxStderr != 1024*1024 {
		t.Errorf("ApplyDefaults MaxStderr = %d, want 1 MiB", r.MaxStderr)
	}
}

func TestExecRequestApplyDefaultsIsIdempotent(t *testing.T) {
	r := ExecRequest{Timeout: 5 * time.Second, MaxStdout: 4096, MaxStderr: 2048}
	r.ApplyDefaults()
	if r.Timeout != 5*time.Second {
		t.Errorf("Timeout overridden by ApplyDefaults: %v", r.Timeout)
	}
	if r.MaxStdout != 4096 {
		t.Errorf("MaxStdout overridden: %d", r.MaxStdout)
	}
	if r.MaxStderr != 2048 {
		t.Errorf("MaxStderr overridden: %d", r.MaxStderr)
	}
	r.ApplyDefaults()
	if r.Timeout != 5*time.Second || r.MaxStdout != 4096 || r.MaxStderr != 2048 {
		t.Errorf("second ApplyDefaults perturbed values: %+v", r)
	}
}

func TestStreamChunkOrdinalIncrements(t *testing.T) {
	c1 := StreamChunk{Ordinal: 1, Stream: StreamStdout, Data: []byte("a")}
	c2 := StreamChunk{Ordinal: 2, Stream: StreamStderr, Data: []byte("b")}
	if c1.Ordinal >= c2.Ordinal {
		t.Errorf("ordinal monotonicity expected: c1=%d c2=%d", c1.Ordinal, c2.Ordinal)
	}
	if c1.Stream == c2.Stream {
		t.Errorf("Stream label should differ: %v vs %v", c1.Stream, c2.Stream)
	}
}

func TestStreamLabelConstants(t *testing.T) {
	if string(StreamStdout) != "stdout" {
		t.Errorf("StreamStdout = %q", StreamStdout)
	}
	if string(StreamStderr) != "stderr" {
		t.Errorf("StreamStderr = %q", StreamStderr)
	}
}

func TestExecResultTruncationFlags(t *testing.T) {
	res := ExecResult{
		ExitCode:           0,
		StdoutTruncated:    true,
		StderrTruncated:    false,
		StdoutBytes:        12_345_678,
		StderrBytes:        100,
		Duration:           time.Second,
		InteractiveBlocked: false,
	}
	if !res.StdoutTruncated {
		t.Fatal("StdoutTruncated false; want true")
	}
	if res.StderrTruncated {
		t.Fatal("StderrTruncated true; want false")
	}
	if res.InteractiveBlocked {
		t.Fatal("InteractiveBlocked true; want false")
	}
}

func TestApplyDefaultsFromDoctrineWidens(t *testing.T) {
	d := &Defaults{
		Timeout:   30 * time.Minute,
		MaxStdout: 64 * 1024 * 1024,
		MaxStderr: 8 * 1024 * 1024,
	}
	var r ExecRequest
	r.ApplyDefaultsFrom(d)
	if r.Timeout != 30*time.Minute {
		t.Errorf("Timeout = %v, want 30m (doctrine widened)", r.Timeout)
	}
	if r.MaxStdout != 64*1024*1024 {
		t.Errorf("MaxStdout = %d, want 64 MiB", r.MaxStdout)
	}
	if r.MaxStderr != 8*1024*1024 {
		t.Errorf("MaxStderr = %d, want 8 MiB", r.MaxStderr)
	}
}

func TestApplyDefaultsFromExplicitOverridesPreserved(t *testing.T) {
	d := &Defaults{Timeout: 30 * time.Minute, MaxStdout: 64 * 1024 * 1024}
	r := ExecRequest{Timeout: 5 * time.Second, MaxStdout: 1024}
	r.ApplyDefaultsFrom(d)
	if r.Timeout != 5*time.Second {
		t.Errorf("explicit Timeout overridden by doctrine: %v", r.Timeout)
	}
	if r.MaxStdout != 1024 {
		t.Errorf("explicit MaxStdout overridden: %d", r.MaxStdout)
	}

	if r.MaxStderr != FloorMaxStderr {
		t.Errorf("MaxStderr = %d, want floor %d", r.MaxStderr, FloorMaxStderr)
	}
}

func TestApplyDefaultsFromNilFallsThroughToFloor(t *testing.T) {
	var r ExecRequest
	r.ApplyDefaultsFrom(nil)
	if r.Timeout != FloorTimeout {
		t.Errorf("Timeout = %v, want floor %v", r.Timeout, FloorTimeout)
	}
	if r.MaxStdout != FloorMaxStdout {
		t.Errorf("MaxStdout = %d, want floor %d", r.MaxStdout, FloorMaxStdout)
	}
	if r.MaxStderr != FloorMaxStderr {
		t.Errorf("MaxStderr = %d, want floor %d", r.MaxStderr, FloorMaxStderr)
	}
}

func TestApplyDefaultsFromZeroDoctrineFallsThroughToFloor(t *testing.T) {
	var r ExecRequest
	r.ApplyDefaultsFrom(&Defaults{})
	if r.Timeout != FloorTimeout {
		t.Errorf("Timeout = %v, want floor", r.Timeout)
	}
	if r.MaxStdout != FloorMaxStdout {
		t.Errorf("MaxStdout = %d, want floor", r.MaxStdout)
	}
	if r.MaxStderr != FloorMaxStderr {
		t.Errorf("MaxStderr = %d, want floor", r.MaxStderr)
	}
}

func TestListAllowedResultShape(t *testing.T) {
	r := ListAllowedResult{
		Project:  "internal-platform-x",
		Patterns: []string{"alembic *", "pytest *"},
		Hosts:    []string{"vps"},
		Source:   "merge(doctrine,zenswarm.toml)",
	}
	if r.Project != "internal-platform-x" {
		t.Errorf("Project = %q", r.Project)
	}
	if len(r.Patterns) != 2 || len(r.Hosts) != 1 {
		t.Errorf("Patterns/Hosts = %+v", r)
	}
}
