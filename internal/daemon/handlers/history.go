// SPDX-License-Identifier: MIT
package handlers

import "net/http"

func History(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		notImplemented(w, 9, "Persistencia + memoria + trace + continuity")
	}
}
