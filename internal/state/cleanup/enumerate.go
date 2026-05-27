// SPDX-License-Identifier: MIT
// Package cleanup — enumerate.go ships state directory enumeration for
// `hades state list` + Apply.
//
// Subsystems enumerated:
//
// doctor-backups under $XDG_STATE_HOME/hades-system/doctor-backups/
// migrate-backups under $XDG_STATE_HOME/hades-system/migrate-backups/
// spike-artifacts under $XDG_STATE_HOME/hades-system/spike-artifacts/
// cache under $XDG_CACHE_HOME/hades-system/ (LRU subdirs)
//
// Each StateEntry carries path + subsystem + ID + size + age + modTime
// for both the human renderer (hades state list) + JSON output (hades state
// list --json).
package cleanup

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

type StateEntry struct {
	Path      string        `json:"path"`
	Subsystem string        `json:"subsystem"`
	ID        string        `json:"id"`
	Size      int64         `json:"sizeBytes"`
	Age       time.Duration `json:"ageNs"`
	ModTime   time.Time     `json:"modTime"`
}

func Enumerate(_ context.Context, stateDir, cacheDir string) ([]StateEntry, error) {
	subsystems := []struct {
		name string
		root string
	}{
		{"doctor-backups", filepath.Join(stateDir, "doctor-backups")},
		{"migrate-backups", filepath.Join(stateDir, "migrate-backups")},
		{"spike-artifacts", filepath.Join(stateDir, "spike-artifacts")},
		{"cache", cacheDir},
	}
	var entries []StateEntry
	for _, sub := range subsystems {
		dirEntries, err := os.ReadDir(sub.root)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range dirEntries {
			path := filepath.Join(sub.root, e.Name())
			info, ierr := e.Info()
			if ierr != nil {
				continue
			}
			size, _ := dirSize(path)
			entries = append(entries, StateEntry{
				Path:      path,
				Subsystem: sub.name,
				ID:        e.Name(),
				Size:      size,
				Age:       time.Since(info.ModTime()),
				ModTime:   info.ModTime(),
			})
		}
	}
	return entries, nil
}

// dirSize sums file sizes under path. For a regular file, returns the
// file's size. For a directory, sums all descendants. Errors during
// walk are tolerated (skipped entries do not contribute).
func dirSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return info.Size(), nil
	}
	var total int64
	err = filepath.WalkDir(path, func(_ string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		i, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		total += i.Size()
		return nil
	})
	return total, err
}
