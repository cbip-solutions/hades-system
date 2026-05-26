package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func startAuditP9TestServer(t *testing.T, route string, handler func(w http.ResponseWriter, r *http.Request)) (*Client, *httptest.Server) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(route, handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return NewWithBaseURL(srv.URL), srv
}

func TestAuditVerifyChain_OK(t *testing.T) {
	c, _ := startAuditP9TestServer(t, "POST /v1/audit-chain/verify-chain",
		func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "internal-platform-x") {
				t.Errorf("body: %s", string(body))
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(AuditVerifyResp{
				ProjectID:    "internal-platform-x",
				RecordsValid: 100,
			})
		})
	res, err := c.AuditVerifyChain(context.Background(), "internal-platform-x", 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.RecordsValid != 100 {
		t.Errorf("records_valid: %d", res.RecordsValid)
	}
}

func TestAuditHistory_QueryParams(t *testing.T) {
	c, _ := startAuditP9TestServer(t, "GET /v1/audit-chain/history",
		func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if q.Get("project_id") != "p" || q.Get("filter") != "audit." {
				t.Errorf("query: %v", q)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"items": []AuditHistoryEntry{{ID: "r1", Type: "audit.tamper_detected"}},
				"count": 1,
			})
		})
	rows, err := c.AuditHistory(context.Background(), AuditHistoryFilter{
		ProjectID: "p", Filter: "audit.", Limit: 10,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("rows: %d", len(rows))
	}
}

func TestAuditRecover_PlanOnly(t *testing.T) {
	c, _ := startAuditP9TestServer(t, "POST /v1/audit-chain/recover",
		func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"plan": AuditRecoverPlan{ProjectID: "p", LitestreamSizeBytes: 1000},
			})
		})
	plan, result, err := c.AuditRecover(context.Background(), "p", 100, false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if plan.LitestreamSizeBytes != 1000 {
		t.Errorf("plan size: %d", plan.LitestreamSizeBytes)
	}
	if result != nil {
		t.Error("result must be nil when confirm=false")
	}
}

func TestAuditRecover_ConfirmReturnsResult(t *testing.T) {
	c, _ := startAuditP9TestServer(t, "POST /v1/audit-chain/recover",
		func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"plan":   AuditRecoverPlan{ProjectID: "p"},
				"result": AuditRecoverResult{Recovered: true, RecordsRestored: 100},
			})
		})
	_, result, err := c.AuditRecover(context.Background(), "p", 100, true)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if result == nil || !result.Recovered {
		t.Errorf("result: %+v", result)
	}
}

func TestAuditPartitionSeals(t *testing.T) {
	c, _ := startAuditP9TestServer(t, "GET /v1/audit-chain/partition-seals",
		func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"items": []AuditPartitionSeal{{PartitionID: "2026_05"}},
				"count": 1,
			})
		})
	rows, err := c.AuditPartitionSeals(context.Background(), "p")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rows) != 1 || rows[0].PartitionID != "2026_05" {
		t.Errorf("rows: %+v", rows)
	}
}

func TestAuditCheckpoint(t *testing.T) {
	c, _ := startAuditP9TestServer(t, "POST /v1/audit-chain/checkpoint",
		func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(AuditCheckpointResp{CheckpointID: "ck1"})
		})
	res, err := c.AuditCheckpoint(context.Background(), "manual", "max-scope")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.CheckpointID != "ck1" {
		t.Errorf("ckid: %q", res.CheckpointID)
	}
}

func TestAuditColdArchiveList(t *testing.T) {
	c, _ := startAuditP9TestServer(t, "GET /v1/audit-chain/cold-archive/list",
		func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"items": []AuditColdArchiveEntry{{PartitionID: "2026_04"}},
				"count": 1,
			})
		})
	rows, err := c.AuditColdArchiveList(context.Background(), "p")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("rows: %d", len(rows))
	}
}

func TestAuditColdArchiveRestore(t *testing.T) {
	c, _ := startAuditP9TestServer(t, "POST /v1/audit-chain/cold-archive/restore",
		func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(AuditRestoreResult{Restored: true})
		})
	res, err := c.AuditColdArchiveRestore(context.Background(), "2026_04", "p")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.Restored {
		t.Error("expected restored=true")
	}
}

func TestAuditWitnessRotate(t *testing.T) {
	c, _ := startAuditP9TestServer(t, "POST /v1/audit-chain/witness/rotate",
		func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(AuditRotateResult{NewKeyFingerprint: "new"})
		})
	res, err := c.AuditWitnessRotate(context.Background(), "scheduled")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.NewKeyFingerprint != "new" {
		t.Errorf("fp: %q", res.NewKeyFingerprint)
	}
}

func TestAuditWitnessPubkey(t *testing.T) {
	c, _ := startAuditP9TestServer(t, "GET /v1/audit-chain/witness/pubkey",
		func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(AuditWitnessPubkey{Fingerprint: "abc"})
		})
	res, err := c.AuditWitnessPubkey(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Fingerprint != "abc" {
		t.Errorf("fp: %q", res.Fingerprint)
	}
}

func TestAuditConfigureS3(t *testing.T) {
	c, _ := startAuditP9TestServer(t, "POST /v1/audit-chain/configure-s3",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
	err := c.AuditConfigureS3(context.Background(), "p", AuditS3Credentials{Bucket: "b"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestAuditVerifyChain_HTTPError(t *testing.T) {
	c, _ := startAuditP9TestServer(t, "POST /v1/audit-chain/verify-chain",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"corrupt"}`))
		})
	_, err := c.AuditVerifyChain(context.Background(), "p", 0)
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 500 {
		t.Errorf("err shape: %v", err)
	}
}

func TestAuditHistory_SinceAndLimit(t *testing.T) {
	c, _ := startAuditP9TestServer(t, "GET /v1/audit-chain/history",
		func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if q.Get("since") != "1000" || q.Get("limit") != "50" {
				t.Errorf("query params: %v", q)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"items": []AuditHistoryEntry{},
				"count": 0,
			})
		})
	rows, err := c.AuditHistory(context.Background(), AuditHistoryFilter{Since: 1000, Limit: 50})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
}

func TestAuditPartitionSeals_QueryParam(t *testing.T) {
	c, _ := startAuditP9TestServer(t, "GET /v1/audit-chain/partition-seals",
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("project_id") != "internal-platform-x" {
				t.Errorf("missing project_id, query: %v", r.URL.Query())
			}
			json.NewEncoder(w).Encode(map[string]any{
				"items": []AuditPartitionSeal{},
				"count": 0,
			})
		})
	rows, err := c.AuditPartitionSeals(context.Background(), "internal-platform-x")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rows == nil {
		t.Error("expected non-nil slice")
	}
}

func TestAuditConfigureS3_BodyShape(t *testing.T) {
	c, _ := startAuditP9TestServer(t, "POST /v1/audit-chain/configure-s3",
		func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			s := string(body)
			if !strings.Contains(s, "my-bucket") || !strings.Contains(s, "project_id") {
				t.Errorf("unexpected body: %s", s)
			}
			w.WriteHeader(http.StatusNoContent)
		})
	err := c.AuditConfigureS3(context.Background(), "proj", AuditS3Credentials{
		Bucket: "my-bucket", Region: "us-east-1",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestAuditRecover_HTTPError(t *testing.T) {
	c, _ := startAuditP9TestServer(t, "POST /v1/audit-chain/recover",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"from_ts must be > 0"}`))
		})
	_, _, err := c.AuditRecover(context.Background(), "p", 0, false)
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 400 {
		t.Errorf("err shape: %v", err)
	}
}

func TestAuditPartitionSeals_HTTPError(t *testing.T) {
	c, _ := startAuditP9TestServer(t, "GET /v1/audit-chain/partition-seals",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"db error"}`))
		})
	_, err := c.AuditPartitionSeals(context.Background(), "p")
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 500 {
		t.Errorf("err shape: %v", err)
	}
}

func TestAuditCheckpoint_HTTPError(t *testing.T) {
	c, _ := startAuditP9TestServer(t, "POST /v1/audit-chain/checkpoint",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"reason required"}`))
		})
	_, err := c.AuditCheckpoint(context.Background(), "", "")
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 400 {
		t.Errorf("err shape: %v", err)
	}
}

func TestAuditColdArchiveList_HTTPError(t *testing.T) {
	c, _ := startAuditP9TestServer(t, "GET /v1/audit-chain/cold-archive/list",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"s3 unavailable"}`))
		})
	_, err := c.AuditColdArchiveList(context.Background(), "p")
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 500 {
		t.Errorf("err shape: %v", err)
	}
}

func TestAuditColdArchiveRestore_HTTPError(t *testing.T) {
	c, _ := startAuditP9TestServer(t, "POST /v1/audit-chain/cold-archive/restore",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"partition_id required"}`))
		})
	_, err := c.AuditColdArchiveRestore(context.Background(), "", "p")
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 400 {
		t.Errorf("err shape: %v", err)
	}
}

func TestAuditWitnessRotate_HTTPError(t *testing.T) {
	c, _ := startAuditP9TestServer(t, "POST /v1/audit-chain/witness/rotate",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"reason required"}`))
		})
	_, err := c.AuditWitnessRotate(context.Background(), "")
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 400 {
		t.Errorf("err shape: %v", err)
	}
}

func TestAuditWitnessPubkey_HTTPError(t *testing.T) {
	c, _ := startAuditP9TestServer(t, "GET /v1/audit-chain/witness/pubkey",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error":"feature not configured","code":"plan9_audit_unavailable"}`))
		})
	_, err := c.AuditWitnessPubkey(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 503 {
		t.Errorf("err shape: %v", err)
	}
}

func TestAuditHistory_NilItemsNormalized(t *testing.T) {
	c, _ := startAuditP9TestServer(t, "GET /v1/audit-chain/history",
		func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"items":null,"count":0}`))
		})
	rows, err := c.AuditHistory(context.Background(), AuditHistoryFilter{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rows == nil {
		t.Error("nil items must be normalized to empty slice")
	}
}

func TestAuditPartitionSeals_NilItemsNormalized(t *testing.T) {
	c, _ := startAuditP9TestServer(t, "GET /v1/audit-chain/partition-seals",
		func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"items":null,"count":0}`))
		})
	rows, err := c.AuditPartitionSeals(context.Background(), "p")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rows == nil {
		t.Error("nil items must be normalized to empty slice")
	}
}

func TestAuditColdArchiveList_NilItemsNormalized(t *testing.T) {
	c, _ := startAuditP9TestServer(t, "GET /v1/audit-chain/cold-archive/list",
		func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"items":null,"count":0}`))
		})
	rows, err := c.AuditColdArchiveList(context.Background(), "p")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rows == nil {
		t.Error("nil items must be normalized to empty slice")
	}
}
