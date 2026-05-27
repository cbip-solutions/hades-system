// SPDX-License-Identifier: MIT
package tessera

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"

	tessera "github.com/transparency-dev/tessera"
	posix "github.com/transparency-dev/tessera/storage/posix"
)

var posixDriverFactory = func(ctx context.Context, cfg posix.Config) (tessera.Driver, error) {
	return posix.New(ctx, cfg)
}

// posixStorage wraps transparency-dev/tessera/storage/posix.
//
// defines the shape; A-5 wires the appender atop this type via
// the Driver() accessor. The wrapper exists so unit tests can mock the
// storage layer (without standing up Tessera) AND so future phases can
// swap the backend (hosted Tessera, etc.) without touching Adapter.
//
// Per spec §7.4 the backing directory MUST have perms 0o700; we re-stat
// at Open time and refuse looser perms (defense in depth on top of the
// 0o700 dir creation NewProjectAdapter performs).
//
// Shutdown semantics: tessera v1.0.2's *posix.Storage does NOT expose a
// public Close() method (its Appender(ctx, opts) launches goroutines
// tied to that ctx; the canonical shutdown path is context cancellation).
// We wrap the parent ctx with a derived cancellable ctx, hand the derived
// ctx to posix.New, and call cancel() on Close so any internal Tessera
// goroutines drain. If a future tessera release exposes an explicit
// Close, swap the cancel call for the explicit shutdown — the contract
// stays "release file handles + flush pending writes".
type posixStorage struct {
	dir    string
	driver *posix.Storage
	cancel context.CancelFunc
	closed atomic.Bool
}

func openPosixStorage(parentCtx context.Context, dir string) (*posixStorage, error) {
	st, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("tessera: stat %s: %w", dir, err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("tessera: %s is not a directory", dir)
	}
	if perm := st.Mode().Perm(); perm != 0o700 {
		return nil, fmt.Errorf("tessera: refusing to open %s with perms %v (spec §7.4 requires 0700)", dir, perm)
	}
	ctx, cancel := context.WithCancel(parentCtx)
	d, err := posixDriverFactory(ctx, posix.Config{Path: dir})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("tessera: posix.New: %w", err)
	}
	driver, ok := d.(*posix.Storage)
	if !ok {
		cancel()
		return nil, fmt.Errorf("tessera: posix.New returned %T, want *posix.Storage", d)
	}
	return &posixStorage{
		dir:    dir,
		driver: driver,
		cancel: cancel,
	}, nil
}

func (s *posixStorage) Dir() string { return s.dir }

func (s *posixStorage) Driver() *posix.Storage { return s.driver }

func (s *posixStorage) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}
