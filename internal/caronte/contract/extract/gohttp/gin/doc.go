// SPDX-License-Identifier: MIT
// Package gin is the release Go gin-gonic/gin router extractor namespace
// (Language: LangGo, framework: "gin"). Concrete RouteExtractor lands in
// (master row D, wave W3) as gin.go. The (LangGo, "gin") tuple is
// reserved for this package by daemon-time Register() call.
//
// What will add: gin.go with Detect()-on-import "github.com/gin-gonic/gin";
// Endpoints()/Calls() walking r.GET/POST/... + r.Group() prefix composition;
// gin_test.go with ≥10 fixtures (groups, middleware exclusion, dynamic-route
// fallback); fixtures/ real router declarations.
//
// Boundary NEVER import internal/store.
package gin
