// SPDX-License-Identifier: MIT
package main

import (
	"net/http"

	chirouter "github.com/go-chi/chi/v5"
)

func main() {
	r := chirouter.NewRouter()
	r.Get("/health", health)
	_ = http.ListenAndServe(":8080", r)
}

func health(w http.ResponseWriter, req *http.Request) { w.WriteHeader(http.StatusOK) }
