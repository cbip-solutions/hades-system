// SPDX-License-Identifier: MIT
package cache

import (
	"net/http"
)

func badFetch() {
	resp, _ := http.Get("https://arxiv.org/abs/2509.17360")
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
