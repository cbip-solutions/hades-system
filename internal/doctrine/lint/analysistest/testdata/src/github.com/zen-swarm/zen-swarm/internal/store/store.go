// SPDX-License-Identifier: MIT
// Package store is a STUB used only by analysistest fixtures. It provides
// the canonical import path "github.com/cbip-solutions/hades-system/internal/store"
// so the bad/* fixtures can import it and the analyzer can detect the
// forbidden import.
//
// This stub is NOT the real internal/store; it is a fixture-side resolution
// target for the analyzer's import-spec walking. Analyzer correctness does
// not depend on this stub's contents — only that the import path exists in
// analysistest's synthetic GOPATH-like layout.
package store

const Stub = "stub"
