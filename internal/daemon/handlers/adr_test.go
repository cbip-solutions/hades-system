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

type fakeADRIndex struct {
	mu sync.Mutex

	proposeArgs    []string
	proposeResults []ADRDoc
	proposeErr     error

	showArgs    []string
	showResults []ADRDoc
	showErr     error

	listArgs    []ADRListFilter
	listResults [][]ADRDoc
	listErr     error

	graphArgs    []graphArgs
	graphResults []ADRGraph
	graphErr     error

	historyArgs    []string
	historyResults [][]ADRTransition
	historyErr     error

	acceptArgs    []transitionArgs
	rejectArgs    []transitionArgs
	supersedeArgs []supersedeArgs
	transitionErr error

	indexResult ADRManifest
	indexErr    error
}

type graphArgs struct {
	From  string
	Depth int
}

type transitionArgs struct {
	ID, Reason string
}

type supersedeArgs struct {
	OldID, NewID, Reason string
}

func (f *fakeADRIndex) Propose(_ context.Context, topic string) (ADRDoc, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.proposeArgs = append(f.proposeArgs, topic)
	if f.proposeErr != nil {
		return ADRDoc{}, f.proposeErr
	}
	if len(f.proposeResults) == 0 {
		return ADRDoc{}, nil
	}
	r := f.proposeResults[0]
	f.proposeResults = f.proposeResults[1:]
	return r, nil
}

func (f *fakeADRIndex) Show(_ context.Context, id string) (ADRDoc, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.showArgs = append(f.showArgs, id)
	if f.showErr != nil {
		return ADRDoc{}, f.showErr
	}
	if len(f.showResults) == 0 {
		return ADRDoc{}, nil
	}
	r := f.showResults[0]
	f.showResults = f.showResults[1:]
	return r, nil
}

func (f *fakeADRIndex) List(_ context.Context, filter ADRListFilter) ([]ADRDoc, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listArgs = append(f.listArgs, filter)
	if f.listErr != nil {
		return nil, f.listErr
	}
	if len(f.listResults) == 0 {
		return nil, nil
	}
	r := f.listResults[0]
	f.listResults = f.listResults[1:]
	return r, nil
}

func (f *fakeADRIndex) Graph(_ context.Context, fromID string, depth int) (ADRGraph, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.graphArgs = append(f.graphArgs, graphArgs{fromID, depth})
	if f.graphErr != nil {
		return ADRGraph{}, f.graphErr
	}
	if len(f.graphResults) == 0 {
		return ADRGraph{}, nil
	}
	r := f.graphResults[0]
	f.graphResults = f.graphResults[1:]
	return r, nil
}

func (f *fakeADRIndex) History(_ context.Context, id string) ([]ADRTransition, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.historyArgs = append(f.historyArgs, id)
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

func (f *fakeADRIndex) Accept(_ context.Context, id, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acceptArgs = append(f.acceptArgs, transitionArgs{id, reason})
	return f.transitionErr
}

func (f *fakeADRIndex) Reject(_ context.Context, id, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rejectArgs = append(f.rejectArgs, transitionArgs{id, reason})
	return f.transitionErr
}

func (f *fakeADRIndex) Supersede(_ context.Context, oldID, newID, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.supersedeArgs = append(f.supersedeArgs, supersedeArgs{oldID, newID, reason})
	return f.transitionErr
}

func (f *fakeADRIndex) RegenerateIndex(_ context.Context, dryRun bool) (ADRManifest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.indexErr != nil {
		return ADRManifest{}, f.indexErr
	}
	return f.indexResult, nil
}

func TestADR_Propose_Happy(t *testing.T) {
	fake := &fakeADRIndex{
		proposeResults: []ADRDoc{{
			ID:          "ADR-0070",
			Status:      "proposed",
			Topic:       "tessera-batch-cadence-tuning",
			Plan:        "plan-9-followup",
			Frontmatter: map[string]string{"id": "ADR-0070"},
		}},
	}
	h := ADRPropose(fake)
	body := map[string]any{"topic": "tessera-batch-cadence-tuning"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/adr/propose", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201", w.Code)
	}
	var resp ADRDoc
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ID != "ADR-0070" {
		t.Errorf("id: got %q, want ADR-0070", resp.ID)
	}
	if len(fake.proposeArgs) != 1 || fake.proposeArgs[0] != "tessera-batch-cadence-tuning" {
		t.Errorf("proposeArgs: %v", fake.proposeArgs)
	}
}

func TestADR_Propose_MissingTopic(t *testing.T) {
	fake := &fakeADRIndex{}
	h := ADRPropose(fake)
	body := map[string]any{}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/adr/propose", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestADR_Propose_AdapterError(t *testing.T) {
	fake := &fakeADRIndex{proposeErr: errors.New("propose fail")}
	h := ADRPropose(fake)
	body := map[string]any{"topic": "x"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/adr/propose", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestADR_Propose_NilAdapter(t *testing.T) {
	h := ADRPropose(nil)
	body := map[string]any{"topic": "x"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/adr/propose", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
}

func TestADR_Show_Happy(t *testing.T) {
	fake := &fakeADRIndex{
		showResults: []ADRDoc{{ID: "ADR-0001", Status: "accepted"}},
	}
	h := ADRShow(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/adr/show?id=ADR-0001", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp ADRDoc
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ID != "ADR-0001" {
		t.Errorf("id: %q", resp.ID)
	}
}

func TestADR_Show_MissingID(t *testing.T) {
	fake := &fakeADRIndex{}
	h := ADRShow(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/adr/show", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestADR_Show_AdapterError(t *testing.T) {
	fake := &fakeADRIndex{showErr: errors.New("show fail")}
	h := ADRShow(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/adr/show?id=ADR-0001", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestADR_Show_NilAdapter(t *testing.T) {
	h := ADRShow(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/adr/show?id=ADR-0001", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
}

func TestADR_Show_NotFound(t *testing.T) {

	fake := &fakeADRIndex{}
	h := ADRShow(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/adr/show?id=ADR-9999", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", w.Code)
	}
}

func TestADR_List_Filter(t *testing.T) {
	fake := &fakeADRIndex{
		listResults: [][]ADRDoc{{
			{ID: "ADR-0060", Status: "accepted", Plan: "plan-9", RiskLevel: "high"},
			{ID: "ADR-0061", Status: "accepted", Plan: "plan-9", RiskLevel: "medium"},
		}},
	}
	h := ADRList(fake)
	req := httptest.NewRequest(http.MethodGet,
		"/v1/adr/list?status=accepted&plan=plan-9&risk_level=high", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	got := fake.listArgs[0]
	if got.Status != "accepted" || got.Plan != "plan-9" || got.RiskLevel != "high" {
		t.Errorf("filter: %+v", got)
	}
	var resp struct {
		Items []ADRDoc `json:"items"`
		Count int      `json:"count"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Count != 2 {
		t.Errorf("count: %d", resp.Count)
	}
}

func TestADR_List_AdapterError(t *testing.T) {
	fake := &fakeADRIndex{listErr: errors.New("list fail")}
	h := ADRList(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/adr/list", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestADR_List_NilAdapter(t *testing.T) {
	h := ADRList(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/adr/list", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
}

func TestADR_Graph_Happy(t *testing.T) {
	fake := &fakeADRIndex{
		graphResults: []ADRGraph{{
			Nodes: []ADRGraphNode{{ID: "ADR-0001"}, {ID: "ADR-0070"}},
			Edges: []ADRGraphEdge{{From: "ADR-0070", To: "ADR-0001", Type: "supersedes"}},
		}},
	}
	h := ADRGraphHandler(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/adr/graph?from=ADR-0070&depth=2", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	if fake.graphArgs[0].Depth != 2 {
		t.Errorf("depth: %d", fake.graphArgs[0].Depth)
	}
	if fake.graphArgs[0].From != "ADR-0070" {
		t.Errorf("from: %q", fake.graphArgs[0].From)
	}
}

func TestADR_Graph_MissingFrom(t *testing.T) {
	fake := &fakeADRIndex{}
	h := ADRGraphHandler(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/adr/graph", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestADR_Graph_AdapterError(t *testing.T) {
	fake := &fakeADRIndex{graphErr: errors.New("graph fail")}
	h := ADRGraphHandler(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/adr/graph?from=ADR-0001", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestADR_Graph_NilAdapter(t *testing.T) {
	h := ADRGraphHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/adr/graph?from=ADR-0001", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
}

func TestADR_History_Happy(t *testing.T) {
	fake := &fakeADRIndex{
		historyResults: [][]ADRTransition{{
			{ID: "ADR-0070", Status: "proposed", At: 100},
			{ID: "ADR-0070", Status: "accepted", At: 200, Reason: "unanimous"},
		}},
	}
	h := ADRHistoryHandler(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/adr/history?id=ADR-0070", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp struct {
		Items []ADRTransition `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Errorf("transitions: %d", len(resp.Items))
	}
}

func TestADR_History_MissingID(t *testing.T) {
	fake := &fakeADRIndex{}
	h := ADRHistoryHandler(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/adr/history", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestADR_History_AdapterError(t *testing.T) {
	fake := &fakeADRIndex{historyErr: errors.New("history fail")}
	h := ADRHistoryHandler(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/adr/history?id=ADR-0070", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestADR_History_NilAdapter(t *testing.T) {
	h := ADRHistoryHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/adr/history?id=ADR-0070", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
}

func TestADR_Accept_Happy(t *testing.T) {
	fake := &fakeADRIndex{}
	h := ADRAccept(fake)
	body := map[string]any{"id": "ADR-0070", "reason": "operator decision"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/adr/accept", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status: got %d, want 204", w.Code)
	}
	if len(fake.acceptArgs) != 1 {
		t.Fatalf("acceptArgs: %v", fake.acceptArgs)
	}
	if fake.acceptArgs[0].ID != "ADR-0070" || fake.acceptArgs[0].Reason != "operator decision" {
		t.Errorf("args: %+v", fake.acceptArgs[0])
	}
}

func TestADR_Accept_MissingReason(t *testing.T) {
	fake := &fakeADRIndex{}
	h := ADRAccept(fake)
	body := map[string]any{"id": "ADR-0070"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/adr/accept", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400 (inv-zen-146)", w.Code)
	}
}

func TestADR_Accept_MissingID(t *testing.T) {
	fake := &fakeADRIndex{}
	h := ADRAccept(fake)
	body := map[string]any{"reason": "approved"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/adr/accept", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestADR_Accept_AdapterError(t *testing.T) {
	fake := &fakeADRIndex{transitionErr: errors.New("accept fail")}
	h := ADRAccept(fake)
	body := map[string]any{"id": "ADR-0070", "reason": "approved"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/adr/accept", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestADR_Accept_NilAdapter(t *testing.T) {
	h := ADRAccept(nil)
	body := map[string]any{"id": "ADR-0070", "reason": "approved"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/adr/accept", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
}

func TestADR_Reject_Happy(t *testing.T) {
	fake := &fakeADRIndex{}
	h := ADRReject(fake)
	body := map[string]any{"id": "ADR-0070", "reason": "alternate path chosen"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/adr/reject", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status: got %d, want 204", w.Code)
	}
}

func TestADR_Reject_MissingReason(t *testing.T) {
	fake := &fakeADRIndex{}
	h := ADRReject(fake)
	body := map[string]any{"id": "ADR-0070"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/adr/reject", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400 (inv-zen-146)", w.Code)
	}
}

func TestADR_Reject_NilAdapter(t *testing.T) {
	h := ADRReject(nil)
	body := map[string]any{"id": "ADR-0070", "reason": "rejected"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/adr/reject", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
}

func TestADR_Supersede_Happy(t *testing.T) {
	fake := &fakeADRIndex{}
	h := ADRSupersede(fake)
	body := map[string]any{"old_id": "ADR-0001", "new_id": "ADR-0070", "reason": "v2 ships"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/adr/supersede", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status: got %d, want 204", w.Code)
	}
	if fake.supersedeArgs[0].OldID != "ADR-0001" || fake.supersedeArgs[0].NewID != "ADR-0070" {
		t.Errorf("supersede dispatched: %+v", fake.supersedeArgs[0])
	}
}

func TestADR_Supersede_MissingReason(t *testing.T) {
	fake := &fakeADRIndex{}
	h := ADRSupersede(fake)
	body := map[string]any{"old_id": "ADR-0001", "new_id": "ADR-0070"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/adr/supersede", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400 (inv-zen-146)", w.Code)
	}
}

func TestADR_Supersede_MissingOldID(t *testing.T) {
	fake := &fakeADRIndex{}
	h := ADRSupersede(fake)
	body := map[string]any{"new_id": "ADR-0070", "reason": "v2 ships"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/adr/supersede", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestADR_Supersede_AdapterError(t *testing.T) {
	fake := &fakeADRIndex{transitionErr: errors.New("supersede fail")}
	h := ADRSupersede(fake)
	body := map[string]any{"old_id": "ADR-0001", "new_id": "ADR-0070", "reason": "v2"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/adr/supersede", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestADR_Supersede_NilAdapter(t *testing.T) {
	h := ADRSupersede(nil)
	body := map[string]any{"old_id": "ADR-0001", "new_id": "ADR-0070", "reason": "v2"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/adr/supersede", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
}

func TestADR_Index_DryRun(t *testing.T) {
	fake := &fakeADRIndex{
		indexResult: ADRManifest{
			GeneratedAt: 100,
			ADRCount:    70,
			Manifest:    `{"adrs":[]}`,
		},
	}
	h := ADRIndex(fake)
	body := map[string]any{"check": true}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/adr/index", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp ADRManifest
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ADRCount != 70 {
		t.Errorf("count: %d", resp.ADRCount)
	}
}

func TestADR_Index_Write(t *testing.T) {
	fake := &fakeADRIndex{
		indexResult: ADRManifest{
			GeneratedAt: 200,
			ADRCount:    5,
			Manifest:    `{"schema_version":1}`,
			Graph:       `{"schema_version":1}`,
		},
	}
	h := ADRIndex(fake)
	body := map[string]any{"check": false}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/adr/index", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var resp ADRManifest
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ADRCount != 5 {
		t.Errorf("count: %d", resp.ADRCount)
	}
}

func TestADR_Index_AdapterError(t *testing.T) {
	fake := &fakeADRIndex{indexErr: errors.New("index fail")}
	h := ADRIndex(fake)
	body := map[string]any{}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/adr/index", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestADR_Index_NilAdapter(t *testing.T) {
	h := ADRIndex(nil)
	body := map[string]any{"check": true}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/adr/index", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
}

func TestADR_Index_EmptyBody(t *testing.T) {
	fake := &fakeADRIndex{
		indexResult: ADRManifest{ADRCount: 3},
	}
	h := ADRIndex(fake)

	req := httptest.NewRequest(http.MethodPost, "/v1/adr/index", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
}
