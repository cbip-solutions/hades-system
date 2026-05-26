// SPDX-License-Identifier: MIT
package main

import "net/http"

func main() {

	http.HandleFunc("GET /api/users", h)
	_ = http.ListenAndServe(":8080", nil)
}

func h(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
