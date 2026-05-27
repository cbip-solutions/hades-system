// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT

// Package main — doctrine_registry.go (release fix-cycle
// re-review pre-existing-gap fix).
//
// bootDoctrineRegistry wires the daemon's active.Accessor singleton with
// the built-in doctrine registry parsed from internal/doctrine/builtin/.
//
// # Why this exists
//
// The active.Accessor singleton's SetRegistry is documented as the
// daemon-startup contract in
// internal/doctrine/active/active.go:53-65. server.go:511-512 also
// explicitly references this contract:
//
// "reloadWatcher is the daemon-owned *reload.Watcher singleton. nil
// until SetReloadWatcher is called by cmd/zen-swarm-ctld AFTER
// builtin.LoadAll + active.SetRegistry."
//
// But the production main.go boot never invoked active.SetRegistry,
// leaving the singleton zero-valued. Consequence: every consumer that
// reads via active.Active() / active.For() panicked (recovered to
// ErrDoctrineNotFound by Server.DoctrineActive's safeActive shim).
// Surface-level effects:
//
// - sessionDoctrine returns "" (init-order fail-closed) → every
// /v1/audit/event/<id> request returns 401 even for valid events
// .
//
// - /v1/doctrine/active returns 404 "doctrine: name not found in
// registry" (the daemon literally cannot name its own active
// doctrine).
//
// The re-reviewer flagged this as a pre-existing daemon gap surfaced by
// the Critical-3 fix-cycle. This file closes it daemon-wide; future
// doctrine-dependent endpoints inherit the wiring automatically.
//
// # Lifecycle
//
// Called ONCE from main.go before srv.Start(). Subsequent calls
// re-load the registry from the embedded TOMLs (atomic-swap semantics
// per active.Accessor.SetRegistry); this matches the future
// `zen doctrine reload` CLI surface.
//
// # Failure semantics
//
// builtin.LoadAll error is a corrupted-binary condition (the embedded
// TOMLs MUST parse + validate; if they do not, no daemon path is safe
// to start). Returns the error verbatim; main.go converts to os.Exit(1).
package main

import (
	"fmt"

	"github.com/cbip-solutions/hades-system/internal/doctrine/active"
	"github.com/cbip-solutions/hades-system/internal/doctrine/builtin"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

func bootDoctrineRegistry() error {
	return bootDoctrineRegistryFrom(builtin.LoadAll)
}

func bootDoctrineRegistryFrom(loader func() (map[string]*v1.Schema, error)) error {
	reg, err := loader()
	if err != nil {
		return fmt.Errorf("doctrine builtin.LoadAll (corrupted binary): %w", err)
	}
	active.SetRegistry(reg)
	return nil
}
