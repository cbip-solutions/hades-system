package migrate_test

import (
	"errors"
	"strings"
	"testing"

	doctrineerrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
	"github.com/cbip-solutions/hades-system/internal/doctrine/migrate"
)

func TestMigrateV1ToV2ReturnsErrMigrationNotImplemented(t *testing.T) {

	data := []byte("schema_version = \"1.0\"\n")

	got, err := migrate.MigrateV1ToV2(data)

	if got != nil {
		t.Errorf("MigrateV1ToV2 returned non-nil schema; want nil for placeholder: %+v", got)
	}
	if err == nil {
		t.Fatal("MigrateV1ToV2 returned nil error; want ErrMigrationNotImplemented (placeholder)")
	}
	if !errors.Is(err, doctrineerrors.ErrMigrationNotImplemented) {
		t.Errorf("err is not ErrMigrationNotImplemented: %v", err)
	}

	msg := err.Error()
	if !strings.Contains(msg, "MigrateV1ToV2") {
		t.Errorf("error message missing function name MigrateV1ToV2; got: %s", msg)
	}
	if !strings.Contains(msg, "Plan 8") {
		t.Errorf("error message missing Plan 8 reference (future-implementer breadcrumb); got: %s", msg)
	}
}

// TestMigrateV1ToV2NilInputReturnsSentinel pins behaviour for the nil-input
// edge: even with a nil byte slice, the placeholder must surface
// ErrMigrationNotImplemented (not a nil-deref panic). Future implementer
// MUST preserve this defensive shape when filling in the body.
func TestMigrateV1ToV2NilInputReturnsSentinel(t *testing.T) {
	got, err := migrate.MigrateV1ToV2(nil)
	if got != nil {
		t.Errorf("MigrateV1ToV2(nil) returned non-nil schema: %+v", got)
	}
	if !errors.Is(err, doctrineerrors.ErrMigrationNotImplemented) {
		t.Errorf("MigrateV1ToV2(nil) err is not ErrMigrationNotImplemented: %v", err)
	}
}

// TestMigrateV1ToV2NotRegisteredInChainAtPhaseC pins the registration
// contract: MigrateV1ToV2 MUST NOT be wired into the chain map at
// Wiring it would shadow the passthrough Migrator on "1.0" source and
// break builtin loader. Future + schema-bump implementer
// must add the chain entry as part of the same commit that fills in the
// body — this test is the contract reminder.
//
// We can't directly inspect the package-private chain map from
// migrate_test (external test package), so we test the BEHAVIOUR:
// MigrateChain on CurrentSchemaVersion must dispatch to passthrough
// (parses successfully), not to MigrateV1ToV2 (which would return
// ErrMigrationNotImplemented).
func TestMigrateV1ToV2NotRegisteredInChainAtPhaseC(t *testing.T) {
	got, err := migrate.MigrateChain([]byte(minimalValidV1TOML), "1.0")
	if err != nil {
		t.Fatalf("MigrateChain on \"1.0\" returned error; expected passthrough success: %v", err)
	}
	if got == nil {
		t.Fatal("MigrateChain on \"1.0\" returned nil schema; expected passthrough success")
	}

	if errors.Is(err, doctrineerrors.ErrMigrationNotImplemented) {
		t.Error("MigrateChain on \"1.0\" routed to MigrateV1ToV2 (placeholder); expected passthrough — registration contract violated")
	}
}
