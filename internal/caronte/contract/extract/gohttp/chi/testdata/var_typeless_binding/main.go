// SPDX-License-Identifier: MIT
package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

var r = chi.NewRouter()

func init() {
	r.Get("/health", health)
}

func main() {
	_ = http.ListenAndServe(":8080", r)
}

func health(w http.ResponseWriter, req *http.Request) { w.WriteHeader(http.StatusOK) }
