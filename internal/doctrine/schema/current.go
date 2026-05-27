// SPDX-License-Identifier: MIT
// Package schema declares the doctrine schema versioning constant. Versioned
// schema struct definitions live in versioned sub-packages (v1, v2,...).
//
// CurrentSchemaVersion bumps when sections/fields are added/removed/renamed
// in the latest schema package. release ships at "1.0"; future minor changes
// (e.g., new section "Capacity") bump to "1.1"; field rename or removal
// bumps to "2.0" and adds a v2 sub-package.
//
// Boundary discipline (invariant): this package imports stdlib only.
package schema

const CurrentSchemaVersion = "1.0"

var SupportedSchemaVersions = []string{"1.0"}
