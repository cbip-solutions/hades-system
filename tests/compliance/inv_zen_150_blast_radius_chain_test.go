package compliance

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/audit/chain"
	"github.com/cbip-solutions/hades-system/internal/store"
)

type chainStoreFromStore struct{ s *store.Store }

func (c *chainStoreFromStore) GetChainTip(ctx context.Context) (string, error) {
	hash, err := c.s.GetChainTip()
	if err == store.ErrNoChainTip {
		return "", chain.ErrNoChainTip
	}
	return hash, err
}

func (c *chainStoreFromStore) GetEventByID(ctx context.Context, id string) (*chain.EventRow, error) {
	r, err := c.s.GetEventByID(id)
	if err == store.ErrEventNotFound {
		return nil, chain.ErrEventNotFound
	}
	if err != nil {
		return nil, err
	}
	out := &chain.EventRow{
		ID:          r.ID,
		ProjectID:   r.ProjectID,
		Type:        r.Type,
		PayloadJSON: r.PayloadJSON,
		EmittedAt:   r.EmittedAt,
		PrevHash:    r.PrevHash,
		RecordHash:  r.RecordHash,
		PartitionID: r.PartitionID,
	}
	if r.TesseraLeafID.Valid {
		s := r.TesseraLeafID.String
		out.TesseraLeafID = &s
	}
	return out, nil
}

func (c *chainStoreFromStore) GetByEventID(ctx context.Context, id string) (*chain.EventRow, error) {
	return c.GetEventByID(ctx, id)
}

func (c *chainStoreFromStore) UpdateChainColumns(ctx context.Context, id, prevHash, recordHash, partitionID string) error {
	return c.s.UpdateChainColumns(id, prevHash, recordHash, partitionID)
}

func (c *chainStoreFromStore) UpdateTesseraLeafID(ctx context.Context, id, leafID string) error {
	return c.s.UpdateTesseraLeafID(id, leafID)
}

func (c *chainStoreFromStore) InsertPartitionSeal(ctx context.Context, seal chain.SealRecord) error {
	return c.s.InsertPartitionSeal(store.AuditPartitionSealRow{
		PartitionID:            seal.PartitionID,
		SealedAt:               seal.SealedAt,
		FinalRecordHash:        seal.FinalRecordHash,
		TesseraSealLeafID:      seal.TesseraSealLeafID,
		DaemonWitnessSignature: seal.DaemonWitnessSignature,
	})
}

func (c *chainStoreFromStore) GetPartitionSeal(ctx context.Context, partitionID string) (*chain.SealRecord, error) {
	r, err := c.s.GetPartitionSeal(partitionID)
	if err == store.ErrPartitionSealNotFound {
		return nil, chain.ErrPartitionSealNotFound
	}
	if err != nil {
		return nil, err
	}
	return &chain.SealRecord{
		PartitionID:            r.PartitionID,
		SealedAt:               r.SealedAt,
		FinalRecordHash:        r.FinalRecordHash,
		TesseraSealLeafID:      r.TesseraSealLeafID,
		DaemonWitnessSignature: r.DaemonWitnessSignature,
	}, nil
}

func (c *chainStoreFromStore) ListPartitions(ctx context.Context) ([]chain.PartitionStat, error) {
	stats, err := c.s.ListPartitions()
	if err != nil {
		return nil, err
	}
	out := make([]chain.PartitionStat, len(stats))
	for i, s := range stats {
		out[i] = chain.PartitionStat{
			PartitionID:     s.PartitionID,
			FirstID:         s.FirstID,
			LastID:          s.LastID,
			EventCount:      s.EventCount,
			FinalRecordHash: s.FinalRecordHash,
		}
	}
	return out, nil
}

func (c *chainStoreFromStore) ListEventsForPartition(ctx context.Context, partitionID string) ([]chain.EventRow, error) {
	rows, err := c.s.ListEventsForPartition(partitionID)
	if err != nil {
		return nil, err
	}
	out := make([]chain.EventRow, len(rows))
	for i, r := range rows {
		out[i] = chain.EventRow{
			ID:          r.ID,
			ProjectID:   r.ProjectID,
			Type:        r.Type,
			PayloadJSON: r.PayloadJSON,
			EmittedAt:   r.EmittedAt,
			PrevHash:    r.PrevHash,
			RecordHash:  r.RecordHash,
			PartitionID: r.PartitionID,
		}
		if r.TesseraLeafID.Valid {
			s := r.TesseraLeafID.String
			out[i].TesseraLeafID = &s
		}
	}
	return out, nil
}

func (c *chainStoreFromStore) BackfillScan(ctx context.Context, afterRowID int64, limit int) ([]chain.BackfillCursorRow, error) {
	rows, err := c.s.BackfillScan(afterRowID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]chain.BackfillCursorRow, len(rows))
	for i, r := range rows {
		out[i] = chain.BackfillCursorRow{RowID: r.RowID}
		out[i].ID = r.ID
		out[i].ProjectID = r.ProjectID
		out[i].Type = r.Type
		out[i].PayloadJSON = r.PayloadJSON
		out[i].EmittedAt = r.EmittedAt
		out[i].PrevHash = r.PrevHash
		out[i].RecordHash = r.RecordHash
		out[i].PartitionID = r.PartitionID
		if r.TesseraLeafID.Valid {
			s := r.TesseraLeafID.String
			out[i].TesseraLeafID = &s
		}
	}
	return out, nil
}

func openCompliantStoreInvZen150(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "inv150.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func seedTwoProjects(t *testing.T, s *store.Store) {
	t.Helper()

	_, err := s.DB().Exec(
		`INSERT INTO audit_events_raw (id, project_id, type, payload_json, emitted_at)
		 VALUES ('evt-A1', 'projA', 't', '{}', 1700000000),
		        ('evt-A2', 'projA', 't', '{}', 1700000001)`,
	)
	if err != nil {
		t.Fatalf("seed A: %v", err)
	}

	_, err = s.DB().Exec(
		`INSERT INTO audit_events_raw (id, project_id, type, payload_json, emitted_at)
		 VALUES ('evt-B1', 'projB', 't', '{}', 1700000010),
		        ('evt-B2', 'projB', 't', '{}', 1700000011)`,
	)
	if err != nil {
		t.Fatalf("seed B: %v", err)
	}

	for _, id := range []string{"evt-A1", "evt-A2", "evt-B1", "evt-B2"} {
		row, _ := s.GetEventByID(id)
		tip, err := s.GetChainTip()
		if err == store.ErrNoChainTip {
			tip = ""
		}
		h, _ := chain.Compute(tip, row.Type, []byte(row.PayloadJSON), row.EmittedAt)
		if err := s.UpdateChainColumns(id, tip, h, chain.PartitionID(row.EmittedAt)); err != nil {
			t.Fatalf("UpdateChainColumns %s: %v", id, err)
		}
	}
}

func TestInvZen150ChainBlastRadiusPerProject(t *testing.T) {
	s := openCompliantStoreInvZen150(t)
	seedTwoProjects(t, s)

	if _, err := s.DB().Exec(`DROP TRIGGER IF EXISTS audit_events_raw_no_update_chain_hashes`); err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	if _, err := s.DB().Exec(`UPDATE audit_events_raw SET record_hash = 'tamperaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' WHERE id = 'evt-A2'`); err != nil {
		t.Fatalf("tamper UPDATE: %v", err)
	}

	if _, err := s.DB().Exec(`CREATE TRIGGER audit_events_raw_no_update_chain_hashes
		BEFORE UPDATE OF prev_hash, record_hash ON audit_events_raw
		WHEN OLD.prev_hash != '' OR OLD.record_hash != ''
		BEGIN
		    SELECT RAISE(FAIL, 'audit_events_raw is append-only (Plan 9 chain integrity inv-zen-143); chain hashes cannot be rewritten once computed');
		END`); err != nil {
		t.Fatalf("recreate trigger: %v", err)
	}

	cs := &chainStoreFromStore{s: s}

	reportA, err := chain.Walk(context.Background(), cs, "projA")
	if err != nil {
		t.Fatalf("Walk A: %v", err)
	}
	if len(reportA.Tampered) == 0 {
		t.Errorf("project A walk did not detect tamper")
	}

	reportB, err := chain.Walk(context.Background(), cs, "projB")
	if err != nil {
		t.Fatalf("Walk B: %v", err)
	}
	if len(reportB.Tampered) != 0 {
		t.Errorf("project B walk falsely reported tamper: %+v (inv-zen-150 violated)", reportB.Tampered)
	}
	if reportB.EventsWalked != 2 {
		t.Errorf("project B EventsWalked = %d, want 2", reportB.EventsWalked)
	}
}
