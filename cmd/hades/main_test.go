package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMainCompiles(t *testing.T) {

	t.Log("cmd/hades package main compiled OK")
}

func helperBuildHades(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "hades")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, ".")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build hades: %v\n%s", err, stderr.String())
	}
	return bin
}

func TestVersionFlag(t *testing.T) {
	bin := helperBuildHades(t)
	out, err := exec.Command(bin, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("hades --version failed: %v\noutput: %s", err, out)
	}
	got := string(out)

	required := []string{
		"HADES",
		"on the Hermes substrate",
	}
	for _, sub := range required {
		if !strings.Contains(got, sub) {
			t.Errorf("--version output missing %q\nfull output:\n%s", sub, got)
		}
	}

	if strings.Contains(got, "zen-swarm") {
		t.Errorf("--version output contains forbidden brand string %q (inv-zen-217 violation)\nfull output:\n%s", "zen-swarm", got)
	}
}

func TestHelpFlag(t *testing.T) {
	bin := helperBuildHades(t)
	out, err := exec.Command(bin, "--help").CombinedOutput()
	if err != nil {
		t.Fatalf("hades --help failed: %v\noutput: %s", err, out)
	}
	got := string(out)

	required := []string{
		"HADES",
		"hades",
		"dashboard",
		"tui",
		"panels",
		"--version",
		"--help",
		"--no-wizard",
		"--panel",
		"Hermes",
	}
	for _, sub := range required {
		if !strings.Contains(got, sub) {
			t.Errorf("--help output missing %q\nfull output:\n%s", sub, got)
		}
	}

	if strings.Contains(got, "zen-swarm") {
		t.Errorf("--help output contains forbidden brand string %q (inv-zen-217)\nfull output:\n%s", "zen-swarm", got)
	}
}

func helperPathWithMockHermes(t *testing.T) (newPATH, recordPath string) {
	t.Helper()
	tmp := t.TempDir()
	recordPath = filepath.Join(tmp, "hermes-record.txt")
	scriptPath := filepath.Join(tmp, "hermes")
	script := "#!/bin/sh\n" +
		"# Mock hermes binary for Plan 18a Phase A tests. Records env + args.\n" +
		"{\n" +
		"  echo \"HERMES_SKIN=${HERMES_SKIN}\"\n" +
		"  echo \"HADES_NO_WIZARD=${HADES_NO_WIZARD}\"\n" +
		"  echo \"args:$*\"\n" +
		"} >> " + recordPath + "\n" +
		"exit 0\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write mock hermes: %v", err)
	}
	newPATH = tmp + string(os.PathListSeparator) + os.Getenv("PATH")
	return newPATH, recordPath
}

func TestBareInvocationExecsHermes(t *testing.T) {
	bin := helperBuildHades(t)
	newPATH, recordPath := helperPathWithMockHermes(t)

	udsPresent := filepath.Join(t.TempDir(), "present.sock")
	if err := os.WriteFile(udsPresent, []byte("x"), 0o600); err != nil {
		t.Fatalf("write present uds: %v", err)
	}
	cmd := exec.Command(bin)

	cmd.Env = append(os.Environ(), "PATH="+newPATH, "ZEN_DAEMON_UDS="+udsPresent)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hades (bare) failed: %v\noutput: %s", err, out)
	}

	rec, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read mock record: %v", err)
	}
	gotRec := string(rec)

	if !strings.Contains(gotRec, "HERMES_SKIN=hades") {
		t.Errorf("mock hermes did not see HERMES_SKIN=hades env\nrecord:\n%s", gotRec)
	}

	if strings.Contains(gotRec, "HADES_NO_WIZARD=1") {
		t.Errorf("mock hermes saw HADES_NO_WIZARD=1 on bare invocation (should be empty)\nrecord:\n%s", gotRec)
	}
}

func helperPathWithMockZen(t *testing.T, exitCode int) (newPATH, recordPath string) {
	t.Helper()
	tmp := t.TempDir()
	recordPath = filepath.Join(tmp, "zen-record.txt")
	scriptPath := filepath.Join(tmp, "zen")
	script := fmt.Sprintf("#!/bin/sh\n"+
		"# Mock zen binary for Plan 18a Phase A tests.\n"+
		"{\n"+
		"  echo \"HERMES_SKIN=${HERMES_SKIN}\"\n"+
		"  echo \"HADES_NO_WIZARD=${HADES_NO_WIZARD}\"\n"+
		"  echo \"args:$*\"\n"+
		"} >> %s\n"+
		"echo \"mock-zen output\"\n"+
		"exit %d\n", recordPath, exitCode)
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write mock zen: %v", err)
	}
	newPATH = tmp + string(os.PathListSeparator) + os.Getenv("PATH")
	return newPATH, recordPath
}

func TestPassthroughExecsZen(t *testing.T) {
	bin := helperBuildHades(t)
	cases := []struct {
		name        string
		hadesArgs   []string
		wantZenArgs string
		exitCode    int
	}{
		{"doctor no args", []string{"doctor"}, "args:doctor", 0},
		{"doctor with flag", []string{"doctor", "--verbose"}, "args:doctor --verbose", 0},
		{"knowledge query", []string{"knowledge", "query", "foo"}, "args:knowledge query foo", 0},
		{"bypass passthrough", []string{"bypass", "status"}, "args:bypass status", 0},
		{"nonzero exit propagated", []string{"doctor", "--will-fail"}, "args:doctor --will-fail", 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			newPATH, recordPath := helperPathWithMockZen(t, tc.exitCode)
			cmd := exec.Command(bin, tc.hadesArgs...)
			cmd.Env = append(os.Environ(), "PATH="+newPATH)
			out, err := cmd.CombinedOutput()

			gotExit := 0
			if exitErr, ok := err.(*exec.ExitError); ok {
				gotExit = exitErr.ExitCode()
			} else if err != nil {
				t.Fatalf("unexpected error type from hades: %v\noutput: %s", err, out)
			}
			if gotExit != tc.exitCode {
				t.Errorf("exit code = %d; want %d (output: %s)", gotExit, tc.exitCode, out)
			}

			if tc.exitCode == 0 && !strings.Contains(string(out), "mock-zen output") {
				t.Errorf("child stdout not forwarded; output:\n%s", out)
			}

			rec, err := os.ReadFile(recordPath)
			if err != nil {
				t.Fatalf("read mock zen record: %v", err)
			}
			if !strings.Contains(string(rec), tc.wantZenArgs) {
				t.Errorf("mock zen received wrong args\nwant line: %q\nrecord:\n%s", tc.wantZenArgs, rec)
			}

			if strings.Contains(string(rec), "HERMES_SKIN=hades") {
				t.Errorf("HERMES_SKIN=hades leaked to zen passthrough path (execZen.doc claims it should NOT)\nrecord:\n%s", rec)
			}
		})
	}
}

func TestDashboardAlias(t *testing.T) {
	bin := helperBuildHades(t)
	cases := []struct {
		name        string
		hadesArgs   []string
		wantZenArgs string
	}{
		{"dashboard bare", []string{"dashboard"}, "args:tui"},
		{"tui bare", []string{"tui"}, "args:tui"},
		{"panels bare", []string{"panels"}, "args:tui"},
		{"dashboard with panel", []string{"dashboard", "--panel=codegraph"}, "args:tui --panel=codegraph"},
		{"tui with panel", []string{"tui", "--panel=workforce"}, "args:tui --panel=workforce"},
		{"panels with panel", []string{"panels", "--panel=cost"}, "args:tui --panel=cost"},
		{"dashboard with extra flag", []string{"dashboard", "--panel=hra", "--no-color"}, "args:tui --panel=hra --no-color"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			newPATH, recordPath := helperPathWithMockZen(t, 0)
			cmd := exec.Command(bin, tc.hadesArgs...)
			cmd.Env = append(os.Environ(), "PATH="+newPATH)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("hades %v failed: %v\noutput: %s", tc.hadesArgs, err, out)
			}
			rec, err := os.ReadFile(recordPath)
			if err != nil {
				t.Fatalf("read mock zen record: %v", err)
			}
			if !strings.Contains(string(rec), tc.wantZenArgs) {
				t.Errorf("alias translation failed\nhades args: %v\nwant zen line: %q\nrecord:\n%s",
					tc.hadesArgs, tc.wantZenArgs, rec)
			}

			if strings.Contains(string(rec), "HERMES_SKIN=hades") {
				t.Errorf("HERMES_SKIN=hades leaked to dashboard alias path (case-body doc claims it should NOT)\nrecord:\n%s", rec)
			}
		})
	}
}

// TestNoWizardFlag asserts that `hades --no-wizard` sets
// HADES_NO_WIZARD=1 in the env of the child hermes process, in
// addition to HERMES_SKIN=hades.
//
// Per spec §3.2 step 4 + §Q7: --no-wizard is the escape hatch that
// prevents the implicit wizard from auto-launching on first run.
// In Phase 18a there is no wizard yet; the env
// var is set as a placeholder so the contract is already honoured
// when 18c lands.
//
// --no-wizard MUST cause the wrapper to take the bare-invocation
// branch (exec hermes), NOT the passthrough branch (exec zen).
func TestNoWizardFlag(t *testing.T) {
	bin := helperBuildHades(t)
	newPATH, recordPath := helperPathWithMockHermes(t)

	udsPresent := filepath.Join(t.TempDir(), "present.sock")
	if err := os.WriteFile(udsPresent, []byte("x"), 0o600); err != nil {
		t.Fatalf("write present uds: %v", err)
	}
	cmd := exec.Command(bin, "--no-wizard")

	cmd.Env = append(os.Environ(), "PATH="+newPATH, "ZEN_DAEMON_UDS="+udsPresent)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hades --no-wizard failed: %v\noutput: %s", err, out)
	}
	rec, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read mock hermes record: %v", err)
	}
	gotRec := string(rec)

	if !strings.Contains(gotRec, "HERMES_SKIN=hades") {
		t.Errorf("--no-wizard did not preserve HERMES_SKIN=hades\nrecord:\n%s", gotRec)
	}
	if !strings.Contains(gotRec, "HADES_NO_WIZARD=1") {
		t.Errorf("--no-wizard did not set HADES_NO_WIZARD=1\nrecord:\n%s", gotRec)
	}
}

func TestNoWizardOnlyAffectsBareInvocation(t *testing.T) {
	bin := helperBuildHades(t)
	newPATH, recordPath := helperPathWithMockZen(t, 0)

	cmd := exec.Command(bin, "doctor", "--no-wizard")
	cmd.Env = append(os.Environ(), "PATH="+newPATH)
	if _, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hades doctor --no-wizard failed: %v", err)
	}
	rec, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read mock zen record: %v", err)
	}
	gotRec := string(rec)

	if !strings.Contains(gotRec, "args:doctor --no-wizard") {
		t.Errorf("--no-wizard not forwarded to zen passthrough\nrecord:\n%s", gotRec)
	}
	if strings.Contains(gotRec, "HADES_NO_WIZARD=1") {
		t.Errorf("--no-wizard env was set on zen passthrough (should only be set on hermes bare path)\nrecord:\n%s", gotRec)
	}
}

// TestPrintVersion_InProcess exercises printVersion(io.Writer) directly,
// outside the subprocess pattern, so Go's coverage tool sees the
// statement execution. Validates the same assertions as TestVersionFlag
// (subprocess), establishing belt-and-suspenders coverage:
//
// - subprocess test (TestVersionFlag) proves the binary's --version
// dispatch path works end-to-end (main() → printVersion() → stdout);
// - in-process test (this one) proves printVersion() itself produces
// the documented output for any io.Writer.
//
// Coverage rationale:
// the subprocess pattern in TestVersionFlag exec's a separate binary, so
// Go's `-cover` instrumentation does NOT see those statements as
// covered. Calling printVersion() directly from a test inside the same
// process closes that gap without removing the subprocess tests (which
// remain the canonical behavioural assertions).
//
// Per spec §Q2 + §11 + invariant: output MUST contain "HADES",
// "Hermes substrate", and the version constant; MUST NOT contain
// "zen-swarm".
func TestPrintVersion_InProcess(t *testing.T) {
	var buf bytes.Buffer
	printVersion(&buf)
	got := buf.String()

	required := []string{
		"HADES",
		"on the Hermes substrate",
		version,
		hermesVersion,
	}
	for _, sub := range required {
		if !strings.Contains(got, sub) {
			t.Errorf("printVersion output missing %q\nfull output:\n%s", sub, got)
		}
	}

	if strings.Contains(got, "zen-swarm") {
		t.Errorf("printVersion output contains forbidden brand string %q (inv-zen-217 violation)\nfull output:\n%s", "zen-swarm", got)
	}
}

// TestPrintHelp_InProcess exercises printHelp(io.Writer) directly, in
// the same process, so Go's coverage tool sees the statement execution.
// Validates the same assertions as TestHelpFlag (subprocess).
//
// See TestPrintVersion_InProcess for coverage rationale.
//
// Per spec §3.2: the four wrapper modes (bare hermes, dashboard alias,
// passthrough, flags) MUST all be documented; the brand string MUST NOT
// be zen-swarm.
func TestPrintHelp_InProcess(t *testing.T) {
	var buf bytes.Buffer
	printHelp(&buf)
	got := buf.String()

	required := []string{
		"HADES",
		"hades",
		"dashboard",
		"tui",
		"panels",
		"--version",
		"--help",
		"--no-wizard",
		"--panel",
		"Hermes",
	}
	for _, sub := range required {
		if !strings.Contains(got, sub) {
			t.Errorf("printHelp output missing %q\nfull output:\n%s", sub, got)
		}
	}

	if strings.Contains(got, "zen-swarm") {
		t.Errorf("printHelp output contains forbidden brand string %q (inv-zen-217)\nfull output:\n%s", "zen-swarm", got)
	}

	for i, line := range strings.Split(strings.TrimRight(got, "\n"), "\n") {
		if len(line) > 80 {
			t.Errorf("printHelp line %d exceeds 80 cols (%d): %q", i+1, len(line), line)
		}
	}
}

func TestMaybeEmitDaemonHint_EmitsWhenUDSAbsent(t *testing.T) {

	dir := t.TempDir()
	absent := filepath.Join(dir, "nope.sock")
	t.Setenv("ZEN_DAEMON_UDS", absent)

	t.Setenv("HOME", t.TempDir())

	var buf bytes.Buffer
	maybeEmitDaemonHint(&buf)
	out := buf.String()

	wantSubs := []string{
		"HADES: daemon not running",
		absent,
		"hades daemon install",
		"hades daemon start",
		"hades-entry-point.md",
	}
	for _, s := range wantSubs {
		if !strings.Contains(out, s) {
			t.Errorf("maybeEmitDaemonHint missing %q\nfull output:\n%s", s, out)
		}
	}

	for _, gone := range []string{"placeholder", "Plan 18c", "zen-swarm-ctld -uds"} {
		if strings.Contains(out, gone) {
			t.Errorf("maybeEmitDaemonHint still contains retired placeholder substring %q\nfull output:\n%s", gone, out)
		}
	}
}

func TestMaybeEmitDaemonHint_SilentWhenUDSPresent(t *testing.T) {
	dir := t.TempDir()
	udsPath := filepath.Join(dir, "fake.sock")

	if err := os.WriteFile(udsPath, []byte("not a real socket"), 0o600); err != nil {
		t.Fatalf("write fake uds: %v", err)
	}
	t.Setenv("ZEN_DAEMON_UDS", udsPath)

	var buf bytes.Buffer
	maybeEmitDaemonHint(&buf)
	if got := buf.String(); got != "" {
		t.Errorf("maybeEmitDaemonHint emitted output when UDS path exists; want empty.\n%s", got)
	}
}

func TestMaybeEmitDaemonHint_DefaultsToCanonicalPath(t *testing.T) {

	if defaultDaemonUDS != "/tmp/zen-swarm.sock" {
		t.Errorf("defaultDaemonUDS = %q; want \"/tmp/zen-swarm.sock\" (Plan 5+ convention)", defaultDaemonUDS)
	}
}

// TestMaybeEmitDaemonHint_FallsBackToDefaultWhenEnvEmpty gates the
// docstring claim that when ZEN_DAEMON_UDS is empty/unset, the helper
// falls back to defaultDaemonUDS. This is the missing-coverage path
// (line 279 of main.go) that the env-set tests do not exercise.
//
// Test strategy: set ZEN_DAEMON_UDS to "" + temporarily move any
// existing /tmp/zen-swarm.sock aside if present (so the probe falls
// through to the emit-hint branch). On a typical dev machine without
// the real daemon running, /tmp/zen-swarm.sock does NOT exist so we
// can simply assert the emitted hint references the canonical default.
func TestMaybeEmitDaemonHint_FallsBackToDefaultWhenEnvEmpty(t *testing.T) {
	t.Setenv("ZEN_DAEMON_UDS", "")

	if _, err := os.Stat(defaultDaemonUDS); err == nil {
		t.Skipf("real daemon UDS exists at %s; cannot exercise fallback path", defaultDaemonUDS)
	}

	var buf bytes.Buffer
	maybeEmitDaemonHint(&buf)
	out := buf.String()

	if !strings.Contains(out, defaultDaemonUDS) {
		t.Errorf("fallback hint does not reference defaultDaemonUDS %q\nfull output:\n%s",
			defaultDaemonUDS, out)
	}
}

func helperInstallFakeLaunchAgent(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	laDir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(laDir, 0o755); err != nil {
		t.Fatalf("mkdir LaunchAgents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(laDir, launchAgentLabel+".plist"), []byte("<plist/>"), 0o644); err != nil {
		t.Fatalf("write fake plist: %v", err)
	}
}

func helperPathWithMockLaunchctl(t *testing.T) (newPATH, recordPath string) {
	t.Helper()
	tmp := t.TempDir()
	recordPath = filepath.Join(tmp, "launchctl-record.txt")
	scriptPath := filepath.Join(tmp, "launchctl")
	script := "#!/bin/sh\n" +
		"echo \"args:$*\" >> " + recordPath + "\n" +
		"case \"$1\" in kickstart) [ -n \"$ZEN_DAEMON_UDS\" ] && : > \"$ZEN_DAEMON_UDS\" ;; esac\n" +
		"exit 0\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write mock launchctl: %v", err)
	}
	newPATH = tmp + string(os.PathListSeparator) + os.Getenv("PATH")
	return newPATH, recordPath
}

func TestEnsureDaemonPreflight_KickstartsWhenAgentInstalled(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("launchctl kickstart is darwin-only")
	}
	uds := filepath.Join(t.TempDir(), "daemon.sock")
	t.Setenv("ZEN_DAEMON_UDS", uds)
	helperInstallFakeLaunchAgent(t)
	newPATH, recordPath := helperPathWithMockLaunchctl(t)
	t.Setenv("PATH", newPATH)

	var buf bytes.Buffer
	ensureDaemonPreflight(&buf)

	rec, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read launchctl record: %v", err)
	}
	if !strings.Contains(string(rec), "kickstart") {
		t.Errorf("expected launchctl kickstart invocation; record:\n%s", rec)
	}
	if !strings.Contains(string(rec), launchAgentLabel) {
		t.Errorf("kickstart target missing canonical label %q; record:\n%s", launchAgentLabel, rec)
	}
	if strings.Contains(string(rec), "com.zen-swarm.ctld") {
		t.Errorf("kickstart used the phantom hyphenated label; record:\n%s", rec)
	}
	if buf.Len() != 0 {
		t.Errorf("preflight emitted a hint despite recovery; output:\n%s", buf.String())
	}
	if !daemonUDSPresent(uds) {
		t.Errorf("mock kickstart did not create the UDS at %s", uds)
	}
}

func TestEnsureDaemonPreflight_HintsWhenNoAgentInstalled(t *testing.T) {
	uds := filepath.Join(t.TempDir(), "daemon.sock")
	t.Setenv("ZEN_DAEMON_UDS", uds)
	t.Setenv("HOME", t.TempDir())
	newPATH, recordPath := helperPathWithMockLaunchctl(t)
	t.Setenv("PATH", newPATH)

	var buf bytes.Buffer
	ensureDaemonPreflight(&buf)

	if rec, err := os.ReadFile(recordPath); err == nil && len(rec) > 0 {
		t.Errorf("launchctl was invoked despite no LaunchAgent installed; record:\n%s", rec)
	}
	if out := buf.String(); !strings.Contains(out, "hades daemon install") {
		t.Errorf("no-agent hint must reference `hades daemon install`\n%s", out)
	}
}

func TestEnsureDaemonPreflight_NoOpWhenUDSPresent(t *testing.T) {
	uds := filepath.Join(t.TempDir(), "daemon.sock")
	if err := os.WriteFile(uds, []byte("x"), 0o600); err != nil {
		t.Fatalf("write uds: %v", err)
	}
	t.Setenv("ZEN_DAEMON_UDS", uds)
	t.Setenv("HOME", t.TempDir())
	newPATH, recordPath := helperPathWithMockLaunchctl(t)
	t.Setenv("PATH", newPATH)

	var buf bytes.Buffer
	ensureDaemonPreflight(&buf)

	if rec, err := os.ReadFile(recordPath); err == nil && len(rec) > 0 {
		t.Errorf("launchctl invoked when UDS already present; record:\n%s", rec)
	}
	if buf.Len() != 0 {
		t.Errorf("preflight emitted output when UDS present; want silent.\n%s", buf.String())
	}
}

func TestWaitForUDS_ReturnsTrueWhenSocketAppears(t *testing.T) {
	uds := filepath.Join(t.TempDir(), "late.sock")
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = os.WriteFile(uds, []byte("x"), 0o600)
	}()
	if !waitForUDS(uds, 30*time.Second) {
		t.Error("waitForUDS returned false though socket appeared within timeout")
	}
}

func TestWaitForUDS_TimesOut(t *testing.T) {
	uds := filepath.Join(t.TempDir(), "never.sock")
	if waitForUDS(uds, 200*time.Millisecond) {
		t.Error("waitForUDS returned true for a socket that never appears")
	}
}

func TestKickstartDaemon_NoOpOnNonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("darwin DOES kickstart; this gates the non-darwin no-op")
	}
	if err := kickstartDaemon(); err != nil {
		t.Errorf("kickstartDaemon should be a nil no-op off darwin; got %v", err)
	}
}
