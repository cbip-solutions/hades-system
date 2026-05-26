// SPDX-License-Identifier: MIT
package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Inline FuncLit-arg form of Mount — non-standard chi API (Mount usually
// takes a chi.Routes / http.Handler), but the extractor's walker MUST
// tolerate it by pushing the prefix and walking the inner func body. The
// test asserts /area/inner appears in the emitted set.
func main() {
	r := chi.NewRouter()
	r.Mount("/area", func(r chi.Router) {
		r.Get("/inner", inner)
	})
	_ = http.ListenAndServe(":8080", r)
}

func inner(w http.ResponseWriter, req *http.Request) { w.WriteHeader(http.StatusOK) }
