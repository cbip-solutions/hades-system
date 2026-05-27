// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT

// go:build darwin
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func registerZenScheme(ctx context.Context) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("registerZenScheme: home dir: %w", err)
	}
	prefsDir := filepath.Join(home, "Library", "Preferences")
	if err := os.MkdirAll(prefsDir, 0o755); err != nil {
		return fmt.Errorf("registerZenScheme: mkdir: %w", err)
	}

	plistPath := filepath.Join(prefsDir, "zen-swarm-ctld-info.plist")
	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("registerZenScheme: executable path: %w", err)
	}
	plist := buildInfoPlist(binPath)
	if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
		return fmt.Errorf("registerZenScheme: write plist: %w", err)
	}

	lsregister := "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"
	if _, err := os.Stat(lsregister); err != nil {
		return fmt.Errorf("registerZenScheme: lsregister not found: %w", err)
	}
	cmd := exec.CommandContext(ctx, lsregister, "-f", plistPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("registerZenScheme: lsregister failed: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func buildInfoPlist(binPath string) string {
	bundleID := "dev.zen-swarm.ctld." + sanitiseBundleID(binPath)
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleExecutable</key>
  <string>zen-swarm-ctld</string>
  <key>CFBundleIdentifier</key>
  <string>%s</string>
  <key>CFBundleName</key>
  <string>zen-swarm-ctld</string>
  <key>CFBundleURLTypes</key>
  <array>
    <dict>
      <key>CFBundleURLName</key>
      <string>Zen URL</string>
      <key>CFBundleURLSchemes</key>
      <array>
        <string>zen</string>
      </array>
    </dict>
  </array>
</dict>
</plist>
`, bundleID)
}

func sanitiseBundleID(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			out = append(out, c)
		case c == '-' || c == '_':
			out = append(out, c)
		}
	}
	if len(out) > 32 {
		out = out[:32]
	}
	if len(out) == 0 {
		return "default"
	}
	return string(out)
}
