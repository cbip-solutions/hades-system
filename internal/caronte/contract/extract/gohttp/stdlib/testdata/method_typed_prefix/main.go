// SPDX-License-Identifier: MIT
package main

import "net/http"

func main() {
	http.HandleFunc("DELETE /resource/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	_ = http.ListenAndServe(":8080", nil)
}
