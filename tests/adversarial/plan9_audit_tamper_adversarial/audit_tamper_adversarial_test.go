// go:build adversarial && cgo
//go:build adversarial && cgo
// +build adversarial,cgo

package plan9_audit_tamper_adversarial

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/chain"
	"github.com/cbip-solutions/hades-system/tests/testhelpers/tamperinject"

	_ "github.com/mattn/go-sqlite3"
)

type mattnEventStore struct {
	db *sql.DB
}

func (s *mattnEventStore) ListPartitions(ctx context.Context) ([]chain.PartitionStat, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			partition_id,
			MIN(id),
			MAX(id),
			COUNT(*)
		FROM audit_events_raw
		WHERE partition_id != ''
		GROUP BY partition_id
		ORDER BY partition_id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []chain.PartitionStat
	for rows.Next() {
		var p chain.PartitionStat
		if err := rows.Scan(&p.PartitionID, &p.FirstID, &p.LastID, &p.EventCount); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *mattnEventStore) ListEventsForPartition(ctx context.Context, partitionID string) ([]chain.EventRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, project_id, type, payload_json, emitted_at, prev_hash, record_hash, partition_id
		FROM audit_events_raw
		WHERE partition_id = ?
		ORDER BY id ASC
	`, partitionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []chain.EventRow
	for rows.Next() {
		var e chain.EventRow
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.Type, &e.PayloadJSON, &e.EmittedAt, &e.PrevHash, &e.RecordHash, &e.PartitionID); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *mattnEventStore) GetChainTip(_ context.Context) (string, error) {
	return "", errors.New("mattnEventStore: GetChainTip not implemented (chain.Walk does not call it)")
}

func (s *mattnEventStore) GetEventByID(_ context.Context, _ string) (*chain.EventRow, error) {
	return nil, errors.New("mattnEventStore: GetEventByID not implemented")
}

func (s *mattnEventStore) GetByEventID(_ context.Context, _ string) (*chain.EventRow, error) {
	return nil, errors.New("mattnEventStore: GetByEventID not implemented")
}

func (s *mattnEventStore) UpdateChainColumns(_ context.Context, _, _, _, _ string) error {
	return errors.New("mattnEventStore: UpdateChainColumns not implemented")
}

func (s *mattnEventStore) UpdateTesseraLeafID(_ context.Context, _, _ string) error {
	return errors.New("mattnEventStore: UpdateTesseraLeafID not implemented")
}

func (s *mattnEventStore) InsertPartitionSeal(_ context.Context, _ chain.SealRecord) error {
	return errors.New("mattnEventStore: InsertPartitionSeal not implemented")
}

func (s *mattnEventStore) GetPartitionSeal(_ context.Context, _ string) (*chain.SealRecord, error) {
	return nil, errors.New("mattnEventStore: GetPartitionSeal not implemented")
}

func (s *mattnEventStore) BackfillScan(_ context.Context, _ int64, _ int) ([]chain.BackfillCursorRow, error) {
	return nil, errors.New("mattnEventStore: BackfillScan not implemented")
}

const migrationSQL = `
CREATE TABLE IF NOT EXISTS audit_events_raw (
    id           TEXT    NOT NULL PRIMARY KEY,
    project_id   TEXT    NOT NULL DEFAULT '',
    type         TEXT    NOT NULL,
    payload_json TEXT    NOT NULL DEFAULT '{}',
    emitted_at   INTEGER NOT NULL CHECK (emitted_at > 0),
    prev_hash    TEXT    NOT NULL DEFAULT '',
    record_hash  TEXT    NOT NULL DEFAULT '',
    partition_id TEXT    NOT NULL DEFAULT '',
    tessera_leaf_id TEXT
);

CREATE TRIGGER IF NOT EXISTS audit_events_raw_no_update_immutable
BEFORE UPDATE OF id, project_id, type, payload_json, emitted_at ON audit_events_raw
BEGIN
    SELECT RAISE(FAIL, 'audit_events_raw is append-only (inv-zen-143); immutable columns cannot be modified');
END;

CREATE TRIGGER IF NOT EXISTS audit_events_raw_no_update_chain_hashes
BEFORE UPDATE OF prev_hash, record_hash ON audit_events_raw
WHEN OLD.prev_hash != '' OR OLD.record_hash != ''
BEGIN
    SELECT RAISE(FAIL, 'audit_events_raw is append-only (inv-zen-143); chain hashes cannot be rewritten');
END;

CREATE TRIGGER IF NOT EXISTS audit_events_raw_no_update_partition
BEFORE UPDATE OF partition_id ON audit_events_raw
WHEN OLD.partition_id != ''
BEGIN
    SELECT RAISE(FAIL, 'audit_events_raw is append-only (inv-zen-143); partition_id cannot be rewritten');
END;

CREATE TRIGGER IF NOT EXISTS audit_events_raw_no_update_tessera_leaf
BEFORE UPDATE OF tessera_leaf_id ON audit_events_raw
WHEN OLD.tessera_leaf_id IS NOT NULL
BEGIN
    SELECT RAISE(FAIL, 'audit_events_raw is append-only (inv-zen-143); tessera_leaf_id cannot be rewritten');
END;

CREATE TRIGGER IF NOT EXISTS audit_events_raw_no_delete
BEFORE DELETE ON audit_events_raw
BEGIN
    SELECT RAISE(FAIL, 'audit_events_raw is append-only (inv-zen-143); DELETE is forbidden');
END;
`

func seedChain(t *testing.T, db *sql.DB, projectID string, n int) []string {
	t.Helper()
	out := make([]string, n)
	tip := ""
	now := time.Now().UTC().Unix()
	for i := 0; i < n; i++ {
		eventID := fmt.Sprintf("evt-tamper-%03d", i)
		payload := fmt.Sprintf(`{"i":%d}`, i)
		eventType := "test.event"
		emittedAt := now + int64(i)
		recordHash, err := chain.Compute(tip, eventType, []byte(payload), emittedAt)
		if err != nil {
			t.Fatalf("chain.Compute[%d]: %v", i, err)
		}
		partitionID := chain.PartitionID(emittedAt)
		if _, err := db.Exec(`
			INSERT INTO audit_events_raw
				(id, project_id, type, payload_json, emitted_at, prev_hash, record_hash, partition_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, eventID, projectID, eventType, payload, emittedAt, tip, recordHash, partitionID); err != nil {
			t.Fatalf("INSERT event[%d]: %v", i, err)
		}
		out[i] = eventID
		tip = recordHash
	}
	return out
}

func TestAdversarial_ModifyRecordHashDetected(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "tamper.db")

	db, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=off")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.Exec(migrationSQL); err != nil {
		_ = db.Close()
		t.Fatalf("migrate: %v", err)
	}

	const projectID = "proj-A"
	const N = 5
	eventIDs := seedChain(t, db, projectID, N)

	var preHash string
	if err := db.QueryRowContext(ctx, `SELECT record_hash FROM audit_events_raw WHERE id = ?`, eventIDs[3]).Scan(&preHash); err != nil {
		_ = db.Close()
		t.Fatalf("read pre-tamper record_hash: %v", err)
	}
	if preHash == "" {
		_ = db.Close()
		t.Fatalf("pre-tamper record_hash empty for %q; seeding failed silently", eventIDs[3])
	}

	if err := db.Close(); err != nil {
		t.Fatalf("db.Close pre-tamper: %v", err)
	}

	forged := sha256.Sum256([]byte("forged-by-attacker"))
	if err := tamperinject.ModifyRecordHashRaw(dbPath, eventIDs[3], forged[:]); err != nil {
		t.Fatalf("tamperinject.ModifyRecordHashRaw: %v", err)
	}

	db2, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=off")
	if err != nil {
		t.Fatalf("sql.Open[2]: %v", err)
	}
	t.Cleanup(func() { _ = db2.Close() })

	var postHash []byte
	if err := db2.QueryRowContext(ctx, `SELECT record_hash FROM audit_events_raw WHERE id = ?`, eventIDs[3]).Scan(&postHash); err != nil {
		t.Fatalf("read post-tamper record_hash: %v", err)
	}

	if string(postHash) == preHash {
		t.Fatalf("record_hash unchanged after tamper; tamperinject helper bypass failed (pre=%q post=%q)", preHash, string(postHash))
	}

	es := &mattnEventStore{db: db2}
	report, err := chain.Walk(ctx, es, projectID)
	if err != nil {
		t.Fatalf("chain.Walk: %v", err)
	}
	if len(report.Tampered) == 0 {
		t.Fatalf("WalkReport.Tampered = 0; expected >=1 for forged event %q (pre=%q post=%q)",
			eventIDs[3], preHash, string(postHash))
	}
	found := false
	for _, tr := range report.Tampered {
		if tr.EventID == eventIDs[3] {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("WalkReport.Tampered missing forged event %q; got %+v", eventIDs[3], report.Tampered)
	}

	row4InTamper := false
	for _, tr := range report.Tampered {
		if tr.EventID == eventIDs[4] {
			row4InTamper = true
			break
		}
	}
	if row4InTamper {
		t.Errorf("row[4] erroneously in Tampered; chain.Walk should only flag the directly-forged row")
	}

	gap4 := false
	for _, g := range report.GapsDetected {
		if g.EventID == eventIDs[4] {
			gap4 = true
			break
		}
	}
	if !gap4 {
		t.Errorf("GapsDetected missing chain-linkage break at event %q; got %+v", eventIDs[4], report.GapsDetected)
	}
}
