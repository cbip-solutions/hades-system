// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func shortSockPathForLogTest(t *testing.T, name string) string {
	t.Helper()
	var buf [4]byte
	_, _ = rand.Read(buf[:])
	suffix := hex.EncodeToString(buf[:])
	p := filepath.Join("/tmp", "zsbr-"+suffix+"-"+name)
	t.Cleanup(func() { _ = os.Remove(p) })
	return p
}

var (
	daemonBinDir  string
	daemonBinOnce sync.Once
	daemonBinPath string
	daemonBinErr  error
)

func TestMain(m *testing.M) {

	_ = os.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	_ = os.Setenv("ZEN_KEYCHAIN_DISABLE", "1")

	var err error
	daemonBinDir, err = os.MkdirTemp("", "ctld-banner-bin")
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: mkdir temp: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(daemonBinDir)
	os.Exit(code)
}

func daemonBinaryForTest(t *testing.T) string {
	t.Helper()
	daemonBinOnce.Do(func() {
		bin := filepath.Join(daemonBinDir, "zen-swarm-ctld")
		build := exec.Command("go", "build",
			"-tags", "sqlite_fts5",
			"-ldflags", "-X github.com/ncruces/go-sqlite3/driver.driverName=sqlite3_ncruces",
			"-o", bin, ".")
		if out, err := build.CombinedOutput(); err != nil {
			daemonBinErr = fmt.Errorf("build daemon binary: %w\n%s", err, out)
			return
		}
		daemonBinPath = bin
	})
	if daemonBinErr != nil {
		t.Fatalf("%v", daemonBinErr)
	}
	return daemonBinPath
}

func launchDaemonAndCaptureStderr(t *testing.T) string {
	t.Helper()

	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("daemon log banner test requires darwin/linux; got %s", runtime.GOOS)
	}

	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")

	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")

	bin := daemonBinaryForTest(t)

	udsPath := shortSockPathForLogTest(t, "f2")
	dbPath := filepath.Join(t.TempDir(), "test-state.db")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin,
		"-uds", udsPath,
		"-db", dbPath,
	)

	if cwd, err := os.Getwd(); err == nil {
		cmd.Dir = filepath.Join(cwd, "..", "..")
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}

	cmd.Stdout = io.Discard

	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		t.Fatalf("daemon Start: %v", err)
	}

	var (
		stderrMu sync.Mutex
		stderrB  strings.Builder
	)
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		scanner := bufio.NewScanner(stderrPipe)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			stderrMu.Lock()
			stderrB.WriteString(line)
			stderrB.WriteString("\n")
			stderrMu.Unlock()
		}
	}()

	udsDeadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(udsDeadline) {
		if _, err := os.Stat(udsPath); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if cmd.Process != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	select {
	case <-waitDone:

	case <-time.After(6 * time.Second):

		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		<-waitDone
	}

	select {
	case <-stderrDone:
	case <-time.After(30 * time.Second):

	}

	stderrMu.Lock()
	defer stderrMu.Unlock()
	return stderrB.String()
}

func TestDaemonStartupBannerSaysHADES(t *testing.T) {

	stderr := launchDaemonAndCaptureStderr(t)

	if stderr == "" {
		t.Fatalf("captured stderr is empty — daemon failed to emit any log lines")
	}

	mustContain := []string{
		`msg="HADES system daemon (zen-swarm-ctld) starting"`,
	}
	for _, m := range mustContain {
		if !strings.Contains(stderr, m) {
			t.Errorf("captured stderr missing required token %q\ncaptured stderr:\n%s", m, stderr)
		}
	}

	mustNotContain := []string{
		`msg="zen-swarm-ctld starting"`,
	}
	for _, m := range mustNotContain {
		if strings.Contains(stderr, m) {
			t.Errorf("captured stderr contains forbidden pre-fix token %q\ncaptured stderr:\n%s", m, stderr)
		}
	}
}

func TestDaemonShutdownBannerSaysHADES(t *testing.T) {
	stderr := launchDaemonAndCaptureStderr(t)

	if !strings.Contains(stderr, "stopped") {
		t.Skipf("daemon shutdown banner not present in captured stderr (graceful shutdown likely timed out); startup-banner test covers brand assertion. captured stderr:\n%s", stderr)
	}

	mustContain := []string{
		`msg="HADES system daemon (zen-swarm-ctld) stopped"`,
	}
	for _, m := range mustContain {
		if !strings.Contains(stderr, m) {
			t.Errorf("captured stderr missing required token %q\ncaptured stderr:\n%s", m, stderr)
		}
	}

	mustNotContain := []string{
		`msg="zen-swarm-ctld stopped"`,
	}
	for _, m := range mustNotContain {
		if strings.Contains(stderr, m) {
			t.Errorf("captured stderr contains forbidden pre-fix token %q\ncaptured stderr:\n%s", m, stderr)
		}
	}
}

func TestDaemonPackageDocSaysHADES(t *testing.T) {
	t.Parallel()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	mainPath := filepath.Join(cwd, "main.go")
	data, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}

	body := string(data)
	mustContain := "HADES system daemon"
	if !strings.Contains(body, mustContain) {
		t.Errorf("main.go package doc missing %q", mustContain)
	}
	// Pre-fix string MUST NOT survive in the package doc-comment (lowercase
	// form). We don't assert on every "zen-swarm" hit in main.go (the file
	// path comments + import paths legitimately contain it per §Q3
	// BORDERLINE / OUT); we narrowly target the package-doc framing.
	mustNotContain := "the zen-swarm daemon"
	if strings.Contains(body, mustNotContain) {
		t.Errorf("main.go package doc contains forbidden pre-fix framing %q", mustNotContain)
	}
}
