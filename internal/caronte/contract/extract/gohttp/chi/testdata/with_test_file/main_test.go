package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// testRouter is a route registration that lives in a _test.go file. The
// extractor's per-file filter MUST skip _test.go files; this route should
// NOT appear in extracted output.
func testRouter() {
	r := chi.NewRouter()
	r.Get("/test-only/route", func(w http.ResponseWriter, req *http.Request) {})
}
