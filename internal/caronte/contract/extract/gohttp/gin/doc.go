// SPDX-License-Identifier: MIT
// Package gin is the Plan 20 Go gin-gonic/gin router extractor namespace
// (Language: LangGo, framework: "gin"). Concrete RouteExtractor lands in
// Phase D (master row D, wave W3) as gin.go. The (LangGo, "gin") tuple is
// reserved for this package by Phase D's daemon-time Register() call.
//
// What Phase D will add: gin.go with Detect()-on-import "github.com/gin-gonic/gin";
// Endpoints()/Calls() walking r.GET/POST/... + r.Group() prefix composition;
// gin_test.go with ≥10 fixtures (groups, middleware exclusion, dynamic-route
// fallback); fixtures/ real router declarations.
//
// Boundary NEVER import internal/store (inv-zen-230 + Plan 20 inv-zen-271).
package gin
