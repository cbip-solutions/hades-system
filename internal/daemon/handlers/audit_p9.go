// SPDX-License-Identifier: MIT
// Package handlers — audit_p9.go (Plan 9 Phase H Task H-1).
//
// 10 NEW operator-facing audit endpoints surfacing the Phase A-C audit
// substrate (tessera tile-log + chain integrity + Litestream + recovery)
// over /v1/audit-chain/*. inv-zen-150 + inv-zen-031: handlers consume the
// AuditCtxP9 interface and never import internal/audit/* directly.
//
//	POST /v1/audit-chain/verify-chain          — walk chain + return tamper events
//	GET  /v1/audit-chain/history               — Plan 5 eventlog with chain proofs
//	POST /v1/audit-chain/recover               — two-phase: plan-only OR plan+execute
//	GET  /v1/audit-chain/partition-seals       — per-project seal list
//	POST /v1/audit-chain/checkpoint            — capa-firewall manual checkpoint
//	GET  /v1/audit-chain/cold-archive/list     — list S3 partitions
//	POST /v1/audit-chain/cold-archive/restore  — restore one partition from S3
//	POST /v1/audit-chain/witness/rotate        — daemon-global witness key rotation
//	GET  /v1/audit-chain/witness/pubkey        — daemon-global witness pubkey + meta
//	POST /v1/audit-chain/configure-s3          — per-project S3 credential setup
//
// Auth ACL (per inv-zen-146): verify-chain / history / recover /
// partition-seals / cold-archive list+restore / configure-s3 are
// project-scoped (caller's effective-project set must include the named
// project — wrapper auth.ProjectScopedMiddleware enforces in Phase H-6).
// checkpoint / witness/* are operator-global (peer-cred only via
// auth.PeerCredOnly — inherited from Plan 7 boundary).
//
// Graceful degradation (Plan 2 pattern): any nil AuditCtxP9 passed to a
// constructor returns an http.HandlerFunc that immediately responds with
// HTTP 503 {"error":"feature not configured","code":"plan9_audit_unavailable"}.
// Phase H-10 wires *daemon.Server to satisfy AuditCtxP9 once the Phase
// A-C adapter is available; during development the 503 makes intent
// explicit.
//
// Boundary invariants:
//
//	inv-zen-031: handler never imports internal/store directly.
//	inv-zen-150: handler never imports internal/audit/{tessera,chain,
//	             litestream,recovery} directly; all calls go via AuditCtxP9.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
)

type VerifyResultP9 struct {
	ProjectID       string             `json:"project_id"`
	RecordsValid    int64              `json:"records_valid"`
	PartitionSeals  int                `json:"partition_seals"`
	WitnessChecks   int                `json:"witness_checks"`
	TamperedRecords []TamperedRecordP9 `json:"tampered_records,omitempty"`
	VerifiedAtUnix  int64              `json:"verified_at_unix"`
}

type TamperedRecordP9 struct {
	RecordID int64  `json:"record_id"`
	Reason   string `json:"reason"`
}

type HistoryFilterP9 struct {
	ProjectID  string `json:"project_id"`
	TypeFilter string `json:"filter"`
	SinceUnix  int64  `json:"since"`
	Limit      int    `json:"limit"`
}

type HistoryEntryP9 struct {
	ID            string `json:"id"`
	ProjectID     string `json:"project_id"`
	Type          string `json:"type"`
	PayloadJSON   string `json:"payload_json"`
	EmittedAt     int64  `json:"emitted_at"`
	PrevHash      string `json:"prev_hash,omitempty"`
	RecordHash    string `json:"record_hash,omitempty"`
	TesseraLeafID *int64 `json:"tessera_leaf_id,omitempty"`
	PartitionID   string `json:"partition_id,omitempty"`
}

type PartitionSealP9 struct {
	PartitionID       string `json:"partition_id"`
	FirstRecordID     int64  `json:"first_record_id"`
	LastRecordID      int64  `json:"last_record_id"`
	FinalRecordHash   string `json:"final_record_hash"`
	TesseraSealLeafID int64  `json:"tessera_seal_leaf_id"`
	DaemonWitnessSig  string `json:"daemon_witness_sig"`
	SealedAtUnix      int64  `json:"sealed_at_unix"`
}

type RecoverPlanP9 struct {
	ProjectID           string `json:"project_id"`
	LitestreamSizeBytes int64  `json:"litestream_size_bytes"`
	ColdArchivePartCnt  int    `json:"cold_archive_partition_count"`
	VerifyStepCount     int64  `json:"verify_step_count"`
	EstimatedDurationS  int    `json:"estimated_duration_seconds"`
}

type RecoverResultP9 struct {
	Recovered          bool  `json:"recovered"`
	RecordsRestored    int64 `json:"records_restored"`
	PartitionsRestored int   `json:"partitions_restored"`
	DurationSeconds    int   `json:"duration_seconds"`
}

type CheckpointResultP9 struct {
	CheckpointID string `json:"checkpoint_id"`
	TesseraSTH   string `json:"tessera_sth"`
	AnchoredAt   int64  `json:"anchored_at_unix"`
}

type ColdArchiveEntryP9 struct {
	PartitionID string `json:"partition_id"`
	SizeBytes   int64  `json:"size_bytes"`
	ArchivedAt  int64  `json:"archived_at_unix"`
	ContentHash string `json:"content_hash"`
}

type RestoreResultP9 struct {
	Restored    bool  `json:"restored"`
	BytesPulled int64 `json:"bytes_pulled"`
	DurationSec int   `json:"duration_seconds"`
}

type RotateResultP9 struct {
	NewKeyFingerprint string `json:"new_key_fingerprint"`
	OldKeyFingerprint string `json:"old_key_fingerprint"`
	RotatedAt         int64  `json:"rotated_at_unix"`
}

type PubkeyEntryP9 struct {
	PubkeyPEM     string `json:"pubkey_pem"`
	Fingerprint   string `json:"fingerprint"`
	CreatedAt     int64  `json:"created_at_unix"`
	RotationCount int    `json:"rotation_count"`
}

type S3CredentialsP9 struct {
	Endpoint  string `json:"endpoint"`
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Region    string `json:"region"`
}

type AuditCtxP9 interface {
	VerifyChain(ctx context.Context, projectID string, sinceTs int64) (VerifyResultP9, error)
	History(ctx context.Context, filter HistoryFilterP9) ([]HistoryEntryP9, error)
	PartitionSeals(ctx context.Context, projectID string) ([]PartitionSealP9, error)
	Recover(ctx context.Context, projectID string, fromTs int64, confirm bool) (RecoverPlanP9, RecoverResultP9, error)
	Checkpoint(ctx context.Context, reason, doctrine string) (CheckpointResultP9, error)
	ColdArchiveList(ctx context.Context, projectID string) ([]ColdArchiveEntryP9, error)
	ColdArchiveRestore(ctx context.Context, partitionID, projectID string) (RestoreResultP9, error)
	WitnessRotate(ctx context.Context, reason string) (RotateResultP9, error)
	WitnessPubkey(ctx context.Context) (PubkeyEntryP9, error)
	ConfigureS3(ctx context.Context, projectID string, creds S3CredentialsP9) error
}

func auditP9Unavailable(w http.ResponseWriter) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{
		"error": "feature not configured",
		"code":  "plan9_audit_unavailable",
	})
}

func AuditP9VerifyChain(s AuditCtxP9) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			auditP9Unavailable(w)
			return
		}
		defer r.Body.Close()
		var req struct {
			ProjectID string `json:"project_id"`
			SinceTs   int64  `json:"since_ts"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if req.ProjectID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project_id required"})
			return
		}
		res, err := s.VerifyChain(r.Context(), req.ProjectID, req.SinceTs)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
	}
}

func AuditP9History(s AuditCtxP9) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			auditP9Unavailable(w)
			return
		}
		filter := HistoryFilterP9{
			ProjectID:  r.URL.Query().Get("project_id"),
			TypeFilter: r.URL.Query().Get("filter"),
			Limit:      100,
		}
		if v := r.URL.Query().Get("since"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
				filter.SinceUnix = n
			}
		}
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
				filter.Limit = n
			}
		}
		rows, err := s.History(r.Context(), filter)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if rows == nil {
			rows = []HistoryEntryP9{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": rows, "count": len(rows)})
	}
}

func AuditP9PartitionSeals(s AuditCtxP9) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			auditP9Unavailable(w)
			return
		}
		projectID := r.URL.Query().Get("project_id")
		if projectID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project_id required"})
			return
		}
		rows, err := s.PartitionSeals(r.Context(), projectID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if rows == nil {
			rows = []PartitionSealP9{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": rows, "count": len(rows)})
	}
}

func AuditP9Recover(s AuditCtxP9) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			auditP9Unavailable(w)
			return
		}
		defer r.Body.Close()
		var req struct {
			ProjectID string `json:"project_id"`
			FromTs    int64  `json:"from_ts"`
			Confirm   bool   `json:"confirm"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if req.ProjectID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project_id required"})
			return
		}
		if req.FromTs <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "from_ts must be > 0"})
			return
		}
		plan, result, err := s.Recover(r.Context(), req.ProjectID, req.FromTs, req.Confirm)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		out := map[string]any{"plan": plan}
		if req.Confirm {
			out["result"] = result
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func AuditP9Checkpoint(s AuditCtxP9) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			auditP9Unavailable(w)
			return
		}
		defer r.Body.Close()
		var req struct {
			Reason   string `json:"reason"`
			Doctrine string `json:"doctrine"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if req.Reason == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "reason required"})
			return
		}
		res, err := s.Checkpoint(r.Context(), req.Reason, req.Doctrine)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
	}
}

func AuditP9ColdArchiveList(s AuditCtxP9) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			auditP9Unavailable(w)
			return
		}
		projectID := r.URL.Query().Get("project_id")
		if projectID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project_id required"})
			return
		}
		rows, err := s.ColdArchiveList(r.Context(), projectID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if rows == nil {
			rows = []ColdArchiveEntryP9{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": rows, "count": len(rows)})
	}
}

func AuditP9ColdArchiveRestore(s AuditCtxP9) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			auditP9Unavailable(w)
			return
		}
		defer r.Body.Close()
		var req struct {
			PartitionID string `json:"partition_id"`
			ProjectID   string `json:"project_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if req.PartitionID == "" || req.ProjectID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "partition_id and project_id required"})
			return
		}
		res, err := s.ColdArchiveRestore(r.Context(), req.PartitionID, req.ProjectID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
	}
}

func AuditP9WitnessRotate(s AuditCtxP9) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			auditP9Unavailable(w)
			return
		}
		defer r.Body.Close()
		var req struct {
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if req.Reason == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "reason required"})
			return
		}
		res, err := s.WitnessRotate(r.Context(), req.Reason)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
	}
}

func AuditP9WitnessPubkey(s AuditCtxP9) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			auditP9Unavailable(w)
			return
		}
		res, err := s.WitnessPubkey(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
	}
}

func AuditP9ConfigureS3(s AuditCtxP9) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			auditP9Unavailable(w)
			return
		}
		defer r.Body.Close()
		var req struct {
			ProjectID   string          `json:"project_id"`
			Credentials S3CredentialsP9 `json:"credentials"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if req.ProjectID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project_id required"})
			return
		}
		if req.Credentials.Bucket == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "credentials.bucket required"})
			return
		}
		if err := s.ConfigureS3(r.Context(), req.ProjectID, req.Credentials); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
