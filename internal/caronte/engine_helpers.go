// SPDX-License-Identifier: MIT
// internal/caronte/engine_helpers.go
//
// Build-tag-agnostic helpers shared by both the cgo and !cgo Engine variants.
// Currently decodeJSONArray federation ops that surface
// JSON-encoded TEXT columns (Lore ADR refs / supersedes) as []string slices.

package caronte

import "encoding/json"

func decodeJSONArray(s string, out *[]string) error {
	if s == "" {
		return nil
	}
	return json.Unmarshal([]byte(s), out)
}
