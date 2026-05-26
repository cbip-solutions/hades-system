// SPDX-License-Identifier: MIT
// Package aggregator — output_json.go ships the batched JSON renderer
// with schemaVersion="1.0" per Q5=C+ + SOTA-4 anti-pattern #6 avoidance.
//
// The Report struct (aggregator.go) declares JSON struct tags; this file
// ships the serializer entry point with indented pretty-print for
// operator readability + a compact variant for CI consumers.
package aggregator

import (
	"encoding/json"
	"io"
)

func RenderJSON(w io.Writer, report *Report) error {
	if report == nil {
		_, err := w.Write([]byte(`{"schemaVersion":"` + SchemaVersion + `"}` + "\n"))
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func RenderJSONCompact(w io.Writer, report *Report) error {
	if report == nil {
		_, err := w.Write([]byte(`{"schemaVersion":"` + SchemaVersion + `"}`))
		return err
	}
	body, err := json.Marshal(report)
	if err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}
