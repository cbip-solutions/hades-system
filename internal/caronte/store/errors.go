// SPDX-License-Identifier: MIT
// Package store owns the per-project caronte.db: the C-4 code-graph
// schema (graph_nodes + graph_edges + co_change_matrix + churn_metrics +
// adr_links + lore_trailers + a vec0 vector index + an FTS5 lexical
// index) and the CRUD surface every Caronte layer (parse → resolve →
// structure → evolution → intent) writes through.
//
// Boundary: this package and ALL its callers under
// internal/caronte/ NEVER import internal/store. The per-project DB FILE
// is opened by path ONLY inside internal/daemon/caronteadapter, which
// injects the resulting *sql.DB here via Open. The compliance test
// tests/compliance/inv_zen_230_caronte_no_store_import_test.go enforces
// the prohibition; caronteBoundarySentinel is the runtime witness.
//
// Isolation: there is exactly one
// caronte.db per canonical project path (<canonical>/.zen/caronte.db).
// This package holds a single injected handle and has no cross-project
// query surface; cross-project federation is a
// distinct, capa-firewall-gated path.
//
// Driver choice: mattn/go-sqlite3 (CGO) hosts the sqlite-vec C extension,
// mirroring internal/knowledge/aggregator. The CGO-disabled build variant
// (store_nocgo.go) returns ErrCGODisabled so the daemon cross-compiles
// and degrades gracefully (no KNN; the engine surfaces degraded_mode).
//
// invariant: this package makes NO web calls; sqlite-vec is a local C
// extension.
package store

import "errors"

const DefaultDriver = "sqlite3"

var ErrCGODisabled = errors.New("caronte/store: sqlite-vec requires CGO_ENABLED=1; degraded_mode active")

var ErrEmptyDB = errors.New("caronte/store: nil *sql.DB injected")

var ErrNotFound = errors.New("caronte/store: not found")

var ErrEmptyWorkspace = errors.New("caronte/store: workspace roster is empty")

var ErrDuplicateProject = errors.New("caronte/store: duplicate projectID in workspace roster")

var ErrUnauthorizedProject = errors.New("caronte/store: project not on workspace roster")

var ErrCrossProjectDenied = errors.New("caronte/store: cross-project federation denied under privacy-locked doctrine")
