package compliance

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/store"
)

const (
	expectedErrorPrefix = "audit_events_raw is append-only"
	expectedInvCitation = "inv-zen-143"
)

func openCompliantStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "compliance.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func seedTwoChainedEvents(t *testing.T, s *store.Store) {
	t.Helper()
	_, err := s.DB().Exec(
		`INSERT INTO audit_events_raw (id, project_id, type, payload_json, emitted_at)
		 VALUES ('evt-A', 'p', 't', '{}', 1700000000),
		        ('evt-B', 'p', 't', '{}', 1700000001)`,
	)
	if err != nil {
		t.Fatalf("seed insert: %v", err)
	}
	if err := s.UpdateChainColumns("evt-A", "", "hashA", "2023_11"); err != nil {
		t.Fatalf("UpdateChainColumns evt-A: %v", err)
	}
	if err := s.UpdateChainColumns("evt-B", "hashA", "hashB", "2023_11"); err != nil {
		t.Fatalf("UpdateChainColumns evt-B: %v", err)
	}
}

func TestInvZen143UpdateImmutableColumnsRefused(t *testing.T) {
	s := openCompliantStore(t)
	seedTwoChainedEvents(t, s)

	cases := []struct {
		col string
		sql string
	}{
		{"id", `UPDATE audit_events_raw SET id = 'rogue' WHERE id = 'evt-A'`},
		{"project_id", `UPDATE audit_events_raw SET project_id = 'rogue' WHERE id = 'evt-A'`},
		{"type", `UPDATE audit_events_raw SET type = 'rogue' WHERE id = 'evt-A'`},
		{"payload_json", `UPDATE audit_events_raw SET payload_json = '{"tampered":1}' WHERE id = 'evt-A'`},
		{"emitted_at", `UPDATE audit_events_raw SET emitted_at = 9999999999 WHERE id = 'evt-A'`},
	}
	for _, c := range cases {
		t.Run(c.col, func(t *testing.T) {
			_, err := s.DB().Exec(c.sql)
			if err == nil {
				t.Fatalf("expected error for column %q UPDATE; got nil", c.col)
			}
			msg := err.Error()
			if !strings.Contains(msg, expectedErrorPrefix) {
				t.Errorf("error message %q missing prefix %q", msg, expectedErrorPrefix)
			}
			if !strings.Contains(msg, expectedInvCitation) {
				t.Errorf("error message %q missing inv citation %q", msg, expectedInvCitation)
			}
		})
	}
}

func TestInvZen143UpdateChainHashRewriteRefused(t *testing.T) {
	s := openCompliantStore(t)
	seedTwoChainedEvents(t, s)

	for _, col := range []string{"prev_hash", "record_hash"} {
		t.Run(col, func(t *testing.T) {
			sql := "UPDATE audit_events_raw SET " + col + " = 'tampered' WHERE id = 'evt-A'"
			_, err := s.DB().Exec(sql)
			if err == nil {
				t.Fatalf("expected error rewriting %s; got nil", col)
			}
			msg := err.Error()
			if !strings.Contains(msg, expectedErrorPrefix) {
				t.Errorf("error message %q missing prefix", msg)
			}
		})
	}
}

func TestInvZen143UpdatePartitionRewriteRefused(t *testing.T) {
	s := openCompliantStore(t)
	seedTwoChainedEvents(t, s)

	_, err := s.DB().Exec(`UPDATE audit_events_raw SET partition_id = '1900_01' WHERE id = 'evt-A'`)
	if err == nil {
		t.Fatal("expected error rewriting partition_id; got nil")
	}
	if !strings.Contains(err.Error(), expectedErrorPrefix) {
		t.Errorf("error message %q missing prefix", err.Error())
	}
}

func TestInvZen143UpdateTesseraLeafRewriteRefused(t *testing.T) {
	s := openCompliantStore(t)
	seedTwoChainedEvents(t, s)
	if err := s.UpdateTesseraLeafID("evt-A", "leaf-1"); err != nil {
		t.Fatalf("UpdateTesseraLeafID first set: %v", err)
	}

	_, err := s.DB().Exec(`UPDATE audit_events_raw SET tessera_leaf_id = 'leaf-2' WHERE id = 'evt-A'`)
	if err == nil {
		t.Fatal("expected error rewriting tessera_leaf_id; got nil")
	}
	if !strings.Contains(err.Error(), expectedErrorPrefix) {
		t.Errorf("error message %q missing prefix", err.Error())
	}
}

func TestInvZen143DeleteRefused(t *testing.T) {
	s := openCompliantStore(t)
	seedTwoChainedEvents(t, s)

	_, err := s.DB().Exec(`DELETE FROM audit_events_raw WHERE id = 'evt-A'`)
	if err == nil {
		t.Fatal("expected error on DELETE single row; got nil")
	}
	if !strings.Contains(err.Error(), expectedErrorPrefix) {
		t.Errorf("error message %q missing prefix", err.Error())
	}
	if !strings.Contains(err.Error(), "DELETE is forbidden") {
		t.Errorf("error message %q missing 'DELETE is forbidden' phrase", err.Error())
	}

	_, err = s.DB().Exec(`DELETE FROM audit_events_raw`)
	if err == nil {
		t.Fatal("expected error on bulk DELETE; got nil")
	}
}

func TestInvZen143PreStatePreservedAfterFailedMutation(t *testing.T) {
	// Crucial: the trigger's RAISE(FAIL) MUST roll back the statement.
	// This test verifies that after a failed UPDATE attempt, the row's
	// data is byte-identical to its pre-attempt state.
	s := openCompliantStore(t)
	seedTwoChainedEvents(t, s)

	pre, err := s.GetEventByID("evt-A")
	if err != nil {
		t.Fatalf("GetEventByID pre: %v", err)
	}

	_, _ = s.DB().Exec(`UPDATE audit_events_raw SET payload_json = '{"tampered":true}' WHERE id = 'evt-A'`)

	post, err := s.GetEventByID("evt-A")
	if err != nil {
		t.Fatalf("GetEventByID post: %v", err)
	}

	if pre.PayloadJSON != post.PayloadJSON {
		t.Errorf("payload mutated despite refusal: pre=%q post=%q", pre.PayloadJSON, post.PayloadJSON)
	}
	if pre.RecordHash != post.RecordHash {
		t.Errorf("record_hash mutated: pre=%q post=%q", pre.RecordHash, post.RecordHash)
	}
}

func TestInvZen143ChainComputeUpdatePathPermitted(t *testing.T) {
	// The migration 059 triggers MUST permit the legitimate chain compute
	// UPDATE path (from store.UpdateChainColumns called by auditadapter
	// post-INSERT). This test verifies that the REFUSE triggers do NOT
	// fire when chain columns transition from '' → non-empty.
	s := openCompliantStore(t)
	_, err := s.DB().Exec(
		`INSERT INTO audit_events_raw (id, project_id, type, payload_json, emitted_at)
		 VALUES ('evt-permit', 'p', 't', '{}', 1700000000)`,
	)
	if err != nil {
		t.Fatalf("seed insert: %v", err)
	}

	if err := s.UpdateChainColumns("evt-permit", "", "h1", "2023_11"); err != nil {
		t.Fatalf("first UpdateChainColumns refused: %v", err)
	}

	got, _ := s.GetEventByID("evt-permit")
	if got.RecordHash != "h1" || got.PartitionID != "2023_11" {
		t.Errorf("chain compute UPDATE blocked or ineffective: %+v", got)
	}
}
