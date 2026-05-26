package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

type fakeOrchService struct {
	session          client.SessionInfo
	pool             client.PoolStatus
	captureResp      client.CaptureResult
	replayResp       client.ReplayResult
	autonomyShow     client.AutonomyShow
	autonomyCheck    client.AutonomyCheckResult
	autonomyModeErr  error
	doctrineList     client.DoctrineProposalList
	doctrineListE    error
	doctrineProp     client.DoctrineProposal
	doctrineProposeR client.DoctrineProposeResponse
	doctrineProposeE error
	doctrineAckE     error
	doctrineDenyE    error
	doctrineRevertE  error
	doctrineShowE    error
	safetynet        client.SafetynetStatus
	prevInstall      map[string]string
	prevShow         map[string]string
	prevExec         map[string]any
	divergence       client.DivergenceReport
	divHistory       []client.DivergenceReport
	regression       []client.RegressionMetric
	driftRun         []client.DriftFinding
	driftHistory     []client.DriftFinding
	healthEventLog   struct {
		writable   bool
		corruption int
	}
	healthResearch bool
	healthCaronte  struct {
		up    bool
		hours int
	}
	healthAdapters bool
	healthSession  bool

	depthIn           client.DepthOverride
	captureIn         client.CaptureRequest
	replayIn          client.ReplayRequest
	autonomyIn        client.AutonomyModeRequest
	doctrineIn        client.DoctrineDecision
	doctrineProposeIn client.DoctrineProposeRequest
	prevExecIn        []string
	regAuthor         string
	regSince          string
	prunedCount       int
	prunedCalled      bool
}

func (f *fakeOrchService) Session() (client.SessionInfo, error) { return f.session, nil }
func (f *fakeOrchService) Pool() (client.PoolStatus, error)     { return f.pool, nil }
func (f *fakeOrchService) PrunePool() (int, error) {
	f.prunedCalled = true
	return f.prunedCount, nil
}
func (f *fakeOrchService) SetDepth(d client.DepthOverride) error {
	f.depthIn = d
	return nil
}
func (f *fakeOrchService) Capture(req client.CaptureRequest) (client.CaptureResult, error) {
	f.captureIn = req
	return f.captureResp, nil
}
func (f *fakeOrchService) Replay(req client.ReplayRequest) (client.ReplayResult, error) {
	f.replayIn = req
	return f.replayResp, nil
}
func (f *fakeOrchService) AutonomyShow() (client.AutonomyShow, error) { return f.autonomyShow, nil }
func (f *fakeOrchService) AutonomyCheck() (client.AutonomyCheckResult, error) {
	return f.autonomyCheck, nil
}
func (f *fakeOrchService) AutonomyMode(req client.AutonomyModeRequest) error {
	f.autonomyIn = req
	return f.autonomyModeErr
}
func (f *fakeOrchService) DoctrineProposeList() (client.DoctrineProposalList, error) {
	return f.doctrineList, f.doctrineListE
}
func (f *fakeOrchService) DoctrineProposeShow(id string) (client.DoctrineProposal, error) {
	if f.doctrineShowE != nil {
		return client.DoctrineProposal{}, f.doctrineShowE
	}
	if f.doctrineProp.ID == "" || f.doctrineProp.ID != id {

		for _, p := range f.doctrineList.Proposals {
			if p.ID == id {
				return p, nil
			}
		}
		return client.DoctrineProposal{}, nil
	}
	return f.doctrineProp, nil
}
func (f *fakeOrchService) DoctrineAck(req client.DoctrineDecision) error {
	f.doctrineIn = req
	return f.doctrineAckE
}
func (f *fakeOrchService) DoctrineDeny(req client.DoctrineDecision) error {
	f.doctrineIn = req
	return f.doctrineDenyE
}
func (f *fakeOrchService) DoctrineRevert(req client.DoctrineDecision) error {
	f.doctrineIn = req
	return f.doctrineRevertE
}
func (f *fakeOrchService) DoctrinePropose(req client.DoctrineProposeRequest) (client.DoctrineProposeResponse, error) {
	f.doctrineProposeIn = req
	return f.doctrineProposeR, f.doctrineProposeE
}
func (f *fakeOrchService) SafetynetStatus() (client.SafetynetStatus, error) { return f.safetynet, nil }
func (f *fakeOrchService) SafetynetPrevInstall() (map[string]string, error) {
	return f.prevInstall, nil
}
func (f *fakeOrchService) SafetynetPrevShow() (map[string]string, error) { return f.prevShow, nil }
func (f *fakeOrchService) SafetynetPrevExec(argv []string) (map[string]any, error) {
	f.prevExecIn = argv
	return f.prevExec, nil
}
func (f *fakeOrchService) SafetynetDivergenceRun() (client.DivergenceReport, error) {
	return f.divergence, nil
}
func (f *fakeOrchService) SafetynetDivergenceHistory(_ string) ([]client.DivergenceReport, error) {
	return f.divHistory, nil
}
func (f *fakeOrchService) SafetynetRegressionQuery(author, since string) ([]client.RegressionMetric, error) {
	f.regAuthor = author
	f.regSince = since
	return f.regression, nil
}
func (f *fakeOrchService) SafetynetDriftRun() ([]client.DriftFinding, error) { return f.driftRun, nil }
func (f *fakeOrchService) SafetynetDriftHistory(_ string) ([]client.DriftFinding, error) {
	return f.driftHistory, nil
}
func (f *fakeOrchService) HealthEventLogWritable() (bool, int, error) {
	return f.healthEventLog.writable, f.healthEventLog.corruption, nil
}
func (f *fakeOrchService) HealthResearchMCPUp() (bool, error) { return f.healthResearch, nil }
func (f *fakeOrchService) HealthCaronteUp() (bool, int, error) {
	return f.healthCaronte.up, f.healthCaronte.hours, nil
}
func (f *fakeOrchService) HealthAdaptersClean() (bool, error)    { return f.healthAdapters, nil }
func (f *fakeOrchService) HealthLastSessionClean() (bool, error) { return f.healthSession, nil }

func TestPlan5Handler_State(t *testing.T) {
	svc := &fakeOrchService{session: client.SessionInfo{
		SessionID: "sess-1", State: "RUNNING", Mode: "semi", BackgroundGoroutines: 11,
	}}
	h := NewPlan5OrchestratorHandler(svc)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/orchestrator/state", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status: got %d, want 200", rr.Code)
	}
	var got client.SessionInfo
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.SessionID != "sess-1" || got.State != "RUNNING" {
		t.Errorf("unexpected session: %+v", got)
	}
}

func TestPlan5Handler_PoolPrune(t *testing.T) {
	svc := &fakeOrchService{prunedCount: 7}
	h := NewPlan5OrchestratorHandler(svc)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/orchestrator/pool/prune", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status: got %d, want 200", rr.Code)
	}
	if !svc.prunedCalled {
		t.Error("svc.PrunePool not called")
	}
	var resp map[string]int
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp["orphans_pruned"] != 7 {
		t.Errorf("orphans_pruned: got %d want 7", resp["orphans_pruned"])
	}
}

func TestPlan5Handler_DepthOverride(t *testing.T) {
	svc := &fakeOrchService{}
	h := NewPlan5OrchestratorHandler(svc)
	body, _ := json.Marshal(client.DepthOverride{ProjectID: "proj", SpecPath: "spec.md", Depth: 5})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/orchestrator/depth", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	if svc.depthIn.ProjectID != "proj" || svc.depthIn.Depth != 5 {
		t.Errorf("depth not propagated: %+v", svc.depthIn)
	}
}

func TestPlan5Handler_DepthBadJSON(t *testing.T) {
	svc := &fakeOrchService{}
	h := NewPlan5OrchestratorHandler(svc)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/orchestrator/depth", bytes.NewReader([]byte("not-json")))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("got %d want 400", rr.Code)
	}
}

func TestPlan5Handler_RejectsBadMethod(t *testing.T) {
	h := NewPlan5OrchestratorHandler(&fakeOrchService{})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/v1/orchestrator/state", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("got %d, want 405", rr.Code)
	}
}

func TestPlan5Handler_NotFound(t *testing.T) {
	h := NewPlan5OrchestratorHandler(&fakeOrchService{})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/orchestrator/nope", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", rr.Code)
	}
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/totally-unrelated", nil)
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", rr2.Code)
	}
}

func TestPlan5Handler_AutonomyMode_Forbidden(t *testing.T) {
	svc := &fakeOrchService{autonomyModeErr: errCapaFirewall{}}
	h := NewPlan5OrchestratorHandler(svc)
	body, _ := json.Marshal(client.AutonomyModeRequest{Mode: "full"})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/autonomy/mode", bytes.NewReader(body))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("got %d want 403", rr.Code)
	}
}

func TestPlan5Handler_DoctrineProposeShowMissingID(t *testing.T) {
	h := NewPlan5OrchestratorHandler(&fakeOrchService{})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/doctrine/propose-show", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("got %d want 400", rr.Code)
	}
}

func TestPlan5Handler_DoctrinePropose_HappyPath_RoundTrips(t *testing.T) {
	svc := &fakeOrchService{
		doctrineProposeR: client.DoctrineProposeResponse{
			ID:         "ADR-0050",
			Status:     "proposed",
			RulePath:   "amendment.cooldown_hours",
			NewValue:   "12",
			Category:   "merge",
			ProposedAt: 1714737600,
			Proposer:   "operator",
		},
	}
	h := NewPlan5OrchestratorHandler(svc)
	body, _ := json.Marshal(client.DoctrineProposeRequest{
		RulePath: "amendment.cooldown_hours", NewValue: "12",
		Justification: "j", Category: "merge",
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/doctrine/propose", bytes.NewReader(body))
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("got %d, body=%s", rr.Code, rr.Body.String())
	}
	if svc.doctrineProposeIn.RulePath != "amendment.cooldown_hours" || svc.doctrineProposeIn.Category != "merge" {
		t.Errorf("request not propagated: %+v", svc.doctrineProposeIn)
	}
	var got client.DoctrineProposeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != "ADR-0050" {
		t.Errorf("response ID: got %q", got.ID)
	}
}

func TestPlan5Handler_DoctrinePropose_BadJSON(t *testing.T) {
	h := NewPlan5OrchestratorHandler(&fakeOrchService{})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/doctrine/propose", strings.NewReader(`{not json`))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", rr.Code)
	}
}

func TestPlan5Handler_DoctrinePropose_InvalidCategory400(t *testing.T) {
	svc := &fakeOrchService{doctrineProposeE: errors.New("invalid_category: garbage")}
	h := NewPlan5OrchestratorHandler(svc)
	body, _ := json.Marshal(client.DoctrineProposeRequest{RulePath: "x", NewValue: "z", Justification: "j", Category: "garbage"})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/doctrine/propose", bytes.NewReader(body))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", rr.Code)
	}
}

func TestPlan5Handler_DoctrinePropose_RuleInCooldown429(t *testing.T) {
	svc := &fakeOrchService{doctrineProposeE: errors.New("rule_in_cooldown: amendment.cooldown_hours")}
	h := NewPlan5OrchestratorHandler(svc)
	body, _ := json.Marshal(client.DoctrineProposeRequest{RulePath: "x", NewValue: "z", Justification: "j", Category: "merge"})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/doctrine/propose", bytes.NewReader(body))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("got %d, want 429", rr.Code)
	}
}

func TestPlan5Handler_DoctrinePropose_UnknownError500(t *testing.T) {
	svc := &fakeOrchService{doctrineProposeE: errors.New("disk full")}
	h := NewPlan5OrchestratorHandler(svc)
	body, _ := json.Marshal(client.DoctrineProposeRequest{RulePath: "x", NewValue: "z", Justification: "j", Category: "merge"})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/doctrine/propose", bytes.NewReader(body))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("got %d, want 500", rr.Code)
	}
}

func TestPlan5Handler_DoctrinePropose_GETReturns405(t *testing.T) {
	h := NewPlan5OrchestratorHandler(&fakeOrchService{})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/doctrine/propose", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("got %d, want 405", rr.Code)
	}
}

func TestPlan5Handler_DoctrineAck_HappyPath(t *testing.T) {
	svc := &fakeOrchService{}
	h := NewPlan5OrchestratorHandler(svc)
	body, _ := json.Marshal(client.DoctrineDecision{ID: "ADR-0050", Reason: "approved"})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/doctrine/ack", bytes.NewReader(body))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200, body=%s", rr.Code, rr.Body.String())
	}
	if svc.doctrineIn.ID != "ADR-0050" || svc.doctrineIn.Reason != "approved" {
		t.Errorf("decision not propagated: %+v", svc.doctrineIn)
	}
	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["status"] != "applied" {
		t.Errorf("status field: got %q want %q", resp["status"], "applied")
	}
}

func TestPlan5Handler_DoctrineAck_BadJSON(t *testing.T) {
	h := NewPlan5OrchestratorHandler(&fakeOrchService{})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/doctrine/ack", strings.NewReader(`{not json`))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", rr.Code)
	}
}

func TestPlan5Handler_DoctrineAck_ServiceError(t *testing.T) {
	svc := &fakeOrchService{doctrineAckE: errors.New("apply failed: write to /tmp denied")}
	h := NewPlan5OrchestratorHandler(svc)
	body, _ := json.Marshal(client.DoctrineDecision{ID: "ADR-0050"})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/doctrine/ack", bytes.NewReader(body))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("got %d, want 500", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "apply failed") {
		t.Errorf("expected service error in response body, got: %s", rr.Body.String())
	}
}

func TestPlan5Handler_DoctrineDeny_HappyPath(t *testing.T) {
	svc := &fakeOrchService{}
	h := NewPlan5OrchestratorHandler(svc)
	body, _ := json.Marshal(client.DoctrineDecision{ID: "ADR-0050", Reason: "wrong scope"})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/doctrine/deny", bytes.NewReader(body))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200, body=%s", rr.Code, rr.Body.String())
	}
	if svc.doctrineIn.ID != "ADR-0050" || svc.doctrineIn.Reason != "wrong scope" {
		t.Errorf("decision not propagated: %+v", svc.doctrineIn)
	}
	var resp map[string]string
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["status"] != "denied" {
		t.Errorf("status field: got %q want %q", resp["status"], "denied")
	}
}

func TestPlan5Handler_DoctrineDeny_BadJSON(t *testing.T) {
	h := NewPlan5OrchestratorHandler(&fakeOrchService{})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/doctrine/deny", strings.NewReader(`{not json`))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", rr.Code)
	}
}

func TestPlan5Handler_DoctrineDeny_ServiceError(t *testing.T) {
	svc := &fakeOrchService{doctrineDenyE: errors.New("eventlog append failed")}
	h := NewPlan5OrchestratorHandler(svc)
	body, _ := json.Marshal(client.DoctrineDecision{ID: "ADR-0050"})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/doctrine/deny", bytes.NewReader(body))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("got %d, want 500", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "eventlog append failed") {
		t.Errorf("expected service error, got: %s", rr.Body.String())
	}
}

func TestPlan5Handler_DoctrineRevert_HappyPath(t *testing.T) {
	svc := &fakeOrchService{}
	h := NewPlan5OrchestratorHandler(svc)
	body, _ := json.Marshal(client.DoctrineDecision{ID: "ADR-0050", Reason: "regression detected"})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/doctrine/revert", bytes.NewReader(body))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200, body=%s", rr.Code, rr.Body.String())
	}
	if svc.doctrineIn.ID != "ADR-0050" || svc.doctrineIn.Reason != "regression detected" {
		t.Errorf("decision not propagated: %+v", svc.doctrineIn)
	}
	var resp map[string]string
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["status"] != "reverted" {
		t.Errorf("status field: got %q want %q", resp["status"], "reverted")
	}
}

func TestPlan5Handler_DoctrineRevert_BadJSON(t *testing.T) {
	h := NewPlan5OrchestratorHandler(&fakeOrchService{})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/doctrine/revert", strings.NewReader(`{not json`))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", rr.Code)
	}
}

func TestPlan5Handler_DoctrineRevert_ServiceError(t *testing.T) {
	svc := &fakeOrchService{doctrineRevertE: errors.New("reverter: ADR not found in history")}
	h := NewPlan5OrchestratorHandler(svc)
	body, _ := json.Marshal(client.DoctrineDecision{ID: "ADR-9999"})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/doctrine/revert", bytes.NewReader(body))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("got %d, want 500", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "ADR not found") {
		t.Errorf("expected service error, got: %s", rr.Body.String())
	}
}

func TestPlan5Handler_DoctrineProposeList_HappyPath(t *testing.T) {
	list := client.DoctrineProposalList{
		Proposals: []client.DoctrineProposal{
			{ID: "ADR-0050", Title: "tune cooldown to 12h", Status: "proposed"},
			{ID: "ADR-0051", Title: "tune flake budget to 3", Status: "proposed"},
		},
	}
	svc := &fakeOrchService{doctrineList: list}
	h := NewPlan5OrchestratorHandler(svc)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/doctrine/propose-list", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200, body=%s", rr.Code, rr.Body.String())
	}
	var got client.DoctrineProposalList
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Proposals) != 2 {
		t.Fatalf("proposals count: got %d want 2", len(got.Proposals))
	}
	if got.Proposals[0].ID != "ADR-0050" || got.Proposals[1].ID != "ADR-0051" {
		t.Errorf("proposals not round-tripped: %+v", got.Proposals)
	}
}

func TestPlan5Handler_DoctrineProposeList_Empty(t *testing.T) {
	svc := &fakeOrchService{doctrineList: client.DoctrineProposalList{}}
	h := NewPlan5OrchestratorHandler(svc)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/doctrine/propose-list", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rr.Code)
	}
	var got client.DoctrineProposalList
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Proposals) != 0 {
		t.Errorf("expected zero proposals, got %d", len(got.Proposals))
	}
}

func TestPlan5Handler_DoctrineProposeList_ServiceError(t *testing.T) {
	svc := &fakeOrchService{doctrineListE: errors.New("eventlog scan failed: i/o timeout")}
	h := NewPlan5OrchestratorHandler(svc)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/doctrine/propose-list", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("got %d, want 500", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "eventlog scan failed") {
		t.Errorf("expected service error, got: %s", rr.Body.String())
	}
}

func TestPlan5Handler_DoctrineProposeShow_HappyPath(t *testing.T) {
	prop := client.DoctrineProposal{
		ID:           "ADR-0050",
		Title:        "tune amendment.cooldown_hours to 12h",
		Status:       "proposed",
		BodyMarkdown: "## Context\noperator-tuned per telemetry",
	}
	svc := &fakeOrchService{doctrineProp: prop}
	h := NewPlan5OrchestratorHandler(svc)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/doctrine/propose-show?id=ADR-0050", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200, body=%s", rr.Code, rr.Body.String())
	}
	var got client.DoctrineProposal
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != "ADR-0050" || got.Title != "tune amendment.cooldown_hours to 12h" {
		t.Errorf("proposal not round-tripped: %+v", got)
	}
}

func TestPlan5Handler_DoctrineProposeShow_NotFound(t *testing.T) {
	svc := &fakeOrchService{doctrineShowE: errors.New("ADR-9999 not found in history")}
	h := NewPlan5OrchestratorHandler(svc)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/doctrine/propose-show?id=ADR-9999", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", rr.Code)
	}
}

func largePlan5Payload(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("a", n)
}

func TestPlan5Handler_DoctrineAck_BodyTooLargeReturns413(t *testing.T) {
	svc := &fakeOrchService{}
	h := NewPlan5OrchestratorHandler(svc)

	body := client.DoctrineDecision{ID: "ADR-0050", Reason: largePlan5Payload(96 << 10)}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest("POST", "/v1/doctrine/ack", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413 Payload Too Large, got %d: %.200s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "body exceeds") {
		t.Errorf("response body missing 'body exceeds' marker: %s", rr.Body.String())
	}
}

func TestPlan5Handler_DoctrineDeny_BodyTooLargeReturns413(t *testing.T) {
	svc := &fakeOrchService{}
	h := NewPlan5OrchestratorHandler(svc)
	body := client.DoctrineDecision{ID: "ADR-0050", Reason: largePlan5Payload(96 << 10)}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest("POST", "/v1/doctrine/deny", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d: %.200s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "body exceeds") {
		t.Errorf("missing 'body exceeds' marker: %s", rr.Body.String())
	}
}

func TestPlan5Handler_DoctrineRevert_BodyTooLargeReturns413(t *testing.T) {
	svc := &fakeOrchService{}
	h := NewPlan5OrchestratorHandler(svc)
	body := client.DoctrineDecision{ID: "ADR-0050", Reason: largePlan5Payload(96 << 10)}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest("POST", "/v1/doctrine/revert", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d: %.200s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "body exceeds") {
		t.Errorf("missing 'body exceeds' marker: %s", rr.Body.String())
	}
}

func TestPlan5Handler_DoctrinePropose_BodyTooLargeReturns413(t *testing.T) {
	svc := &fakeOrchService{}
	h := NewPlan5OrchestratorHandler(svc)
	body := client.DoctrineProposeRequest{
		RulePath:      "amendment.cooldown_hours",
		NewValue:      "12",
		Category:      "merge",
		Justification: largePlan5Payload(96 << 10),
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest("POST", "/v1/doctrine/propose", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d: %.200s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "body exceeds") {
		t.Errorf("missing 'body exceeds' marker: %s", rr.Body.String())
	}
}

func TestPlan5Handler_DoctrineAck_BodyAtCapAccepted(t *testing.T) {
	svc := &fakeOrchService{}
	h := NewPlan5OrchestratorHandler(svc)

	body := client.DoctrineDecision{ID: "ADR-0050", Reason: largePlan5Payload((64 << 10) - 256)}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(b) >= 64<<10 {
		t.Fatalf("test setup error: payload %d ≥ 64 KiB cap", len(b))
	}
	req := httptest.NewRequest("POST", "/v1/doctrine/ack", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200 (just under cap), got %d: %.200s", rr.Code, rr.Body.String())
	}
}

func TestPlan5Handler_RegressionQuery_PassesArgs(t *testing.T) {
	svc := &fakeOrchService{regression: []client.RegressionMetric{{CommitSHA: "abc"}}}
	h := NewPlan5OrchestratorHandler(svc)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/safetynet/regression/query?author=substrate&since=7d", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("got %d", rr.Code)
	}
	if svc.regAuthor != "substrate" || svc.regSince != "7d" {
		t.Errorf("query args not propagated: author=%q since=%q", svc.regAuthor, svc.regSince)
	}
}

func TestPlan5Handler_DriftRun(t *testing.T) {
	svc := &fakeOrchService{driftRun: []client.DriftFinding{{CommitSHA: "deadbeef", Rule: "no_attribution"}}}
	h := NewPlan5OrchestratorHandler(svc)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/safetynet/drift/run", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("got %d", rr.Code)
	}
}

func TestPlan5Handler_PrevExecRequiresArgv(t *testing.T) {
	h := NewPlan5OrchestratorHandler(&fakeOrchService{})
	body, _ := json.Marshal(map[string]any{"argv": []string{}})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/safetynet/prev/exec", bytes.NewReader(body))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("got %d want 400", rr.Code)
	}
}

func TestPlan5Handler_HealthProbes(t *testing.T) {
	svc := &fakeOrchService{}
	svc.healthEventLog.writable = true
	svc.healthEventLog.corruption = 2
	svc.healthResearch = true
	svc.healthCaronte.up = true
	svc.healthCaronte.hours = 5
	svc.healthAdapters = true
	svc.healthSession = true
	h := NewPlan5OrchestratorHandler(svc)
	for _, path := range []string{
		"/v1/orchestrator/health/event_log_writable",
		"/v1/orchestrator/health/research_mcp_up",
		"/v1/orchestrator/health/caronte_up",
		"/v1/orchestrator/health/adapters_clean",
		"/v1/orchestrator/health/last_session_clean",
	} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, nil)
		h.ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Errorf("%s: got %d want 200", path, rr.Code)
		}
	}
}

type errCapaFirewall struct{}

func (errCapaFirewall) Error() string { return "capa-firewall: mode override forbidden" }
