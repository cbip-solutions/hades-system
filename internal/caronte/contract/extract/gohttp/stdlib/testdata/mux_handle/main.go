// SPDX-License-Identifier: MIT
package main

import "net/http"

func main() {
	mux := http.NewServeMux()
	mux.Handle("GET /static/{path...}", http.HandlerFunc(static))
	_ = http.ListenAndServe(":8080", mux)
}

func static(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
