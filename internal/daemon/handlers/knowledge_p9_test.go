package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type fakeKnowledgeAdapterP9 struct {
	mu sync.Mutex

	queryArgs    []KnowledgeQueryReqP9
	queryResults [][]KnowledgeResultP9
	queryErr     error

	promoteArgs []promoteCallArgs
	promoteErr  error

	unpromoteArgs []promoteCallArgs
	unpromoteErr  error

	listArgs    []listCallArgs
	listResults [][]KnowledgeNoteP9
	listErr     error

	rebuildArgs    []string
	rebuildResults []KnowledgeRebuildRespP9
	rebuildErr     error
}

type promoteCallArgs struct {
	NoteID, Reason, OperatorID string
}

type listCallArgs struct {
	ProjectID  string
	PinnedOnly bool
}

func (f *fakeKnowledgeAdapterP9) Query(_ context.Context, req KnowledgeQueryReqP9) ([]KnowledgeResultP9, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queryArgs = append(f.queryArgs, req)
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	if len(f.queryResults) == 0 {
		return nil, nil
	}
	r := f.queryResults[0]
	f.queryResults = f.queryResults[1:]
	return r, nil
}

func (f *fakeKnowledgeAdapterP9) Promote(_ context.Context, noteID, reason, operatorID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.promoteArgs = append(f.promoteArgs, promoteCallArgs{noteID, reason, operatorID})
	return f.promoteErr
}

func (f *fakeKnowledgeAdapterP9) Unpromote(_ context.Context, noteID, reason, operatorID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unpromoteArgs = append(f.unpromoteArgs, promoteCallArgs{noteID, reason, operatorID})
	return f.unpromoteErr
}

func (f *fakeKnowledgeAdapterP9) List(_ context.Context, projectID string, pinnedOnly bool) ([]KnowledgeNoteP9, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listArgs = append(f.listArgs, listCallArgs{projectID, pinnedOnly})
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

func (f *fakeKnowledgeAdapterP9) Rebuild(_ context.Context, projectID string) (KnowledgeRebuildRespP9, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rebuildArgs = append(f.rebuildArgs, projectID)
	if f.rebuildErr != nil {
		return KnowledgeRebuildRespP9{}, f.rebuildErr
	}
	if len(f.rebuildResults) == 0 {
		return KnowledgeRebuildRespP9{}, nil
	}
	r := f.rebuildResults[0]
	f.rebuildResults = f.rebuildResults[1:]
	return r, nil
}

func TestKnowledgeP9_Query_Federated(t *testing.T) {
	fake := &fakeKnowledgeAdapterP9{
		queryResults: [][]KnowledgeResultP9{{
			{NoteID: "internal-platform-x/M0", Score: 0.92, Snippet: "max-scope methodology"},
		}},
	}
	h := KnowledgeP9Query(fake)
	req := httptest.NewRequest(http.MethodGet,
		"/v1/knowledge/query?q=max-scope&scope=global&audit_chain=true", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Items []KnowledgeResultP9 `json:"items"`
		Count int                 `json:"count"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Count != 1 {
		t.Fatalf("count: %d", resp.Count)
	}
	got := fake.queryArgs[0]
	if got.Query != "max-scope" || got.Scope != "global" || !got.AuditChain {
		t.Errorf("dispatched: %+v", got)
	}
}

func TestKnowledgeP9_Query_MissingQ(t *testing.T) {
	fake := &fakeKnowledgeAdapterP9{}
	h := KnowledgeP9Query(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/knowledge/query?scope=project", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
	if len(fake.queryArgs) != 0 {
		t.Error("adapter must not be called when q is missing")
	}
}

func TestKnowledgeP9_Query_AdapterError(t *testing.T) {
	fake := &fakeKnowledgeAdapterP9{queryErr: errors.New("index unavailable")}
	h := KnowledgeP9Query(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/knowledge/query?q=test", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestKnowledgeP9_Query_NilAdapter(t *testing.T) {
	h := KnowledgeP9Query(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/knowledge/query?q=test", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
}

func TestKnowledgeP9_Query_LimitParam(t *testing.T) {
	fake := &fakeKnowledgeAdapterP9{}
	h := KnowledgeP9Query(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/knowledge/query?q=test&limit=10", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d", w.Code)
	}
	if fake.queryArgs[0].Limit != 10 {
		t.Errorf("limit: got %d, want 10", fake.queryArgs[0].Limit)
	}
}

func TestKnowledgeP9_Promote_OK(t *testing.T) {
	fake := &fakeKnowledgeAdapterP9{}
	h := KnowledgeP9Promote(fake)
	body := map[string]any{"note_id": "internal-platform-x/M0", "reason": "applies cross-project"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/promote", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status: got %d, want 204; body=%s", w.Code, w.Body.String())
	}
	if len(fake.promoteArgs) != 1 {
		t.Fatalf("dispatch count: %d", len(fake.promoteArgs))
	}
	if fake.promoteArgs[0].NoteID != "internal-platform-x/M0" {
		t.Errorf("note_id: %q", fake.promoteArgs[0].NoteID)
	}
	if fake.promoteArgs[0].Reason != "applies cross-project" {
		t.Errorf("reason: %q", fake.promoteArgs[0].Reason)
	}
}

func TestKnowledgeP9_Promote_MissingReason(t *testing.T) {

	fake := &fakeKnowledgeAdapterP9{}
	h := KnowledgeP9Promote(fake)
	body := map[string]any{"note_id": "x"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/promote", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400 (inv-zen-146)", w.Code)
	}
	if len(fake.promoteArgs) != 0 {
		t.Errorf("adapter must not be called when reason missing")
	}
}

func TestKnowledgeP9_Promote_WhitespaceReason(t *testing.T) {

	fake := &fakeKnowledgeAdapterP9{}
	h := KnowledgeP9Promote(fake)
	body := map[string]any{"note_id": "x", "reason": "   "}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/promote", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400 (inv-zen-146 whitespace)", w.Code)
	}
	if len(fake.promoteArgs) != 0 {
		t.Errorf("adapter must not be called when reason is whitespace-only")
	}
}

func TestKnowledgeP9_Promote_MissingNoteID(t *testing.T) {
	fake := &fakeKnowledgeAdapterP9{}
	h := KnowledgeP9Promote(fake)
	body := map[string]any{"reason": "some reason"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/promote", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestKnowledgeP9_Promote_AdapterError(t *testing.T) {
	fake := &fakeKnowledgeAdapterP9{promoteErr: errors.New("vault locked")}
	h := KnowledgeP9Promote(fake)
	body := map[string]any{"note_id": "x", "reason": "good reason"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/promote", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestKnowledgeP9_Promote_NilAdapter(t *testing.T) {
	h := KnowledgeP9Promote(nil)
	body := map[string]any{"note_id": "x", "reason": "r"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/promote", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
}

func TestKnowledgeP9_Unpromote_OK(t *testing.T) {
	fake := &fakeKnowledgeAdapterP9{}
	h := KnowledgeP9Unpromote(fake)
	body := map[string]any{"note_id": "internal-platform-x/M0", "reason": "no longer relevant"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/unpromote", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status: got %d", w.Code)
	}
	if len(fake.unpromoteArgs) != 1 || fake.unpromoteArgs[0].NoteID != "internal-platform-x/M0" {
		t.Errorf("dispatch: %+v", fake.unpromoteArgs)
	}
}

func TestKnowledgeP9_Unpromote_MissingReason(t *testing.T) {

	fake := &fakeKnowledgeAdapterP9{}
	h := KnowledgeP9Unpromote(fake)
	body := map[string]any{"note_id": "internal-platform-x/M0"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/unpromote", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400 (inv-zen-146)", w.Code)
	}
	if len(fake.unpromoteArgs) != 0 {
		t.Errorf("adapter must not be called")
	}
}

func TestKnowledgeP9_Unpromote_NilAdapter(t *testing.T) {
	h := KnowledgeP9Unpromote(nil)
	body := map[string]any{"note_id": "x", "reason": "r"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/unpromote", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
}

func TestKnowledgeP9_List_PinnedOnly(t *testing.T) {
	fake := &fakeKnowledgeAdapterP9{
		listResults: [][]KnowledgeNoteP9{{
			{NoteID: "p/n1", Pinned: true, Path: "vault/n1.md"},
		}},
	}
	h := KnowledgeP9List(fake)
	req := httptest.NewRequest(http.MethodGet,
		"/v1/knowledge/list?project_id=internal-platform-x&pinned_only=true", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d", w.Code)
	}
	if !fake.listArgs[0].PinnedOnly {
		t.Error("pinned_only must dispatch true")
	}
	if fake.listArgs[0].ProjectID != "internal-platform-x" {
		t.Errorf("project_id: %q", fake.listArgs[0].ProjectID)
	}
}

func TestKnowledgeP9_List_All(t *testing.T) {
	fake := &fakeKnowledgeAdapterP9{}
	h := KnowledgeP9List(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/knowledge/list?project_id=my-proj", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d", w.Code)
	}
	if fake.listArgs[0].PinnedOnly {
		t.Error("pinned_only must default false")
	}
}

func TestKnowledgeP9_List_AdapterError(t *testing.T) {
	fake := &fakeKnowledgeAdapterP9{listErr: errors.New("db error")}
	h := KnowledgeP9List(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/knowledge/list?project_id=x", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestKnowledgeP9_List_NilAdapter(t *testing.T) {
	h := KnowledgeP9List(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/knowledge/list?project_id=x", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
}

func TestKnowledgeP9_Rebuild(t *testing.T) {
	fake := &fakeKnowledgeAdapterP9{
		rebuildResults: []KnowledgeRebuildRespP9{{JobID: "rebuild-001", StartedAt: 100}},
	}
	h := KnowledgeP9Rebuild(fake)
	body := map[string]any{"project_id": "internal-platform-x"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/rebuild", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status: got %d, want 202", w.Code)
	}
	var resp KnowledgeRebuildRespP9
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.JobID != "rebuild-001" {
		t.Errorf("job_id: %q", resp.JobID)
	}
	if fake.rebuildArgs[0] != "internal-platform-x" {
		t.Errorf("project_id dispatched: %q", fake.rebuildArgs[0])
	}
}

func TestKnowledgeP9_Rebuild_MissingProjectID(t *testing.T) {
	fake := &fakeKnowledgeAdapterP9{}
	h := KnowledgeP9Rebuild(fake)
	body := map[string]any{}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/rebuild", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
	if len(fake.rebuildArgs) != 0 {
		t.Error("adapter must not be called when project_id missing")
	}
}

func TestKnowledgeP9_Rebuild_AdapterError(t *testing.T) {
	fake := &fakeKnowledgeAdapterP9{rebuildErr: errors.New("embed failed")}
	h := KnowledgeP9Rebuild(fake)
	body := map[string]any{"project_id": "x"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/rebuild", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestKnowledgeP9_Rebuild_NilAdapter(t *testing.T) {
	h := KnowledgeP9Rebuild(nil)
	body := map[string]any{"project_id": "x"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/rebuild", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
}

func TestKnowledgeP9_Unpromote_AdapterError(t *testing.T) {
	fake := &fakeKnowledgeAdapterP9{unpromoteErr: errors.New("vault locked")}
	h := KnowledgeP9Unpromote(fake)
	body := map[string]any{"note_id": "x", "reason": "good reason"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/unpromote", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", w.Code)
	}
}

func TestKnowledgeP9_Unpromote_InvalidJSON(t *testing.T) {
	fake := &fakeKnowledgeAdapterP9{}
	h := KnowledgeP9Unpromote(fake)
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/unpromote",
		strings.NewReader("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestKnowledgeP9_Unpromote_WhitespaceReason(t *testing.T) {
	fake := &fakeKnowledgeAdapterP9{}
	h := KnowledgeP9Unpromote(fake)
	body := map[string]any{"note_id": "x", "reason": strings.Repeat(" ", 5)}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/knowledge/unpromote", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400 (inv-zen-146 whitespace)", w.Code)
	}
}
