// SPDX-License-Identifier: MIT
// Package client — audit_p9.go.
//
// Typed wrappers for the 10 audit-chain endpoints declared in
// internal/daemon/handlers/audit_p9.go. Wire types mirror the handler
// declarations; duplication is intentional (client compiles standalone
// without importing internal/daemon — N convention).
//
// POST /v1/audit-chain/verify-chain AuditVerifyChain
// GET /v1/audit-chain/history AuditHistory
// POST /v1/audit-chain/recover AuditRecover (two-phase)
// GET /v1/audit-chain/partition-seals AuditPartitionSeals
// POST /v1/audit-chain/checkpoint AuditCheckpoint
// GET /v1/audit-chain/cold-archive/list AuditColdArchiveList
// POST /v1/audit-chain/cold-archive/restore AuditColdArchiveRestore
// POST /v1/audit-chain/witness/rotate AuditWitnessRotate
// GET /v1/audit-chain/witness/pubkey AuditWitnessPubkey
// POST /v1/audit-chain/configure-s3 AuditConfigureS3
//
// All 10 methods compile against the *Client plumbing in client.go
// (getJSON / postJSON / NewWithBaseURL). No dependency on internal/daemon
// or internal/store.
package client

import (
	"context"
	"net/url"
	"strconv"
)

type AuditVerifyResp struct {
	ProjectID       string                `json:"project_id"`
	RecordsValid    int64                 `json:"records_valid"`
	PartitionSeals  int                   `json:"partition_seals"`
	WitnessChecks   int                   `json:"witness_checks"`
	TamperedRecords []AuditTamperedRecord `json:"tampered_records,omitempty"`
	VerifiedAtUnix  int64                 `json:"verified_at_unix"`
}

type AuditTamperedRecord struct {
	RecordID int64  `json:"record_id"`
	Reason   string `json:"reason"`
}

type AuditHistoryFilter struct {
	ProjectID string
	Filter    string
	Since     int64
	Limit     int
}

type AuditHistoryEntry struct {
	ID            string  `json:"id"`
	ProjectID     string  `json:"project_id"`
	Type          string  `json:"type"`
	PayloadJSON   string  `json:"payload_json"`
	EmittedAt     int64   `json:"emitted_at"`
	PrevHash      string  `json:"prev_hash,omitempty"`
	RecordHash    string  `json:"record_hash,omitempty"`
	TesseraLeafID *string `json:"tessera_leaf_id,omitempty"`
	PartitionID   string  `json:"partition_id,omitempty"`
}

type AuditPartitionSeal struct {
	PartitionID       string `json:"partition_id"`
	FirstRecordID     string `json:"first_record_id"`
	LastRecordID      string `json:"last_record_id"`
	FinalRecordHash   string `json:"final_record_hash"`
	TesseraSealLeafID string `json:"tessera_seal_leaf_id"`
	DaemonWitnessSig  string `json:"daemon_witness_sig"`
	SealedAtUnix      int64  `json:"sealed_at_unix"`
}

type AuditRecoverPlan struct {
	ProjectID           string `json:"project_id"`
	LitestreamSizeBytes int64  `json:"litestream_size_bytes"`
	ColdArchivePartCnt  int    `json:"cold_archive_partition_count"`
	VerifyStepCount     int64  `json:"verify_step_count"`
	EstimatedDurationS  int    `json:"estimated_duration_seconds"`
}

type AuditRecoverResult struct {
	Recovered          bool  `json:"recovered"`
	RecordsRestored    int64 `json:"records_restored"`
	PartitionsRestored int   `json:"partitions_restored"`
	DurationSeconds    int   `json:"duration_seconds"`
}

type AuditCheckpointResp struct {
	CheckpointID string `json:"checkpoint_id"`
	TesseraSTH   string `json:"tessera_sth"`
	AnchoredAt   int64  `json:"anchored_at_unix"`
}

type AuditColdArchiveEntry struct {
	PartitionID string `json:"partition_id"`
	SizeBytes   int64  `json:"size_bytes"`
	ArchivedAt  int64  `json:"archived_at_unix"`
	ContentHash string `json:"content_hash"`
}

type AuditRestoreResult struct {
	Restored    bool  `json:"restored"`
	BytesPulled int64 `json:"bytes_pulled"`
	DurationSec int   `json:"duration_seconds"`
}

type AuditRotateResult struct {
	NewKeyFingerprint string `json:"new_key_fingerprint"`
	OldKeyFingerprint string `json:"old_key_fingerprint"`
	RotatedAt         int64  `json:"rotated_at_unix"`
}

type AuditWitnessPubkey struct {
	PubkeyPEM     string `json:"pubkey_pem"`
	Fingerprint   string `json:"fingerprint"`
	CreatedAt     int64  `json:"created_at_unix"`
	RotationCount int    `json:"rotation_count"`
}

type AuditS3Credentials struct {
	Endpoint  string `json:"endpoint"`
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Region    string `json:"region"`
}

func (c *Client) AuditVerifyChain(ctx context.Context, projectID string, sinceTs int64) (AuditVerifyResp, error) {
	body := map[string]any{"project_id": projectID, "since_ts": sinceTs}
	var out AuditVerifyResp
	if err := c.postJSON(ctx, "/v1/audit-chain/verify-chain", body, &out); err != nil {
		return AuditVerifyResp{}, err
	}
	return out, nil
}

func (c *Client) AuditHistory(ctx context.Context, filter AuditHistoryFilter) ([]AuditHistoryEntry, error) {
	q := url.Values{}
	if filter.ProjectID != "" {
		q.Set("project_id", filter.ProjectID)
	}
	if filter.Filter != "" {
		q.Set("filter", filter.Filter)
	}
	if filter.Since > 0 {
		q.Set("since", strconv.FormatInt(filter.Since, 10))
	}
	if filter.Limit > 0 {
		q.Set("limit", strconv.Itoa(filter.Limit))
	}
	path := "/v1/audit-chain/history"
	if e := q.Encode(); e != "" {
		path += "?" + e
	}
	var out struct {
		Items []AuditHistoryEntry `json:"items"`
		Count int                 `json:"count"`
	}
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	if out.Items == nil {
		out.Items = []AuditHistoryEntry{}
	}
	return out.Items, nil
}

// AuditRecover calls POST /v1/audit-chain/recover (two-phase semantics).
// confirm=false returns a preview plan only; result is nil. confirm=true
// executes the recovery and returns both plan and result. Callers MUST
// check result != nil before dereferencing.
func (c *Client) AuditRecover(ctx context.Context, projectID string, fromTs int64, confirm bool) (AuditRecoverPlan, *AuditRecoverResult, error) {
	body := map[string]any{
		"project_id": projectID,
		"from_ts":    fromTs,
		"confirm":    confirm,
	}
	var out struct {
		Plan   AuditRecoverPlan    `json:"plan"`
		Result *AuditRecoverResult `json:"result,omitempty"`
	}
	if err := c.postJSON(ctx, "/v1/audit-chain/recover", body, &out); err != nil {
		return AuditRecoverPlan{}, nil, err
	}
	return out.Plan, out.Result, nil
}

func (c *Client) AuditPartitionSeals(ctx context.Context, projectID string) ([]AuditPartitionSeal, error) {
	q := url.Values{"project_id": []string{projectID}}
	var out struct {
		Items []AuditPartitionSeal `json:"items"`
		Count int                  `json:"count"`
	}
	if err := c.getJSON(ctx, "/v1/audit-chain/partition-seals?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	if out.Items == nil {
		out.Items = []AuditPartitionSeal{}
	}
	return out.Items, nil
}

func (c *Client) AuditCheckpoint(ctx context.Context, reason, doctrine string) (AuditCheckpointResp, error) {
	body := map[string]any{"reason": reason, "doctrine": doctrine}
	var out AuditCheckpointResp
	if err := c.postJSON(ctx, "/v1/audit-chain/checkpoint", body, &out); err != nil {
		return AuditCheckpointResp{}, err
	}
	return out, nil
}

func (c *Client) AuditColdArchiveList(ctx context.Context, projectID string) ([]AuditColdArchiveEntry, error) {
	q := url.Values{"project_id": []string{projectID}}
	var out struct {
		Items []AuditColdArchiveEntry `json:"items"`
		Count int                     `json:"count"`
	}
	if err := c.getJSON(ctx, "/v1/audit-chain/cold-archive/list?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	if out.Items == nil {
		out.Items = []AuditColdArchiveEntry{}
	}
	return out.Items, nil
}

func (c *Client) AuditColdArchiveRestore(ctx context.Context, partitionID, projectID string) (AuditRestoreResult, error) {
	body := map[string]any{"partition_id": partitionID, "project_id": projectID}
	var out AuditRestoreResult
	if err := c.postJSON(ctx, "/v1/audit-chain/cold-archive/restore", body, &out); err != nil {
		return AuditRestoreResult{}, err
	}
	return out, nil
}

func (c *Client) AuditWitnessRotate(ctx context.Context, reason string) (AuditRotateResult, error) {
	body := map[string]any{"reason": reason}
	var out AuditRotateResult
	if err := c.postJSON(ctx, "/v1/audit-chain/witness/rotate", body, &out); err != nil {
		return AuditRotateResult{}, err
	}
	return out, nil
}

func (c *Client) AuditWitnessPubkey(ctx context.Context) (AuditWitnessPubkey, error) {
	var out AuditWitnessPubkey
	if err := c.getJSON(ctx, "/v1/audit-chain/witness/pubkey", &out); err != nil {
		return AuditWitnessPubkey{}, err
	}
	return out, nil
}

func (c *Client) AuditConfigureS3(ctx context.Context, projectID string, creds AuditS3Credentials) error {
	body := map[string]any{"project_id": projectID, "credentials": creds}
	return c.postJSON(ctx, "/v1/audit-chain/configure-s3", body, nil)
}
