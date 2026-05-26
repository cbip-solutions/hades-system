// SPDX-License-Identifier: MIT
// Package cache ships the Plan 13 Phase F doctor freshness cache layer
// per Q5=C+ + Q12=D state model. Last-run JSON output is persisted at
// `$XDG_CACHE_HOME/zen-swarm/doctor/last-run.json` (default
// `~/.cache/zen-swarm/doctor/last-run.json`). Per-operator freshness ≤5min
// TTL (configurable via Q5=C+ `--cache-ttl=N` flag, Phase F5).
//
// Boundary (inv-zen-031): cache writes ONLY to XDG paths; no DB; no
// internal/store import.
//
// Cross-platform: tests inject t.TempDir() + t.Setenv("HOME", ...)
// + t.Setenv("XDG_CACHE_HOME", ...) for deterministic state. Production
// honours $XDG_CACHE_HOME first, then $HOME/.cache.
package cache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctor/aggregator"
)

const DefaultTTL = 5 * time.Minute

const CacheFileName = "last-run.json"

type Cache struct {
	dir string
}

func New() *Cache {
	xdg := os.Getenv("XDG_CACHE_HOME")
	if xdg == "" {
		if home := os.Getenv("HOME"); home != "" {
			xdg = filepath.Join(home, ".cache")
		} else if home, err := os.UserHomeDir(); err == nil && home != "" {
			xdg = filepath.Join(home, ".cache")
		} else {
			xdg = filepath.Join(".", ".zen-swarm-cache")
		}
	}
	return NewWithDir(filepath.Join(xdg, "zen-swarm", "doctor"))
}

func NewWithDir(dir string) *Cache {
	return &Cache{dir: dir}
}

func (c *Cache) Path() string {
	return filepath.Join(c.dir, CacheFileName)
}

func (c *Cache) Write(r *aggregator.Report) error {
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return err
	}
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	tmpPath := c.Path() + ".tmp"
	if err := os.WriteFile(tmpPath, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, c.Path())
}

// Read returns the cached report + fresh flag + error.
//
// fresh = true iff (a) the file exists, (b) JSON parses, (c) FinishedAt
// is within ttl of time.Now(). Corrupt or missing file returns
// (nil, false, nil) — no error; aggregator re-runs.
//
// Defensive behavior: a corrupt cache MUST NOT cascade into an aggregator
// run failure. The aggregator's contract is "best-effort cache"; a
// freshness miss simply triggers re-run.
func (c *Cache) Read(ttl time.Duration) (*aggregator.Report, bool, error) {
	body, err := os.ReadFile(c.Path())
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var r aggregator.Report
	if jerr := json.Unmarshal(body, &r); jerr != nil {
		// Corrupt file → treat as stale; do NOT error (operator re-runs).
		return nil, false, nil
	}
	fresh := time.Since(r.FinishedAt) <= ttl
	return &r, fresh, nil
}

func (c *Cache) Invalidate() error {
	err := os.Remove(c.Path())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
