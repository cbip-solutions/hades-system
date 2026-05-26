// SPDX-License-Identifier: MIT
// Package filewatcher monitors file changes for the daemon.
//
// Used by Plan 9 (doc-live mode: watch openspec/changes/<feature>/*.md
// and emit events when operator saves) and Plan 11 (config reload on
// projects.toml edit). Plan 1 establishes types + the debouncer (REAL);
// the watcher loop itself is Plan 9.
package filewatcher

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type Event struct {
	Path string
	Op   string
	TS   int64
}

type Watcher struct {
}

func NewWatcher(rootPath string) (*Watcher, error) {
	return nil, zerrors.ErrNotImplementedPlan9
}

func (w *Watcher) Events() <-chan Event {
	return nil
}

func (w *Watcher) Close() error {
	return zerrors.ErrNotImplementedPlan9
}
