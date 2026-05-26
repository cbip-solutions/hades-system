package migrate_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	doctrineerrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
	"github.com/cbip-solutions/hades-system/internal/doctrine/migrate"
	"github.com/cbip-solutions/hades-system/internal/doctrine/schema"
)

const minimalValidV1TOML = `schema_version = "1.0"
doctrine_version = "1.0.0"
auto_upgrade = "none"
`

func TestMigrateChainPassthroughCurrentVersion(t *testing.T) {

	data := []byte(minimalValidV1TOML)

	got, err := migrate.MigrateChain(data, schema.CurrentSchemaVersion)

	if err != nil {
		t.Fatalf("MigrateChain passthrough returned error: %v", err)
	}
	if got == nil {
		t.Fatal("MigrateChain returned nil *Schema for passthrough")
	}
	if got.SchemaVersion != schema.CurrentSchemaVersion {
		t.Errorf("schema.SchemaVersion = %q, want %q", got.SchemaVersion, schema.CurrentSchemaVersion)
	}

	if got.DoctrineVersion != "1.0.0" {
		t.Errorf("schema.DoctrineVersion = %q, want %q", got.DoctrineVersion, "1.0.0")
	}
}

func TestMigrateChainErrorsAreSentinels(t *testing.T) {
	t.Run("sentinel errors exported", func(t *testing.T) {

		var _ error = doctrineerrors.ErrSchemaVersionTooOld
		var _ error = doctrineerrors.ErrSchemaVersionUnsupported
		var _ error = doctrineerrors.ErrSchemaVersionDowngradeRejected
		var _ error = doctrineerrors.ErrMigrationNotImplemented
		var _ error = doctrineerrors.ErrMigrationFailed

		if !errors.Is(doctrineerrors.ErrSchemaVersionTooOld, doctrineerrors.ErrSchemaVersionTooOld) {
			t.Fatal("errors.Is identity broken on sentinel ErrSchemaVersionTooOld")
		}
	})
}

func TestMigrateChainDoesNotMutateInput(t *testing.T) {
	data := []byte(minimalValidV1TOML)
	original := make([]byte, len(data))
	copy(original, data)

	_, _ = migrate.MigrateChain(data, schema.CurrentSchemaVersion)

	if !bytes.Equal(data, original) {
		t.Error("MigrateChain mutated input bytes")
	}
}

func TestMigrateChainErrorMessagesUseMigratePrefix(t *testing.T) {

	_, err := migrate.MigrateChain([]byte(minimalValidV1TOML), "")
	if err == nil {
		t.Fatal("MigrateChain(\"\") returned nil error")
	}
	if !strings.HasPrefix(err.Error(), "migrate") {
		t.Errorf("err message does not start with 'migrate' prefix: %s", err.Error())
	}
}

// TestMigrateChainRejectsVersionTooOld pins ErrSchemaVersionTooOld emission
// for inputs older than the supported floor (CurrentSchemaVersion - 1 minor).
// The error message MUST include the source version, the supported range
// `[N-1, N]`, and the suggested CLI invocation per spec §6.5 Workflow B.
func TestMigrateChainRejectsVersionTooOld(t *testing.T) {
	cases := []struct {
		name        string
		fromVersion string
		wantInMsg   []string // substrings that MUST appear in error message
	}{
		{
			name:        "one minor too old (0.8 vs current 1.0)",
			fromVersion: "0.8",
			wantInMsg:   []string{"0.8", "0.9", "1.0", "zen doctrine migrate"},
		},
		{
			name:        "many minors too old",
			fromVersion: "0.1",
			wantInMsg:   []string{"0.1", "0.9", "1.0", "zen doctrine migrate"},
		},
		{
			name:        "major version too old (0.0)",
			fromVersion: "0.0",
			wantInMsg:   []string{"0.0", "0.9", "1.0", "zen doctrine migrate"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := migrate.MigrateChain([]byte(minimalValidV1TOML), tc.fromVersion)
			if err == nil {
				t.Fatalf("MigrateChain(%q) returned nil error, want ErrSchemaVersionTooOld", tc.fromVersion)
			}
			if !errors.Is(err, doctrineerrors.ErrSchemaVersionTooOld) {
				t.Errorf("err is not ErrSchemaVersionTooOld: %v", err)
			}
			msg := err.Error()
			for _, want := range tc.wantInMsg {
				if !strings.Contains(msg, want) {
					t.Errorf("error message missing %q; got: %s", want, msg)
				}
			}
		})
	}
}

func TestMigrateChainBoundaryNMinusOneNotTooOld(t *testing.T) {
	_, err := migrate.MigrateChain([]byte(minimalValidV1TOML), "0.9")
	if err == nil {
		t.Fatal("MigrateChain(\"0.9\") returned nil error")
	}
	if errors.Is(err, doctrineerrors.ErrSchemaVersionTooOld) {
		t.Errorf("MigrateChain(\"0.9\") returned ErrSchemaVersionTooOld; expected ErrSchemaVersionUnsupported (no Migrator registered): %v", err)
	}
	if !errors.Is(err, doctrineerrors.ErrSchemaVersionUnsupported) {
		t.Errorf("MigrateChain(\"0.9\") err is not ErrSchemaVersionUnsupported: %v", err)
	}
}

// TestMigrateChainRejectsVersionNewerThanCurrent pins
// ErrSchemaVersionUnsupported emission for source versions newer than the
// daemon supports. Operator likely hand-edited a future-schema file or
// installed the wrong binary version; the error message MUST include the
// source version so the operator can correlate it with the file under review.
func TestMigrateChainRejectsVersionNewerThanCurrent(t *testing.T) {
	cases := []string{"1.1", "2.0", "999.0"}
	for _, version := range cases {
		t.Run(version, func(t *testing.T) {
			_, err := migrate.MigrateChain([]byte(minimalValidV1TOML), version)
			if err == nil {
				t.Fatalf("MigrateChain(%q) returned nil error", version)
			}
			if !errors.Is(err, doctrineerrors.ErrSchemaVersionUnsupported) {
				t.Errorf("MigrateChain(%q) err is not ErrSchemaVersionUnsupported: %v", version, err)
			}
			if !strings.Contains(err.Error(), version) {
				t.Errorf("error message missing source version %q; got: %s", version, err.Error())
			}
		})
	}
}

func TestMigrateChainRejectsEmptyFromVersion(t *testing.T) {
	_, err := migrate.MigrateChain([]byte(minimalValidV1TOML), "")
	if err == nil {
		t.Fatal("MigrateChain(\"\") returned nil error")
	}
	if !errors.Is(err, doctrineerrors.ErrSchemaVersionUnsupported) {
		t.Errorf("MigrateChain(\"\") err is not ErrSchemaVersionUnsupported: %v", err)
	}
}

func TestMigrateChainRejectsMalformedFromVersion(t *testing.T) {
	_, err := migrate.MigrateChain([]byte(minimalValidV1TOML), "not-a-version")
	if err == nil {
		t.Fatal("MigrateChain(\"not-a-version\") returned nil error")
	}

	if !errors.Is(err, doctrineerrors.ErrSchemaVersionTooOld) {
		t.Errorf("MigrateChain(\"not-a-version\") err is not ErrSchemaVersionTooOld: %v", err)
	}
}

func TestMigrateChainPassthroughParseError(t *testing.T) {

	malformed := []byte(`schema_version = "1.0
doctrine_version = "1.0.0"
`)
	_, err := migrate.MigrateChain(malformed, schema.CurrentSchemaVersion)
	if err == nil {
		t.Fatal("MigrateChain on malformed TOML returned nil error")
	}
	if !errors.Is(err, doctrineerrors.ErrParseFailed) {
		t.Errorf("err is not ErrParseFailed: %v", err)
	}

	if !strings.Contains(err.Error(), "migrate "+schema.CurrentSchemaVersion+" -> "+schema.CurrentSchemaVersion) {
		t.Errorf("error message missing migrate from->to prefix: %s", err.Error())
	}

	if !strings.Contains(err.Error(), "migrate.passthrough") {
		t.Errorf("error message missing migrate.passthrough wrapping: %s", err.Error())
	}
}
