// SPDX-License-Identifier: MIT
package main

import nh "net/http"

func main() {
	mux := nh.NewServeMux()
	mux.HandleFunc("GET /health", h)
	_ = nh.ListenAndServe(":8080", mux)
}

func h(w nh.ResponseWriter, r *nh.Request) { w.WriteHeader(nh.StatusOK) }
