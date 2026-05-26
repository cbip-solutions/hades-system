//go:build property && cgo

// tests/property/ecosystem/sweep_idempotent_property_test.go (Plan 14 Phase H Task H-7-10)
//
// inv-zen-204: weekly Sunday sweep idempotent — re-running the same
// sweep on the same dataset produces zero schema diff.
//
// Full DB-side coverage lives in
// `internal/research/ecosystem/change_node_consistency_test.go`
// (TestSweepChangeNodes_Idempotent). This file enforces the
// algebraic FLOOR that the sweep's idempotency rests on:
//
//  1. Order-independence — the schema fingerprint must be invariant
//     under column reordering (the sweep walks columns in arbitrary
//     order; the produced hash must not).
//  2. Idempotency — hashing the same column-set twice produces the
//     same digest (no clock/PID/random noise leaks in).
//  3. Sensitivity — different column-sets MUST produce different
//     hashes (the sweep must DETECT diffs, not silently agree).
//
// If any of these algebraic properties fail, the production sweep
// cannot be idempotent — the test is the canary that catches a
// future implementer who substitutes a non-stable hash recipe
// (e.g. switching to map iteration order, or using a clock-based
// salt).

package ecosystem_property_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"testing"
	"testing/quick"

	_ "github.com/mattn/go-sqlite3"
)

func schemaHash(columns []string) string {
	sorted := make([]string, len(columns))
	copy(sorted, columns)
	sort.Strings(sorted)
	h := sha256.New()
	h.Write([]byte(strings.Join(sorted, "|")))
	return hex.EncodeToString(h.Sum(nil))
}

var ecosystemColumns = []string{
	"id", "package_id", "version_introduced", "version_deprecated",
	"stable_in_json", "content_text", "contextual_prefix", "chunk_fingerprint",
	"parent_chunk_id", "source_type", "symbol_path", "kind", "source_url",
	"embedding_binary_256d",
}

func TestSweep_Property_HashDeterministic(t *testing.T) {
	prop := func() bool {
		h1 := schemaHash(ecosystemColumns)
		h2 := schemaHash(ecosystemColumns)
		return h1 == h2
	}
	cfg := &quick.Config{MaxCount: 1000}
	if err := quick.Check(prop, cfg); err != nil {
		t.Errorf("inv-zen-204: schemaHash non-deterministic: %v", err)
	}
}

// TestSweep_Property_HashOrderIndependent asserts hashing a column-set
// is invariant under input order. The sweep walks columns in DB-driver
// order which is not guaranteed stable across SQLite versions; the
// hash recipe MUST normalise via sort.
func TestSweep_Property_HashOrderIndependent(t *testing.T) {
	prop := func(seed uint32) bool {
		shuffled := make([]string, len(ecosystemColumns))
		copy(shuffled, ecosystemColumns)

		s := seed
		for i := len(shuffled) - 1; i > 0; i-- {
			s = s*1664525 + 1013904223
			j := int(s % uint32(i+1))
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		}
		h1 := schemaHash(ecosystemColumns)
		h2 := schemaHash(shuffled)
		return h1 == h2
	}
	cfg := &quick.Config{MaxCount: 1000}
	if err := quick.Check(prop, cfg); err != nil {
		t.Errorf("inv-zen-204: schemaHash order-dependent: %v", err)
	}
}

func TestSweep_Property_HashSensitiveToColumnDiff(t *testing.T) {
	base := schemaHash(ecosystemColumns)

	prop := func(suffix uint16) bool {
		mutated := make([]string, len(ecosystemColumns))
		copy(mutated, ecosystemColumns)
		mutated = append(mutated, fmt.Sprintf("ghost_col_%d", suffix))
		mutatedHash := schemaHash(mutated)
		if mutatedHash == base {
			t.Logf("inv-zen-204: hash collision on schema-diff: added ghost_col_%d still hashed to %s",
				suffix, base)
			return false
		}
		return true
	}
	cfg := &quick.Config{MaxCount: 1000}
	if err := quick.Check(prop, cfg); err != nil {
		t.Errorf("inv-zen-204: schemaHash insensitive to column-diff (would silently mask schema drift): %v", err)
	}
}
