// SPDX-License-Identifier: MIT
// Package backup ships the release backup-before-modify
// substrate per spec §2.5 + §2.12 + §5.1 + invariant.
//
// Backups land at $XDG_STATE_HOME/zen-swarm/doctor-backups/<ISO8601>/<check>/
// with manifest.json (mode 0600) + content.tar.gz tarball. Manifest
// carries: BackupID (ISO8601 UTC) + CheckName + SourcePath + TarballPath +
// AuditEventHash + Files (list of paths relative to SourcePath).
//
// Boundary: backup package consumes ONLY stdlib (os, io,
// archive/tar, compress/gzip, encoding/json, path/filepath, time); MUST
// NOT import internal/store.
//
// Defense-in-depth posture:
// - Walks skip symlinks during backup (avoid following hostile targets)
// - Tar extraction rejects path-traversal entries (isPathWithin guard)
// - Manifest is mode 0600 (operator-only)
// - Conflict-halt on restore unless --overwrite explicit
// - Atomic tarball + manifest write via temp+rename
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

type Manifest struct {
	BackupID       string    `json:"backupId"`
	CheckName      string    `json:"checkName"`
	SourcePath     string    `json:"sourcePath"`
	TarballPath    string    `json:"tarballPath"`
	Path           string    `json:"-"`
	CreatedAt      time.Time `json:"createdAt"`
	Files          []string  `json:"files"`
	AuditEventHash string    `json:"auditEventHash,omitempty"`
	RestoreCommand string    `json:"restoreCommand"`
}

type Backuper struct {
	stateDir string
}

type Config struct {
	StateDir string
}

func NewBackuper(cfg Config) *Backuper {
	dir := cfg.StateDir
	if dir == "" {
		xdg := os.Getenv("XDG_STATE_HOME")
		if xdg == "" {
			home, _ := os.UserHomeDir()
			xdg = filepath.Join(home, ".local", "state")
		}
		dir = filepath.Join(xdg, "zen-swarm")
	}
	return &Backuper{stateDir: dir}
}

func (b *Backuper) StateDir() string {
	return b.stateDir
}

func (b *Backuper) backupRoot() string {
	return filepath.Join(b.stateDir, "doctor-backups")
}

func (b *Backuper) BackupTarget(ctx context.Context, checkName, sourcePath string) (Manifest, error) {
	id := time.Now().UTC().Format("20060102T150405Z")
	dir := filepath.Join(b.backupRoot(), id, checkName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Manifest{}, fmt.Errorf("backup: mkdir %s: %w", dir, err)
	}
	tarPath := filepath.Join(dir, "content.tar.gz")
	manifestPath := filepath.Join(dir, "manifest.json")

	files, err := writeTarGz(sourcePath, tarPath)
	if err != nil {
		_ = os.RemoveAll(dir)
		return Manifest{}, fmt.Errorf("backup: tar.gz %s: %w", sourcePath, err)
	}

	m := Manifest{
		BackupID:       id,
		CheckName:      checkName,
		SourcePath:     sourcePath,
		TarballPath:    tarPath,
		Path:           manifestPath,
		CreatedAt:      time.Now().UTC(),
		Files:          files,
		RestoreCommand: fmt.Sprintf("zen doctor restore %s", id),
	}
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		_ = os.RemoveAll(dir)
		return Manifest{}, fmt.Errorf("backup: marshal manifest: %w", err)
	}
	tmpPath := manifestPath + ".tmp"
	if err := os.WriteFile(tmpPath, body, 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return Manifest{}, fmt.Errorf("backup: write manifest: %w", err)
	}
	if err := os.Rename(tmpPath, manifestPath); err != nil {
		_ = os.RemoveAll(dir)
		return Manifest{}, fmt.Errorf("backup: rename manifest: %w", err)
	}

	_ = os.Chmod(manifestPath, 0o600)
	_ = ctx
	return m, nil
}

func (b *Backuper) RemoveAfterBackup(_ context.Context, sourcePath string) error {
	return os.RemoveAll(sourcePath)
}

func (b *Backuper) LoadManifestByID(_ context.Context, id string) (Manifest, error) {
	idDir := filepath.Join(b.backupRoot(), id)
	entries, err := os.ReadDir(idDir)
	if errors.Is(err, fs.ErrNotExist) {
		return Manifest{}, ErrNotFound
	}
	if err != nil {
		return Manifest{}, fmt.Errorf("backup: list %s: %w", idDir, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		mPath := filepath.Join(idDir, e.Name(), "manifest.json")
		body, rerr := os.ReadFile(mPath)
		if rerr != nil {
			continue
		}
		var m Manifest
		if jerr := json.Unmarshal(body, &m); jerr != nil {
			continue
		}
		m.Path = mPath
		return m, nil
	}
	return Manifest{}, ErrNotFound
}

var ErrNotFound = errors.New("backup: manifest not found for backup ID")

func writeTarGz(sourcePath, tarPath string) ([]string, error) {
	tmpTar := tarPath + ".tmp"
	f, err := os.OpenFile(tmpTar, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	var files []string
	walkErr := filepath.WalkDir(sourcePath, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, rerr := filepath.Rel(sourcePath, path)
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}

		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		header, herr := tar.FileInfoHeader(info, "")
		if herr != nil {
			return herr
		}
		header.Name = rel
		if werr := tw.WriteHeader(header); werr != nil {
			return werr
		}
		if d.IsDir() {
			return nil
		}
		files = append(files, rel)
		src, oerr := os.Open(path)
		if oerr != nil {
			return oerr
		}
		_, cerr := io.Copy(tw, src)
		_ = src.Close()
		return cerr
	})

	if cerr := tw.Close(); cerr != nil && walkErr == nil {
		walkErr = cerr
	}
	if cerr := gz.Close(); cerr != nil && walkErr == nil {
		walkErr = cerr
	}
	if cerr := f.Close(); cerr != nil && walkErr == nil {
		walkErr = cerr
	}
	if walkErr != nil {
		_ = os.Remove(tmpTar)
		return nil, walkErr
	}
	if err := os.Rename(tmpTar, tarPath); err != nil {
		return nil, err
	}
	return files, nil
}
