package store

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"
)

func openMigrated(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestInsertBypassAuditWithBody(t *testing.T) {
	s := openMigrated(t)
	id, err := s.InsertBypassAuditFull(BypassAuditFullRow{
		TS: time.Now().Unix(), RequestHash: "rh", ResponseHash: "rs",
		Success: true, LatencyMs: 120, TierUsed: "in-house", ConversationID: "conv-1",
	})
	if err != nil || id <= 0 {
		t.Fatalf("InsertBypassAuditFull: id=%d err=%v", id, err)
	}
	if err := s.InsertBypassAuditBody(id, []byte("ENC-REQ"), []byte("ENC-RES"), 1); err != nil {
		t.Fatalf("InsertBypassAuditBody: %v", err)
	}
	req, res, ver, err := s.GetBypassAuditBody(id)
	if err != nil {
		t.Fatalf("GetBypassAuditBody: %v", err)
	}
	if !bytes.Equal(req, []byte("ENC-REQ")) || !bytes.Equal(res, []byte("ENC-RES")) || ver != 1 {
		t.Errorf("body mismatch req=%q res=%q ver=%d", req, res, ver)
	}
}

func TestInsertBypassAuditMetadataOnly(t *testing.T) {

	s := openMigrated(t)
	id, err := s.InsertBypassAuditFull(BypassAuditFullRow{
		TS: time.Now().Unix(), RequestHash: "rh", ResponseHash: "rs",
		Success: true, TierUsed: "payg", ConversationID: "conv-payg",
	})
	if err != nil || id <= 0 {
		t.Fatalf("InsertBypassAuditFull: %v", err)
	}
	if _, _, _, err := s.GetBypassAuditBody(id); err == nil {
		t.Error("expected sql.ErrNoRows when no body inserted (inv-zen-054)")
	}
}

func TestListBypassAuditByConversation(t *testing.T) {
	s := openMigrated(t)
	for i := 0; i < 3; i++ {
		_, _ = s.InsertBypassAuditFull(BypassAuditFullRow{
			TS: time.Now().Unix(), RequestHash: "h", ResponseHash: "h",
			Success: true, TierUsed: "in-house", ConversationID: "conv-A",
		})
	}
	_, _ = s.InsertBypassAuditFull(BypassAuditFullRow{
		TS: time.Now().Unix(), RequestHash: "h", ResponseHash: "h",
		Success: true, TierUsed: "in-house", ConversationID: "conv-B",
	})
	rows, err := s.ListBypassAuditByConversation("conv-A")
	if err != nil || len(rows) != 3 {
		t.Errorf("len=%d err=%v, want 3 rows", len(rows), err)
	}
}

func TestPinRoundtrip(t *testing.T) {
	s := openMigrated(t)
	now := time.Now().Unix()
	if err := s.UpsertBypassAuditPin("conv-X", now, "operator review"); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	pins, err := s.ListBypassAuditPins()
	if err != nil || len(pins) != 1 || pins[0].ConversationID != "conv-X" {
		t.Errorf("pins=%+v err=%v", pins, err)
	}
	pinned, err := s.IsConversationPinned("conv-X")
	if err != nil || !pinned {
		t.Errorf("IsConversationPinned = %v err=%v", pinned, err)
	}
	if err := s.DeleteBypassAuditPin("conv-X"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	pinned, _ = s.IsConversationPinned("conv-X")
	if pinned {
		t.Error("expected unpinned after delete")
	}
}

func TestUpsertPinReplacesReason(t *testing.T) {
	s := openMigrated(t)
	now := time.Now().Unix()
	_ = s.UpsertBypassAuditPin("conv-Y", now, "first")
	_ = s.UpsertBypassAuditPin("conv-Y", now+10, "second")
	pins, _ := s.ListBypassAuditPins()
	if len(pins) != 1 {
		t.Fatalf("expected 1 row after upsert collision, got %d", len(pins))
	}
	if pins[0].Reason != "second" || pins[0].PinnedAt != now+10 {
		t.Errorf("upsert did not replace: %+v", pins[0])
	}
}

func TestPurgeRespectsAgeAndPinAndCascade(t *testing.T) {
	s := openMigrated(t)
	old := time.Now().Add(-40 * 24 * time.Hour).Unix()
	idOld, _ := s.InsertBypassAuditFull(BypassAuditFullRow{
		TS: old, RequestHash: "h", ResponseHash: "h",
		Success: true, TierUsed: "in-house", ConversationID: "conv-old",
	})
	_ = s.InsertBypassAuditBody(idOld, []byte("ENC"), []byte("ENC"), 1)

	idPin, _ := s.InsertBypassAuditFull(BypassAuditFullRow{
		TS: old, RequestHash: "h", ResponseHash: "h",
		Success: true, TierUsed: "in-house", ConversationID: "conv-pinned",
	})
	_ = s.InsertBypassAuditBody(idPin, []byte("ENC"), []byte("ENC"), 1)

	cutoff := time.Now().Add(-30 * 24 * time.Hour).Unix()
	purged, err := s.PurgeBypassAuditOlderThan(cutoff, []string{"conv-pinned"})
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if purged != 1 {
		t.Errorf("purged = %d, want 1", purged)
	}
	var n int
	_ = s.DB().QueryRow(`SELECT COUNT(*) FROM bypass_audit_bodies WHERE audit_id = ?`, idOld).Scan(&n)
	if n != 0 {
		t.Errorf("body cascade failed: %d rows still present", n)
	}
	_ = s.DB().QueryRow(`SELECT COUNT(*) FROM bypass_audit_bodies WHERE audit_id = ?`, idPin).Scan(&n)
	if n != 1 {
		t.Errorf("pinned body wrongly purged: %d", n)
	}
}

func TestCountAndSizeOlderThan(t *testing.T) {
	s := openMigrated(t)
	old := time.Now().Add(-40 * 24 * time.Hour).Unix()
	id1, _ := s.InsertBypassAuditFull(BypassAuditFullRow{
		TS: old, RequestHash: "h", ResponseHash: "h",
		Success: true, TierUsed: "in-house", ConversationID: "conv-a",
	})
	_ = s.InsertBypassAuditBody(id1, []byte("0123456789"), []byte("ABCDE"), 1)
	id2, _ := s.InsertBypassAuditFull(BypassAuditFullRow{
		TS: old, RequestHash: "h", ResponseHash: "h",
		Success: true, TierUsed: "in-house", ConversationID: "conv-b",
	})
	_ = s.InsertBypassAuditBody(id2, []byte("ZZZZ"), []byte("YY"), 1)
	cutoff := time.Now().Add(-30 * 24 * time.Hour).Unix()

	n, err := s.CountBypassAuditOlderThan(cutoff, nil)
	if err != nil || n != 2 {
		t.Errorf("count=%d err=%v", n, err)
	}
	n, err = s.CountBypassAuditOlderThan(cutoff, []string{"conv-a"})
	if err != nil || n != 1 {
		t.Errorf("with exempt count=%d err=%v", n, err)
	}
	size, err := s.SizeBypassAuditBodiesOlderThan(cutoff, nil)
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	if size != int64(10+5+4+2) {
		t.Errorf("size=%d, want %d", size, 10+5+4+2)
	}
	size2, _ := s.SizeBypassAuditBodiesOlderThan(cutoff, []string{"conv-a"})
	if size2 != int64(4+2) {
		t.Errorf("size with exempt=%d, want 6", size2)
	}
}

func TestPinnedConversationIDs(t *testing.T) {
	s := openMigrated(t)
	now := time.Now().Unix()
	_ = s.UpsertBypassAuditPin("a", now, "")
	_ = s.UpsertBypassAuditPin("b", now, "")
	ids, err := s.PinnedConversationIDs()
	if err != nil {
		t.Fatalf("PinnedConversationIDs: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("ids=%v", ids)
	}
}
