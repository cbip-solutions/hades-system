// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseFlagsDefaultMode(t *testing.T) {
	cfg, err := parseFlags([]string{})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if cfg.Mode != ModeVerify {
		t.Errorf("mode = %v, want ModeVerify", cfg.Mode)
	}
}

func TestParseFlagsFreshnessMode(t *testing.T) {
	cfg, err := parseFlags([]string{"--freshness"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if cfg.Mode != ModeFreshness {
		t.Errorf("mode = %v, want ModeFreshness", cfg.Mode)
	}
}

func TestParseFlagsFreshnessMaxAgeOverride(t *testing.T) {
	cfg, err := parseFlags([]string{"--freshness", "--max-age-days=14"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if cfg.MaxAgeDays != 14 {
		t.Errorf("MaxAgeDays = %d, want 14", cfg.MaxAgeDays)
	}
}

func TestParseFlagsCaptureMode(t *testing.T) {
	cfg, err := parseFlags([]string{
		"--capture-seed=42",
		"--golden-out", "/tmp/out.golden",
		"--trace-out", "/tmp/out.trace",
	})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if cfg.Mode != ModeCapture {
		t.Errorf("mode = %v, want ModeCapture", cfg.Mode)
	}
	if cfg.Seed != 42 {
		t.Errorf("seed = %d, want 42", cfg.Seed)
	}
	if cfg.GoldenOut != "/tmp/out.golden" {
		t.Errorf("GoldenOut = %q, want /tmp/out.golden", cfg.GoldenOut)
	}
	if cfg.TraceOut != "/tmp/out.trace" {
		t.Errorf("TraceOut = %q, want /tmp/out.trace", cfg.TraceOut)
	}
}

func TestParseFlagsCaptureMissingOutputs(t *testing.T) {
	cases := map[string][]string{
		"missing_golden_out": {"--capture-seed=42", "--trace-out", "/tmp/t"},
		"missing_trace_out":  {"--capture-seed=42", "--golden-out", "/tmp/g"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseFlags(args)
			if err == nil {
				t.Errorf("parseFlags(%v) succeeded; want error", args)
			}
		})
	}
}

func TestRunVerifyHappyPath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cfg := Config{
		Mode:   ModeVerify,
		Stdout: &stdout,
		Stderr: &stderr,
		runner: &fakeRunner{verifyErr: nil},
	}
	if err := run(cfg); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stdout.String(), "verify-chaos-suite: ALL PASS") {
		t.Errorf("stdout = %q, want ALL PASS marker", stdout.String())
	}
}

func TestRunVerifyFailurePropagates(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cfg := Config{
		Mode:   ModeVerify,
		Stdout: &stdout,
		Stderr: &stderr,
		runner: &fakeRunner{verifyErr: errors.New("smoke-chaos: gofail step failed")},
	}
	err := run(cfg)
	if err == nil {
		t.Fatal("run: expected error; got nil")
	}
	if !strings.Contains(err.Error(), "smoke-chaos") {
		t.Errorf("err = %v, want error containing smoke-chaos", err)
	}
}

func TestRunFreshnessFailurePropagates(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cfg := Config{
		Mode:       ModeFreshness,
		MaxAgeDays: 8,
		Stdout:     &stdout,
		Stderr:     &stderr,
		runner:     &fakeRunner{freshnessErr: errors.New("no successful chaos.yml runs in last 8 days")},
	}
	err := run(cfg)
	if err == nil {
		t.Fatal("run: expected error; got nil")
	}
}

func TestRunCaptureHappyPath(t *testing.T) {
	dir := t.TempDir()
	goldenPath := filepath.Join(dir, "test.golden")
	tracePath := filepath.Join(dir, "test.trace")
	var stdout, stderr bytes.Buffer
	cfg := Config{
		Mode:      ModeCapture,
		Seed:      0,
		GoldenOut: goldenPath,
		TraceOut:  tracePath,
		Stdout:    &stdout,
		Stderr:    &stderr,
		runner:    &fakeRunner{},
	}
	if err := run(cfg); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestRunUnknownModeRejected(t *testing.T) {
	cfg := Config{
		Mode:   ModeUnknown,
		runner: &fakeRunner{},
	}
	if err := run(cfg); err == nil {
		t.Fatal("run: expected error for unknown mode; got nil")
	}
}

type fakeRunner struct {
	verifyErr    error
	captureErr   error
	freshnessErr error
}

func (f *fakeRunner) Verify() error { return f.verifyErr }

func (f *fakeRunner) Capture(seed int64, goldenOut, traceOut string) error {
	return f.captureErr
}

func (f *fakeRunner) Freshness(maxAgeDays int) error { return f.freshnessErr }

func TestModeString(t *testing.T) {
	cases := []struct {
		mode Mode
		want string
	}{
		{ModeVerify, "verify"},
		{ModeCapture, "capture"},
		{ModeFreshness, "freshness"},
		{ModeUnknown, "unknown"},
		{Mode(99), "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			if got := tc.mode.String(); got != tc.want {
				t.Errorf("Mode(%d).String() = %q, want %q", tc.mode, got, tc.want)
			}
		})
	}
}

func TestRealRunnerCaptureWritesEnvelope(t *testing.T) {
	dir := t.TempDir()
	goldenPath := filepath.Join(dir, "case.golden")
	tracePath := filepath.Join(dir, "case.trace")
	r := &realRunner{}
	if err := r.Capture(12345, goldenPath, tracePath); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	goldenBytes, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var env captureEnvelope
	if err := json.Unmarshal(goldenBytes, &env); err != nil {
		t.Fatalf("unmarshal golden: %v", err)
	}
	if env.Seed != 12345 {
		t.Errorf("Seed = %d, want 12345", env.Seed)
	}
	if env.CapturedAt == "" {
		t.Error("CapturedAt empty; want RFC3339 timestamp")
	}
	if env.Note == "" {
		t.Error("Note empty; want capture provenance")
	}
	if _, err := os.Stat(tracePath); err != nil {
		t.Errorf("trace file missing: %v", err)
	}
}

// TestRealRunnerCaptureRejectsUnwritablePath pins the failure path —
// writing to a non-existent parent directory MUST error so operator
// triage catches typos.
func TestRealRunnerCaptureRejectsUnwritablePath(t *testing.T) {
	r := &realRunner{}

	err := r.Capture(0, "/this/path/does/not/exist/golden", "/tmp/trace")
	if err == nil {
		t.Fatal("Capture: expected error for non-existent golden path; got nil")
	}
}

func TestRealRunnerCaptureRejectsUnwritableTrace(t *testing.T) {
	r := &realRunner{}
	dir := t.TempDir()
	goldenPath := filepath.Join(dir, "case.golden")
	tracePath := "/this/path/does/not/exist/trace"
	err := r.Capture(0, goldenPath, tracePath)
	if err == nil {
		t.Fatal("Capture: expected error for non-existent trace path; got nil")
	}
}

func TestRealRunnerFreshnessRejectsNonPositiveWindow(t *testing.T) {
	r := &realRunner{}
	for _, days := range []int{0, -1, -100} {
		err := r.Freshness(days)
		if err == nil {
			t.Errorf("Freshness(%d): expected error; got nil", days)
		}
	}
}

func TestRunCaptureFailurePropagates(t *testing.T) {
	cfg := Config{
		Mode:      ModeCapture,
		Seed:      777,
		GoldenOut: "/tmp/g",
		TraceOut:  "/tmp/t",
		runner:    &fakeRunner{captureErr: errors.New("disk full")},
	}
	err := run(cfg)
	if err == nil {
		t.Fatal("run: expected error; got nil")
	}
	if !strings.Contains(err.Error(), "seed=777") {
		t.Errorf("err = %v, want error containing seed=777", err)
	}
}

func TestRunFreshnessHappyPath(t *testing.T) {
	var stdout bytes.Buffer
	cfg := Config{
		Mode:       ModeFreshness,
		MaxAgeDays: 8,
		Stdout:     &stdout,
		runner:     &fakeRunner{freshnessErr: nil},
	}
	if err := run(cfg); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stdout.String(), "within 8 days") {
		t.Errorf("stdout = %q, want freshness PASS marker", stdout.String())
	}
}

func TestRunCaptureProducesCaptureStdout(t *testing.T) {
	var stdout bytes.Buffer
	cfg := Config{
		Mode:      ModeCapture,
		Seed:      42,
		GoldenOut: "/tmp/g",
		TraceOut:  "/tmp/t",
		Stdout:    &stdout,
		runner:    &fakeRunner{},
	}
	if err := run(cfg); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stdout.String(), "captured seed=42") {
		t.Errorf("stdout = %q, want captured-seed marker", stdout.String())
	}
}

func TestParseFlagsCaptureWithSeedZeroValid(t *testing.T) {
	cfg, err := parseFlags([]string{
		"--capture-seed=0",
		"--golden-out", "/tmp/g",
		"--trace-out", "/tmp/t",
	})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if cfg.Mode != ModeCapture {
		t.Errorf("mode = %v, want ModeCapture", cfg.Mode)
	}
	if cfg.Seed != 0 {
		t.Errorf("seed = %d, want 0", cfg.Seed)
	}
}

func TestRealRunnerVerifyDelegatesToInjectedCommand(t *testing.T) {
	r := newRealRunner(io.Discard, io.Discard)
	r.makeCommand = func(name string, args ...string) *exec.Cmd {

		return exec.Command("true")
	}
	if err := r.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

// TestRealRunnerVerifyPropagatesSubprocessFailure pins the failure
// path — a non-zero subprocess exit MUST propagate.
func TestRealRunnerVerifyPropagatesSubprocessFailure(t *testing.T) {
	r := newRealRunner(io.Discard, io.Discard)
	r.makeCommand = func(name string, args ...string) *exec.Cmd {

		return exec.Command("false")
	}
	if err := r.Verify(); err == nil {
		t.Error("Verify: expected error from false; got nil")
	}
}

func TestRealRunnerFreshnessParsesGhJSON(t *testing.T) {
	r := newRealRunner(io.Discard, io.Discard)

	recent := time.Now().UTC().Add(-2 * 24 * time.Hour).Format(time.RFC3339)
	jsonOut := fmt.Sprintf(`[{"createdAt":%q,"databaseId":12345}]`, recent)
	r.ghCommand = func(name string, args ...string) *exec.Cmd {

		return exec.Command("echo", jsonOut)
	}
	if err := r.Freshness(8); err != nil {
		t.Errorf("Freshness: %v", err)
	}
}

// TestRealRunnerFreshnessRejectsStaleRun pins the freshness window
// gate — a run older than --max-age-days MUST error.
func TestRealRunnerFreshnessRejectsStaleRun(t *testing.T) {
	r := newRealRunner(io.Discard, io.Discard)
	stale := time.Now().UTC().Add(-30 * 24 * time.Hour).Format(time.RFC3339)
	jsonOut := fmt.Sprintf(`[{"createdAt":%q,"databaseId":99999}]`, stale)
	r.ghCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("echo", jsonOut)
	}
	err := r.Freshness(8)
	if err == nil {
		t.Fatal("Freshness: expected error for 30-day-old run; got nil")
	}
	if !strings.Contains(err.Error(), "ago") {
		t.Errorf("err = %v, want age-marker in error", err)
	}
}

func TestRealRunnerFreshnessRejectsEmptyRunList(t *testing.T) {
	r := newRealRunner(io.Discard, io.Discard)
	r.ghCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("echo", "[]")
	}
	err := r.Freshness(8)
	if err == nil {
		t.Fatal("Freshness: expected error for empty list; got nil")
	}
	if !strings.Contains(err.Error(), "no successful") {
		t.Errorf("err = %v, want no-successful-runs marker", err)
	}
}

func TestRealRunnerFreshnessRejectsMalformedJSON(t *testing.T) {
	r := newRealRunner(io.Discard, io.Discard)
	r.ghCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("echo", "not-json")
	}
	if err := r.Freshness(8); err == nil {
		t.Error("Freshness: expected unmarshal error; got nil")
	}
}

func TestRealRunnerFreshnessRejectsMalformedTimestamp(t *testing.T) {
	r := newRealRunner(io.Discard, io.Discard)
	r.ghCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("echo", `[{"createdAt":"not-a-timestamp","databaseId":1}]`)
	}
	if err := r.Freshness(8); err == nil {
		t.Error("Freshness: expected time-parse error; got nil")
	}
}

func TestNewRealRunnerWiresProductionCommands(t *testing.T) {
	r := newRealRunner(io.Discard, io.Discard)
	if r.makeCommand == nil {
		t.Error("newRealRunner: makeCommand is nil; want exec.Command wiring")
	}
	if r.ghCommand == nil {
		t.Error("newRealRunner: ghCommand is nil; want exec.Command wiring")
	}
}

// TestRealRunnerVerifyFallbackWhenMakeCommandNil pins the defensive
// fallback — a realRunner constructed bare (without newRealRunner)
// MUST still fall back to exec.Command rather than NPE.
func TestRealRunnerVerifyFallbackWhenMakeCommandNil(t *testing.T) {
	r := &realRunner{stdout: io.Discard, stderr: io.Discard}

	_ = r.Verify()
}

func TestRealRunnerFreshnessFallbackWhenGhCommandNil(t *testing.T) {
	r := &realRunner{stdout: io.Discard, stderr: io.Discard}
	// Same defensive-fallback assertion — Freshness MUST NOT panic
	// when ghCommand is nil; it should fall back to exec.Command and
	// surface whatever the resulting subprocess returns (likely an
	// error in the test bin context, which is fine).
	_ = r.Freshness(8)
}
