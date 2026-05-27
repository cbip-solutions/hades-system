// SPDX-License-Identifier: MIT
package migrate

import (
	"fmt"

	doctrineerrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
	"github.com/cbip-solutions/hades-system/internal/doctrine/parser"
	"github.com/cbip-solutions/hades-system/internal/doctrine/schema"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

// Migrator is a registered V→V+1 converter for doctrine schema migration.
//
// Per Q15 A + invariant: every Migrator MUST return *v1.Schema only.
// No Migrator may write to disk; persistent write-back is operator-explicit
// via the CLI (`hades doctrine migrate <path> --confirm`).
//
// Per invariant defence-in-depth: the dispatcher (MigrateChain) verifies
// the returned schema's SchemaVersion >= the source version and returns
// ErrSchemaVersionDowngradeRejected if a Migrator misbehaves. Migrator
// authors should still set SchemaVersion to the target version in their
// returned struct to avoid the dispatcher catching them by accident.
type Migrator func(data []byte) (*v1.Schema, error)

var chain = map[string]Migrator{
	schema.CurrentSchemaVersion: passthrough,
}

func MigrateChain(data []byte, fromVersion string) (*v1.Schema, error) {
	if fromVersion == "" {
		return nil, fmt.Errorf("migrate: %w (empty fromVersion)", doctrineerrors.ErrSchemaVersionUnsupported)
	}

	if isNewerThanCurrent(fromVersion) {
		return nil, fmt.Errorf(
			"migrate: source schema_version %q is newer than supported %q: %w",
			fromVersion, schema.CurrentSchemaVersion, doctrineerrors.ErrSchemaVersionUnsupported,
		)
	}

	if isTooOld(fromVersion) {
		return nil, fmt.Errorf(
			"migrate: source schema_version %q is older than supported range [%s, %s]; run `hades doctrine migrate <path>` for manual chain migration: %w",
			fromVersion, previousSupportedVersion(), schema.CurrentSchemaVersion, doctrineerrors.ErrSchemaVersionTooOld,
		)
	}

	migrator, ok := chain[fromVersion]
	if !ok {

		return nil, fmt.Errorf(
			"migrate: no Migrator registered for source schema_version %q (binary build error or unsupported in-range version): %w",
			fromVersion, doctrineerrors.ErrSchemaVersionUnsupported,
		)
	}

	migrated, err := migrator(data)
	if err != nil {

		return nil, fmt.Errorf("migrate %s -> %s: %w", fromVersion, schema.CurrentSchemaVersion, err)
	}
	if migrated == nil {
		return nil, fmt.Errorf(
			"migrate: Migrator for %q returned nil *Schema without error (build bug): %w",
			fromVersion, doctrineerrors.ErrMigrationFailed,
		)
	}

	if isLower(migrated.SchemaVersion, fromVersion) {
		return nil, fmt.Errorf(
			"migrate: Migrator returned schema_version %q < source %q (downgrade attempt): %w",
			migrated.SchemaVersion, fromVersion, doctrineerrors.ErrSchemaVersionDowngradeRejected,
		)
	}

	return migrated, nil
}

// passthrough is the Migrator for "data is already at the current schema; no
// migration work required". It parses the TOML into *v1.Schema and returns it
// verbatim. Registered in the chain at CurrentSchemaVersion.
//
// Per Q15 A: even passthrough returns *v1.Schema only — never writes.
//
// Trust-tier note: passthrough invokes parser.ParseStrict with
// AllowTransverseDeclaration=true because the migrate package's Migrator
// signature is opts-free by design (see doc.go "Trust-tier delegation note").
// Callers that need transverse-rejection enforcement ( reload of
// user-edited files) MUST handle that concern separately. Passthrough cares
// only about structural decode-ability; invariant transverse policy is
// adjacent, not core, to the migrate package.
func passthrough(data []byte) (*v1.Schema, error) {
	var target v1.Schema
	err := parser.ParseStrict(data, "migrate.passthrough", &target, parser.ParseOpts{
		AllowTransverseDeclaration: true,
	})
	if err != nil {
		return nil, fmt.Errorf("migrate.passthrough: %w", err)
	}
	return &target, nil
}

func isNewerThanCurrent(version string) bool {
	return compareVersions(version, schema.CurrentSchemaVersion) > 0
}

func isTooOld(version string) bool {
	return compareVersions(version, previousSupportedVersion()) < 0
}

func isLower(a, b string) bool {
	return compareVersions(a, b) < 0
}

func previousSupportedVersion() string {
	return decrementMinor(schema.CurrentSchemaVersion)
}

func compareVersions(a, b string) int {
	majA, minA := splitVersion(a)
	majB, minB := splitVersion(b)
	if majA != majB {
		if majA < majB {
			return -1
		}
		return 1
	}
	if minA < minB {
		return -1
	}
	if minA > minB {
		return 1
	}
	return 0
}

func splitVersion(version string) (int, int) {
	var major, minor int
	_, err := fmt.Sscanf(version, "%d.%d", &major, &minor)
	if err != nil {
		return 0, 0
	}
	return major, minor
}

func decrementMinor(version string) string {
	major, minor := splitVersion(version)
	if minor > 0 {
		return fmt.Sprintf("%d.%d", major, minor-1)
	}

	if major == 0 {
		return "0.0"
	}
	return fmt.Sprintf("%d.9", major-1)
}
