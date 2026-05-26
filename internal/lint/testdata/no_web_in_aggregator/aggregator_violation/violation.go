// SPDX-License-Identifier: MIT
package aggregator

import (
	"net/http"
)

func badQuery() {
	resp, _ := http.Get("https://example.com/notes")
	_ = resp
}
