// SPDX-License-Identifier: MIT
// Package errors declares the canonical sentinel errors for the doctrine
// system design spec §4.3. Phases B-N consume this package by
// import; in-package short aliases inside internal/doctrine/schema/v1
// (ErrTightenViolation etc.) point HERE so errors.Is across packages
// resolves to one canonical identity.
//
// Boundary discipline (invariant): this package imports stdlib only.
package errors

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrSchemaVersionUnsupported = errors.New("doctrine: schema version unsupported")

	ErrSchemaVersionTooOld = errors.New("doctrine: schema version older than N-1; manual migration required")

	ErrTightenViolation = errors.New("doctrine: per-project override attempts to loosen baseline (invariant)")

	ErrParseFailed = errors.New("doctrine: TOML parse failed")

	ErrValidationFailed = errors.New("doctrine: schema validation failed")

	ErrMigrationFailed = errors.New("doctrine: schema migration converter failed")

	ErrMigrationNotImplemented = errors.New("doctrine: schema migration converter not implemented")

	ErrReinforcementTemplateExec = errors.New("doctrine: reinforcement template execution failed")

	ErrTemplateNotFound = errors.New("doctrine: reinforcement template not found")

	ErrAmendmentApplyFailed = errors.New("doctrine: amendment apply (file write) failed")

	ErrDoctrineNotFound = errors.New("doctrine: name not found in registry")

	ErrWatcherStalled = errors.New("doctrine: file-watcher stalled")

	ErrSchemaVersionDowngradeRejected = errors.New("doctrine: refusing to downgrade schema version")

	ErrTransverseOverrideAttempted = errors.New("doctrine: transverse axiom override attempt rejected (invariant hardcoded operator-only)")
)

type TransverseOverrideAttempt struct {
	Source  string
	Section string
	Fields  []string
}

func (e *TransverseOverrideAttempt) Error() string {
	if len(e.Fields) > 0 {
		return fmt.Sprintf("doctrine: source=%s section=%s attempted to override transverse fields [%s] (invariant hardcoded operator-only)",
			e.Source, e.Section, strings.Join(e.Fields, ","))
	}
	return fmt.Sprintf("doctrine: source=%s section=%s attempted to override transverse axioms (invariant hardcoded operator-only)",
		e.Source, e.Section)
}

func (e *TransverseOverrideAttempt) Is(target error) bool {
	return target == ErrTransverseOverrideAttempted
}
