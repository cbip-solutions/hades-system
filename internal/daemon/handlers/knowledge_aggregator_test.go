package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeAggregatorService struct {
	queryResults []AggQueryResult
	queryErr     error

	promoteResult *AggPromoteResult
	promoteErr    error

	unpromoteResult *AggUnpromoteResult
	unpromoteErr    error

	listNotes []AggPinNote
	listErr   error

	rebuildErr error
}

func (f *fakeAggregatorService) AggQueryFTS(_ context.Context, _ string, _ int) ([]AggQueryResult, error) {
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	if f.queryResults == nil {
		return []AggQueryResult{}, nil
	}
	return f.queryResults, nil
}

func (f *fakeAggregatorService) AggPromote(_ context.Context, _, _, _, _ string) (*AggPromoteResult, error) {
	return f.promoteResult, f.promoteErr
}

func (f *fakeAggregatorService) AggUnpromote(_ context.Context, _, _, _ string) (*AggUnpromoteResult, error) {
	return f.unpromoteResult, f.unpromoteErr
}

func (f *fakeAggregatorService) AggListPins(_ context.Context, _ string) ([]AggPinNote, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if f.listNotes == nil {
		return []AggPinNote{}, nil
	}
	return f.listNotes, nil
}

func (f *fakeAggregatorService) AggEnqueueRebuild(_ context.Context, _ string) error {
	return f.rebuildErr
}

func newFakeHandlers(fake *fakeAggregatorService) *KnowledgeAggregatorHandlers {
	return &KnowledgeAggregatorHandlers{Agg: fake}
}

func aggPost(handler http.HandlerFunc, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

func TestHandleAggQuery_BadJSON_400(t *testing.T) {
	h := newFakeHandlers(&fakeAggregatorService{})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("not-json"))
	w := httptest.NewRecorder()
	h.handleAggQuery(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("code = %d; want 400", w.Code)
	}
}

func TestHandleAggQuery_EmptyText_400(t *testing.T) {
	h := newFakeHandlers(&fakeAggregatorService{})
	w := aggPost(h.handleAggQuery, AggQueryRequest{Text: ""})
	if w.Code != http.StatusBadRequest {
		t.Errorf("code = %d; want 400", w.Code)
	}
}

func TestHandleAggQuery_HappyPath_200(t *testing.T) {
	fake := &fakeAggregatorService{
		queryResults: []AggQueryResult{
			{NoteID: "note-1", Title: "Test Note", Score: 0.9, Source: "fts"},
		},
	}
	h := newFakeHandlers(fake)
	w := aggPost(h.handleAggQuery, AggQueryRequest{Text: "test"})
	if w.Code != http.StatusOK {
		t.Errorf("code = %d; want 200 (body: %s)", w.Code, w.Body.String())
	}
	var resp AggQueryResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Errorf("results = %d; want 1", len(resp.Results))
	}
	if resp.Results[0].NoteID != "note-1" {
		t.Errorf("NoteID = %q; want note-1", resp.Results[0].NoteID)
	}
}

func TestHandleAggQuery_EmptyResults_NonNil(t *testing.T) {
	h := newFakeHandlers(&fakeAggregatorService{})
	w := aggPost(h.handleAggQuery, AggQueryRequest{Text: "nothing"})
	if w.Code != http.StatusOK {
		t.Errorf("code = %d; want 200", w.Code)
	}
	var resp AggQueryResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Results == nil {
		t.Error("Results must not be nil (empty array on no matches)")
	}
}

func TestHandleAggPromote_BadJSON_400(t *testing.T) {
	h := newFakeHandlers(&fakeAggregatorService{})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("oops"))
	w := httptest.NewRecorder()
	h.handleAggPromote(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("code = %d; want 400", w.Code)
	}
}

func TestHandleAggPromote_ErrPromoteReasonRequired_400(t *testing.T) {

	fake := &fakeAggregatorService{promoteErr: ErrAggPromoteReasonRequired}
	h := newFakeHandlers(fake)
	w := aggPost(h.handleAggPromote, AggPromoteRequest{
		NoteID:     "note-abc",
		ProjectID:  "proj-xyz",
		OperatorID: "op-1",
		Reason:     "",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("code = %d; want 400 (inv-zen-146)", w.Code)
	}
}

func TestHandleAggPromote_HappyPath_200(t *testing.T) {
	fake := &fakeAggregatorService{
		promoteResult: &AggPromoteResult{
			NoteID:           "note-abc",
			AuditChainAnchor: "2026_05:evt-1:hash1",
			PromotedAt:       time.Now(),
		},
	}
	h := newFakeHandlers(fake)
	w := aggPost(h.handleAggPromote, AggPromoteRequest{
		NoteID:     "note-abc",
		ProjectID:  "proj-xyz",
		OperatorID: "op-1",
		Reason:     "cross-project ref",
	})
	if w.Code != http.StatusOK {
		t.Errorf("code = %d; want 200 (body: %s)", w.Code, w.Body.String())
	}
	var resp AggPromoteResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.NoteID != "note-abc" {
		t.Errorf("NoteID = %q; want note-abc", resp.NoteID)
	}
}

func TestHandleAggUnpromote_ErrPromoteReasonRequired_400(t *testing.T) {

	fake := &fakeAggregatorService{unpromoteErr: ErrAggPromoteReasonRequired}
	h := newFakeHandlers(fake)
	w := aggPost(h.handleAggUnpromote, AggUnpromoteRequest{
		NoteID:     "note-abc",
		OperatorID: "op-1",
		Reason:     "",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("code = %d; want 400 (inv-zen-146 symmetric)", w.Code)
	}
}

func TestHandleAggUnpromote_HappyPath_200(t *testing.T) {
	fake := &fakeAggregatorService{
		unpromoteResult: &AggUnpromoteResult{
			NoteID:       "note-abc",
			UnpromotedAt: time.Now(),
			Idempotent:   false,
		},
	}
	h := newFakeHandlers(fake)
	w := aggPost(h.handleAggUnpromote, AggUnpromoteRequest{
		NoteID:     "note-abc",
		OperatorID: "op-1",
		Reason:     "stale reference",
	})
	if w.Code != http.StatusOK {
		t.Errorf("code = %d; want 200 (body: %s)", w.Code, w.Body.String())
	}
	var resp AggUnpromoteResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.NoteID != "note-abc" {
		t.Errorf("NoteID = %q; want note-abc", resp.NoteID)
	}
}

func TestHandleAggList_HappyPath_200(t *testing.T) {
	fake := &fakeAggregatorService{
		listNotes: []AggPinNote{
			{NoteID: "n1", Title: "First Note", ProjectID: "proj-abc"},
			{NoteID: "n2", Title: "Second Note", ProjectID: "proj-abc"},
		},
	}
	h := newFakeHandlers(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/knowledge/aggregator/list", nil)
	w := httptest.NewRecorder()
	h.handleAggList(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("code = %d; want 200 (body: %s)", w.Code, w.Body.String())
	}
	var resp AggListResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Notes) != 2 {
		t.Errorf("notes = %d; want 2", len(resp.Notes))
	}
}

func TestHandleAggList_EmptyNotes_NonNil(t *testing.T) {
	h := newFakeHandlers(&fakeAggregatorService{})
	req := httptest.NewRequest(http.MethodGet, "/v1/knowledge/aggregator/list", nil)
	w := httptest.NewRecorder()
	h.handleAggList(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("code = %d; want 200", w.Code)
	}
	var resp AggListResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Notes == nil {
		t.Error("Notes must not be nil on empty result")
	}
}

func TestHandleAggRebuild_WorkerNotStarted_202(t *testing.T) {

	fake := &fakeAggregatorService{rebuildErr: ErrAggWorkerNotStarted}
	h := newFakeHandlers(fake)
	w := aggPost(h.handleAggRebuild, map[string]string{})
	if w.Code != http.StatusAccepted {
		t.Errorf("code = %d; want 202 (forward-compat rebuild)", w.Code)
	}
	var resp AggRebuildResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Status != "queued" {
		t.Errorf("status = %q; want queued", resp.Status)
	}
}

func TestHandleAggRebuild_WorkerStarted_202(t *testing.T) {

	fake := &fakeAggregatorService{rebuildErr: nil}
	h := newFakeHandlers(fake)
	w := aggPost(h.handleAggRebuild, map[string]string{"project_id": "proj-abc"})
	if w.Code != http.StatusAccepted {
		t.Errorf("code = %d; want 202", w.Code)
	}
}

func TestRegisterKnowledgeAggregatorRoutes_MountsAllFive(t *testing.T) {
	h := newFakeHandlers(&fakeAggregatorService{
		promoteResult:   &AggPromoteResult{NoteID: "x", PromotedAt: time.Now()},
		unpromoteResult: &AggUnpromoteResult{NoteID: "x", UnpromotedAt: time.Now()},
	})
	mux := http.NewServeMux()
	RegisterKnowledgeAggregatorRoutes(mux, h)

	routes := []struct {
		method string
		path   string
	}{
		{"POST", "/v1/knowledge/aggregator/query"},
		{"POST", "/v1/knowledge/aggregator/promote"},
		{"POST", "/v1/knowledge/aggregator/unpromote"},
		{"GET", "/v1/knowledge/aggregator/list"},
		{"POST", "/v1/knowledge/aggregator/rebuild"},
	}
	for _, r := range routes {
		req := httptest.NewRequest(r.method, r.path, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("route %s %s not mounted (got 404)", r.method, r.path)
		}
	}
}

func TestHandleAggPromote_InvZen146StringMatch_400(t *testing.T) {
	customErr := &invZen146Error{}
	fake := &fakeAggregatorService{promoteErr: customErr}
	h := newFakeHandlers(fake)
	w := aggPost(h.handleAggPromote, AggPromoteRequest{
		NoteID:     "note-abc",
		ProjectID:  "proj-xyz",
		OperatorID: "op-1",
		Reason:     "",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("code = %d; want 400 (inv-zen-146 string match)", w.Code)
	}
}

type invZen146Error struct{}

func (*invZen146Error) Error() string {
	return "aggregator: promote reason required (inv-zen-146)"
}

func TestHandleAggQuery_LimitClamped(t *testing.T) {

	h := newFakeHandlers(&fakeAggregatorService{})
	w := aggPost(h.handleAggQuery, AggQueryRequest{Text: "test", Limit: 999})
	if w.Code != http.StatusOK {
		t.Errorf("code = %d; want 200 (limit clamped)", w.Code)
	}
}

func TestHandleAggQuery_ServiceError_500(t *testing.T) {
	fake := &fakeAggregatorService{queryErr: errors.New("database error")}
	h := newFakeHandlers(fake)
	w := aggPost(h.handleAggQuery, AggQueryRequest{Text: "test"})
	if w.Code != http.StatusInternalServerError {
		t.Errorf("code = %d; want 500", w.Code)
	}
}

func TestHandleAggPromote_ServiceError_500(t *testing.T) {
	fake := &fakeAggregatorService{promoteErr: errors.New("promote internal error")}
	h := newFakeHandlers(fake)
	w := aggPost(h.handleAggPromote, AggPromoteRequest{
		NoteID: "note-1", ProjectID: "p1", OperatorID: "op1", Reason: "reason",
	})
	if w.Code != http.StatusInternalServerError {
		t.Errorf("code = %d; want 500", w.Code)
	}
}

func TestHandleAggUnpromote_BadJSON_400(t *testing.T) {
	h := newFakeHandlers(&fakeAggregatorService{})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("{bad"))
	w := httptest.NewRecorder()
	h.handleAggUnpromote(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("code = %d; want 400", w.Code)
	}
}

func TestHandleAggUnpromote_ServiceError_500(t *testing.T) {
	fake := &fakeAggregatorService{unpromoteErr: errors.New("unpromote internal error")}
	h := newFakeHandlers(fake)
	w := aggPost(h.handleAggUnpromote, AggUnpromoteRequest{
		NoteID: "note-1", OperatorID: "op1", Reason: "reason",
	})
	if w.Code != http.StatusInternalServerError {
		t.Errorf("code = %d; want 500", w.Code)
	}
}

func TestHandleAggList_ServiceError_500(t *testing.T) {
	fake := &fakeAggregatorService{listErr: errors.New("list internal error")}
	h := newFakeHandlers(fake)
	req := httptest.NewRequest(http.MethodGet, "/v1/knowledge/aggregator/list", nil)
	w := httptest.NewRecorder()
	h.handleAggList(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("code = %d; want 500", w.Code)
	}
}

func TestHandleAggRebuild_NonWorkerError_500(t *testing.T) {

	fake := &fakeAggregatorService{rebuildErr: errors.New("unexpected rebuild error")}
	h := newFakeHandlers(fake)
	w := aggPost(h.handleAggRebuild, map[string]string{})
	if w.Code != http.StatusInternalServerError {
		t.Errorf("code = %d; want 500 (non-worker error)", w.Code)
	}
}
