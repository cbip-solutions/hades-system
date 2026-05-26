// SPDX-License-Identifier: MIT
package main

import "net/http"

func main() {
	http.HandleFunc("/legacy", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	_ = http.ListenAndServe(":8080", nil)
}
