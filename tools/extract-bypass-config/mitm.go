// SPDX-License-Identifier: MIT
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type MitmStatus struct {
	Available   bool
	BinaryPath  string
	CertTrusted bool
	InstallHint string
}

func (s MitmStatus) String() string {
	if !s.Available {
		return fmt.Sprintf("mitmproxy: NOT installed\n  install: %s", s.InstallHint)
	}
	cert := "trusted"
	if !s.CertTrusted {
		cert = "NOT trusted (run mitmproxy once + add ~/.mitmproxy/mitmproxy-ca-cert.pem to System keychain)"
	}
	return fmt.Sprintf("mitmproxy: installed at %s\n  cert: %s", s.BinaryPath, cert)
}

func CheckMitmStatus() MitmStatus {
	path, err := exec.LookPath("mitmdump")
	if err != nil {
		return mitmStatusForPath("")
	}
	return mitmStatusForPath(path)
}

func mitmStatusForPath(path string) MitmStatus {
	hint := "brew install mitmproxy  # macOS"
	if runtime.GOOS == "linux" {
		hint = "pip install mitmproxy  # or distro package"
	}
	if path == "" {
		return MitmStatus{Available: false, InstallHint: hint}
	}
	if _, err := os.Stat(path); err != nil {
		return MitmStatus{Available: false, BinaryPath: path, InstallHint: hint}
	}
	return MitmStatus{Available: true, BinaryPath: path, CertTrusted: certInstalled(), InstallHint: hint}
}

func certInstalled() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(home, ".mitmproxy", "mitmproxy-ca-cert.pem"))
	return err == nil
}

func buildMitmArgs(listen, outPath string) []string {
	return []string{
		"--listen-host", "127.0.0.1",
		"--listen-port", listenPort(listen),

		"--set", "hardump=" + outPath,
		"--set", "block_global=false",

		"--allow-hosts", `^api\.anthropic\.com(:\d+)?$`,
		"--quiet",
	}
}

func listenPort(addr string) string {
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[i+1:]
	}
	return "8888"
}

func LaunchMitm(ctx context.Context, st MitmStatus, listen, outPath string) (*exec.Cmd, error) {
	if !st.Available {
		return nil, fmt.Errorf("mitmproxy not available: %s", st.InstallHint)
	}
	cmd := exec.CommandContext(ctx, st.BinaryPath, buildMitmArgs(listen, outPath)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start mitmdump: %w", err)
	}
	return cmd, nil
}
