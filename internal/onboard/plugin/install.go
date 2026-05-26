// SPDX-License-Identifier: MIT
package plugin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type InstallOptions struct {
	Location Location

	Manifest []byte

	Scope string

	ProjectDir string

	Slug string
}

func Install(ctx context.Context, opts InstallOptions) (canonical string, err error) {
	if opts.Location.Path == "" {
		return "", fmt.Errorf("plugin: empty Location.Path")
	}
	if opts.Location.Kind != LocationKindProjectScope && opts.Location.Kind != LocationKindUserScope {
		return "", fmt.Errorf("plugin: invalid Location.Kind=%v", opts.Location.Kind)
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("plugin: ctx canceled: %w", err)
	}

	target := opts.Location.Path
	if err := os.MkdirAll(target, 0o700); err != nil {
		return "", fmt.Errorf("mkdir plugin dir: %w", err)
	}

	if err := os.Chmod(target, 0o700); err != nil {
		return "", fmt.Errorf("chmod plugin dir: %w", err)
	}

	manifestPath := filepath.Join(target, "plugin.toml")
	tmp := manifestPath + ".tmp"
	if err := os.WriteFile(tmp, opts.Manifest, 0o600); err != nil {
		return "", fmt.Errorf("write manifest tmp: %w", err)
	}
	if err := os.Rename(tmp, manifestPath); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("rename manifest: %w", err)
	}

	if err := os.Chmod(manifestPath, 0o600); err != nil {
		return "", fmt.Errorf("chmod manifest: %w", err)
	}
	return target, nil
}

func Uninstall(loc Location) error {
	if loc.Path == "" {
		return fmt.Errorf("plugin: empty Location.Path")
	}
	if err := os.RemoveAll(loc.Path); err != nil {
		return fmt.Errorf("rm plugin dir: %w", err)
	}
	return nil
}
