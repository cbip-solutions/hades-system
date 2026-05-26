// SPDX-License-Identifier: MIT
package writer

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/cbip-solutions/hades-system/internal/migrate/mapping"
)

func (w *Writer) backupIfNeeded(plan *mapping.Plan) error {
	if w.cfg.BackupRoot == "" {
		return nil
	}
	if !w.cfg.ForceOverwrite && !w.anyTargetExists(plan) {
		return nil
	}
	if err := os.MkdirAll(w.cfg.BackupRoot, 0o700); err != nil {
		return err
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	tarPath := filepath.Join(w.cfg.BackupRoot, stamp+".tar.gz")
	f, err := os.OpenFile(tarPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	roots := w.touchedRoots()
	for _, root := range roots {
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		}
		if err := walkAndArchive(root, tw); err != nil {
			return err
		}
	}
	return nil
}

func (w *Writer) anyTargetExists(plan *mapping.Plan) bool {
	for _, e := range plan.Entries {
		root, joinAsIs, err := w.routeTarget(e)
		if err != nil || root == "" {
			continue
		}
		var full string
		if joinAsIs {
			full = filepath.Join(root, filepath.FromSlash(stripPluginPrefix(e.TargetPath)))
		} else {
			full = filepath.Join(root, filepath.Base(e.TargetPath))
		}
		if _, err := os.Stat(full); err == nil {
			return true
		}
	}

	for _, root := range w.touchedRoots() {
		if entries, err := os.ReadDir(root); err == nil && len(entries) > 0 {
			return true
		}
	}
	return false
}

func (w *Writer) touchedRoots() []string {
	out := []string{}
	if w.cfg.HermesPluginRoot != "" {
		out = append(out, w.cfg.HermesPluginRoot)
	}
	if w.cfg.ZenConfigRoot != "" {
		out = append(out, w.cfg.ZenConfigRoot)
	}
	if w.cfg.HermesConfigPath != "" {
		dir := filepath.Dir(w.cfg.HermesConfigPath)

		seen := false
		for _, r := range out {
			if r == dir {
				seen = true
				break
			}
		}
		if !seen {
			out = append(out, dir)
		}
	}
	return out
}

func walkAndArchive(root string, tw *tar.Writer) error {
	rootParent := filepath.Dir(root)
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(rootParent, path)
		if relErr != nil {
			return relErr
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}

		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		hdr, hdrErr := tar.FileInfoHeader(info, "")
		if hdrErr != nil {
			return hdrErr
		}
		hdr.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := io.Copy(tw, f); err != nil {
			return fmt.Errorf("copy %s: %w", path, err)
		}
		return nil
	})
}
