// SPDX-License-Identifier: MIT
package handlers

import "net/http"

func MemoryGet(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		notImplemented(w, 9, "Persistencia + memoria + trace + continuity")
	}
}

func MemoryWrite(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		notImplemented(w, 9, "Persistencia + memoria + trace + continuity")
	}
}

func MemoryUpdate(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		notImplemented(w, 9, "Persistencia + memoria + trace + continuity")
	}
}
