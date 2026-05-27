// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT

// go:build !darwin
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func registerZenScheme(ctx context.Context) error {
	if runtime.GOOS != "linux" {
		return ErrUnsupportedPlatform
	}
	if _, err := exec.LookPath("xdg-mime"); err != nil {
		return fmt.Errorf("registerZenScheme: xdg-mime not found: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("registerZenScheme: home dir: %w", err)
	}
	appsDir := filepath.Join(home, ".local", "share", "applications")
	if err := os.MkdirAll(appsDir, 0o755); err != nil {
		return fmt.Errorf("registerZenScheme: mkdir: %w", err)
	}

	desktopPath := filepath.Join(appsDir, "zen-swarm-ctld.desktop")
	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("registerZenScheme: executable path: %w", err)
	}
	desktop := buildDesktopFile(binPath)
	if err := os.WriteFile(desktopPath, []byte(desktop), 0o644); err != nil {
		return fmt.Errorf("registerZenScheme: write desktop file: %w", err)
	}

	updCmd := exec.CommandContext(ctx, "update-desktop-database", appsDir)
	_ = updCmd.Run()

	cmd := exec.CommandContext(ctx, "xdg-mime", "default", "zen-swarm-ctld.desktop", "x-scheme-handler/zen")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("registerZenScheme: xdg-mime failed: %w (output: %s)", err, string(out))
	}
	return nil
}

func buildDesktopFile(binPath string) string {
	return fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=zen-swarm-ctld
Exec=%s url-handler %%u
Terminal=true
NoDisplay=true
MimeType=x-scheme-handler/zen;
`, binPath)
}
