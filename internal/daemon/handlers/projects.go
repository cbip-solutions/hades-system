// SPDX-License-Identifier: MIT
package handlers

import "net/http"

func ProjectsList(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		notImplemented(w, 7, "Multi-project + tmux + scheduling")
	}
}

func ProjectsGet(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		notImplemented(w, 7, "Multi-project + tmux + scheduling")
	}
}

func ProjectsAgentsMD(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		notImplemented(w, 14, "Documentation system + RAG hybrid")
	}
}

func ProjectsSync(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		notImplemented(w, 7, "Multi-project + tmux + scheduling")
	}
}
