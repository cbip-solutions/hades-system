// SPDX-License-Identifier: MIT
// Package doctrine implements the doctrine configuration substrate
// for hades-system (spec §2.2 Capa 2 + §4.4) and the per-project policy
// doctrines: max-scope, default, capa-firewall.
//
// The package exposes:
//
// - Schema: pure value type describing the full TOML schema
// (resolver, loader, builtin defaults, additive-only validator).
// - Resolver / Resolved: the four-layer chain (builtin → custom
// TOML → project hadessystem.toml → --doctrine flag) with field-
// level provenance tracking and ceiling-aware clamping.
// - LoadFile / Loaded: pure I/O wrapper over BurntSushi/toml with
// strict-mode unknown-field rejection (invariant).
// - ValidateAdditive / ValidateRange: invariant enforcement
// (additive-only Schema evolution unless a doctrine-schema ADR
// is referenced).
// - Doctrine + Get / MaxScope / Default / CapaFirewall: the
// HADES design minimal interface contract; HADES design wires substantive
// bodies.
//
// # Concurrency
//
// All exported functions in this package are safe for concurrent use
// by multiple goroutines. Resolver, LoadFile, ValidateAdditive,
// ValidateRange, and the builtin constructors hold no shared mutable
// state — each call returns a fresh Schema / Loaded / Resolved /
// ValidationResult value whose maps are owned by the caller and may
// be modified freely. The Resolver itself is a value type and is
// goroutine-safe by virtue of being immutable per-call (the receiver
// is copied; no methods on Resolver mutate it).
//
// Note specifically that the loader's Loaded.Provenance is NOT
// aliased into the Resolved.Provenance returned by Resolve() — the
// resolver copies provenance maps before applying clamp / mismatch
// markers (C-3 / I-1 fixes), so any future caching layer
// can rely on Loaded values remaining stable across the call
// boundary.
//
// # HADES design backfill
//
// the interface contract; HADES design delivers the configuration
// substrate (Schema, loader, resolver, validator, builtin defaults).
package doctrine

import (
	"errors"
	"fmt"
)

// ErrDoctrineValidation is the sentinel error every validation failure
// from the doctrine loader (TOML parse, unknown-field rejection, additive-
// only violation, range check, money/duration parse) MUST be wrapped with.
//
// Daemon HTTP handlers discriminate validation failures (HTTP 422) from
// system failures (HTTP 500) via errors.Is(err, ErrDoctrineValidation).
// Post-review C-2 fix: prior code surfaced every
// reload error as 422, masking real system faults (disk full, Keychain
// unavailable, SQLite locked) as misleading "validation failure" responses.
//
// Wrap with %w when surfacing a typed validation error:
//
// return fmt.Errorf("%w: unknown field %q", doctrine.ErrDoctrineValidation, field)
var ErrDoctrineValidation = errors.New("doctrine: validation failed")

type Name string

const (
	NameMaxScope Name = "max-scope"

	NameDefault Name = "default"

	NameCapaFirewall Name = "capa-firewall"
)

func IsValid(n Name) bool {
	switch n {
	case NameMaxScope, NameDefault, NameCapaFirewall:
		return true
	}
	return false
}

type Doctrine interface {
	Name() Name

	ArchiveStrategy() string

	RequireAdvisoryDefault() bool

	PrivacyLocked() bool

	PreFlightExtras() []string

	PreArchiveExtras() []string
}

func Get(n Name) (Doctrine, error) {
	switch n {
	case NameMaxScope:
		return MaxScope{}, nil
	case NameDefault:
		return Default{}, nil
	case NameCapaFirewall:
		return CapaFirewall{}, nil
	}
	return nil, fmt.Errorf("doctrine: unknown name %q (expected: max-scope|default|capa-firewall)", n)
}
