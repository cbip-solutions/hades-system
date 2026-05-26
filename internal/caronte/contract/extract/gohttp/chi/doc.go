// SPDX-License-Identifier: MIT
// Package chi is the Plan 20 Go go-chi/chi router extractor (Language:
// LangGo, framework: "chi"). The (LangGo, "chi") tuple is reserved for this
// package by Phase D's daemon-time Register() call (the init() in chi.go).
//
// What this package provides:
//   - chi.go: New() *Extractor; (*Extractor).Language() = LangGo;
//     Frameworks() = []string{"chi"}; Detect() sniffs imports for
//     "github.com/go-chi/chi". Endpoints()/ExtractFromPackage() walk
//     r.Get/Post/... call chains with group / mount / route composition
//     via a syntactic-only AST pass (no semantic resolver dependency —
//     R6d divergence from the original brainstorm proposal: chi's call
//     surface is regular enough that a per-package AST sweep + a
//     chiRouterVars binding set covers the spec corpus without the
//     tracker that the package's earlier scaffold suggested).
//   - chi_test.go: per-fixture extraction tests (≥10 fixtures per spec
//     §13.1) covering nested router groups, middleware exclusion,
//     dynamic-route fallback, inline-FuncLit Mount, co-existence with
//     stdlib.
//   - testdata/: real chi router declarations from the SOTA research
//     catalog. Lives under testdata/ (not fixtures/) so the Go toolchain
//     skips it during `go test ./...` recursion — the fixture .go files
//     import chi by name without the parent module having chi as a real
//     dep; the extractor parses them syntactically via go/parser.
//
// Boundary this package and Phase D's code MUST NOT import internal/store
// (inv-zen-230 + Plan 20 inv-zen-271); only internal/caronte/store
// APIEndpoint/APICall types Phase B provides.
package chi
