// SPDX-License-Identifier: MIT
package main

import "net/http"

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /static", h)
	// Dynamic-pattern call — argument is a variable, NOT a string literal;
	// the extractor MUST drop this call silently.
	pat := buildPattern()
	mux.HandleFunc(pat, h)
	_ = http.ListenAndServe(":8080", mux)
}

func buildPattern() string                     { return "GET /computed" }
func h(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
