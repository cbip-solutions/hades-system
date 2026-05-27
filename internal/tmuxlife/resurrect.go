// SPDX-License-Identifier: MIT
package tmuxlife

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Snapshot struct {
	SessionName string `json:"session_name"`

	Path string `json:"path"`

	CreatedAt time.Time `json:"created_at"`

	SizeBytes int64 `json:"size_bytes"`
}

func (s Snapshot) IsValid() bool {
	return s.SessionName != "" && s.Path != "" && !s.CreatedAt.IsZero() && s.SizeBytes > 0
}

// SnapshotPath returns the canonical filesystem path for a snapshot.
// Format <dir>/<alias>-YYYYMMDDTHHMMSSZ.tar.gz (UTC, second precision,
// no millis). The format is sortable lexicographically — important for
// PruneOldSnapshots and Restore which order by name to find the K most
// recent / latest.
//
// Operator-facing forensics: the path encodes the alias + timestamp so
// `ls -1 ~/.config/hades-system/tmux-snapshots/` yields a chronological
// browseable history. Stable across phases; do NOT change format
// without a migration path for old snapshots.
func SnapshotPath(dir, alias string, ts time.Time) string {
	stamp := ts.UTC().Format("20060102T150405Z")
	name := fmt.Sprintf("%s-%s.tar.gz", alias, stamp)
	return filepath.Join(dir, name)
}

func DefaultSnapshotDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("DefaultSnapshotDir: %w", err)
	}
	dir := filepath.Join(home, SnapshotDirSubpath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("DefaultSnapshotDir mkdir %q: %w", dir, err)
	}
	return dir, nil
}

const scratchSentinel = "@scratch-window-content"

type resurrectExec interface {
	save(ctx context.Context, sessionName string) ([]byte, error)

	restore(ctx context.Context, sessionName string, tarball []byte) error
}

type realResurrectExec struct{}

func (realResurrectExec) save(ctx context.Context, sessionName string) ([]byte, error) {
	resurrectDir, err := defaultResurrectDir()
	if err != nil {
		return nil, fmt.Errorf("realResurrectExec.save: %w", err)
	}
	scriptPath := filepath.Join(homeOrEmpty(), ".tmux", "plugins", "tmux-resurrect", "scripts", "save.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		return nil, fmt.Errorf("realResurrectExec.save: plugin not installed at %q: %w", scriptPath, err)
	}
	if _, err := ExecTmux(ctx, "-S", SocketPath, "run-shell", scriptPath); err != nil {
		return nil, fmt.Errorf("realResurrectExec.save: run-shell save: %w", err)
	}
	return tarResurrectFiltered(resurrectDir, sessionName)
}

func (realResurrectExec) restore(ctx context.Context, sessionName string, tarball []byte) error {
	resurrectDir, err := defaultResurrectDir()
	if err != nil {
		return fmt.Errorf("realResurrectExec.restore: %w", err)
	}
	if err := os.MkdirAll(resurrectDir, 0o755); err != nil {
		return fmt.Errorf("realResurrectExec.restore: mkdir resurrectDir: %w", err)
	}
	if err := untarTo(resurrectDir, tarball); err != nil {
		return fmt.Errorf("realResurrectExec.restore: untar: %w", err)
	}
	scriptPath := filepath.Join(homeOrEmpty(), ".tmux", "plugins", "tmux-resurrect", "scripts", "restore.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		return fmt.Errorf("realResurrectExec.restore: plugin not installed at %q: %w", scriptPath, err)
	}
	if _, err := ExecTmux(ctx, "-S", SocketPath, "run-shell", scriptPath); err != nil {
		return fmt.Errorf("realResurrectExec.restore: run-shell restore: %w", err)
	}
	return nil
}

func homeOrEmpty() string {
	h, _ := os.UserHomeDir()
	return h
}

func defaultResurrectDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("defaultResurrectDir: %w", err)
	}
	return filepath.Join(home, ".local", "share", "tmux", "resurrect"), nil
}

type tarPair struct {
	name string
	body []byte
}

func tarResurrectFiltered(resurrectDir, sessionName string) ([]byte, error) {
	_ = sessionName

	entries, err := os.ReadDir(resurrectDir)
	if err != nil {
		return nil, fmt.Errorf("tarResurrectFiltered ReadDir %q: %w", resurrectDir, err)
	}

	pairs := make([]tarPair, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(resurrectDir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {

			continue
		}
		pairs = append(pairs, tarPair{name: e.Name(), body: stripScratchLines(raw)})
	}

	var buf bytes.Buffer

	if err := writeTarballToWriter(&buf, pairs, nil); err != nil {
		return nil, fmt.Errorf("tarResurrectFiltered: %w", err)
	}
	return buf.Bytes(), nil
}

func writeTarballToWriter(w io.Writer, pairs []tarPair, sizeOverride func(tarPair) int64) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	for _, p := range pairs {
		size := int64(len(p.body))
		if sizeOverride != nil {
			size = sizeOverride(p)
		}
		hdr := &tar.Header{Name: p.name, Mode: 0o644, Size: size}
		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("WriteHeader %q: %w", p.name, err)
		}
		if _, err := tw.Write(p.body); err != nil {
			return fmt.Errorf("Write %q: %w", p.name, err)
		}
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("tar Close: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("gzip Close: %w", err)
	}
	return nil
}

func stripScratchLines(raw []byte) []byte {
	var out bytes.Buffer
	for _, line := range bytes.Split(raw, []byte("\n")) {

		if bytes.Contains(line, []byte("\tscratch\t")) {
			continue
		}

		if bytes.Contains(line, []byte(scratchSentinel)) {
			continue
		}
		out.Write(line)
		out.WriteByte('\n')
	}
	return out.Bytes()
}

func untarTo(dir string, payload []byte) error {
	gzr, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("untarTo gunzip: %w", err)
	}
	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("untarTo tar.Next: %w", err)
		}
		path := filepath.Join(dir, hdr.Name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("untarTo mkdir parent %q: %w", filepath.Dir(path), err)
		}
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, os.FileMode(hdr.Mode))
		if err != nil {
			return fmt.Errorf("untarTo OpenFile %q: %w", path, err)
		}
		if _, err := io.Copy(f, tr); err != nil {
			_ = f.Close()
			return fmt.Errorf("untarTo Copy %q: %w", path, err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("untarTo Close %q: %w", path, err)
		}
	}
	return nil
}

func scratchInPayload(payload []byte) bool {
	gzr, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {

		return true
	}
	tr := tar.NewReader(gzr)
	for {
		_, err := tr.Next()
		if err == io.EOF {
			return false
		}
		if err != nil {

			return true
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			return true
		}
		if bytes.Contains(body, []byte(scratchSentinel)) {
			return true
		}
		if bytes.Contains(body, []byte("\tscratch\t")) {
			return true
		}
	}
}

func (m *Manager) Save(ctx context.Context, alias string) (*Snapshot, error) {
	s, err := m.resolveAlias(alias)
	if err != nil {

		if errors.Is(err, ErrSessionNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("Manager.Save: %w", err)
	}

	payload, err := m.resurrect.save(ctx, s.Name)
	if err != nil {
		return nil, fmt.Errorf("Manager.Save: resurrect.save: %w", err)
	}

	if scratchInPayload(payload) {
		return nil, ErrScratchExclusionViolated
	}

	dir := m.snapshotDir
	if dir == "" {
		var err error
		dir, err = DefaultSnapshotDir()
		if err != nil {
			return nil, fmt.Errorf("Manager.Save: %w", err)
		}
		m.snapshotDir = dir
	} else {

		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("Manager.Save: mkdir %q: %w", dir, err)
		}
	}

	now := m.now()
	path := SnapshotPath(dir, alias, now)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return nil, fmt.Errorf("Manager.Save: write %q: %w", path, err)
	}

	stat, err := m.statFn(path)
	if err != nil {

		return nil, fmt.Errorf("Manager.Save: stat %q: %w", path, err)
	}

	return &Snapshot{
		SessionName: s.Name,
		Path:        path,
		CreatedAt:   now.UTC(),
		SizeBytes:   stat.Size(),
	}, nil
}

func (m *Manager) Restore(ctx context.Context, alias string) error {
	dir := m.snapshotDir
	if dir == "" {
		var err error
		dir, err = DefaultSnapshotDir()
		if err != nil {
			return fmt.Errorf("Manager.Restore: %w", err)
		}
		m.snapshotDir = dir
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("Manager.Restore: ReadDir %q: %w", dir, err)
	}
	matching := make([]string, 0)
	prefix := alias + "-"
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".tar.gz") {
			continue
		}
		matching = append(matching, e.Name())
	}
	if len(matching) == 0 {
		return fmt.Errorf("Manager.Restore: %w", ErrSessionNotFound)
	}
	sort.Strings(matching)
	latest := matching[len(matching)-1]
	path := filepath.Join(dir, latest)
	payload, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("Manager.Restore: read %q: %w (%v)", path, ErrSnapshotCorrupt, err)
	}

	s, err := m.resolveAlias(alias)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return err
		}
		return fmt.Errorf("Manager.Restore: %w", err)
	}

	if err := m.resurrect.restore(ctx, s.Name, payload); err != nil {
		return fmt.Errorf("Manager.Restore: resurrect.restore: %w", err)
	}
	return nil
}

// PruneOldSnapshots keeps the keepLast most recent.tar.gz files in dir
// and deletes the rest. Files are ordered by name (lexicographic; the
// SnapshotPath naming convention guarantees this is chronological for
// a single alias). Files not matching.tar.gz are left alone (a stray
// .DS_Store or operator's own file must survive).
//
// keepLast == 0 (or negative) is a no-op (failsafe; operator misconfig
// must not nuke history). Errors during a single delete do not abort
// the sweep — best-effort cleanup of independent files.
//
// Doctrine matrix: max-scope=7, default=7, capa-firewall=3 (per spec
// §1 Q7 D + §3.6); the caller chooses the value via doctrine config
// at the daemon-startup wiring layer.
//
// This function is package-level (not a Manager method) because it
// operates on a directory path with no Manager state required; the
// daemon scheduler can invoke it directly without instantiating a
// Manager just to prune.
func PruneOldSnapshots(dir string, keepLast int) error {
	if keepLast <= 0 {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("PruneOldSnapshots ReadDir %q: %w", dir, err)
	}
	tarballs := make([]string, 0)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".tar.gz") {
			continue
		}
		tarballs = append(tarballs, e.Name())
	}
	if len(tarballs) <= keepLast {
		return nil
	}
	sort.Strings(tarballs)
	toDelete := tarballs[:len(tarballs)-keepLast]
	for _, name := range toDelete {

		_ = os.Remove(filepath.Join(dir, name))
	}
	return nil
}
