// SPDX-License-Identifier: MIT
package sources

import (
	"net/http"
)

// badSourceFetch violates inv-zen-191 by calling http.Get directly.
// Sources MUST delegate to cache.Revalidator.Fetch (ADR-0087).
func badSourceFetch(url string) {
	resp, _ := http.Get(url)
	_ = resp
}

func badSourceNewRequest() {
	req, _ := http.NewRequest("GET", "https://crates.io/api/v1/crates/foo", nil)
	resp, _ := http.DefaultClient.Do(req)
	_ = resp
}
