package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func helperBuildZen(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "zen")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}

	cmd := exec.Command("go", "build",
		"-tags=sqlite_fts5",
		"-ldflags=-X github.com/ncruces/go-sqlite3/driver.driverName=sqlite3_ncruces",
		"-o", bin, ".")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build zen: %v\n%s", err, stderr.String())
	}
	return bin
}

func TestSubprocess_UnknownSubcommand_RendersHADESBlock(t *testing.T) {
	bin := helperBuildZen(t)

	out, err := exec.Command(bin, "no-such-cmd").CombinedOutput()
	exitErr, _ := err.(*exec.ExitError)
	if exitErr == nil {
		t.Fatalf("expected non-zero exit for unknown subcommand; got nil err. Output:\n%s", out)
	}

	gotStr := string(out)

	if !strings.Contains(gotStr, "HADES:") {
		t.Errorf("output missing 'HADES:' headline\nfull output:\n%s", gotStr)
	}

	lowerOut := strings.ToLower(gotStr)
	if !strings.Contains(lowerOut, "unknown") && !strings.Contains(lowerOut, "command") {
		t.Errorf("output missing 'unknown' or 'command' text\nfull output:\n%s", gotStr)
	}
}

func TestSubprocess_UnknownSubcommand_NearMiss_SuggestsCommand(t *testing.T) {
	bin := helperBuildZen(t)

	out, err := exec.Command(bin, "doctr").CombinedOutput()
	exitErr, _ := err.(*exec.ExitError)
	if exitErr == nil {
		t.Fatalf("expected non-zero exit for near-miss subcommand; got nil err. Output:\n%s", out)
	}
	if got := exitErr.ExitCode(); got != 1 {
		t.Errorf("near-miss exit code = %d; want 1 (error severity)\noutput:\n%s", got, out)
	}

	gotStr := string(out)
	if !strings.Contains(gotStr, "HADES:") {
		t.Fatalf("output missing 'HADES:' headline\nfull output:\n%s", gotStr)
	}

	const wantPhrase = "did you mean `hades doctor`?"
	if !strings.Contains(gotStr, wantPhrase) {
		t.Errorf("near-miss output missing exact phrase %q\nfull output:\n%s", wantPhrase, gotStr)
	}

	if strings.Contains(gotStr, "{{") {
		t.Errorf("output has dangling '{{' template literal\nfull output:\n%s", gotStr)
	}
}

func TestSubprocess_HelpFlag_NoHADESBlock(t *testing.T) {
	bin := helperBuildZen(t)

	out, err := exec.Command(bin, "--help").CombinedOutput()
	if err != nil {
		t.Fatalf("zen --help unexpected non-zero exit: %v\noutput:\n%s", err, out)
	}
	if strings.Contains(string(out), "HADES:") {
		t.Errorf("zen --help leaked HADES error block:\n%s", out)
	}
}

func TestSubprocess_VerboseFlag_AppendsTraceback(t *testing.T) {
	bin := helperBuildZen(t)

	out, _ := exec.Command(bin, "--verbose", "no-such-cmd").CombinedOutput()

	gotStr := string(out)
	if !strings.Contains(gotStr, "HADES:") {
		t.Fatalf("output missing HADES headline:\n%s", gotStr)
	}
	arrowIdx := strings.Index(gotStr, "→ ")
	if arrowIdx == -1 {
		t.Fatalf("output missing recovery arrow:\n%s", gotStr)
	}
	tail := gotStr[arrowIdx:]

	if !strings.Contains(tail, "\n") {
		t.Errorf("verbose output missing traceback after recovery hint:\n%s", gotStr)
	}

	if !strings.Contains(gotStr, strings.Repeat("─", 60)) {
		t.Errorf("verbose output missing 60-char divider:\n%s", gotStr)
	}
}

func TestSubprocess_NoColorFlag_SuppressesANSI(t *testing.T) {
	bin := helperBuildZen(t)

	out, _ := exec.Command(bin, "--no-color", "no-such-cmd").CombinedOutput()
	if strings.Contains(string(out), "\x1b[") {
		t.Errorf("--no-color leaked ANSI escape:\n%s", out)
	}
}

func TestSubprocess_PanicCaught_RendersHADESBlock(t *testing.T) {
	bin := helperBuildZen(t)

	cmd := exec.Command(bin, "--help")
	cmd.Env = append(cmd.Environ(), "ZEN_TEST_PANIC=injected-test-panic-string")
	out, err := cmd.CombinedOutput()
	exitErr, _ := err.(*exec.ExitError)
	if exitErr == nil {
		t.Fatalf("expected non-zero exit for panic injection; got nil err. Output:\n%s", out)
	}
	if got := exitErr.ExitCode(); got != 2 {
		t.Errorf("panic exit code = %d; want 2 (fatal)\noutput:\n%s", got, out)
	}

	gotStr := string(out)
	required := []string{
		"HADES:",
		"injected-test-panic-string",
	}
	for _, sub := range required {
		if !strings.Contains(gotStr, sub) {
			t.Errorf("panic output missing %q\nfull output:\n%s", sub, gotStr)
		}
	}

	stackMarkers := []string{"main.main", "goroutine", "runtime/"}
	foundStack := false
	for _, m := range stackMarkers {
		if strings.Contains(gotStr, m) {
			foundStack = true
			break
		}
	}
	if !foundStack {
		t.Errorf("panic output missing stack-trace marker (one of %v)\nfull output:\n%s", stackMarkers, gotStr)
	}
}
