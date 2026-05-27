// SPDX-License-Identifier: MIT
package safetynet

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

var (
	ErrPrevNotInstalled = errors.New("safetynet/prev: hades-prev binary not installed")
	ErrPrevHashMismatch = errors.New("safetynet/prev: sha256 mismatch — refusing install")
	ErrPrevManifest     = errors.New("safetynet/prev: invalid manifest")
)

type Manifest struct {
	Version    string
	BinaryPath string
	Sha256     string
}

type Info struct {
	Installed bool
	Path      string
	Version   string
	Sha256    string
}

type Emitter interface {
	Emit(ctx context.Context, e Event) error
}

type Prev struct {
	dst     string
	emit    Emitter
	version string
}

func NewPrev(dst string, emit Emitter) *Prev {
	return &Prev{dst: dst, emit: emit}
}

func (p *Prev) Install(ctx context.Context, m Manifest) error {
	if m.BinaryPath == "" || m.Sha256 == "" || m.Version == "" {
		return fmt.Errorf("%w: empty field in %+v", ErrPrevManifest, m)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("safetynet/prev: ctx cancelled before start: %w", err)
	}
	src, err := os.Open(m.BinaryPath)
	if err != nil {
		return fmt.Errorf("safetynet/prev: open source: %w", err)
	}
	defer src.Close()

	if err := os.MkdirAll(filepath.Dir(p.dst), 0o755); err != nil {
		return fmt.Errorf("safetynet/prev: mkdir: %w", err)
	}
	tmp := p.dst + ".tmp"
	gotHash, err := writeHashedTmp(tmp, src)
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if gotHash != m.Sha256 {
		_ = os.Remove(tmp)
		return fmt.Errorf("%w: want=%s got=%s", ErrPrevHashMismatch, m.Sha256, gotHash)
	}
	if err := os.Rename(tmp, p.dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("safetynet/prev: rename: %w", err)
	}
	p.version = m.Version
	return nil
}

func writeHashedTmp(tmp string, src io.Reader) (string, error) {
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return "", fmt.Errorf("safetynet/prev: create tmp: %w", err)
	}
	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(out, hasher), src); err != nil {
		_ = out.Close()
		return "", fmt.Errorf("safetynet/prev: copy: %w", err)
	}
	if err := out.Close(); err != nil {
		return "", fmt.Errorf("safetynet/prev: close tmp: %w", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func (p *Prev) Show(ctx context.Context) (Info, error) {
	st, err := os.Stat(p.dst)
	if err != nil {
		_ = p.emit.Emit(ctx, Event{
			Type:    EventSafetynetPrevMissing,
			Payload: map[string]any{"path": p.dst, "reason": "stat failed"},
		})
		return Info{Installed: false, Path: p.dst}, ErrPrevNotInstalled
	}
	if st.IsDir() {
		return Info{Installed: false, Path: p.dst}, fmt.Errorf("safetynet/prev: dst is a directory: %s", p.dst)
	}
	b, err := os.ReadFile(p.dst)
	if err != nil {
		return Info{Installed: false, Path: p.dst}, fmt.Errorf("safetynet/prev: read: %w", err)
	}
	s := sha256.Sum256(b)
	return Info{
		Installed: true,
		Path:      p.dst,
		Version:   p.version,
		Sha256:    hex.EncodeToString(s[:]),
	}, nil
}

func (p *Prev) Exec(ctx context.Context, args []string) ([]byte, error) {
	if _, err := os.Stat(p.dst); err != nil {
		return nil, ErrPrevNotInstalled
	}
	cmd := exec.CommandContext(ctx, p.dst, args...)
	return cmd.CombinedOutput()
}
