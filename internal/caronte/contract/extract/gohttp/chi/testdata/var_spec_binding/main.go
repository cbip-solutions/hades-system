// SPDX-License-Identifier: MIT
package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

var router chi.Router = chi.NewRouter()

func init() {
	router.Get("/health", health)
}

func main() {
	_ = http.ListenAndServe(":8080", router)
}

func health(w http.ResponseWriter, req *http.Request) { w.WriteHeader(http.StatusOK) }
