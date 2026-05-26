// SPDX-License-Identifier: MIT
package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type userService struct{}

func (u *userService) GetUsers(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func main() {
	svc := &userService{}
	r := chi.NewRouter()
	r.Get("/users", svc.GetUsers)
	_ = http.ListenAndServe(":8080", r)
}
