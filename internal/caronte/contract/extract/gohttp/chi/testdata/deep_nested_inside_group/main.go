// SPDX-License-Identifier: MIT
package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func main() {
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Route("/v1", func(r chi.Router) {
			r.Get("/inner", inner)
			r.Mount("/sub", func(r chi.Router) {
				r.Post("/x", post)
			})
		})
	})
	_ = http.ListenAndServe(":8080", r)
}

func inner(w http.ResponseWriter, req *http.Request) { w.WriteHeader(http.StatusOK) }
func post(w http.ResponseWriter, req *http.Request)  { w.WriteHeader(http.StatusOK) }
