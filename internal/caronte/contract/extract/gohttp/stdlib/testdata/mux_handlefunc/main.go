// SPDX-License-Identifier: MIT
package main

import "net/http"

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/login", login)
	_ = http.ListenAndServe(":8080", mux)
}

func login(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
