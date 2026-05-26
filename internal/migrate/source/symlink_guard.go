// SPDX-License-Identifier: MIT
package source

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func assertNoSymlinkEscape(absRoot string) error {

	rootEval, err := filepath.EvalSymlinks(absRoot)
	if err != nil {

		rootEval = absRoot
	}
	return filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsPermission(err) {

				return nil
			}
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return nil
		}

		target, err := os.Readlink(path)
		if err != nil {
			return fmt.Errorf("readlink %q: %w", path, err)
		}

		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		absTarget, err := filepath.Abs(target)
		if err != nil {
			return fmt.Errorf("abs target %q: %w", target, err)
		}

		resolved, err := filepath.EvalSymlinks(absTarget)
		if err != nil {

			return nil
		}
		rel, err := filepath.Rel(rootEval, resolved)
		if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			return fmt.Errorf("%w: %s -> %s", ErrSymlinkOutsideRoot, path, resolved)
		}
		return nil
	})
}
