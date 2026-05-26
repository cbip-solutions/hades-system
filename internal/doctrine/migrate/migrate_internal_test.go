package migrate

import (
	"errors"
	"strings"
	"testing"

	doctrineerrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
	"github.com/cbip-solutions/hades-system/internal/doctrine/schema"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

// TestMigrateChainRejectsDowngradeFromMaliciousMigrator pins the
// inv-zen-142 defence-in-depth check: even if a buggy or malicious
// Migrator is registered that produces a *v1.Schema with a lower
// SchemaVersion than the source, the dispatcher MUST refuse to return
// it via ErrSchemaVersionDowngradeRejected.
func TestMigrateChainRejectsDowngradeFromMaliciousMigrator(t *testing.T) {

	maliciousDowngrader := func(data []byte) (*v1.Schema, error) {
		return &v1.Schema{
			SchemaVersion:   "0.5",
			DoctrineVersion: "1.0.0",
		}, nil
	}
	registerMigratorForTest(t, schema.CurrentSchemaVersion, maliciousDowngrader)

	_, err := MigrateChain([]byte("ignored"), schema.CurrentSchemaVersion)

	if err == nil {
		t.Fatal("MigrateChain returned nil error against malicious downgrade Migrator")
	}
	if !errors.Is(err, doctrineerrors.ErrSchemaVersionDowngradeRejected) {
		t.Errorf("err is not ErrSchemaVersionDowngradeRejected: %v", err)
	}

	msg := err.Error()
	if !strings.Contains(msg, "0.5") {
		t.Errorf("error message missing claimed-target version 0.5; got: %s", msg)
	}
	if !strings.Contains(msg, schema.CurrentSchemaVersion) {
		t.Errorf("error message missing source version %s; got: %s", schema.CurrentSchemaVersion, msg)
	}
}

func TestMigrateChainSameVersionReturnIsNotDowngrade(t *testing.T) {
	noopMigrator := func(data []byte) (*v1.Schema, error) {
		return &v1.Schema{
			SchemaVersion:   schema.CurrentSchemaVersion,
			DoctrineVersion: "1.0.0",
		}, nil
	}
	registerMigratorForTest(t, schema.CurrentSchemaVersion, noopMigrator)

	got, err := MigrateChain([]byte("ignored"), schema.CurrentSchemaVersion)

	if err != nil {
		t.Fatalf("MigrateChain returned error against no-op same-version Migrator: %v", err)
	}
	if got == nil || got.SchemaVersion != schema.CurrentSchemaVersion {
		t.Errorf("schema = %+v, want SchemaVersion = %q", got, schema.CurrentSchemaVersion)
	}
}

func TestMigrateChainUpgradeAllowed(t *testing.T) {
	upgrader := func(data []byte) (*v1.Schema, error) {
		return &v1.Schema{
			SchemaVersion:   "2.0",
			DoctrineVersion: "1.0.0",
		}, nil
	}
	registerMigratorForTest(t, schema.CurrentSchemaVersion, upgrader)

	got, err := MigrateChain([]byte("ignored"), schema.CurrentSchemaVersion)

	if err != nil {
		t.Fatalf("MigrateChain returned error against legitimate upgrade Migrator: %v", err)
	}
	if got.SchemaVersion != "2.0" {
		t.Errorf("schema.SchemaVersion = %q, want %q", got.SchemaVersion, "2.0")
	}
}

func TestMigrateChainRejectsNilSchemaWithoutError(t *testing.T) {
	buggyMigrator := func(data []byte) (*v1.Schema, error) {
		return nil, nil
	}
	registerMigratorForTest(t, schema.CurrentSchemaVersion, buggyMigrator)

	_, err := MigrateChain([]byte("ignored"), schema.CurrentSchemaVersion)

	if err == nil {
		t.Fatal("MigrateChain returned nil error against buggy nil-schema Migrator")
	}
	if !errors.Is(err, doctrineerrors.ErrMigrationFailed) {
		t.Errorf("err is not ErrMigrationFailed: %v", err)
	}
}

func TestMigrateChainWrapsNonSentinelMigratorError(t *testing.T) {
	customErr := errors.New("custom converter failure")
	failingMigrator := func(data []byte) (*v1.Schema, error) {
		return nil, customErr
	}
	registerMigratorForTest(t, schema.CurrentSchemaVersion, failingMigrator)

	_, err := MigrateChain([]byte("ignored"), schema.CurrentSchemaVersion)

	if err == nil {
		t.Fatal("MigrateChain returned nil error against failing Migrator")
	}
	if !errors.Is(err, customErr) {
		t.Errorf("err does not wrap customErr (errors.Is failed): %v", err)
	}
	wantPrefix := "migrate " + schema.CurrentSchemaVersion + " -> " + schema.CurrentSchemaVersion
	if !strings.Contains(err.Error(), wantPrefix) {
		t.Errorf("error message missing wrapping prefix %q; got: %s", wantPrefix, err.Error())
	}
}

func TestMigrateChainPlaceholderMigratorReturnsSentinel(t *testing.T) {
	placeholder := func(data []byte) (*v1.Schema, error) {
		return nil, doctrineerrors.ErrMigrationNotImplemented
	}
	registerMigratorForTest(t, schema.CurrentSchemaVersion, placeholder)

	_, err := MigrateChain([]byte("ignored"), schema.CurrentSchemaVersion)

	if err == nil {
		t.Fatal("MigrateChain returned nil error against placeholder Migrator")
	}
	if !errors.Is(err, doctrineerrors.ErrMigrationNotImplemented) {
		t.Errorf("err is not ErrMigrationNotImplemented (lost via wrapping): %v", err)
	}
}

func TestMigrateChainCoverageHelpers(t *testing.T) {
	t.Run("decrementMinor wraps X.0 to (X-1).9", func(t *testing.T) {
		cases := map[string]string{
			"1.0": "0.9",
			"2.0": "1.9",
			"5.3": "5.2",
			"0.0": "0.0",
			"0.5": "0.4",
		}
		for input, want := range cases {
			if got := decrementMinor(input); got != want {
				t.Errorf("decrementMinor(%q) = %q, want %q", input, got, want)
			}
		}
	})

	t.Run("splitVersion returns 0,0 on malformed input", func(t *testing.T) {
		major, minor := splitVersion("not-a-version")
		if major != 0 || minor != 0 {
			t.Errorf("splitVersion(\"not-a-version\") = (%d, %d), want (0, 0)", major, minor)
		}
	})

	t.Run("compareVersions covers major drift branch", func(t *testing.T) {

		if compareVersions("1.0", "2.0") != -1 {
			t.Error("compareVersions(\"1.0\", \"2.0\") != -1")
		}

		if compareVersions("3.0", "1.0") != 1 {
			t.Error("compareVersions(\"3.0\", \"1.0\") != 1")
		}

		if compareVersions("1.5", "1.5") != 0 {
			t.Error("compareVersions(\"1.5\", \"1.5\") != 0")
		}
	})
}
