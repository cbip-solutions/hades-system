// SPDX-License-Identifier: MIT
package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/labstack/echo/v4"
)

func main() {
	e := echo.New()
	e.GET("/echo", h)

	r := chi.NewRouter()
	r.Get("/chi", chiH)

	http.HandleFunc("/legacy", legacyH)

	_ = e.Start(":8080")
	_ = http.ListenAndServe(":8081", r)
}

func h(c echo.Context) error                           { return nil }
func chiH(w http.ResponseWriter, req *http.Request)    { w.WriteHeader(http.StatusOK) }
func legacyH(w http.ResponseWriter, req *http.Request) { w.WriteHeader(http.StatusOK) }
