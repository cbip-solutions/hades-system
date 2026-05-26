// SPDX-License-Identifier: MIT
package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func main() {
	r := chi.NewRouter()
	r.Get("/prod", prod)
	_ = http.ListenAndServe(":8080", r)
}

func prod(w http.ResponseWriter, req *http.Request) { w.WriteHeader(http.StatusOK) }
