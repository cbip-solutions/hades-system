// SPDX-License-Identifier: MIT
// Package filewatcher monitors file changes for the daemon.
//
// Used (doc-live mode: watch openspec/changes/<feature>/*.md
// and emit events when operator saves) and HADES design (config reload on
// projects.toml edit). HADES design establishes types + the debouncer (REAL);
// the watcher loop itself is HADES design
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
