package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func aggStubServer(t *testing.T, mux *http.ServeMux) (*httptest.Server, *Client) {
	t.Helper()
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	c := NewWithBaseURL(ts.URL)
	return ts, c
}

func TestClientAggQuery_HappyPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/knowledge/aggregator/query", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "want POST", http.StatusMethodNotAllowed)
			return
		}
		var req AggQueryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp := AggQueryResponse{
			Results: []AggQueryResultRow{{
				NoteID:    "note-1",
				Title:     "Test Note",
				ProjectID: "proj-abc",
				Score:     0.9,
				Source:    "fts",
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	_, c := aggStubServer(t, mux)

	rows, err := c.AggQuery(context.Background(), AggQueryRequest{Text: "test query"})
	if err != nil {
		t.Fatalf("AggQuery: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].NoteID != "note-1" {
		t.Errorf("NoteID = %q; want note-1", rows[0].NoteID)
	}
}

func TestClientAggQuery_NonNilOnEmpty(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/knowledge/aggregator/query", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(AggQueryResponse{Results: nil})
	})
	_, c := aggStubServer(t, mux)

	rows, err := c.AggQuery(context.Background(), AggQueryRequest{Text: "x"})
	if err != nil {
		t.Fatalf("AggQuery: %v", err)
	}
	if rows == nil {
		t.Error("rows is nil; want empty non-nil slice")
	}
}

func TestClientAggQuery_NonOK_HTTPError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/knowledge/aggregator/query", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "backend down", http.StatusInternalServerError)
	})
	_, c := aggStubServer(t, mux)

	_, err := c.AggQuery(context.Background(), AggQueryRequest{Text: "x"})
	if err == nil {
		t.Fatal("expected error on 500, got nil")
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected *HTTPError, got %T: %v", err, err)
	}
	if httpErr.Status != 500 {
		t.Errorf("HTTPError.Status = %d; want 500", httpErr.Status)
	}
}

func TestClientAggPromote_HappyPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/knowledge/aggregator/promote", func(w http.ResponseWriter, r *http.Request) {
		var req AggPromoteRequest
		json.NewDecoder(r.Body).Decode(&req)
		resp := AggPromoteResponse{
			NoteID:           req.NoteID,
			AuditChainAnchor: "2026_05:evt-1:hash1",
			PromotedAt:       time.Now(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	_, c := aggStubServer(t, mux)

	resp, err := c.AggPromote(context.Background(), AggPromoteRequest{
		NoteID:     "note-abc",
		ProjectID:  "proj-xyz",
		OperatorID: "op-1",
		Reason:     "cross-project ref",
	})
	if err != nil {
		t.Fatalf("AggPromote: %v", err)
	}
	if resp.NoteID != "note-abc" {
		t.Errorf("NoteID = %q; want note-abc", resp.NoteID)
	}
	if resp.AuditChainAnchor == "" {
		t.Error("AuditChainAnchor must not be empty")
	}
}

func TestClientAggPromote_EmptyReason_400(t *testing.T) {

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/knowledge/aggregator/promote", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "reason required", http.StatusBadRequest)
	})
	_, c := aggStubServer(t, mux)

	_, err := c.AggPromote(context.Background(), AggPromoteRequest{
		NoteID:     "note-abc",
		ProjectID:  "proj-xyz",
		OperatorID: "op-1",
		Reason:     "",
	})
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected *HTTPError, got %T: %v", err, err)
	}
	if httpErr.Status != 400 {
		t.Errorf("HTTPError.Status = %d; want 400", httpErr.Status)
	}
}

func TestClientAggUnpromote_HappyPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/knowledge/aggregator/unpromote", func(w http.ResponseWriter, r *http.Request) {
		resp := AggUnpromoteResponse{
			NoteID:       "note-abc",
			UnpromotedAt: time.Now(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	_, c := aggStubServer(t, mux)

	resp, err := c.AggUnpromote(context.Background(), AggUnpromoteRequest{
		NoteID:     "note-abc",
		OperatorID: "op-1",
		Reason:     "stale ref",
	})
	if err != nil {
		t.Fatalf("AggUnpromote: %v", err)
	}
	if resp.NoteID != "note-abc" {
		t.Errorf("NoteID = %q; want note-abc", resp.NoteID)
	}
}

func TestClientAggUnpromote_EmptyReason_400(t *testing.T) {

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/knowledge/aggregator/unpromote", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "reason required", http.StatusBadRequest)
	})
	_, c := aggStubServer(t, mux)

	_, err := c.AggUnpromote(context.Background(), AggUnpromoteRequest{
		NoteID:     "note-abc",
		OperatorID: "op-1",
		Reason:     "",
	})
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected *HTTPError, got %T: %v", err, err)
	}
	if httpErr.Status != 400 {
		t.Errorf("HTTPError.Status = %d; want 400", httpErr.Status)
	}
}

func TestClientAggList_HappyPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/knowledge/aggregator/list", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "want GET", http.StatusMethodNotAllowed)
			return
		}
		resp := AggListResponse{
			Notes: []AggPinNote{
				{NoteID: "n1", Title: "First Note", ProjectID: "proj-abc"},
				{NoteID: "n2", Title: "Second Note", ProjectID: "proj-abc"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	_, c := aggStubServer(t, mux)

	notes, err := c.AggList(context.Background(), "", false)
	if err != nil {
		t.Fatalf("AggList: %v", err)
	}
	if len(notes) != 2 {
		t.Fatalf("expected 2 notes, got %d", len(notes))
	}
}

func TestClientAggList_ProjectFilter(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/knowledge/aggregator/list", func(w http.ResponseWriter, r *http.Request) {
		got := r.URL.Query().Get("project_id")
		if got != "proj-abc" {
			http.Error(w, "wrong project_id: "+got, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(AggListResponse{Notes: []AggPinNote{}})
	})
	_, c := aggStubServer(t, mux)

	notes, err := c.AggList(context.Background(), "proj-abc", false)
	if err != nil {
		t.Fatalf("AggList with project filter: %v", err)
	}
	if notes == nil {
		t.Error("notes must not be nil on empty result")
	}
}

func TestClientAggList_NonNilOnNull(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/knowledge/aggregator/list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(AggListResponse{Notes: nil})
	})
	_, c := aggStubServer(t, mux)

	notes, err := c.AggList(context.Background(), "", false)
	if err != nil {
		t.Fatalf("AggList: %v", err)
	}
	if notes == nil {
		t.Error("notes is nil; want empty non-nil slice")
	}
}

func TestClientAggRebuild_HappyPath_202(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/knowledge/aggregator/rebuild", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(AggRebuildResponse{Status: "queued"})
	})
	_, c := aggStubServer(t, mux)

	if err := c.AggRebuild(context.Background(), ""); err != nil {
		t.Fatalf("AggRebuild: %v", err)
	}
}

func TestClientAggRebuild_ProjectIDForwarded(t *testing.T) {
	var gotProjectID string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/knowledge/aggregator/rebuild", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ProjectID string `json:"project_id"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		gotProjectID = req.ProjectID
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(AggRebuildResponse{Status: "queued", ProjectID: req.ProjectID})
	})
	_, c := aggStubServer(t, mux)

	if err := c.AggRebuild(context.Background(), "proj-abc"); err != nil {
		t.Fatalf("AggRebuild: %v", err)
	}
	if gotProjectID != "proj-abc" {
		t.Errorf("project_id forwarded = %q; want proj-abc", gotProjectID)
	}
}

func TestClientAggRebuild_NonOK_HTTPError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/knowledge/aggregator/rebuild", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	})
	_, c := aggStubServer(t, mux)

	err := c.AggRebuild(context.Background(), "proj-abc")
	if err == nil {
		t.Fatal("expected error on 503, got nil")
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected *HTTPError, got %T: %v", err, err)
	}
	if httpErr.Status != 503 {
		t.Errorf("HTTPError.Status = %d; want 503", httpErr.Status)
	}
}

func TestClientAggList_NonOK_HTTPError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/knowledge/aggregator/list", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	})
	_, c := aggStubServer(t, mux)

	_, err := c.AggList(context.Background(), "", false)
	if err == nil {
		t.Fatal("expected error on 500, got nil")
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected *HTTPError, got %T: %v", err, err)
	}
	if httpErr.Status != 500 {
		t.Errorf("HTTPError.Status = %d; want 500", httpErr.Status)
	}
}
