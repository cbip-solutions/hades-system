// Package compliance — invariant: severity is a frozen 4-tier enum
// enforced at BOTH the SQLite schema layer (CHECK constraint on the
// inbox.severity column) AND the Go domain layer (inbox.ValidSeverity
// + inbox.ParseSeverity).
//
// Spec §1 Q12 B + §7.2 invariant wording:
//
// "Severity column CHECK constraint enforces the 4-tier enum
// {urgent, action-needed, info-immediate, info-digest}; any string
// outside this set MUST be rejected at the SQL layer in addition to
// the Go layer."
//
// This test is the cross-package, boundary-side defense-in-depth witness:
// the in-package coverage in internal/inbox/severity_test.go locks the Go
// surface (ValidSeverity, ParseSeverity, AllSeverities ordering); this
// file exercises the SQL CHECK constraint directly via raw INSERT
// statements so a future refactor that drops the constraint while
// keeping the Go enum surface gets caught at the public surface.
//
// Coverage matrix:
//
// (a) SQL CHECK rejects every probe outside the 4-tier set:
// empty string, capitalized variants ("Urgent", "URGENT"),
// arbitrary strings, alternate-format strings, neighbor labels
// from other severity vocabularies (panic, warn, info, trace),
// and qualified strings ("Severity::Urgent").
// (b) SQL CHECK accepts every canonical tier from
// inbox.AllSeverities() — i.e. the Go-side and SQL-side
// enum sets are byte-identical.
//
// Boundary: this test imports internal/inbox + internal/
// store. internal/inbox does NOT import internal/store; this file
// composes the two only at the test surface to assert defense-in-depth
// across the layers, never producing a one-way dependency in production.
//
// Inv-zen-124 contract.
package compliance

import (
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/inbox"
	"github.com/cbip-solutions/hades-system/internal/store"
)

var _ = severity4TierEnumAnchorReference()

func severity4TierEnumAnchorReference() error {
	return inbox.ErrSeverity4TierAnchor
}

// TestInvZen124SeverityCheckRejectsNonTier asserts the SQLite CHECK
// constraint refuses any severity outside the 4-tier enum. Each probe
// is a real INSERT against an in-memory store with a freshly-migrated
// schema; rejection MUST surface as a non-nil error from Exec.
func TestInvZen124SeverityCheckRejectsNonTier(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	pid := "a" + strings.Repeat("0", 63)
	bad := []string{
		"",
		"Urgent",
		"URGENT",
		"panic",
		"warn",
		"info",
		"trace",
		"Severity::Urgent",
		"action_needed",
		"info_immediate",
		"info_digest",
		"Action-Needed",
		"urgent ",
		" urgent",
		"urgent\n",
		"urgentX",
		"xurgent",
	}
	for _, sv := range bad {
		_, err := s.DB().Exec(
			`INSERT INTO inbox
				(project_id, severity, event_type, content_hash,
				 payload, created_at, created_at_bucket)
			 VALUES (?,?,?,?,?,?,?)`,
			pid, sv, "evt-"+sv, strings.Repeat("a", 64),
			`{}`, int64(1714560000), int64(1714560000/300),
		)
		if err == nil {
			t.Errorf("inv-zen-124 violation: severity %q was accepted "+
				"(SQL CHECK constraint missing or weakened)", sv)
		}
	}

	for _, sv := range bad {
		if inbox.ValidSeverity(sv) {
			t.Errorf("inv-zen-124 Go-layer drift: ValidSeverity(%q) "+
				"returned true; SQL CHECK rejects it", sv)
		}
	}
}

func TestInvZen124SeverityCheckAcceptsAllFour(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	pid := "a" + strings.Repeat("0", 63)
	all := inbox.AllSeverities()
	if len(all) != 4 {
		t.Fatalf("inv-zen-124 cardinality drift: AllSeverities() returned "+
			"%d entries, want exactly 4", len(all))
	}
	for i, sv := range all {

		_, err := s.DB().Exec(
			`INSERT INTO inbox
				(project_id, severity, event_type, content_hash,
				 payload, created_at, created_at_bucket)
			 VALUES (?,?,?,?,?,?,?)`,
			pid, string(sv),
			"evt"+string(rune('a'+i)),
			strings.Repeat(string(rune('a'+i)), 64),
			`{}`,
			int64(1714560000+i*300),
			int64((1714560000+i*300)/300),
		)
		if err != nil {
			t.Errorf("inv-zen-124 false reject: SQL CHECK rejected "+
				"canonical severity %q: %v "+
				"(SQL CHECK list drifted from Go enum)", sv, err)
		}
	}

	for _, sv := range all {
		if !inbox.ValidSeverity(string(sv)) {
			t.Errorf("inv-zen-124 Go-layer drift: ValidSeverity(%q) "+
				"returned false; SQL CHECK accepts it", sv)
		}
	}
}

func TestInvZen124SeverityCanonicalOrder(t *testing.T) {
	all := inbox.AllSeverities()
	want := []inbox.Severity{
		inbox.SeverityUrgent,
		inbox.SeverityActionNeeded,
		inbox.SeverityInfoImmediate,
		inbox.SeverityInfoDigest,
	}
	if len(all) != len(want) {
		t.Fatalf("inv-zen-124 cardinality drift: got %d, want %d",
			len(all), len(want))
	}
	for i := range want {
		if all[i] != want[i] {
			t.Errorf("inv-zen-124 order drift at index %d: got %q, want %q",
				i, all[i], want[i])
		}
	}
}
