// SPDX-License-Identifier: MIT
package main

import "net/http"

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /prod", h)
	_ = http.ListenAndServe(":8080", mux)
}

func h(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
