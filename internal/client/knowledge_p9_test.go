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

func startKnowledgeP9TestServer(t *testing.T, route string, handler func(w http.ResponseWriter, r *http.Request)) (*Client, *httptest.Server) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(route, handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return NewWithBaseURL(srv.URL), srv
}

func TestKnowledgeQuery(t *testing.T) {
	c, _ := startKnowledgeP9TestServer(t, "GET /v1/knowledge/query",
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("q") != "max-scope" {
				t.Errorf("q: %q", r.URL.Query().Get("q"))
			}
			json.NewEncoder(w).Encode(map[string]any{
				"items": []KnowledgeResult{{NoteID: "n1", Score: 0.9}},
				"count": 1,
			})
		})
	rows, err := c.KnowledgeQueryP9(context.Background(), KnowledgeQueryReq{Q: "max-scope"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("rows: %d", len(rows))
	}
	if rows[0].NoteID != "n1" {
		t.Errorf("note_id: %q", rows[0].NoteID)
	}
}

func TestKnowledgeQuery_AllParams(t *testing.T) {
	c, _ := startKnowledgeP9TestServer(t, "GET /v1/knowledge/query",
		func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if q.Get("scope") != "project" {
				t.Errorf("scope: %q", q.Get("scope"))
			}
			if q.Get("project_id") != "proj-1" {
				t.Errorf("project_id: %q", q.Get("project_id"))
			}
			if q.Get("audit_chain") != "true" {
				t.Errorf("audit_chain: %q", q.Get("audit_chain"))
			}
			if q.Get("limit") != "42" {
				t.Errorf("limit: %q", q.Get("limit"))
			}
			if q.Get("pinned_only") != "true" {
				t.Errorf("pinned_only: %q", q.Get("pinned_only"))
			}
			json.NewEncoder(w).Encode(map[string]any{
				"items": []KnowledgeResult{},
				"count": 0,
			})
		})
	rows, err := c.KnowledgeQueryP9(context.Background(), KnowledgeQueryReq{
		Q:          "test",
		Scope:      "project",
		ProjectID:  "proj-1",
		PinnedOnly: true,
		AuditChain: true,
		Limit:      42,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected empty; got %d", len(rows))
	}
}

func TestKnowledgeQuery_ServerError(t *testing.T) {
	c, _ := startKnowledgeP9TestServer(t, "GET /v1/knowledge/query",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"boom"}`))
		})
	_, err := c.KnowledgeQueryP9(context.Background(), KnowledgeQueryReq{Q: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != http.StatusInternalServerError {
		t.Errorf("unexpected error type/status: %v", err)
	}
}

func TestKnowledgeQuery_NilItemsBecomesEmpty(t *testing.T) {
	c, _ := startKnowledgeP9TestServer(t, "GET /v1/knowledge/query",
		func(w http.ResponseWriter, r *http.Request) {

			json.NewEncoder(w).Encode(map[string]any{"count": 0})
		})
	rows, err := c.KnowledgeQueryP9(context.Background(), KnowledgeQueryReq{Q: "x"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rows == nil {
		t.Error("expected non-nil empty slice")
	}
}

func TestKnowledgePromote(t *testing.T) {
	c, _ := startKnowledgeP9TestServer(t, "POST /v1/knowledge/promote",
		func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "applies cross") {
				t.Errorf("body: %s", string(body))
			}
			if !strings.Contains(string(body), "n1") {
				t.Errorf("note_id missing: %s", string(body))
			}
			w.WriteHeader(http.StatusNoContent)
		})
	err := c.KnowledgePromoteP9(context.Background(), "n1", "applies cross-project")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestKnowledgePromote_ServerError(t *testing.T) {
	c, _ := startKnowledgeP9TestServer(t, "POST /v1/knowledge/promote",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"reason required"}`))
		})
	err := c.KnowledgePromoteP9(context.Background(), "n1", "")
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != http.StatusBadRequest {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestKnowledgeUnpromote(t *testing.T) {
	c, _ := startKnowledgeP9TestServer(t, "POST /v1/knowledge/unpromote",
		func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "no longer relevant") {
				t.Errorf("body: %s", string(body))
			}
			w.WriteHeader(http.StatusNoContent)
		})
	if err := c.KnowledgeUnpromoteP9(context.Background(), "n1", "no longer relevant"); err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestKnowledgeUnpromote_ServerError(t *testing.T) {
	c, _ := startKnowledgeP9TestServer(t, "POST /v1/knowledge/unpromote",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"adapter offline"}`))
		})
	err := c.KnowledgeUnpromoteP9(context.Background(), "n1", "reason")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestKnowledgeList(t *testing.T) {
	c, _ := startKnowledgeP9TestServer(t, "GET /v1/knowledge/list",
		func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if q.Get("project_id") != "p" {
				t.Errorf("project_id: %q", q.Get("project_id"))
			}
			if q.Get("pinned_only") != "true" {
				t.Errorf("pinned_only: %q", q.Get("pinned_only"))
			}
			json.NewEncoder(w).Encode(map[string]any{
				"items": []KnowledgeNote{{NoteID: "n1", Pinned: true}},
				"count": 1,
			})
		})
	rows, err := c.KnowledgeListP9(context.Background(), "p", true)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rows) != 1 || !rows[0].Pinned {
		t.Errorf("rows: %+v", rows)
	}
}

func TestKnowledgeList_NoFilter(t *testing.T) {
	c, _ := startKnowledgeP9TestServer(t, "GET /v1/knowledge/list",
		func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()

			if q.Get("project_id") != "" {
				t.Errorf("unexpected project_id: %q", q.Get("project_id"))
			}
			if q.Get("pinned_only") != "" {
				t.Errorf("unexpected pinned_only: %q", q.Get("pinned_only"))
			}
			json.NewEncoder(w).Encode(map[string]any{
				"items": []KnowledgeNote{{NoteID: "n2"}, {NoteID: "n3"}},
				"count": 2,
			})
		})
	rows, err := c.KnowledgeListP9(context.Background(), "", false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("rows: %d", len(rows))
	}
}

func TestKnowledgeList_ServerError(t *testing.T) {
	c, _ := startKnowledgeP9TestServer(t, "GET /v1/knowledge/list",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error":"feature not configured"}`))
		})
	_, err := c.KnowledgeListP9(context.Background(), "", false)
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != http.StatusServiceUnavailable {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestKnowledgeRebuild(t *testing.T) {
	c, _ := startKnowledgeP9TestServer(t, "POST /v1/knowledge/rebuild",
		func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"p"`) {
				t.Errorf("body: %s", string(body))
			}
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(KnowledgeRebuildResp{JobID: "rebuild-1"})
		})
	res, err := c.KnowledgeRebuildP9(context.Background(), "p")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.JobID != "rebuild-1" {
		t.Errorf("job_id: %q", res.JobID)
	}
}

func TestKnowledgeRebuild_ServerError(t *testing.T) {
	c, _ := startKnowledgeP9TestServer(t, "POST /v1/knowledge/rebuild",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"project_id required"}`))
		})
	_, err := c.KnowledgeRebuildP9(context.Background(), "")
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != http.StatusBadRequest {
		t.Errorf("unexpected error: %v", err)
	}
}
