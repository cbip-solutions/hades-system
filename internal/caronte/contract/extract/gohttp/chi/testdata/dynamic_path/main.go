// SPDX-License-Identifier: MIT
package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func main() {
	r := chi.NewRouter()

	r.Get("/static/health", okHandler)
	// Dynamic path — argument is a variable, not a string literal. The
	// extractor MUST drop this call silently (no panic; the row is just not
	// emitted; the user gets a hint via `unresolved` row tier).
	dynamicPath := "/" + computePath()
	r.Get(dynamicPath, okHandler)
	_ = http.ListenAndServe(":8080", r)
}

func computePath() string                                { return "computed" }
func okHandler(w http.ResponseWriter, req *http.Request) { w.WriteHeader(http.StatusOK) }
