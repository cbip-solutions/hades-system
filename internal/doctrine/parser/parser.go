// SPDX-License-Identifier: MIT
// Package parser decodes per-doctrine self-contained TOML files into the
// v1 Schema struct (release Q2 A: no merge engine; one TOML = one
// effective doctrine). The parser is the SOLE entry point used by the
// embedded built-in loader and the user-override
// reload loop so both surfaces share one code path
// (Q9 B). Strict-mode decoding is mandatory: any unknown key surfaces
// as ErrParseFailed (Q5 B + spec §1; research SOTA "silent strictness
// drift" anti-pattern guard). Per-Q3 C Tier 1, user/override TOMLs that
// declare the [doctrine_transverse] section are rejected with a typed
// *errors.TransverseOverrideAttempt (invariant enforcement).
//
// The parser is policy-free with respect to schema_version: it returns
// the literal value via ExtractSchemaVersion and lets the caller (daemon
// startup) decide whether to dispatch to internal/doctrine/migrate/
// (Q15 A). It is also I/O-free: callers supply []byte; the `source`
// argument is a free-form label used only for error wrapping.
//
// Boundary parser ⊥ internal/store (invariant). No store imports;
// no I/O outside the bytes passed in.
package parser

import (
	"bytes"
	"errors"
	"fmt"
	"sort"

	"github.com/BurntSushi/toml"

	doctrineerrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

// schemaVersionProbe is a single-field anonymous-equivalent struct used
// by ExtractSchemaVersion. We decode into this minimal shape so we never
// pay the full v1.Schema decode just to learn the version. BurntSushi
// silently ignores all other top-level keys when decoding into a partial
// struct — perfect for a probe (we explicitly DO NOT call Undecoded()
// here; that strictness is for ParseStrict).
type schemaVersionProbe struct {
	SchemaVersion string `toml:"schema_version"`
}

func ExtractSchemaVersion(data []byte) (string, error) {
	var probe schemaVersionProbe
	_, err := toml.NewDecoder(bytes.NewReader(data)).Decode(&probe)
	if err != nil {
		return "", wrapParseError(err, "<unknown>")
	}
	return probe.SchemaVersion, nil
}

func wrapParseError(err error, source string) error {
	var pe toml.ParseError
	if errors.As(err, &pe) {
		return fmt.Errorf("doctrine TOML at %s:%d:%d: %s: %w",
			source, pe.Position.Line, pe.Position.Col, pe.Message,
			doctrineerrors.ErrParseFailed)
	}
	return fmt.Errorf("doctrine TOML at %s: %v: %w",
		source, err, doctrineerrors.ErrParseFailed)
}

// ParseOpts gates per-source parser behavior. Callers MUST pass the
// correct opts for their trust tier:
// - Embedded built-in TOMLs: ParseOpts{AllowTransverseDeclaration: true}
// - User override / per-project override: ParseOpts{} (default)
//
// Passing the wrong opts is an invariant vulnerability; integration
// tests in assert the correct opts at each call site.
type ParseOpts struct {
	// AllowTransverseDeclaration enables the [doctrine_transverse]
	// section to appear in the input. ONLY embedded built-in TOMLs
	// (max-scope, default, capa-firewall) MAY declare transverse
	// axioms (Q3 C Tier 1: hardcoded operator-only). User TOMLs and
	// per-project overrides MUST NOT, and the parser returns
	// *errors.TransverseOverrideAttempt when they do (invariant).
	AllowTransverseDeclaration bool
}

// ParseStrict decodes data into target, rejecting any unknown keys
// (MetaData.Undecoded() check, B-3) and any transverse-section
// violation (B-4). source identifies the input origin for error
// wrapping ("embed:max-scope.toml", "user:~/.config/.../foo.toml",
// "project:/repo/.hades/doctrine-override.toml", etc.).
//
// Pre/postconditions:
// - data must be a complete, self-contained TOML byte slice (Q2 A;
// no merge engine — what's in the file is the effective doctrine).
// - target must be a non-nil *v1.Schema; on nil target the parser
// returns an error wrapping ErrParseFailed (we do NOT panic on
// caller programming bugs in production paths).
// - On success, target is fully populated with TOML values; defaults
// fill any field absent from the input per struct tags.
// - On any error, target's state is unspecified — callers must
// either discard target or re-init before retry.
//
// Returns
// - nil on success.
// - *errors.TransverseOverrideAttempt if opts.AllowTransverseDeclaration=false
// and the input declares [doctrine_transverse] (B-4).
// - error wrapping ErrParseFailed for syntax errors (B-5) or unknown keys (B-3).
//
// Example:
//
// data, _ := embedFS.ReadFile("max-scope.toml")
// var s v1.Schema
// if err := parser.ParseStrict(data, "embed:max-scope.toml", &s, parser.ParseOpts{AllowTransverseDeclaration: true}); err != nil {
// panic(err) // F1: built-in parse failure is a build bug
// }
//
// Example:
//
// data, _ := os.ReadFile(path)
// var s v1.Schema
// if err := parser.ParseStrict(data, "user:"+path, &s, parser.ParseOpts{}); err != nil {
// // F2 / F3: emit DoctrineLoadFailed; keep last-good
// return err
// }
func ParseStrict(data []byte, source string, target *v1.Schema, opts ParseOpts) error {
	if target == nil {
		return fmt.Errorf("doctrine TOML at %s: nil target *v1.Schema: %w",
			source, doctrineerrors.ErrParseFailed)
	}
	md, err := toml.NewDecoder(bytes.NewReader(data)).Decode(target)
	if err != nil {
		return wrapParseError(err, source)
	}

	if !opts.AllowTransverseDeclaration && md.IsDefined("doctrine_transverse") {
		return &doctrineerrors.TransverseOverrideAttempt{
			Source:  source,
			Section: "doctrine_transverse",
		}
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {

		sort.Slice(undecoded, func(i, j int) bool {
			return undecoded[i].String() < undecoded[j].String()
		})
		first := undecoded[0].String()
		return fmt.Errorf("doctrine TOML at %s: unknown key %q (strict mode: typo or schema drift; total %d unknown keys): %w",
			source, first, len(undecoded), doctrineerrors.ErrParseFailed)
	}
	return nil
}
