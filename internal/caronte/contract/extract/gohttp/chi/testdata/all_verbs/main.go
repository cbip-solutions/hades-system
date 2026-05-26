// SPDX-License-Identifier: MIT
package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func main() {
	r := chi.NewRouter()
	r.Get("/g", h)
	r.Post("/p", h)
	r.Put("/u", h)
	r.Delete("/d", h)
	r.Patch("/a", h)
	r.Head("/he", h)
	r.Options("/o", h)
	_ = http.ListenAndServe(":8080", r)
}

func h(w http.ResponseWriter, req *http.Request) { w.WriteHeader(http.StatusOK) }
