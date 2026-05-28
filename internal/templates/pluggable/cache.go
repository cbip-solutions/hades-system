// SPDX-License-Identifier: MIT
// pluggable/cache.go — LRU 7-day cache at
// ~/.cache/hades-system/templates/<sha256-of-url-plus-ref>/.
//
// Per design choice persistence policy (spec §2.12): cache directories with
// retention 7 days, eviction LRU on access. The cache is operator-
// readable (mode 0755 dirs, 0644 files) — operators can inspect
// fetched templates for trust verification.
package pluggable

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Cache struct {
	Root   string
	MaxAge time.Duration
}

func DefaultCache() (*Cache, error) {
	xdg := os.Getenv("XDG_CACHE_HOME")
	if xdg == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("cache root: %w", err)
		}
		xdg = filepath.Join(home, ".cache")
	}
	root := filepath.Join(xdg, "hades-system", "templates")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir cache root: %w", err)
	}
	return &Cache{Root: root, MaxAge: 7 * 24 * time.Hour}, nil
}

func CacheKey(cloneURL, ref string) string {
	h := sha256.Sum256([]byte(cloneURL + "@" + ref))
	return hex.EncodeToString(h[:])[:32]
}

func (c *Cache) PathFor(cloneURL, ref string) string {
	return filepath.Join(c.Root, CacheKey(cloneURL, ref))
}

func (c *Cache) TryGet(cloneURL, ref string) (string, bool) {
	p := c.PathFor(cloneURL, ref)
	info, err := os.Stat(p)
	if err != nil {
		return "", false
	}
	if !info.IsDir() {
		return "", false
	}
	if time.Since(info.ModTime()) > c.MaxAge {
		return "", false
	}
	return p, true
}

func (c *Cache) Touch(cloneURL, ref string) error {
	now := time.Now()
	return os.Chtimes(c.PathFor(cloneURL, ref), now, now)
}

func (c *Cache) Evict() (int, error) {
	entries, err := os.ReadDir(c.Root)
	if err != nil {
		return 0, fmt.Errorf("read cache root: %w", err)
	}
	now := time.Now()
	var evicted int
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > c.MaxAge {
			if err := os.RemoveAll(filepath.Join(c.Root, e.Name())); err != nil {
				return evicted, fmt.Errorf("remove %q: %w", e.Name(), err)
			}
			evicted++
		}
	}
	return evicted, nil
}
