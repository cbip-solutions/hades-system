// SPDX-License-Identifier: MIT
// Package recovery owns the chain integrity verification (VerifyChain),
// recovery orchestration (Restore), per-doctrine tamper response
// dispatcher (DispatchTamperResponse), and the doctor checks for
// audit backup + chain-integrity health.
//
// inv-hades-031: this package MUST NOT import internal/store. All chain
// state + seal metadata flows through interfaces defined here, with
// concrete implementations in internal/daemon/auditadapter/.
//
// inv-hades-150 (per-project blast radius): VerifyChain operates on one
// project at a time; Restore halts ONE project at a time (cascade for
// capa-firewall doctrine is explicit in DispatchTamperResponse, not
// implicit in VerifyChain).
package recovery
