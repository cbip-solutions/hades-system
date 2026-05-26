// SPDX-License-Identifier: MIT
package main

import "net/http"

var (
	muxA *http.ServeMux = http.NewServeMux()
	muxB                = http.NewServeMux()
)

func init() {
	muxA.HandleFunc("GET /a", h)
	muxB.HandleFunc("GET /b", h)
}

func main() {
	_ = http.ListenAndServe(":8080", muxA)
}

func h(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
