// SPDX-License-Identifier: MIT
//
// Real chi backend exposing GET /users/{id}. The chi extractor sees the
// chi.Router.Get(...) method chain + the {id} URL parameter as a static_path
// endpoint. The K-3 test extracts this file via the gohttp/chi extractor and
// asserts:
//   - api_endpoints row: kind=http, method=GET, path_template=/users/{id}
//   - extractor_id=gohttp-chi-v1
//
// The fixture has its OWN go.mod (sub-module isolation) so chi v5 does NOT
// need to be a require of the parent module. The K-3 test invokes the
// extractor in-process via chi.New().ExtractFromPackage — it does NOT
// shell out to `go build` against this fixture (the extractor is pure
// AST analysis).
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
)

func main() {
	r := chi.NewRouter()
	r.Get("/users/{id}", getUser)
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":0"
	}
	log.Printf("backend listening on %s", addr)
	_ = http.ListenAndServe(addr, r)
}

func getUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "name": "alice"})
}
