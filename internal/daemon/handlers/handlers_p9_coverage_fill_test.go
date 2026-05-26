package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestADR_Propose_InvalidJSON(t *testing.T) {
	fake := &fakeADRIndex{}
	h := ADRPropose(fake)
	req := httptest.NewRequest(http.MethodPost, "/v1/adr/propose",
		bytes.NewBufferString("{not valid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestADR_List_LimitParam(t *testing.T) {
	fake := &fakeADRIndex{}
	h := ADRList(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/adr/list?limit=50", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	if fake.listArgs[0].Limit != 50 {
		t.Errorf("limit: got %d, want 50", fake.listArgs[0].Limit)
	}
}

func TestADR_List_NilRowsCoercion(t *testing.T) {

	fake := &fakeADRIndex{}
	h := ADRList(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/adr/list", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !bytes.Contains([]byte(body), []byte(`"items":[]`)) {
		t.Errorf("nil rows should coerce to []: %s", body)
	}
}

func TestADR_History_NilRowsCoercion(t *testing.T) {
	fake := &fakeADRIndex{}
	h := ADRHistoryHandler(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/adr/history?id=ADR-0001", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !bytes.Contains([]byte(body), []byte(`"items":[]`)) {
		t.Errorf("nil rows should coerce to []: %s", body)
	}
}

func TestADR_Accept_InvalidJSON(t *testing.T) {
	fake := &fakeADRIndex{}
	h := ADRAccept(fake)
	req := httptest.NewRequest(http.MethodPost, "/v1/adr/accept",
		bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestADR_Reject_InvalidJSON(t *testing.T) {
	fake := &fakeADRIndex{}
	h := ADRReject(fake)
	req := httptest.NewRequest(http.MethodPost, "/v1/adr/reject",
		bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestADR_Supersede_InvalidJSON(t *testing.T) {
	fake := &fakeADRIndex{}
	h := ADRSupersede(fake)
	req := httptest.NewRequest(http.MethodPost, "/v1/adr/supersede",
		bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestAuditP9_History_LimitParam(t *testing.T) {
	fake := &fakeAuditAdapterP9{}
	h := AuditP9History(fake)
	req := httptest.NewRequest(http.MethodGet,
		"/v1/audit-chain/history?project_id=p1&limit=200", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	if len(fake.historyArgs) == 0 {
		t.Fatal("historyArgs empty")
	}
	if fake.historyArgs[0].Limit != 200 {
		t.Errorf("limit: got %d, want 200", fake.historyArgs[0].Limit)
	}
}

func TestAuditP9_History_NilRowsCoercion(t *testing.T) {
	fake := &fakeAuditAdapterP9{}
	h := AuditP9History(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit-chain/history?project_id=p1", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !bytes.Contains([]byte(body), []byte(`"items":[]`)) {
		t.Errorf("nil rows should coerce to []: %s", body)
	}
}

func TestAuditP9_PartitionSeals_NilRowsCoercion(t *testing.T) {
	fake := &fakeAuditAdapterP9{}
	h := AuditP9PartitionSeals(fake)
	req := httptest.NewRequest(http.MethodGet,
		"/v1/audit-chain/partition-seals?project_id=p1", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !bytes.Contains([]byte(body), []byte(`"items":[]`)) {
		t.Errorf("nil rows should coerce to []: %s", body)
	}
}

func TestAuditP9_Recover_InvalidJSON(t *testing.T) {
	fake := &fakeAuditAdapterP9{}
	h := AuditP9Recover(fake)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/recover",
		bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestAuditP9_Checkpoint_InvalidJSON(t *testing.T) {
	fake := &fakeAuditAdapterP9{}
	h := AuditP9Checkpoint(fake)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/checkpoint",
		bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestAuditP9_ColdArchiveList_NilRowsCoercion(t *testing.T) {
	fake := &fakeAuditAdapterP9{}
	h := AuditP9ColdArchiveList(fake)
	req := httptest.NewRequest(http.MethodGet,
		"/v1/audit-chain/cold-archive/list?project_id=p1", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !bytes.Contains([]byte(body), []byte(`"items":[]`)) {
		t.Errorf("nil rows should coerce to []: %s", body)
	}
}

func TestAuditP9_ColdArchiveRestore_InvalidJSON(t *testing.T) {
	fake := &fakeAuditAdapterP9{}
	h := AuditP9ColdArchiveRestore(fake)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/cold-archive/restore",
		bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestAuditP9_WitnessRotate_InvalidJSON(t *testing.T) {
	fake := &fakeAuditAdapterP9{}
	h := AuditP9WitnessRotate(fake)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/witness/rotate",
		bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestAuditP9_ConfigureS3_InvalidJSON(t *testing.T) {
	fake := &fakeAuditAdapterP9{}
	h := AuditP9ConfigureS3(fake)
	req := httptest.NewRequest(http.MethodPost, "/v1/audit-chain/configure-s3",
		bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestKnowledgeP9_Promote_InvalidJSON(t *testing.T) {
	fake := &fakeKnowledgeAdapterP9{}
	h := KnowledgeP9Promote(fake)
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/promote",
		bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestKnowledgeP9_Unpromote_InvalidJSON_Fill(t *testing.T) {
	fake := &fakeKnowledgeAdapterP9{}
	h := KnowledgeP9Unpromote(fake)
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/unpromote",
		bytes.NewBufferString("{bad json here"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestKnowledgeP9_Unpromote_MissingNoteID(t *testing.T) {
	fake := &fakeKnowledgeAdapterP9{}
	h := KnowledgeP9Unpromote(fake)
	body := map[string]any{"reason": "r"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/unpromote", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400 (note_id required)", w.Code)
	}
}

func TestKnowledgeP9_Rebuild_InvalidJSON(t *testing.T) {
	fake := &fakeKnowledgeAdapterP9{}
	h := KnowledgeP9Rebuild(fake)
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/rebuild",
		bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestResearchP9_History_NilRowsCoercion(t *testing.T) {
	fake := &fakeResearchStoreP9{}
	h := ResearchP9History(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/research/history", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !bytes.Contains([]byte(body), []byte(`"items":[]`)) {
		t.Errorf("nil rows should coerce to []: %s", body)
	}
}

func TestResearchP9_CacheInvalidate_InvalidJSON(t *testing.T) {
	fake := &fakeResearchStoreP9{}
	h := ResearchP9CacheInvalidate(fake)
	req := httptest.NewRequest(http.MethodPost, "/v1/research/cache/invalidate",
		bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestResearchP9_CacheList_NilRowsCoercion(t *testing.T) {
	fake := &fakeResearchStoreP9{}
	h := ResearchP9CacheList(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/research/cache/list", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !bytes.Contains([]byte(body), []byte(`"items":[]`)) {
		t.Errorf("nil rows should coerce to []: %s", body)
	}
}

func TestState_Pin_InvalidJSON(t *testing.T) {
	fake := &fakeStateService{}
	h := StatePin(fake)
	req := httptest.NewRequest(http.MethodPost, "/v1/state/pin",
		bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestState_Pin_MissingValue(t *testing.T) {
	fake := &fakeStateService{}
	h := StatePin(fake)
	body := map[string]any{"field": "x", "reason": "r"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/state/pin", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400 (value required)", w.Code)
	}
}

func TestState_History_NilRowsCoercion(t *testing.T) {

	fake := &fakeStateService{}
	h := StateHistory(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/state/history", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !bytes.Contains([]byte(body), []byte(`"items":[]`)) {
		t.Errorf("nil rows should coerce to []: %s", body)
	}
}
