// SPDX-License-Identifier: MIT
package federation

import (
	"errors"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

// DefaultDriver is the canonical SQLite driver name workspace.db is opened
// with. Aliased to internal/caronte/store.DefaultDriver — release chose
// mattn/go-sqlite3 (registered as "sqlite3") so the sqlite-vec C extension
// links for the per-project caronte.db; the federation package REUSES that
// driver for build-tag symmetry (the federation tables themselves do NOT
// use vec0). Exported from BOTH build variants so callers reference it
// unconditionally.
const DefaultDriver = store.DefaultDriver

var ErrCGODisabled = errors.New("caronte/store/federation: workspace.db requires CGO_ENABLED=1; degraded_mode active")

var ErrEmptyDB = errors.New("caronte/store/federation: nil *sql.DB inside WorkspaceFederationDB")

// ErrEmptyStatePath is returned by Open when statePath is the empty string.
// The composition root MUST resolve the path via WorkspaceDBPath (path.go)
// before calling Open — an empty path is a config bug, not a "use defaults"
// signal (Open is not the right place for defaulting; the caller is).
var ErrEmptyStatePath = errors.New("caronte/store/federation: workspace state path is empty; call WorkspaceDBPath first")

var ErrNotFound = errors.New("caronte/store/federation: not found")

var ErrUnknownEventType = errors.New("caronte/store/federation: unknown audit event type (must be one of plan20.* constants)")

var ErrCorruptAuditLeaf = errors.New("caronte/store/federation: tessera adapter rejected synthesized audit leaf")
