// Drives the contract documented in regenerate.go:
//
// 1. Determinism — twin Regenerate calls with the same (fresh, existing)
// inputs must produce byte-identical merged Manifests. invariant
// (regenerate-and-diff CI gate) depends on this: if Regenerate is
// non-deterministic, the CI gate raises false positives every run.
//
// 2. No panic on any TOML payload — the on-disk system-state.toml is
// in scope of the spec §5.2 supply-chain attack model (T10 threat:
// attacker plants malformed TOML to silently lose manual-field
// pinning). The parser must surface ErrManifestInvalid + return
// cleanly, never panic deep in BurntSushi/toml.
//
// 3. Manual-field preservation — when the fuzzed existing TOML parses
// cleanly AND populates a manual field, the merged Manifest's
// manual-field value MUST equal the existing value, NOT the fresh
// value. This is the operator-pin invariant invariant.
//
// Fuzz strategy: feed (fresh Manifest scalar fields, existing TOML
// bytes). The fresh Manifest is constructed from a small set of fuzzed
// scalar inputs (we can't fuzz a struct directly — Go fuzzing supports
// only built-in types). The existing TOML is fed verbatim to a temp
// file, then passed as existingPath to Regenerator.Regenerate.
//
// Seed corpus: well-formed TOML with manual fields populated, empty
// TOML, malformed TOML (gibberish + invalid escapes + truncated), TOML
// with unknown sections (BurntSushi/toml accepts extra keys silently).
//
// Note on naming: the plan-file calls this FuzzRegenerate_Determinism
// for historical alignment with spec §5.5.
package manifest

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
)

func FuzzRegenerate_Determinism(f *testing.F) {

	f.Add(
		[]byte(`[zen-swarm]
version = "0.8.0"
substrate = "openclaude"
substrate_min_version = "0.7.1"

[doctrines]
declared = []
default = "max-scope"

[autonomous-mode]
status = "enabled"
prerequisites-met = false
last-check = 2026-05-01T00:00:00Z
`),
		"0.9.0",
		"max-scope",
		"enabled",
	)

	f.Add([]byte(""), "0.9.0", "", "")

	f.Add(
		[]byte(`[doctrines]
default = "capa-firewall"
`),
		"0.9.0",
		"",
		"",
	)

	f.Add([]byte(`[zen-swarm
broken = bracket
`), "0.9.0", "", "")
	f.Add([]byte(`not toml at all just gibberish 'unclosed string`), "0.9.0", "", "")
	f.Add([]byte(`[zen-swarm]
version = "unclosed`), "0.9.0", "", "")

	f.Add([]byte{0x00, 0x01, 0x02, 0xff, 0xfe, 0xfd}, "0.9.0", "", "")

	f.Add(
		[]byte(`[unknown-section]
weird = "value"

[zen-swarm]
substrate_min_version = "1.0.0"
`),
		"0.9.0",
		"",
		"",
	)

	f.Add(
		[]byte(`[a]
[b]
[c]
[d.e.f.g.h.i.j]
`),
		"0.9.0",
		"",
		"",
	)

	f.Fuzz(func(t *testing.T, existingTOML []byte, freshVersion, freshDefault, freshStatus string) {

		if len(freshVersion) > 256 || len(freshDefault) > 256 || len(freshStatus) > 256 {
			t.Skip()
		}
		if len(existingTOML) > 64*1024 {
			t.Skip()
		}

		fresh := Manifest{
			ZenSwarm: ZenSwarmSection{
				Version:   freshVersion,
				Substrate: "openclaude",
			},
			Doctrines: DoctrinesSection{
				Declared: []string{},
				Default:  freshDefault,
			},
			AutonomousMode: AutonomousModeSection{
				Status:           freshStatus,
				PrerequisitesMet: true,
			},
			Provenance: Provenance{},
		}

		dir, err := os.MkdirTemp("", "manifest-fuzz-")
		if err != nil {
			t.Fatalf("mkdir temp: %v", err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		schemaPath := filepath.Join(dir, "schema.json")
		if err := os.WriteFile(schemaPath, []byte(fixtureSchema), 0o644); err != nil {
			t.Fatalf("write schema fixture: %v", err)
		}
		schema, err := LoadSchema(schemaPath)
		if err != nil {
			t.Fatalf("LoadSchema: %v", err)
		}
		r := NewRegenerator(schema)

		manifestPath := filepath.Join(dir, "system-state.toml")
		if err := os.WriteFile(manifestPath, existingTOML, 0o644); err != nil {
			t.Fatalf("write existing manifest: %v", err)
		}

		ctx := context.Background()
		m1, err1 := r.Regenerate(ctx, fresh, manifestPath)
		m2, err2 := r.Regenerate(ctx, fresh, manifestPath)

		if (err1 == nil) != (err2 == nil) {
			t.Fatalf("non-deterministic error presence: err1=%v err2=%v", err1, err2)
		}
		if err1 != nil && err2 != nil {

			if err1.Error() != err2.Error() {
				t.Fatalf("non-deterministic error: %q vs %q", err1, err2)
			}

			if !errors.Is(err1, ErrManifestInvalid) {
				t.Fatalf("malformed TOML must return ErrManifestInvalid, got %v", err1)
			}
			return
		}

		b1, err := r.Emit(m1)
		if err != nil {
			t.Fatalf("Emit m1: %v", err)
		}
		b2, err := r.Emit(m2)
		if err != nil {
			t.Fatalf("Emit m2: %v", err)
		}
		if !bytes.Equal(b1, b2) {
			t.Fatalf("non-deterministic Emit: %d bytes vs %d bytes", len(b1), len(b2))
		}

		// Manual-field preservation: when existing parsed cleanly, the
		// merged Manifest's manual-field values MUST come from existing,
		// not fresh. We only assert this for the three known manual
		// fields and only when the existing TOML successfully decoded
		// into a non-zero value for that field (else the merged value
		// stays the fresh value, which is also correct).
		var existing Manifest
		if _, decErr := toml.NewDecoder(bytes.NewReader(existingTOML)).Decode(&existing); decErr == nil {
			if existing.ZenSwarm.SubstrateMinVersion != "" &&
				m1.ZenSwarm.SubstrateMinVersion != existing.ZenSwarm.SubstrateMinVersion {
				t.Errorf("SubstrateMinVersion: merged=%q, want existing=%q",
					m1.ZenSwarm.SubstrateMinVersion, existing.ZenSwarm.SubstrateMinVersion)
			}
			if existing.Doctrines.Default != "" &&
				m1.Doctrines.Default != existing.Doctrines.Default {
				t.Errorf("Doctrines.Default: merged=%q, want existing=%q",
					m1.Doctrines.Default, existing.Doctrines.Default)
			}
			if existing.AutonomousMode.Status != "" &&
				m1.AutonomousMode.Status != existing.AutonomousMode.Status {
				t.Errorf("AutonomousMode.Status: merged=%q, want existing=%q",
					m1.AutonomousMode.Status, existing.AutonomousMode.Status)
			}
		}
	})
}
