// SPDX-License-Identifier: MIT
package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Imports BOTH net/http AND chi → the stdlib extractor's per-file guard
// hands ownership to chi; this extractor MUST emit zero rows for this file.
func main() {
	r := chi.NewRouter()
	r.Get("/chi", chiH)

	http.HandleFunc("/should-not-extract", stdlibH)
	_ = http.ListenAndServe(":8080", r)
}

func chiH(w http.ResponseWriter, r *http.Request)    { w.WriteHeader(http.StatusOK) }
func stdlibH(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
