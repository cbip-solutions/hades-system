// SPDX-License-Identifier: MIT
package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func main() {
	r := chi.NewRouter()
	r.Get("/users", listUsers)
	r.Post("/users", createUser)
	_ = http.ListenAndServe(":8080", r)
}

func listUsers(w http.ResponseWriter, req *http.Request)  { w.WriteHeader(http.StatusOK) }
func createUser(w http.ResponseWriter, req *http.Request) { w.WriteHeader(http.StatusCreated) }
