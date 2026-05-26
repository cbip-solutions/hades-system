package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

type fakeAuditAdapterP9 struct {
	mu sync.Mutex

	verifyArgs    []verifyChainArgs
	verifyResults []VerifyResultP9
	verifyErr     error

	historyArgs    []HistoryFilterP9
	historyResults [][]HistoryEntryP9
	historyErr     error

	partitionArgs    []string
	partitionResults [][]PartitionSealP9
	partitionErr     error

	recoverArgs    []recoverArgs
	recoverPlans   []RecoverPlanP9
	recoverResults []RecoverResultP9
	recoverErr     error

	checkpointArgs    []checkpointArgs
	checkpointResults []CheckpointResultP9
	checkpointErr     error

	coldListArgs    []string
	coldListResults [][]ColdArchiveEntryP9
	coldListErr     error

	coldRestoreArgs    []coldRestoreArgs
	coldRestoreResults []RestoreResultP9
	coldRestoreErr     error

	witnessRotateArgs    []string
	witnessRotateResults []RotateResultP9
	witnessRotateErr     error

	witnessPubkeyResult PubkeyEntryP9
	witnessPubkeyErr    error

	configureS3Args []configureS3Args
	configureS3Err  error
}

type verifyChainArgs struct {
	ProjectID string
	SinceTs   int64
}

type recoverArgs struct {
	ProjectID string
	FromTs    int64
	Confirm   bool
}

type checkpointArgs struct {
	Reason   string
	Doctrine string
}

type coldRestoreArgs struct {
	PartitionID string
	ProjectID   string
}

type configureS3Args struct {
	ProjectID   string
	Credentials S3CredentialsP9
}

func (f *fakeAuditAdapterP9) VerifyChain(_ context.Context, projectID string, sinceTs int64) (VerifyResultP9, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.verifyArgs = append(f.verifyArgs, verifyChainArgs{projectID, sinceTs})
	if f.verifyErr != nil {
		return VerifyResultP9{}, f.verifyErr
	}
	if len(f.verifyResults) == 0 {
		return VerifyResultP9{}, nil
	}
	r := f.verifyResults[0]
	f.verifyResults = f.verifyResults[1:]
	return r, nil
}

func (f *fakeAuditAdapterP9) History(_ context.Context, filter HistoryFilterP9) ([]HistoryEntryP9, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.historyArgs = append(f.historyArgs, filter)
	if f.historyErr != nil {
		return nil, f.historyErr
	}
	if len(f.historyResults) == 0 {
		return nil, nil
	}
	r := f.historyResults[0]
	f.historyResults = f.historyResults[1:]
	return r, nil
}

func (f *fakeAuditAdapterP9) PartitionSeals(_ context.Context, projectID string) ([]PartitionSealP9, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.partitionArgs = append(f.partitionArgs, projectID)
	if f.partitionErr != nil {
		return nil, f.partitionErr
	}
	if len(f.partitionResults) == 0 {
		return nil, nil
	}
	r := f.partitionResults[0]
	f.partitionResults = f.partitionResults[1:]
	return r, nil
}

func (f *fakeAuditAdapterP9) Recover(_ context.Context, projectID string, fromTs int64, confirm bool) (RecoverPlanP9, RecoverResultP9, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recoverArgs = append(f.recoverArgs, recoverArgs{projectID, fromTs, confirm})
	if f.recoverErr != nil {
		return RecoverPlanP9{}, RecoverResultP9{}, f.recoverErr
	}
	plan := RecoverPlanP9{}
	if len(f.recoverPlans) > 0 {
		plan = f.recoverPlans[0]
		f.recoverPlans = f.recoverPlans[1:]
	}
	res := RecoverResultP9{}
	if len(f.recoverResults) > 0 {
		res = f.recoverResults[0]
		f.recoverResults = f.recoverResults[1:]
	}
	return plan, res, nil
}

func (f *fakeAuditAdapterP9) Checkpoint(_ context.Context, reason, doctrine string) (CheckpointResultP9, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checkpointArgs = append(f.checkpointArgs, checkpointArgs{reason, doctrine})
	if f.checkpointErr != nil {
		return CheckpointResultP9{}, f.checkpointErr
	}
	if len(f.checkpointResults) == 0 {
		return CheckpointResultP9{}, nil
	}
	r := f.checkpointResults[0]
	f.checkpointResults = f.checkpointResults[1:]
	return r, nil
}

func (f *fakeAuditAdapterP9) ColdArchiveList(_ context.Context, projectID string) ([]ColdArchiveEntryP9, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.coldListArgs = append(f.coldListArgs, projectID)
	if f.coldListErr != nil {
		return nil, f.coldListErr
	}
	if len(f.coldListResults) == 0 {
		return nil, nil
	}
	r := f.coldListResults[0]
	f.coldListResults = f.coldListResults[1:]
	return r, nil
}

func (f *fakeAuditAdapterP9) ColdArchiveRestore(_ context.Context, partitionID, projectID string) (RestoreResultP9, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.coldRestoreArgs = append(f.coldRestoreArgs, coldRestoreArgs{partitionID, projectID})
	if f.coldRestoreErr != nil {
		return RestoreResultP9{}, f.coldRestoreErr
	}
	if len(f.coldRestoreResults) == 0 {
		return RestoreResultP9{}, nil
	}
	r := f.coldRestoreResults[0]
	f.coldRestoreResults = f.coldRestoreResults[1:]
	return r, nil
}

func (f *fakeAuditAdapterP9) WitnessRotate(_ context.Context, reason string) (RotateResultP9, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.witnessRotateArgs = append(f.witnessRotateArgs, reason)
	if f.witnessRotateErr != nil {
		return RotateResultP9{}, f.witnessRotateErr
	}
	if len(f.witnessRotateResults) == 0 {
		return RotateResultP9{}, nil
	}
	r := f.witnessRotateResults[0]
	f.witnessRotateResults = f.witnessRotateResults[1:]
	return r, nil
}

func (f *fakeAuditAdapterP9) WitnessPubkey(_ context.Context) (PubkeyEntryP9, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.witnessPubkeyErr != nil {
		return PubkeyEntryP9{}, f.witnessPubkeyErr
	}
	return f.witnessPubkeyResult, nil
}

func (f *fakeAuditAdapterP9) ConfigureS3(_ context.Context, projectID string, creds S3CredentialsP9) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.configureS3Args = append(f.configureS3Args, configureS3Args{projectID, creds})
	return f.configureS3Err
}

func TestAuditP9_VerifyChain_OK(t *testing.T) {
	fake := &fakeAuditAdapterP9{
		verifyResults: []VerifyResultP9{{
			ProjectID:       "internal-platform-x",
			RecordsValid:    847239,
			PartitionSeals:  12,
			WitnessChecks:   12,
			TamperedRecords: nil,
			VerifiedAtUnix:  1714752000,
		}},
	}
	h := AuditP9VerifyChain(fake)
	body := map[string]any{"project_id": "internal-platform-x", "since_ts": 0}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/verify-chain", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp VerifyResultP9
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ProjectID != "internal-platform-x" {
		t.Errorf("project_id: %q", resp.ProjectID)
	}
	if resp.RecordsValid != 847239 {
		t.Errorf("records_valid: %d", resp.RecordsValid)
	}
	if len(fake.verifyArgs) != 1 {
		t.Fatalf("dispatch count: %d", len(fake.verifyArgs))
	}
	if fake.verifyArgs[0].ProjectID != "internal-platform-x" {
		t.Errorf("dispatch project_id: %q", fake.verifyArgs[0].ProjectID)
	}
}

func TestAuditP9_VerifyChain_TamperDetected(t *testing.T) {
	fake := &fakeAuditAdapterP9{
		verifyResults: []VerifyResultP9{{
			ProjectID:       "zen-swarm",
			RecordsValid:    847238,
			TamperedRecords: []TamperedRecordP9{{RecordID: 847239, Reason: "chain hash mismatch"}},
		}},
	}
	h := AuditP9VerifyChain(fake)
	body := map[string]any{"project_id": "zen-swarm", "since_ts": 0}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/verify-chain", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp VerifyResultP9
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.TamperedRecords) != 1 {
		t.Fatalf("tampered_records: %d", len(resp.TamperedRecords))
	}
	if resp.TamperedRecords[0].RecordID != 847239 {
		t.Errorf("record_id: %d", resp.TamperedRecords[0].RecordID)
	}
}

func TestAuditP9_VerifyChain_MissingProjectID(t *testing.T) {
	fake := &fakeAuditAdapterP9{}
	h := AuditP9VerifyChain(fake)
	body := map[string]any{"since_ts": 0}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/verify-chain", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestAuditP9_VerifyChain_InvalidJSON(t *testing.T) {
	fake := &fakeAuditAdapterP9{}
	h := AuditP9VerifyChain(fake)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/verify-chain",
		bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestAuditP9_VerifyChain_AdapterErr(t *testing.T) {
	fake := &fakeAuditAdapterP9{verifyErr: errors.New("storage corrupt")}
	h := AuditP9VerifyChain(fake)
	body := map[string]any{"project_id": "p"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/verify-chain", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestAuditP9_History_FilterTypeAndSince(t *testing.T) {
	fake := &fakeAuditAdapterP9{
		historyResults: [][]HistoryEntryP9{{
			{ID: "r1", ProjectID: "internal-platform-x", Type: "audit.tamper_detected", EmittedAt: 100},
			{ID: "r2", ProjectID: "internal-platform-x", Type: "audit.recovery_completed", EmittedAt: 110},
		}},
	}
	h := AuditP9History(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit-chain/history?project_id=internal-platform-x&filter=audit.&since=50", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Items []HistoryEntryP9 `json:"items"`
		Count int              `json:"count"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Count != 2 {
		t.Fatalf("count: %d", resp.Count)
	}
	if len(fake.historyArgs) != 1 {
		t.Fatalf("dispatch count: %d", len(fake.historyArgs))
	}
	got := fake.historyArgs[0]
	if got.ProjectID != "internal-platform-x" || got.TypeFilter != "audit." || got.SinceUnix != 50 {
		t.Errorf("filter: %+v", got)
	}
}

func TestAuditP9_History_Empty(t *testing.T) {
	fake := &fakeAuditAdapterP9{}
	h := AuditP9History(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit-chain/history?project_id=p", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d", w.Code)
	}
	var resp struct {
		Items []HistoryEntryP9 `json:"items"`
		Count int              `json:"count"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Count != 0 {
		t.Errorf("count: %d", resp.Count)
	}
	if resp.Items == nil {
		t.Error("items should be [] not null")
	}
}

func TestAuditP9_History_AdapterErr(t *testing.T) {
	fake := &fakeAuditAdapterP9{historyErr: errors.New("db gone")}
	h := AuditP9History(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit-chain/history?project_id=p", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestAuditP9_PartitionSeals_OK(t *testing.T) {
	fake := &fakeAuditAdapterP9{
		partitionResults: [][]PartitionSealP9{{
			{PartitionID: "2026_04", FirstRecordID: 1, LastRecordID: 1000, SealedAtUnix: 100},
			{PartitionID: "2026_05", FirstRecordID: 1001, LastRecordID: 2000, SealedAtUnix: 200},
		}},
	}
	h := AuditP9PartitionSeals(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit-chain/partition-seals?project_id=internal-platform-x", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Items []PartitionSealP9 `json:"items"`
		Count int               `json:"count"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Count != 2 {
		t.Fatalf("count: %d", resp.Count)
	}
	if len(fake.partitionArgs) != 1 || fake.partitionArgs[0] != "internal-platform-x" {
		t.Errorf("dispatch arg: %v", fake.partitionArgs)
	}
}

func TestAuditP9_PartitionSeals_MissingProjectID(t *testing.T) {
	fake := &fakeAuditAdapterP9{}
	h := AuditP9PartitionSeals(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit-chain/partition-seals", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestAuditP9_PartitionSeals_AdapterErr(t *testing.T) {
	fake := &fakeAuditAdapterP9{partitionErr: errors.New("io error")}
	h := AuditP9PartitionSeals(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit-chain/partition-seals?project_id=p", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestAuditP9_Recover_PlanOnly(t *testing.T) {
	fake := &fakeAuditAdapterP9{
		recoverPlans: []RecoverPlanP9{{
			ProjectID:           "zen-swarm",
			LitestreamSizeBytes: 1_200_000_000,
			ColdArchivePartCnt:  8,
			VerifyStepCount:     847239,
		}},
	}
	h := AuditP9Recover(fake)
	body := map[string]any{
		"project_id": "zen-swarm",
		"from_ts":    1714737600,
		"confirm":    false,
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/recover", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp struct {
		Plan   RecoverPlanP9    `json:"plan"`
		Result *RecoverResultP9 `json:"result,omitempty"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Plan.ProjectID != "zen-swarm" {
		t.Errorf("plan project_id: %q", resp.Plan.ProjectID)
	}
	if resp.Result != nil {
		t.Error("result must be nil when confirm=false")
	}
	if fake.recoverArgs[0].Confirm != false {
		t.Errorf("confirm dispatched: %v", fake.recoverArgs[0].Confirm)
	}
}

func TestAuditP9_Recover_Confirm(t *testing.T) {
	fake := &fakeAuditAdapterP9{
		recoverPlans:   []RecoverPlanP9{{ProjectID: "p", LitestreamSizeBytes: 100}},
		recoverResults: []RecoverResultP9{{Recovered: true, RecordsRestored: 100}},
	}
	h := AuditP9Recover(fake)
	body := map[string]any{"project_id": "p", "from_ts": 1, "confirm": true}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/recover", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d", w.Code)
	}
	var resp struct {
		Plan   RecoverPlanP9    `json:"plan"`
		Result *RecoverResultP9 `json:"result,omitempty"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Result == nil {
		t.Fatal("result must be set when confirm=true")
	}
	if !resp.Result.Recovered {
		t.Error("expected recovered=true")
	}
}

func TestAuditP9_Recover_MissingProjectID(t *testing.T) {
	fake := &fakeAuditAdapterP9{}
	h := AuditP9Recover(fake)
	body := map[string]any{"from_ts": 100}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/recover", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestAuditP9_Recover_MissingFromTs(t *testing.T) {
	fake := &fakeAuditAdapterP9{}
	h := AuditP9Recover(fake)
	body := map[string]any{"project_id": "p"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/recover", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestAuditP9_Recover_AdapterErr(t *testing.T) {
	fake := &fakeAuditAdapterP9{recoverErr: errors.New("recover failed")}
	h := AuditP9Recover(fake)
	body := map[string]any{"project_id": "p", "from_ts": 100}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/recover", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestAuditP9_Checkpoint_Doctrine(t *testing.T) {
	fake := &fakeAuditAdapterP9{
		checkpointResults: []CheckpointResultP9{{
			CheckpointID: "ck-001",
			TesseraSTH:   "abc",
			AnchoredAt:   100,
		}},
	}
	h := AuditP9Checkpoint(fake)
	body := map[string]any{"reason": "manual capa-firewall", "doctrine": "capa-firewall"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/checkpoint", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d", w.Code)
	}
	if fake.checkpointArgs[0].Doctrine != "capa-firewall" {
		t.Errorf("doctrine dispatched: %q", fake.checkpointArgs[0].Doctrine)
	}
}

func TestAuditP9_Checkpoint_MissingReason(t *testing.T) {
	fake := &fakeAuditAdapterP9{}
	h := AuditP9Checkpoint(fake)
	body := map[string]any{"doctrine": "max-scope"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/checkpoint", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestAuditP9_Checkpoint_AdapterErr(t *testing.T) {
	fake := &fakeAuditAdapterP9{checkpointErr: errors.New("tessera unavail")}
	h := AuditP9Checkpoint(fake)
	body := map[string]any{"reason": "test"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/checkpoint", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestAuditP9_ColdArchiveList(t *testing.T) {
	fake := &fakeAuditAdapterP9{
		coldListResults: [][]ColdArchiveEntryP9{{
			{PartitionID: "2026_04", SizeBytes: 1024, ArchivedAt: 100},
			{PartitionID: "2026_05", SizeBytes: 2048, ArchivedAt: 200},
		}},
	}
	h := AuditP9ColdArchiveList(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit-chain/cold-archive/list?project_id=p", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d", w.Code)
	}
	var resp struct {
		Items []ColdArchiveEntryP9 `json:"items"`
		Count int                  `json:"count"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Count != 2 {
		t.Fatalf("count: %d", resp.Count)
	}
}

func TestAuditP9_ColdArchiveList_MissingProjectID(t *testing.T) {
	fake := &fakeAuditAdapterP9{}
	h := AuditP9ColdArchiveList(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit-chain/cold-archive/list", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestAuditP9_ColdArchiveList_AdapterErr(t *testing.T) {
	fake := &fakeAuditAdapterP9{coldListErr: errors.New("s3 unreachable")}
	h := AuditP9ColdArchiveList(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit-chain/cold-archive/list?project_id=p", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestAuditP9_ColdArchiveRestore(t *testing.T) {
	fake := &fakeAuditAdapterP9{
		coldRestoreResults: []RestoreResultP9{{
			Restored:    true,
			BytesPulled: 1_000_000,
			DurationSec: 12,
		}},
	}
	h := AuditP9ColdArchiveRestore(fake)
	body := map[string]any{"partition_id": "2026_05", "project_id": "p"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/cold-archive/restore", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d", w.Code)
	}
}

func TestAuditP9_ColdArchiveRestore_MissingFields(t *testing.T) {
	fake := &fakeAuditAdapterP9{}
	h := AuditP9ColdArchiveRestore(fake)
	body := map[string]any{"partition_id": "2026_05"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/cold-archive/restore", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestAuditP9_ColdArchiveRestore_AdapterErr(t *testing.T) {
	fake := &fakeAuditAdapterP9{coldRestoreErr: errors.New("s3 timeout")}
	h := AuditP9ColdArchiveRestore(fake)
	body := map[string]any{"partition_id": "2026_05", "project_id": "p"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/cold-archive/restore", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestAuditP9_WitnessRotate(t *testing.T) {
	fake := &fakeAuditAdapterP9{
		witnessRotateResults: []RotateResultP9{{
			NewKeyFingerprint: "abc123",
			OldKeyFingerprint: "def456",
			RotatedAt:         100,
		}},
	}
	h := AuditP9WitnessRotate(fake)
	body := map[string]any{"reason": "scheduled 90d rotation"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/witness/rotate", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d", w.Code)
	}
	var resp RotateResultP9
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.NewKeyFingerprint != "abc123" {
		t.Errorf("new fingerprint: %q", resp.NewKeyFingerprint)
	}
}

func TestAuditP9_WitnessRotate_MissingReason(t *testing.T) {
	fake := &fakeAuditAdapterP9{}
	h := AuditP9WitnessRotate(fake)
	body := map[string]any{}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/witness/rotate", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestAuditP9_WitnessRotate_AdapterErr(t *testing.T) {
	fake := &fakeAuditAdapterP9{witnessRotateErr: errors.New("key store locked")}
	h := AuditP9WitnessRotate(fake)
	body := map[string]any{"reason": "emergency"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/witness/rotate", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestAuditP9_WitnessPubkey(t *testing.T) {
	fake := &fakeAuditAdapterP9{
		witnessPubkeyResult: PubkeyEntryP9{
			PubkeyPEM:     "-----BEGIN PUBLIC KEY-----\n...",
			Fingerprint:   "abc123",
			CreatedAt:     100,
			RotationCount: 3,
		},
	}
	h := AuditP9WitnessPubkey(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit-chain/witness/pubkey", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d", w.Code)
	}
	var resp PubkeyEntryP9
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Fingerprint != "abc123" {
		t.Errorf("fingerprint: %q", resp.Fingerprint)
	}
}

func TestAuditP9_WitnessPubkey_AdapterErr(t *testing.T) {
	fake := &fakeAuditAdapterP9{witnessPubkeyErr: errors.New("key not initialized")}
	h := AuditP9WitnessPubkey(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit-chain/witness/pubkey", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestAuditP9_ConfigureS3(t *testing.T) {
	fake := &fakeAuditAdapterP9{}
	h := AuditP9ConfigureS3(fake)
	body := map[string]any{
		"project_id": "internal-platform-x",
		"credentials": map[string]string{
			"endpoint":   "s3.eu-central-1.amazonaws.com",
			"bucket":     "internal-platform-x-audit",
			"prefix":     "tessera/",
			"access_key": "AKIA...",
			"secret_key": "secret-redacted",
			"region":     "eu-central-1",
		},
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/configure-s3", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status: got %d, want 204", w.Code)
	}
	if len(fake.configureS3Args) != 1 {
		t.Fatalf("dispatch count: %d", len(fake.configureS3Args))
	}
	if fake.configureS3Args[0].ProjectID != "internal-platform-x" {
		t.Errorf("project_id: %q", fake.configureS3Args[0].ProjectID)
	}
	if fake.configureS3Args[0].Credentials.Bucket != "internal-platform-x-audit" {
		t.Errorf("bucket: %q", fake.configureS3Args[0].Credentials.Bucket)
	}
}

func TestAuditP9_ConfigureS3_MissingProjectID(t *testing.T) {
	fake := &fakeAuditAdapterP9{}
	h := AuditP9ConfigureS3(fake)
	body := map[string]any{"credentials": map[string]string{"bucket": "b"}}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/configure-s3", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestAuditP9_ConfigureS3_MissingBucket(t *testing.T) {
	fake := &fakeAuditAdapterP9{}
	h := AuditP9ConfigureS3(fake)
	body := map[string]any{"project_id": "p", "credentials": map[string]string{}}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/configure-s3", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestAuditP9_ConfigureS3_AdapterErr(t *testing.T) {
	fake := &fakeAuditAdapterP9{configureS3Err: errors.New("keychain locked")}
	h := AuditP9ConfigureS3(fake)
	body := map[string]any{
		"project_id":  "p",
		"credentials": map[string]string{"bucket": "b"},
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/configure-s3", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestAuditP9_NilAdapter_VerifyChain(t *testing.T) {
	var s AuditCtxP9
	h := AuditP9VerifyChain(s)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/verify-chain", bytes.NewReader([]byte(`{}`)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["code"] != "plan9_audit_unavailable" {
		t.Errorf("code: %q", body["code"])
	}
}

func TestAuditP9_NilAdapter_History(t *testing.T) {
	var s AuditCtxP9
	h := AuditP9History(s)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit-chain/history", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
}

func TestAuditP9_NilAdapter_PartitionSeals(t *testing.T) {
	var s AuditCtxP9
	h := AuditP9PartitionSeals(s)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit-chain/partition-seals?project_id=p", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
}

func TestAuditP9_NilAdapter_Recover(t *testing.T) {
	var s AuditCtxP9
	h := AuditP9Recover(s)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/recover", bytes.NewReader([]byte(`{}`)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
}

func TestAuditP9_NilAdapter_Checkpoint(t *testing.T) {
	var s AuditCtxP9
	h := AuditP9Checkpoint(s)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/checkpoint", bytes.NewReader([]byte(`{}`)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
}

func TestAuditP9_NilAdapter_ColdArchiveList(t *testing.T) {
	var s AuditCtxP9
	h := AuditP9ColdArchiveList(s)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit-chain/cold-archive/list?project_id=p", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
}

func TestAuditP9_NilAdapter_ColdArchiveRestore(t *testing.T) {
	var s AuditCtxP9
	h := AuditP9ColdArchiveRestore(s)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/cold-archive/restore", bytes.NewReader([]byte(`{}`)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
}

func TestAuditP9_NilAdapter_WitnessRotate(t *testing.T) {
	var s AuditCtxP9
	h := AuditP9WitnessRotate(s)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/witness/rotate", bytes.NewReader([]byte(`{}`)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
}

func TestAuditP9_NilAdapter_WitnessPubkey(t *testing.T) {
	var s AuditCtxP9
	h := AuditP9WitnessPubkey(s)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit-chain/witness/pubkey", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
}

func TestAuditP9_NilAdapter_ConfigureS3(t *testing.T) {
	var s AuditCtxP9
	h := AuditP9ConfigureS3(s)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/configure-s3", bytes.NewReader([]byte(`{}`)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
}
