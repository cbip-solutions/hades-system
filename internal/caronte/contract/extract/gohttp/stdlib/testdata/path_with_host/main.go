// SPDX-License-Identifier: MIT
package main

import "net/http"

func main() {
	http.HandleFunc("GET example.com/x", func(w http.ResponseWriter, r *http.Request) {})
	_ = http.ListenAndServe(":8080", nil)
}
