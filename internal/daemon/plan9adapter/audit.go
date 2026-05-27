// SPDX-License-Identifier: MIT
package plan9adapter

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/litestream"
	"github.com/cbip-solutions/hades-system/internal/audit/recovery"
	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/internal/daemon/auditadapter"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/redact"
	"github.com/cbip-solutions/hades-system/internal/store"
)

type auditS3Store interface {
	Save(ctx context.Context, projectID string, creds litestream.S3Credentials) error
}

type auditColdArchiveDownloader interface {
	DownloadColdArchive(ctx context.Context, projectID, partitionID, dst string) error
}

type auditRecoveryExecutor interface {
	Restore(ctx context.Context, projectID string, fromTS time.Time, stdin io.Reader, stdout io.Writer) (*recovery.RestoreResult, error)
}

type AuditAdapterDeps struct {
	Store                 *store.Store
	Chain                 *auditadapter.Adapter
	Tessera               *tessera.Manager
	S3Store               auditS3Store
	ColdArchiveDownloader auditColdArchiveDownloader
	RecoveryExecutor      auditRecoveryExecutor
	StagingRoot           string
	Now                   func() time.Time
}

type AuditAdapter struct {
	store      *store.Store
	chain      *auditadapter.Adapter
	tessera    *tessera.Manager
	seals      *auditadapter.PartitionSealStore
	s3         auditS3Store
	downloader auditColdArchiveDownloader
	recovery   auditRecoveryExecutor
	staging    string
	now        func() time.Time
}

var _ handlers.AuditCtxP9 = (*AuditAdapter)(nil)

func NewAuditAdapter(deps AuditAdapterDeps) (*AuditAdapter, error) {
	if deps.Store == nil {
		return nil, errors.New("plan9adapter: audit Store is required")
	}
	if deps.Chain == nil {
		return nil, errors.New("plan9adapter: audit Chain adapter is required")
	}
	if deps.Tessera == nil {
		return nil, errors.New("plan9adapter: audit Tessera manager is required")
	}
	s3 := deps.S3Store
	if s3 == nil {
		s3 = litestream.NewS3CredentialsStore()
	}
	now := deps.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	staging := deps.StagingRoot
	if staging == "" {
		staging = filepath.Join(filepath.Dir(deps.Store.Path()), "recovery")
	}
	recoveryExec := deps.RecoveryExecutor
	if recoveryExec == nil {
		if credsStore, ok := s3.(*litestream.S3CredentialsStore); ok && deps.ColdArchiveDownloader != nil {
			recoveryExec = newAuditRecoveryExecutor(deps.Store, credsStore, deps.ColdArchiveDownloader, staging)
		}
	}
	return &AuditAdapter{
		store:      deps.Store,
		chain:      deps.Chain,
		tessera:    deps.Tessera,
		seals:      auditadapter.NewPartitionSealStore(deps.Store.DB()),
		s3:         s3,
		downloader: deps.ColdArchiveDownloader,
		recovery:   recoveryExec,
		staging:    staging,
		now:        now,
	}, nil
}

func (a *AuditAdapter) VerifyChain(ctx context.Context, projectID string, sinceTs int64) (handlers.VerifyResultP9, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return handlers.VerifyResultP9{}, errors.New("plan9adapter: audit VerifyChain projectID required")
	}
	projectAdapter, err := a.tessera.ProjectAdapter(ctx, projectID)
	if err != nil {
		return handlers.VerifyResultP9{}, fmt.Errorf("plan9adapter: audit project tessera adapter: %w", err)
	}
	verifier := recovery.NewVerifier(auditChainReader{db: a.store.DB()}, projectAdapter, projectAdapter, a.seals)
	res, err := verifier.VerifyChain(ctx, projectID)
	if err != nil {
		return handlers.VerifyResultP9{}, err
	}
	recordsValid := int64(res.RecordsChecked)
	if res.Clean && sinceTs > 0 {
		recordsValid, err = a.countChainRecords(ctx, projectID, sinceTs)
		if err != nil {
			return handlers.VerifyResultP9{}, err
		}
	}
	out := handlers.VerifyResultP9{
		ProjectID:       projectID,
		RecordsValid:    recordsValid,
		PartitionSeals:  res.PartitionSealsChecked,
		WitnessChecks:   res.PartitionSealsChecked,
		VerifiedAtUnix:  a.now().UTC().Unix(),
		TamperedRecords: nil,
	}
	if !res.Clean {
		out.TamperedRecords = []handlers.TamperedRecordP9{{
			RecordID: res.FirstTamperRecordID,
			Reason:   res.FirstTamperPath.String(),
		}}
	}
	return out, nil
}

func (a *AuditAdapter) History(ctx context.Context, filter handlers.HistoryFilterP9) ([]handlers.HistoryEntryP9, error) {
	limit := normalizeAuditLimit(filter.Limit)
	where := []string{"1=1"}
	args := []any{}
	if projectID := strings.TrimSpace(filter.ProjectID); projectID != "" {
		where = append(where, "project_id = ?")
		args = append(args, projectID)
	}
	if typ := strings.TrimSpace(filter.TypeFilter); typ != "" {
		where = append(where, "type LIKE ?")
		args = append(args, typ+"%")
	}
	if filter.SinceUnix > 0 {
		where = append(where, "emitted_at >= ?")
		args = append(args, filter.SinceUnix)
	}
	args = append(args, limit)
	rows, err := a.store.DB().QueryContext(ctx,
		`SELECT id, project_id, type, payload_json, emitted_at,
		        prev_hash, record_hash, tessera_leaf_id, partition_id
		   FROM audit_events_raw
		  WHERE `+strings.Join(where, " AND ")+`
		  ORDER BY rowid DESC
		  LIMIT ?`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("plan9adapter: audit history: %w", err)
	}
	defer rows.Close()
	out := make([]handlers.HistoryEntryP9, 0, limit)
	for rows.Next() {
		var row handlers.HistoryEntryP9
		var leaf sql.NullString
		if err := rows.Scan(
			&row.ID,
			&row.ProjectID,
			&row.Type,
			&row.PayloadJSON,
			&row.EmittedAt,
			&row.PrevHash,
			&row.RecordHash,
			&leaf,
			&row.PartitionID,
		); err != nil {
			return nil, fmt.Errorf("plan9adapter: audit history scan: %w", err)
		}
		if leaf.Valid {
			v := leaf.String
			row.TesseraLeafID = &v
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("plan9adapter: audit history rows: %w", err)
	}
	return out, nil
}

func (a *AuditAdapter) PartitionSeals(ctx context.Context, projectID string) ([]handlers.PartitionSealP9, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, errors.New("plan9adapter: audit PartitionSeals projectID required")
	}
	rows, err := a.store.DB().QueryContext(ctx,
		`SELECT s.partition_id,
		        COALESCE(p.first_id, ''),
		        COALESCE(p.last_id, ''),
		        s.final_record_hash,
		        s.tessera_seal_leaf_id,
		        s.daemon_witness_signature,
		        s.sealed_at
		   FROM audit_partition_seals s
		   LEFT JOIN audit_events_partitions p ON p.partition_id = s.partition_id
		  ORDER BY s.sealed_at ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("plan9adapter: audit partition seals: %w", err)
	}
	defer rows.Close()
	out := make([]handlers.PartitionSealP9, 0)
	for rows.Next() {
		var sealedAt int64
		var row handlers.PartitionSealP9
		if err := rows.Scan(
			&row.PartitionID,
			&row.FirstRecordID,
			&row.LastRecordID,
			&row.FinalRecordHash,
			&row.TesseraSealLeafID,
			&row.DaemonWitnessSig,
			&sealedAt,
		); err != nil {
			return nil, fmt.Errorf("plan9adapter: audit partition seal scan: %w", err)
		}
		row.SealedAtUnix = unixFromMillisOrSeconds(sealedAt)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("plan9adapter: audit partition seal rows: %w", err)
	}
	return out, nil
}

func (a *AuditAdapter) Recover(ctx context.Context, projectID string, fromTs int64, confirm bool) (handlers.RecoverPlanP9, handlers.RecoverResultP9, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return handlers.RecoverPlanP9{}, handlers.RecoverResultP9{}, errors.New("plan9adapter: audit Recover projectID required")
	}
	if fromTs <= 0 {
		return handlers.RecoverPlanP9{}, handlers.RecoverResultP9{}, errors.New("plan9adapter: audit Recover fromTs must be > 0")
	}
	plan, err := a.recoveryPlan(ctx, projectID)
	if err != nil {
		return handlers.RecoverPlanP9{}, handlers.RecoverResultP9{}, err
	}
	if !confirm {
		return plan, handlers.RecoverResultP9{}, nil
	}
	if a.recovery == nil {
		return plan, handlers.RecoverResultP9{}, errors.New("plan9adapter: recovery execution dependencies not configured")
	}
	start := a.now()
	res, err := a.recovery.Restore(ctx, projectID, time.Unix(fromTs, 0).UTC(), strings.NewReader("yes\n"), io.Discard)
	if err != nil {
		return plan, handlers.RecoverResultP9{}, err
	}
	return plan, handlers.RecoverResultP9{
		Recovered:          res.Promoted,
		RecordsRestored:    int64(res.RecordsRestored),
		PartitionsRestored: res.PartitionsRestored,
		DurationSeconds:    int(a.now().Sub(start).Seconds()),
	}, nil
}

func (a *AuditAdapter) Checkpoint(ctx context.Context, reason, doctrine string) (handlers.CheckpointResultP9, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return handlers.CheckpointResultP9{}, errors.New("plan9adapter: audit Checkpoint reason required")
	}
	doctrine = strings.TrimSpace(doctrine)
	if doctrine == "" {
		doctrine = "max-scope"
	}
	now := a.now().UTC()
	root := sha256.Sum256([]byte(reason + "\x00" + doctrine + "\x00" + now.Format(time.RFC3339Nano)))
	sth := tessera.STH{
		ProjectID: "__manual_checkpoint__",
		Size:      uint64(now.UnixNano()),
		RootHash:  root[:],
		Timestamp: now,
	}
	signed, err := a.tessera.CoSigner().Sign(ctx, sth)
	if err != nil {
		return handlers.CheckpointResultP9{}, fmt.Errorf("plan9adapter: audit checkpoint sign: %w", err)
	}
	if err := a.tessera.Checkpoint().Append(ctx, signed); err != nil {
		return handlers.CheckpointResultP9{}, fmt.Errorf("plan9adapter: audit checkpoint append: %w", err)
	}
	digest := sth.Digest()
	return handlers.CheckpointResultP9{
		CheckpointID: "manual:" + hex.EncodeToString(digest[:8]),
		TesseraSTH:   hex.EncodeToString(digest[:]),
		AnchoredAt:   now.Unix(),
	}, nil
}

func (a *AuditAdapter) ColdArchiveList(ctx context.Context, projectID string) ([]handlers.ColdArchiveEntryP9, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, errors.New("plan9adapter: audit ColdArchiveList projectID required")
	}
	rows, err := a.store.DB().QueryContext(ctx,
		`SELECT partition_id, sealed_at, COALESCE(cold_archive_content_hash, '')
		   FROM audit_partition_seals
		  WHERE COALESCE(cold_archive_url, '') != ''
		     OR COALESCE(cold_archive_content_hash, '') != ''
		  ORDER BY sealed_at ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("plan9adapter: audit cold archive list: %w", err)
	}
	defer rows.Close()
	out := make([]handlers.ColdArchiveEntryP9, 0)
	for rows.Next() {
		var row handlers.ColdArchiveEntryP9
		var sealedAt int64
		if err := rows.Scan(&row.PartitionID, &sealedAt, &row.ContentHash); err != nil {
			return nil, fmt.Errorf("plan9adapter: audit cold archive scan: %w", err)
		}
		row.ArchivedAt = unixFromMillisOrSeconds(sealedAt)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("plan9adapter: audit cold archive rows: %w", err)
	}
	return out, nil
}

func (a *AuditAdapter) ColdArchiveRestore(ctx context.Context, partitionID, projectID string) (handlers.RestoreResultP9, error) {
	partitionID = strings.TrimSpace(partitionID)
	projectID = strings.TrimSpace(projectID)
	if partitionID == "" || projectID == "" {
		return handlers.RestoreResultP9{}, errors.New("plan9adapter: audit ColdArchiveRestore partitionID and projectID required")
	}
	if a.downloader == nil {
		return handlers.RestoreResultP9{}, errors.New("plan9adapter: cold archive downloader not configured")
	}
	meta, err := a.seals.ColdArchiveMetaFor(ctx, projectID, partitionID)
	if err != nil {
		return handlers.RestoreResultP9{}, err
	}
	if meta.ContentHash == "" {
		return handlers.RestoreResultP9{}, fmt.Errorf("plan9adapter: cold archive content hash missing for partition %s", partitionID)
	}
	if err := os.MkdirAll(a.staging, 0o700); err != nil {
		return handlers.RestoreResultP9{}, fmt.Errorf("plan9adapter: mkdir recovery staging: %w", err)
	}
	start := a.now()
	dst := filepath.Join(a.staging, projectID+"-"+partitionID+".tar.gz")
	if err := a.downloader.DownloadColdArchive(ctx, projectID, partitionID, dst); err != nil {
		return handlers.RestoreResultP9{}, err
	}
	size, hash, err := fileSizeAndSHA256(dst)
	if err != nil {
		return handlers.RestoreResultP9{}, err
	}
	if hash != meta.ContentHash {
		return handlers.RestoreResultP9{}, fmt.Errorf("plan9adapter: cold archive hash mismatch partition %s: got %s want %s", partitionID, hash, meta.ContentHash)
	}
	return handlers.RestoreResultP9{
		Restored:    true,
		BytesPulled: size,
		DurationSec: int(a.now().Sub(start).Seconds()),
	}, nil
}

func (a *AuditAdapter) WitnessRotate(ctx context.Context, reason string) (handlers.RotateResultP9, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return handlers.RotateResultP9{}, errors.New("plan9adapter: audit WitnessRotate reason required")
	}
	res, err := tessera.NewRotation(a.tessera.Witness(), a.tessera.Checkpoint()).Rotate(ctx, reason)
	if err != nil {
		return handlers.RotateResultP9{}, err
	}
	return handlers.RotateResultP9{
		NewKeyFingerprint: pubkeyFingerprint(res.NewPubkey),
		OldKeyFingerprint: pubkeyFingerprint(res.OldPubkey),
		RotatedAt:         res.Timestamp.Unix(),
	}, nil
}

func (a *AuditAdapter) WitnessPubkey(ctx context.Context) (handlers.PubkeyEntryP9, error) {
	pemBytes, err := a.tessera.Witness().PubkeyPEM()
	if err != nil {
		return handlers.PubkeyEntryP9{}, err
	}
	createdAt, rotations, err := a.witnessRotationStats(ctx)
	if err != nil {
		return handlers.PubkeyEntryP9{}, err
	}
	return handlers.PubkeyEntryP9{
		PubkeyPEM:     string(pemBytes),
		Fingerprint:   fingerprintBytes(pemBytes),
		CreatedAt:     createdAt,
		RotationCount: rotations,
	}, nil
}

func (a *AuditAdapter) ConfigureS3(ctx context.Context, projectID string, creds handlers.S3CredentialsP9) error {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return errors.New("plan9adapter: audit ConfigureS3 projectID required")
	}
	if strings.TrimSpace(creds.Bucket) == "" {
		return errors.New("plan9adapter: audit ConfigureS3 bucket required")
	}
	if strings.TrimSpace(creds.AccessKey) == "" || strings.TrimSpace(creds.SecretKey) == "" {
		return errors.New("plan9adapter: audit ConfigureS3 access_key and secret_key required")
	}
	region := strings.TrimSpace(creds.Region)
	if region == "" {
		region = "us-east-1"
	}
	stored := litestream.S3Credentials{
		AccessKeyID:     redact.NewSecret(strings.TrimSpace(creds.AccessKey)),
		SecretAccessKey: redact.NewSecret(strings.TrimSpace(creds.SecretKey)),
		Region:          region,
		Endpoint:        strings.TrimSpace(creds.Endpoint),
	}
	defer stored.Wipe()
	if err := a.s3.Save(ctx, projectID, stored); err != nil {
		return fmt.Errorf("plan9adapter: audit ConfigureS3 save: %w", err)
	}
	return nil
}

type auditChainReader struct {
	db *sql.DB
}

func (r auditChainReader) QueryAll(ctx context.Context, projectID string) ([]recovery.ChainRecord, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT rowid, project_id, type, payload_json, emitted_at,
		        prev_hash, record_hash, partition_id, tessera_leaf_id
		   FROM audit_events_raw
		  WHERE project_id = ?
		  ORDER BY rowid ASC`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]recovery.ChainRecord, 0)
	for rows.Next() {
		var row recovery.ChainRecord
		var leaf sql.NullString
		if err := rows.Scan(
			&row.ID,
			&row.ProjectID,
			&row.EventType,
			&row.Payload,
			&row.CreatedAt,
			&row.PrevHash,
			&row.RecordHash,
			&row.PartitionID,
			&leaf,
		); err != nil {
			return nil, err
		}
		if leaf.Valid {
			row.TesseraLeafID = leaf.String
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (a *AuditAdapter) countChainRecords(ctx context.Context, projectID string, sinceTs int64) (int64, error) {
	row := a.store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_events_raw WHERE project_id = ? AND emitted_at >= ?`,
		projectID,
		sinceTs,
	)
	var n int64
	if err := row.Scan(&n); err != nil {
		return 0, fmt.Errorf("plan9adapter: audit count chain records: %w", err)
	}
	return n, nil
}

func (a *AuditAdapter) recoveryPlan(ctx context.Context, projectID string) (handlers.RecoverPlanP9, error) {
	var verifyCount int64
	if err := a.store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_events_raw WHERE project_id = ?`,
		projectID,
	).Scan(&verifyCount); err != nil {
		return handlers.RecoverPlanP9{}, fmt.Errorf("plan9adapter: audit recovery count records: %w", err)
	}
	var archiveCount int
	if err := a.store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_partition_seals
		  WHERE COALESCE(cold_archive_url, '') != ''
		     OR COALESCE(cold_archive_content_hash, '') != ''`,
	).Scan(&archiveCount); err != nil {
		return handlers.RecoverPlanP9{}, fmt.Errorf("plan9adapter: audit recovery count archives: %w", err)
	}
	dbSize := int64(0)
	if info, err := os.Stat(a.store.Path()); err == nil {
		dbSize = info.Size()
	}
	estimated := int(verifyCount/1000) + archiveCount*2 + 1
	return handlers.RecoverPlanP9{
		ProjectID:           projectID,
		LitestreamSizeBytes: dbSize,
		ColdArchivePartCnt:  archiveCount,
		VerifyStepCount:     verifyCount,
		EstimatedDurationS:  estimated,
	}, nil
}

func (a *AuditAdapter) witnessRotationStats(ctx context.Context) (int64, int, error) {
	row := a.store.DB().QueryRowContext(ctx,
		`SELECT COALESCE(MIN(emitted_at), 0), COUNT(*)
		   FROM audit_events_raw
		  WHERE type = 'daemon.witness_rotated'`,
	)
	var createdAt int64
	var rotations int
	if err := row.Scan(&createdAt, &rotations); err != nil {
		return 0, 0, fmt.Errorf("plan9adapter: audit witness stats: %w", err)
	}
	return createdAt, rotations, nil
}

func normalizeAuditLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > 500 {
		return 500
	}
	return limit
}

func unixFromMillisOrSeconds(v int64) int64 {
	if v > 1_000_000_000_000 {
		return v / 1000
	}
	return v
}

func fingerprintBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

func pubkeyFingerprint(pub any) string {
	if pub == nil {
		return ""
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return ""
	}
	return fingerprintBytes(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func fileSizeAndSHA256(path string) (int64, string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return 0, "", fmt.Errorf("plan9adapter: read cold archive: %w", err)
	}
	sum := sha256.Sum256(body)
	return int64(len(body)), hex.EncodeToString(sum[:]), nil
}
