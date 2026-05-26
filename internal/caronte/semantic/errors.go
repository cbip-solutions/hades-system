// SPDX-License-Identifier: MIT
// Package semantic is Caronte's L2 resolution layer for Go: it turns the
// syntactic symbol table Phase B parsed into a semantically resolved
// call/implements graph, attaching a store.Confidence tier to EVERY edge
// (inv-zen-233 — precision > recall for agents).
//
// Resolution paths, precision-ordered (§6):
//
//	buildable Go     → go/packages → SSA → VTA call graph + types.Implements
//	                   ⇒ ConfExactVTA, Reachable = &true
//	non-buildable Go → CHA fallback (sound over-approximation)
//	                   ⇒ ConfExactCHA, Reachable = nil (NULL; CHA has no
//	                     reachable set), OR last-valid VTA snapshot (stale)
//	residual tail    → reflection / DI / dynamic dispatch the static
//	                   analyser flags ambiguous ⇒ the C-2 single-egress seam
//	                   (CaronteDispatcher.Forward, Profile "local-code")
//	                   ⇒ ConfLLMHint, bounded to the unresolved tail
//
// Boundary (inv-zen-031/230): this package NEVER imports internal/store. It
// writes edges through the injected *store.Store (Phase A locked API) and
// reaches the LLM ONLY through the CaronteDispatcher seam (inv-zen-088/236);
// the daemon wires the real *orchestrator.Orchestrator at the composition
// root (Phase J). No net/http, no direct backend dial lives here — the
// compliance test tests/compliance/inv_zen_236_caronte_single_egress_test.go
// enforces that.
//
// Scheduling (§21 risk register): ResolveProject runs ON-DEMAND + cached,
// NEVER during initial indexing — the go/types cold load is 10-60 s on
// 500 k LOC; the fast indexing path (Phase B parse) does not block on it.
//
// inv-zen-129: this package makes NO web calls of its own (the dispatcher
// seam is the single egress; embeddings are not used here).
package semantic

import "errors"

const DefaultLLMProfile = "local-code"

var ErrCGODisabled = errors.New("caronte/semantic: resolution requires CGO_ENABLED=1 store; degraded_mode active")

// ErrNoDispatcher is returned by the LLM-tail path when a Resolver was
// constructed without a CaronteDispatcher (the seam is nil). The static
// paths (VTA/CHA/Implements) still run; only the residual-tail LLM
// disambiguation is skipped — §15 "LLM tail unavailable → omit llm_hint
// edges, mark unresolved; do not block". Surfaced so the caller knows the
// tail was not attempted, vs attempted-and-empty.
var ErrNoDispatcher = errors.New("caronte/semantic: no CaronteDispatcher wired; llm_hint tail skipped")

var ErrBuildBroken = errors.New("caronte/semantic: go/packages reported type errors (build broken); CHA fallback")
