// SPDX-License-Identifier: MIT
package manifest

import "errors"

// Sentinel errors. All MUST be errors.Is-comparable per package convention
// (see tier1-sidecar for prior art).
var (
	ErrSchemaNotFound = errors.New("manifest: schema file not found")

	ErrSchemaInvalid = errors.New("manifest: schema invalid")

	ErrManifestNotFound = errors.New("manifest: state.toml file not found")

	ErrManifestInvalid = errors.New("manifest: state.toml invalid")

	ErrManifestParse = errors.New("manifest: state.toml parse error")

	ErrSectionMissing = errors.New("manifest: required section missing")

	ErrManualFieldNotFound = errors.New("manifest: manual field path not found in schema")

	ErrManualFieldDrift = errors.New("manifest: manual edit detected on auto-source field")

	ErrEmptyReason = errors.New("manifest: --reason required for manual field change")

	ErrAutoSourceUnavailable = errors.New("manifest: auto-source unavailable")

	ErrFreshnessExceeded = errors.New("manifest: freshness threshold exceeded (invariant)")

	ErrDiffMismatch = errors.New("manifest: regenerate-and-diff mismatch (invariant)")

	// ErrEventEmissionFailed is returned when the chain integration call
	// fails; manual change MUST be reverted (no silent state divergence).
	ErrEventEmissionFailed = errors.New("manifest: event emission to audit chain failed")
)
