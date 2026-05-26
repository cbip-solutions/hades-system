package main

import "net/http"

func testReg() {
	http.HandleFunc("GET /test-only", func(w http.ResponseWriter, r *http.Request) {})
}
