// SPDX-License-Identifier: MIT
package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func RegisterRoutes(r chi.Router) {
	r.Get("/health", health)
}

func main() {
	r := chi.NewRouter()
	RegisterRoutes(r)
	_ = http.ListenAndServe(":8080", r)
}

func health(w http.ResponseWriter, req *http.Request) { w.WriteHeader(http.StatusOK) }
