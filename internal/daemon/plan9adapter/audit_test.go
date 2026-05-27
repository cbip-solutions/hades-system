// SPDX-License-Identifier: MIT
package plan9adapter

import (
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/litestream"
	"github.com/cbip-solutions/hades-system/internal/audit/recovery"
	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/internal/daemon/auditadapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/redact"
	"github.com/cbip-solutions/hades-system/internal/store"
)

func TestAuditAdapterHistoryPreservesTextLeafIDs(t *testing.T) {
	fix := newAuditAdapterFixture(t)
	seedAuditEvent(t, fix.store, "evt-a", "proj-a", "audit.alpha", `{"ok":true}`, 1770000000)
	if _, err := fix.chain.OnEmitRaw(context.Background(), "evt-a", "proj-a", "audit.alpha", []byte(`{"ok":true}`), 1770000000); err != nil {
		t.Fatalf("OnEmitRaw: %v", err)
	}
	if err := fix.chain.UpdateTesseraLeafID(context.Background(), "evt-a", "proj-a:leaf-0001"); err != nil {
		t.Fatalf("UpdateTesseraLeafID: %v", err)
	}

	rows, err := fix.adapter.History(context.Background(), handlers.HistoryFilterP9{ProjectID: "proj-a", Limit: 10})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("History rows = %d, want 1", len(rows))
	}
	if rows[0].TesseraLeafID == nil || *rows[0].TesseraLeafID != "proj-a:leaf-0001" {
		t.Fatalf("TesseraLeafID = %v, want text leaf id", rows[0].TesseraLeafID)
	}
	if rows[0].PrevHash != "" || rows[0].RecordHash == "" || rows[0].PartitionID == "" {
		t.Fatalf("History did not expose chain columns: %+v", rows[0])
	}
}

func TestAuditAdapterVerifyChainUsesRealChainRows(t *testing.T) {
	fix := newAuditAdapterFixture(t)
	seedAuditEvent(t, fix.store, "evt-a", "proj-a", "audit.alpha", `{"n":1}`, 1770000000)
	seedAuditEvent(t, fix.store, "evt-b", "proj-a", "audit.beta", `{"n":2}`, 1770000010)
	if _, err := fix.chain.OnEmitRaw(context.Background(), "evt-a", "proj-a", "audit.alpha", []byte(`{"n":1}`), 1770000000); err != nil {
		t.Fatalf("OnEmitRaw evt-a: %v", err)
	}
	if _, err := fix.chain.OnEmitRaw(context.Background(), "evt-b", "proj-a", "audit.beta", []byte(`{"n":2}`), 1770000010); err != nil {
		t.Fatalf("OnEmitRaw evt-b: %v", err)
	}

	res, err := fix.adapter.VerifyChain(context.Background(), "proj-a", 0)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if res.ProjectID != "proj-a" || res.RecordsValid != 2 || len(res.TamperedRecords) != 0 {
		t.Fatalf("VerifyChain = %+v, want two clean records", res)
	}
	if res.VerifiedAtUnix != 1770000100 {
		t.Fatalf("VerifiedAtUnix = %d, want fixture clock", res.VerifiedAtUnix)
	}
}

func TestAuditAdapterPartitionSealsExposeStringIDs(t *testing.T) {
	fix := newAuditAdapterFixture(t)
	seedAuditEvent(t, fix.store, "evt-a", "proj-a", "audit.alpha", `{"n":1}`, 1770000000)
	if _, err := fix.chain.OnEmitRaw(context.Background(), "evt-a", "proj-a", "audit.alpha", []byte(`{"n":1}`), 1770000000); err != nil {
		t.Fatalf("OnEmitRaw: %v", err)
	}
	parts, err := fix.store.ListPartitions()
	if err != nil {
		t.Fatalf("ListPartitions: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("partitions = %d, want 1", len(parts))
	}
	if err := fix.store.InsertPartitionSeal(store.AuditPartitionSealRow{
		PartitionID:            parts[0].PartitionID,
		SealedAt:               1770000050000,
		FinalRecordHash:        parts[0].FinalRecordHash,
		TesseraSealLeafID:      "proj-a:seal-leaf-1",
		DaemonWitnessSignature: "sig-1",
	}); err != nil {
		t.Fatalf("InsertPartitionSeal: %v", err)
	}

	rows, err := fix.adapter.PartitionSeals(context.Background(), "proj-a")
	if err != nil {
		t.Fatalf("PartitionSeals: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("seals = %d, want 1", len(rows))
	}
	got := rows[0]
	if got.FirstRecordID != "evt-a" || got.LastRecordID != "evt-a" || got.TesseraSealLeafID != "proj-a:seal-leaf-1" {
		t.Fatalf("seal IDs not preserved as strings: %+v", got)
	}
}

func TestAuditAdapterWitnessPubkeyAndConfigureS3UseRealSubstrates(t *testing.T) {
	fix := newAuditAdapterFixture(t)
	const accessKeyFixture = "synthetic-access-key-0001"
	pub, err := fix.adapter.WitnessPubkey(context.Background())
	if err != nil {
		t.Fatalf("WitnessPubkey: %v", err)
	}
	if !strings.Contains(pub.PubkeyPEM, "BEGIN PUBLIC KEY") || len(pub.Fingerprint) != 16 {
		t.Fatalf("WitnessPubkey = %+v", pub)
	}

	err = fix.adapter.ConfigureS3(context.Background(), "proj-a", handlers.S3CredentialsP9{
		Endpoint:  "https://s3.example.test",
		Bucket:    "zen-swarm-audit-proj-a",
		Prefix:    "audit/",
		AccessKey: accessKeyFixture,
		SecretKey: "01234567890123456789",
		Region:    "eu-west-1",
	})
	if err != nil {
		t.Fatalf("ConfigureS3: %v", err)
	}
	if fix.creds.projectID != "proj-a" || fix.creds.saved.Region != "eu-west-1" || fix.creds.saved.Endpoint != "https://s3.example.test" {
		t.Fatalf("saved creds = %+v project=%q", fix.creds.saved, fix.creds.projectID)
	}
	if string(fix.creds.saved.AccessKeyID.Reveal()) != accessKeyFixture {
		t.Fatal("access key did not flow into redacted credential store")
	}
}

func TestAuditAdapterCheckpointAppendsToTesseraCheckpointLog(t *testing.T) {
	fix := newAuditAdapterFixture(t)

	res, err := fix.adapter.Checkpoint(context.Background(), "operator boundary review", "max-scope")
	if err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}
	if !strings.HasPrefix(res.CheckpointID, "manual:") || len(res.TesseraSTH) != 64 || res.AnchoredAt != 1770000100 {
		t.Fatalf("Checkpoint result = %+v", res)
	}
	var size uint64
	var latestErr error
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		_, size, latestErr = fix.tessera.Checkpoint().Latest(context.Background())
		if latestErr == nil && size > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if size == 0 {
		t.Fatalf("checkpoint log size = 0, want appended SignedSTH; last err=%v", latestErr)
	}
}

func TestAuditAdapterColdArchiveListReadsSealMetadata(t *testing.T) {
	fix := newAuditAdapterFixture(t)
	if err := fix.store.InsertPartitionSeal(store.AuditPartitionSealRow{
		PartitionID:            "2026_02",
		SealedAt:               1770000050000,
		FinalRecordHash:        strings.Repeat("a", 64),
		TesseraSealLeafID:      "proj-a:seal-leaf-1",
		DaemonWitnessSignature: "sig-1",
		ColdArchiveURL:         sql.NullString{String: "s3://zen-swarm-audit-proj-a/cold-archive/2026_02.tar.gz", Valid: true},
		ColdArchiveContentHash: sql.NullString{String: "sha256-cold", Valid: true},
	}); err != nil {
		t.Fatalf("InsertPartitionSeal: %v", err)
	}

	rows, err := fix.adapter.ColdArchiveList(context.Background(), "proj-a")
	if err != nil {
		t.Fatalf("ColdArchiveList: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("cold archive rows = %d, want 1", len(rows))
	}
	if rows[0].PartitionID != "2026_02" || rows[0].ContentHash != "sha256-cold" || rows[0].ArchivedAt != 1770000050 {
		t.Fatalf("ColdArchiveList row = %+v", rows[0])
	}
}

func TestAuditAdapterRecoverPlansFromRealStateAndRefusesUnwiredExecution(t *testing.T) {
	fix := newAuditAdapterFixture(t)
	seedAuditEvent(t, fix.store, "evt-a", "proj-a", "audit.alpha", `{"n":1}`, 1770000000)
	if _, err := fix.chain.OnEmitRaw(context.Background(), "evt-a", "proj-a", "audit.alpha", []byte(`{"n":1}`), 1770000000); err != nil {
		t.Fatalf("OnEmitRaw: %v", err)
	}

	plan, result, err := fix.adapter.Recover(context.Background(), "proj-a", 1770000000, false)
	if err != nil {
		t.Fatalf("Recover plan: %v", err)
	}
	if plan.ProjectID != "proj-a" || plan.VerifyStepCount != 1 || plan.LitestreamSizeBytes == 0 || plan.EstimatedDurationS == 0 {
		t.Fatalf("Recover plan = %+v", plan)
	}
	if result.Recovered {
		t.Fatalf("Recover confirm=false result = %+v, want zero-value", result)
	}

	_, _, err = fix.adapter.Recover(context.Background(), "proj-a", 1770000000, true)
	if err == nil || !strings.Contains(err.Error(), "recovery execution dependencies not configured") {
		t.Fatalf("Recover confirm=true err = %v, want explicit unwired execution error", err)
	}
}

func TestAuditAdapterRecoverConfirmUsesInjectedExecutor(t *testing.T) {
	fix := newAuditAdapterFixture(t)
	exec := &recordingRecoveryExecutor{
		res: &recovery.RestoreResult{Promoted: true, RecordsRestored: 7, PartitionsRestored: 2},
	}
	fix.adapter.recovery = exec
	seedAuditEvent(t, fix.store, "evt-a", "proj-a", "audit.alpha", `{"n":1}`, 1770000000)
	if _, err := fix.chain.OnEmitRaw(context.Background(), "evt-a", "proj-a", "audit.alpha", []byte(`{"n":1}`), 1770000000); err != nil {
		t.Fatalf("OnEmitRaw: %v", err)
	}

	plan, result, err := fix.adapter.Recover(context.Background(), "proj-a", 1770000000, true)
	if err != nil {
		t.Fatalf("Recover confirm=true: %v", err)
	}
	if plan.ProjectID != "proj-a" {
		t.Fatalf("plan project = %q, want proj-a", plan.ProjectID)
	}
	if !result.Recovered || result.RecordsRestored != 7 || result.PartitionsRestored != 2 {
		t.Fatalf("Recover result = %+v, want promoted executor result", result)
	}
	if exec.projectID != "proj-a" || !exec.fromTS.Equal(time.Unix(1770000000, 0).UTC()) {
		t.Fatalf("executor project/time = %q %s", exec.projectID, exec.fromTS)
	}
	if exec.stdin != "yes\n" {
		t.Fatalf("executor stdin = %q, want yes newline", exec.stdin)
	}
}

func TestAuditAtomicPromoterMovesStagedAuditTreeAndBacksUpExisting(t *testing.T) {
	root := t.TempDir()
	staging := filepath.Join(root, "staging")
	prodAudit := filepath.Join(root, "projects", "proj-a", "audit")
	stagedAudit := filepath.Join(staging, "projects", "proj-a", "audit")
	mustWriteFile(t, filepath.Join(prodAudit, "audit.db"), "old-db")
	mustWriteFile(t, filepath.Join(prodAudit, "audit.db-wal"), "old-wal")
	mustWriteFile(t, filepath.Join(prodAudit, "tessera", "old.tile"), "old-tessera")
	mustWriteFile(t, filepath.Join(stagedAudit, "audit.db"), "new-db")
	mustWriteFile(t, filepath.Join(stagedAudit, "audit.db-wal"), "new-wal")
	mustWriteFile(t, filepath.Join(stagedAudit, "tessera", "new.tile"), "new-tessera")

	err := auditAtomicPromoter{dataRoot: root}.Promote(staging, "proj-a")
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}
	assertFileContent(t, filepath.Join(prodAudit, "audit.db"), "new-db")
	assertFileContent(t, filepath.Join(prodAudit, "audit.db-wal"), "new-wal")
	assertFileContent(t, filepath.Join(prodAudit, "tessera", "new.tile"), "new-tessera")
	if _, err := os.Stat(filepath.Join(stagedAudit, "audit.db")); !os.IsNotExist(err) {
		t.Fatalf("staged audit.db stat err = %v, want not exist", err)
	}

	backups, err := filepath.Glob(filepath.Join(root, "global", "recovery-backups", "proj-a-*"))
	if err != nil {
		t.Fatalf("backup glob: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("backup dirs = %d, want 1: %v", len(backups), backups)
	}
	assertFileContent(t, filepath.Join(backups[0], "audit.db"), "old-db")
	assertFileContent(t, filepath.Join(backups[0], "audit.db-wal"), "old-wal")
	assertFileContent(t, filepath.Join(backups[0], "tessera", "old.tile"), "old-tessera")
}

type auditAdapterFixture struct {
	store   *store.Store
	chain   *auditadapter.Adapter
	tessera *tessera.Manager
	adapter *AuditAdapter
	creds   *recordingS3Store
}

func newAuditAdapterFixture(t *testing.T) auditAdapterFixture {
	t.Helper()
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "state.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := st.Migrate(); err != nil {
		_ = st.Close()
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	mgr, err := tessera.NewManager(ctx, t.TempDir(), tessera.Config{
		BatchMaxAge:         25 * time.Millisecond,
		BatchMaxSize:        1,
		RotationCadenceDays: 365,
	})
	if err != nil {
		t.Fatalf("tessera.NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	creds := &recordingS3Store{}
	chainAdapter := auditadapter.New(st)
	adapter, err := NewAuditAdapter(AuditAdapterDeps{
		Store:       st,
		Chain:       chainAdapter,
		Tessera:     mgr,
		S3Store:     creds,
		StagingRoot: filepath.Join(t.TempDir(), "recovery"),
		Now:         func() time.Time { return time.Unix(1770000100, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("NewAuditAdapter: %v", err)
	}
	return auditAdapterFixture{store: st, chain: chainAdapter, tessera: mgr, adapter: adapter, creds: creds}
}

func seedAuditEvent(t *testing.T, st *store.Store, id, projectID, typ, payload string, emittedAt int64) {
	t.Helper()
	_, err := st.DB().Exec(
		`INSERT INTO audit_events_raw (id, project_id, type, payload_json, emitted_at)
		 VALUES (?, ?, ?, ?, ?)`,
		id, projectID, typ, payload, emittedAt,
	)
	if err != nil {
		t.Fatalf("insert audit event %s: %v", id, err)
	}
}

type recordingS3Store struct {
	projectID string
	saved     litestream.S3Credentials
}

func (s *recordingS3Store) Save(_ context.Context, projectID string, creds litestream.S3Credentials) error {
	s.projectID = projectID
	s.saved = litestream.S3Credentials{
		AccessKeyID:     redact.NewSecret(string(creds.AccessKeyID.Reveal())),
		SecretAccessKey: redact.NewSecret(string(creds.SecretAccessKey.Reveal())),
		Region:          creds.Region,
		Endpoint:        creds.Endpoint,
	}
	return nil
}

type recordingRecoveryExecutor struct {
	res       *recovery.RestoreResult
	err       error
	projectID string
	fromTS    time.Time
	stdin     string
}

func (e *recordingRecoveryExecutor) Restore(_ context.Context, projectID string, fromTS time.Time, stdin io.Reader, _ io.Writer) (*recovery.RestoreResult, error) {
	body, _ := io.ReadAll(stdin)
	e.projectID = projectID
	e.fromTS = fromTS
	e.stdin = string(body)
	return e.res, e.err
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, string(got), want)
	}
}
