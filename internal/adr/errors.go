// SPDX-License-Identifier: MIT
package adr

import "errors"

var (
	ErrIDCollision = errors.New("adr: id collision detected")

	ErrSupersedeCycle = errors.New("adr: supersede cycle detected")

	ErrInvalidFrontmatter = errors.New("adr: invalid frontmatter")

	ErrUnknownStatus = errors.New("adr: unknown status value")

	ErrFileNotFound = errors.New("adr: file not found")

	ErrInvalidTransition = errors.New("adr: invalid transition")

	ErrReservedStatusNotTransitionable = errors.New("adr: Reserved status not transitionable via API")

	ErrEmptyReason = errors.New("adr: reason is required for transition")

	ErrFrontmatterMissing = errors.New("adr: frontmatter missing")

	ErrSchemaViolation = errors.New("adr: schema violation")
)
