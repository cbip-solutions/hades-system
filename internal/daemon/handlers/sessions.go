// SPDX-License-Identifier: MIT
package handlers

import "net/http"

func SessionsRegister(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		notImplemented(w, 7, "Multi-project + tmux + scheduling")
	}
}

func SessionsEnd(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		notImplemented(w, 7, "Multi-project + tmux + scheduling")
	}
}

func SessionsGet(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		notImplemented(w, 7, "Multi-project + tmux + scheduling")
	}
}
