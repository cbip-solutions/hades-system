// SPDX-License-Identifier: MIT
package main

import "net/http"

func main() {
	http.HandleFunc("/x", func(w http.ResponseWriter, req *http.Request) {})
	_ = http.ListenAndServe(":8080", nil)
}
