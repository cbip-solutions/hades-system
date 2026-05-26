// SPDX-License-Identifier: MIT
// Package stdlib is the Plan 20 Go net/http stdlib (1.22+ "METHOD /path/{param}"
// pattern) extractor namespace (Language: LangGo, framework: "stdlib"). The
// concrete RouteExtractor lands in Phase D (master row D, wave W3) as
// stdlib.go. The (LangGo, "stdlib") tuple is reserved for this package by
// Phase D's daemon-time Register() call.
//
// What Phase D will add: stdlib.go with Detect()-on-import "net/http";
// Endpoints()/Calls() walking http.HandleFunc / mux.HandleFunc + the Go 1.22+
// method-prefixed pattern grammar ("METHOD /path/{param}"); stdlib_test.go
// with ≥10 fixtures covering both the legacy `path` and the new
// `"METHOD /path"` grammar; fixtures/ real handler declarations.
//
// Boundary NEVER import internal/store (inv-zen-230 + Plan 20 inv-zen-271).
package stdlib
