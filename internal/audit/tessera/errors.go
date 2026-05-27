// SPDX-License-Identifier: MIT
package tessera

import "errors"

var (
	// ErrEmptyProjectID is returned by NewProjectAdapter when the
	// caller passes an empty project_id. Per invariant, every
	// per-project tile-log MUST be addressable by a non-empty
	// project identifier.
	ErrEmptyProjectID = errors.New("tessera: project_id must be non-empty (inv-hades-144)")

	ErrWitnessKeyMissing = errors.New("tessera: witness keypair missing")

	ErrWitnessKeyAlreadyExists = errors.New("tessera: witness keypair already exists; use Rotate")

	// ErrUnsignedSTH is returned by CoSigner.Append when the caller
	// attempts to publish an STH without a daemon witness signature
	// to the daemon_global_checkpoint_log. Per invariant, every
	// daemon-global checkpoint entry MUST be signed.
	ErrUnsignedSTH = errors.New("tessera: refusing to publish unsigned STH (inv-hades-145)")

	ErrInvalidConfig = errors.New("tessera: invalid config")

	// ErrCrossProjectAccess is returned when a caller attempts to
	// read or append to a tile-log addressed by a project_id
	// different from the one the Adapter was constructed for.
	// Per invariant isolation MUST be enforced at the API surface.
	ErrCrossProjectAccess = errors.New("tessera: cross-project access refused (inv-hades-144)")

	ErrAdapterClosed = errors.New("tessera: adapter is closed")

	ErrCheckpointLogClosed = errors.New("tessera: checkpoint log is closed")
)
