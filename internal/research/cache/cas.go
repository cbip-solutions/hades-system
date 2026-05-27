//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var ErrBlobMissing = errors.New("research/cache: blob missing from CAS")

type CAS struct {
	root string
}

func NewCAS(root string) (*CAS, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("research/cache: create CAS root %q: %w", root, err)
	}

	if err := os.Chmod(root, 0o700); err != nil {
		return nil, fmt.Errorf("research/cache: chmod CAS root %q: %w", root, err)
	}
	return &CAS{root: root}, nil
}

func (c *CAS) Root() string {
	return c.root
}

func (c *CAS) Path(hash, ext string) string {
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return filepath.Join(c.root, hash[:2], hash+ext)
}

func (c *CAS) Write(data []byte, ext string) (string, error) {

	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])

	dest := c.Path(hash, ext)

	if _, err := os.Stat(dest); err == nil {
		return hash, nil
	}

	prefixDir := filepath.Join(c.root, hash[:2])
	if err := os.MkdirAll(prefixDir, 0o700); err != nil {
		return "", fmt.Errorf("research/cache: create prefix dir %q: %w", prefixDir, err)
	}

	tmpPath := dest + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {

			if _, statErr := os.Stat(dest); statErr == nil {

				return hash, nil
			}
		}
		return "", fmt.Errorf("research/cache: open tmp %q: %w", tmpPath, err)
	}

	writeOK := false
	defer func() {
		if !writeOK {
			_ = f.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := f.Write(data); err != nil {
		return "", fmt.Errorf("research/cache: write tmp %q: %w", tmpPath, err)
	}

	if err := f.Sync(); err != nil {
		return "", fmt.Errorf("research/cache: fsync tmp %q: %w", tmpPath, err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("research/cache: close tmp %q: %w", tmpPath, err)
	}

	if err := os.Rename(tmpPath, dest); err != nil {
		return "", fmt.Errorf("research/cache: rename tmp→dest %q: %w", dest, err)
	}

	writeOK = true
	return hash, nil
}

func (c *CAS) Read(hash, ext string) ([]byte, error) {
	data, err := os.ReadFile(c.Path(hash, ext))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrBlobMissing
		}
		return nil, fmt.Errorf("research/cache: read blob %q: %w", hash, err)
	}
	return data, nil
}

func (c *CAS) Open(hash, ext string) (io.ReadCloser, error) {
	f, err := os.Open(c.Path(hash, ext))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrBlobMissing
		}
		return nil, fmt.Errorf("research/cache: open blob %q: %w", hash, err)
	}
	return f, nil
}

func (c *CAS) Delete(hash, ext string) error {
	err := os.Remove(c.Path(hash, ext))
	if err != nil && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
