// SPDX-License-Identifier: MIT
package main

import "net/http"

// AuthWrap wraps a handler with auth middleware. The extractor MUST extract
// the route registration regardless of the wrap (the handler argument is
// `AuthWrap(http.HandlerFunc(h))` — the wrap is opaque; we record the route
// with the wrapped handler form).
func AuthWrap(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r)
	})
}

func main() {
	mux := http.NewServeMux()
	mux.Handle("/x", AuthWrap(http.HandlerFunc(h)))
	_ = http.ListenAndServe(":8080", mux)
}

func h(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
