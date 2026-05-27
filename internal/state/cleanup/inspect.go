// SPDX-License-Identifier: MIT
// Package cleanup — inspect.go ships JSON serialization for
// `hades state list`.
//
// Schema version follows release's aggregator + recognize JSON pattern:
// schemaVersion="1.0" with additive growth. Operators MAY parse via
// `jq '.entries[] | select(.subsystem == "doctor-backups")'` etc.
package cleanup

import (
	"context"
	"encoding/json"
	"io"
)

const SchemaVersion = "1.0"

type ListReport struct {
	SchemaVersion string       `json:"schemaVersion"`
	Entries       []StateEntry `json:"entries"`
}

func RenderJSON(_ context.Context, w io.Writer, entries []StateEntry) error {
	r := ListReport{
		SchemaVersion: SchemaVersion,
		Entries:       entries,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}
