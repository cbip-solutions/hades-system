// SPDX-License-Identifier: MIT
// Package stdlib is the HADES design Go net/http stdlib (1.22+ "METHOD /path/{param}"
// pattern) extractor namespace (Language: LangGo, framework: "stdlib"). The
// concrete RouteExtractor lands in (master row D, wave W3) as
// stdlib.go. The (LangGo, "stdlib") tuple is reserved for this package by
// daemon-time Register() call.
//
// What will add: stdlib.go with Detect()-on-import "net/http";
// Endpoints()/Calls() walking http.HandleFunc / mux.HandleFunc + the Go 1.22+
// method-prefixed pattern grammar ("METHOD /path/{param}"); stdlib_test.go
// with ≥10 fixtures covering both the legacy `path` and the new
// `"METHOD /path"` grammar; fixtures/ real handler declarations.
//
// Boundary NEVER import internal/store.
package stdlib
