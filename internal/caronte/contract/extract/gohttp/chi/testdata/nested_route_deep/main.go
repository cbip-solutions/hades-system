// SPDX-License-Identifier: MIT
package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func main() {
	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		r.Route("/admin", func(r chi.Router) {
			r.Get("/stats", showStats)
		})
	})
	_ = http.ListenAndServe(":8080", r)
}

func showStats(w http.ResponseWriter, req *http.Request) { w.WriteHeader(http.StatusOK) }
