// SPDX-License-Identifier: MIT
// Package echo is the release Go labstack/echo router extractor namespace
// (Language: LangGo, framework: "echo"). Concrete RouteExtractor lands in
// (master row D, wave W3) as echo.go. The (LangGo, "echo") tuple is
// reserved for this package by daemon-time Register() call.
//
// What will add: echo.go with Detect()-on-import "github.com/labstack/echo";
// Endpoints()/Calls() walking e.GET/POST/... + e.Group() prefix composition;
// echo_test.go with ≥10 fixtures; fixtures/ real router declarations.
//
// Boundary NEVER import internal/store.
package echo
