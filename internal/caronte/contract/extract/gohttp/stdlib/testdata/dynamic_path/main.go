// SPDX-License-Identifier: MIT
package main

import "net/http"

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}/posts/{postId...}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	_ = http.ListenAndServe(":8080", mux)
}
