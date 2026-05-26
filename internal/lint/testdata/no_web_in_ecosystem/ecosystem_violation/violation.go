// SPDX-License-Identifier: MIT
package ecosystem

import (
	"net/http"
)

func badFetch() {
	resp, _ := http.Get("https://pkg.go.dev/crypto/sha256")
	_ = resp
}

func badPost() {
	resp, _ := http.Post("https://example.com", "application/json", nil)
	_ = resp
}

func badHead() {
	resp, _ := http.Head("https://example.com")
	_ = resp
}

func badNewRequestWithContext() {

	req, _ := http.NewRequestWithContext(nil, "GET", "https://example.com", nil)
	_ = req
}
