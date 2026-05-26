// SPDX-License-Identifier: MIT
package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func main() {
	r := chi.NewRouter()
	r.Get("/chi", chiHandler)

	http.HandleFunc("/legacy", legacyHandler)

	_ = http.ListenAndServe(":8080", r)
}

func chiHandler(w http.ResponseWriter, req *http.Request)    { w.WriteHeader(http.StatusOK) }
func legacyHandler(w http.ResponseWriter, req *http.Request) { w.WriteHeader(http.StatusOK) }
