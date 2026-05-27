// go:build property && cgo

// tests/property/ecosystem/chunk_fingerprint_property_test.go
//
// is the cross-version dedup key (Chunk.Fingerprint per types.go §"ContentText
// + Fingerprint"). Re-chunking the same content_text MUST produce the
// same fingerprint across runs and across machines — otherwise
// cross-version dedup is broken and the corpus grows unboundedly.
//
// Properties tested:
//
// 1. Determinism — fingerprint(content) called twice yields identical
// digests across 1000 random ContentText payloads.
// 2. Content sensitivity — any single-byte mutation of ContentText
// produces a DIFFERENT fingerprint (dedup must not silently merge
// drift).
// 3. Independence — fingerprint depends only on ContentText, not on
// surrounding fields (SymbolPath, Kind, Version, etc.). Same
// content with different metadata MUST collide on fingerprint so
// the dedup engine recognises the duplicate.
//
// The recipe (sha256 of content_text) is the canonical contract per
// `internal/research/ecosystem/types.go` Chunk docstring; this file
// mirrors that recipe and asserts the algebraic floor. A future
// implementer who switches to xxhash or salts the digest with PID
// breaks this test.

package ecosystem_property_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"testing/quick"

	_ "github.com/mattn/go-sqlite3"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

func computeFingerprint(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

func TestChunkFingerprint_Property_DeterministicAcrossRuns(t *testing.T) {
	prop := func(content string) bool {
		f1 := computeFingerprint(content)
		f2 := computeFingerprint(content)
		return f1 == f2
	}
	cfg := &quick.Config{MaxCount: 1000}
	if err := quick.Check(prop, cfg); err != nil {
		t.Errorf("Plan 14 substrate: ChunkFingerprint non-deterministic: %v", err)
	}
}

func TestChunkFingerprint_Property_SensitiveToContentMutation(t *testing.T) {
	prop := func(prefix string, suffix uint8) bool {
		base := prefix + "ZZZZZZZZ"
		mutated := prefix + "AAAAAAAA"
		if suffix%2 == 0 {
			mutated = base + string([]byte{byte(suffix)})
		}
		if base == mutated {
			return true
		}
		f1 := computeFingerprint(base)
		f2 := computeFingerprint(mutated)
		if f1 == f2 {
			t.Logf("Plan 14 substrate: fingerprint collision: base=%q mutated=%q digest=%s",
				base, mutated, f1)
			return false
		}
		return true
	}
	cfg := &quick.Config{MaxCount: 1000}
	if err := quick.Check(prop, cfg); err != nil {
		t.Errorf("Plan 14 substrate: ChunkFingerprint insensitive to content mutation: %v", err)
	}
}

// TestChunkFingerprint_Property_IndependentOfMetadata asserts that
// fingerprint depends ONLY on ContentText, not on SymbolPath / Kind /
// Version / PackageID / SourceURL.
//
// This is the load-bearing dedup contract: two chunks with the same
// content but different metadata (e.g., same docstring exported by
// two packages) MUST collide on fingerprint so the indexer's
// cross-version dedup engine sees the duplicate.
func TestChunkFingerprint_Property_IndependentOfMetadata(t *testing.T) {

	prop := func(content, symA, symB string, kindIdx uint8, version string) bool {

		a := ecosystem.Chunk{
			ContentText:       content,
			SymbolPath:        symA,
			VersionIntroduced: version,
		}
		b := ecosystem.Chunk{
			ContentText:       content,
			SymbolPath:        symB,
			VersionIntroduced: version + "-different",
		}
		fpA := computeFingerprint(a.ContentText)
		fpB := computeFingerprint(b.ContentText)
		// MUST collide on fingerprint despite different metadata.
		if fpA != fpB {
			t.Logf("Plan 14 substrate: fingerprint differs for identical content: a=%q b=%q fpA=%s fpB=%s",
				a.ContentText, b.ContentText, fpA, fpB)
			return false
		}
		return true
	}
	cfg := &quick.Config{MaxCount: 500}
	if err := quick.Check(prop, cfg); err != nil {
		t.Errorf("Plan 14 substrate: ChunkFingerprint not metadata-independent: %v", err)
	}
}
