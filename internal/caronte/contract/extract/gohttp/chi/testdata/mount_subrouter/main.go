// SPDX-License-Identifier: MIT
package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func adminRouter() *chi.Mux {
	r := chi.NewRouter()
	r.Get("/stats", adminStats)
	return r
}

func main() {
	r := chi.NewRouter()
	r.Get("/health", health)
	r.Mount("/admin", adminRouter())
	_ = http.ListenAndServe(":8080", r)
}

func health(w http.ResponseWriter, req *http.Request)     { w.WriteHeader(http.StatusOK) }
func adminStats(w http.ResponseWriter, req *http.Request) { w.WriteHeader(http.StatusOK) }
