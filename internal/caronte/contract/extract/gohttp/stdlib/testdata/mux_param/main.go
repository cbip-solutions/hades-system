// SPDX-License-Identifier: MIT
package main

import "net/http"

func RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", h)
}

func main() {
	mux := http.NewServeMux()
	RegisterRoutes(mux)
	_ = http.ListenAndServe(":8080", mux)
}

func h(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
