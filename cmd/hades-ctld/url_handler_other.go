// Copyright 2026 hades-system contributors. SPDX-License-Identifier: MIT

//go:build !darwin

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func registerHadesScheme(ctx context.Context) error {
	if runtime.GOOS != "linux" {
		return ErrUnsupportedPlatform
	}
	if _, err := exec.LookPath("xdg-mime"); err != nil {
		return fmt.Errorf("registerHadesScheme: xdg-mime not found: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("registerHadesScheme: home dir: %w", err)
	}
	appsDir := filepath.Join(home, ".local", "share", "applications")
	if err := os.MkdirAll(appsDir, 0o755); err != nil {
		return fmt.Errorf("registerHadesScheme: mkdir: %w", err)
	}

	desktopPath := filepath.Join(appsDir, "hades-ctld.desktop")
	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("registerHadesScheme: executable path: %w", err)
	}
	desktop := buildDesktopFile(binPath)
	if err := os.WriteFile(desktopPath, []byte(desktop), 0o644); err != nil {
		return fmt.Errorf("registerHadesScheme: write desktop file: %w", err)
	}

	updCmd := exec.CommandContext(ctx, "update-desktop-database", appsDir)
	_ = updCmd.Run()

	cmd := exec.CommandContext(ctx, "xdg-mime", "default", "hades-ctld.desktop", "x-scheme-handler/hades")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("registerHadesScheme: xdg-mime failed: %w (output: %s)", err, string(out))
	}
	return nil
}

func buildDesktopFile(binPath string) string {
	return fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=hades-ctld
Exec=%s url-handler %%u
Terminal=true
NoDisplay=true
MimeType=x-scheme-handler/hades;
`, binPath)
}
