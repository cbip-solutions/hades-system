// SPDX-License-Identifier: MIT
// transactional.go — temp-dir-swap primitives.
//
// Building blocks for the SOTA-1 #5 "write to temp dir then swap"
// invariant. Run() composes them; tests exercise them independently
// to verify the rollback contracts.
package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func StageTempDir(tag string) (string, error) {
	tmp, err := os.MkdirTemp("", "hades-scaffold-"+sanitizeID(tag)+"-*")
	if err != nil {
		return "", fmt.Errorf("stage tmp: %w", err)
	}
	return tmp, nil
}

func swapInto(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir parent: %w", err)
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !isCrossDeviceError(err) {
		return fmt.Errorf("rename %q -> %q: %w", src, dst, err)
	}

	if err := copyTree(src, dst); err != nil {
		_ = os.RemoveAll(dst)
		return fmt.Errorf("copy %q -> %q: %w", src, dst, err)
	}
	if err := os.RemoveAll(src); err != nil {
		return fmt.Errorf("remove src after copy: %w", err)
	}
	return nil
}

func isCrossDeviceError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, needle := range []string{"cross-device", "EXDEV", "invalid cross-device link"} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

func copyTree(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		out := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(out, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(out, data, info.Mode())
	})
}
