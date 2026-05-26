// SPDX-License-Identifier: MIT
package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.Logger)
		r.Get("/me", me)
	})
	_ = http.ListenAndServe(":8080", r)
}

func me(w http.ResponseWriter, req *http.Request) { w.WriteHeader(http.StatusOK) }
