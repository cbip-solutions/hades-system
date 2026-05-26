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

func startADRTestServer(t *testing.T, route string, handler func(w http.ResponseWriter, r *http.Request)) (*Client, *httptest.Server) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(route, handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return NewWithBaseURL(srv.URL), srv
}

func TestADRPropose_OK(t *testing.T) {
	c, _ := startADRTestServer(t, "POST /v1/adr/propose",
		func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "dispatcher") {
				t.Errorf("body: %s", string(body))
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(ADR{ID: "ADR-0042", Status: "proposed", Topic: "dispatcher"})
		})
	doc, err := c.ADRPropose(context.Background(), "dispatcher")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if doc.ID != "ADR-0042" {
		t.Errorf("id: %q", doc.ID)
	}
	if doc.Status != "proposed" {
		t.Errorf("status: %q", doc.Status)
	}
}

func TestADRPropose_HTTPError(t *testing.T) {
	c, _ := startADRTestServer(t, "POST /v1/adr/propose",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"topic required"}`))
		})
	_, err := c.ADRPropose(context.Background(), "")
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 400 {
		t.Errorf("err shape: %v", err)
	}
}

func TestADRPropose_503(t *testing.T) {
	c, _ := startADRTestServer(t, "POST /v1/adr/propose",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error":"feature not configured","code":"plan9_adr_unavailable"}`))
		})
	_, err := c.ADRPropose(context.Background(), "topic")
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 503 {
		t.Errorf("err shape: %v", err)
	}
}

func TestADRShow_OK(t *testing.T) {
	c, _ := startADRTestServer(t, "GET /v1/adr/show",
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("id") != "ADR-0042" {
				t.Errorf("id: %q", r.URL.Query().Get("id"))
			}
			json.NewEncoder(w).Encode(ADR{
				ID: "ADR-0042", Status: "accepted", Topic: "dispatcher",
				Body: "full body",
			})
		})
	doc, err := c.ADRShow(context.Background(), "ADR-0042")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if doc.Body != "full body" {
		t.Errorf("body: %q", doc.Body)
	}
}

func TestADRShow_NotFound(t *testing.T) {
	c, _ := startADRTestServer(t, "GET /v1/adr/show",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"not found"}`))
		})
	_, err := c.ADRShow(context.Background(), "ADR-9999")
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 404 {
		t.Errorf("err shape: %v", err)
	}
}

func TestADRShow_HTTPError(t *testing.T) {
	c, _ := startADRTestServer(t, "GET /v1/adr/show",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"internal error"}`))
		})
	_, err := c.ADRShow(context.Background(), "ADR-0001")
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 500 {
		t.Errorf("err shape: %v", err)
	}
}

func TestADRList_OK(t *testing.T) {
	c, _ := startADRTestServer(t, "GET /v1/adr/list",
		func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if q.Get("status") != "accepted" {
				t.Errorf("status: %q", q.Get("status"))
			}
			if q.Get("plan") != "plan-9" {
				t.Errorf("plan: %q", q.Get("plan"))
			}
			if q.Get("limit") != "50" {
				t.Errorf("limit: %q", q.Get("limit"))
			}
			json.NewEncoder(w).Encode(map[string]any{
				"items": []ADR{{ID: "ADR-0001"}, {ID: "ADR-0002"}},
				"count": 2,
			})
		})
	rows, err := c.ADRList(context.Background(), ADRListClientFilter{Status: "accepted", Plan: "plan-9", Limit: 50})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("rows: %d", len(rows))
	}
}

func TestADRList_NilItemsNormalized(t *testing.T) {
	c, _ := startADRTestServer(t, "GET /v1/adr/list",
		func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"items":null,"count":0}`))
		})
	rows, err := c.ADRList(context.Background(), ADRListClientFilter{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rows == nil {
		t.Error("nil items must be normalized to empty slice")
	}
}

func TestADRList_NoFilter(t *testing.T) {
	c, _ := startADRTestServer(t, "GET /v1/adr/list",
		func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()

			if q.Get("status") != "" {
				t.Errorf("unexpected status: %q", q.Get("status"))
			}
			json.NewEncoder(w).Encode(map[string]any{
				"items": []ADR{},
				"count": 0,
			})
		})
	rows, err := c.ADRList(context.Background(), ADRListClientFilter{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rows == nil {
		t.Error("expected non-nil slice")
	}
}

func TestADRList_RiskLevelFilter(t *testing.T) {
	c, _ := startADRTestServer(t, "GET /v1/adr/list",
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("risk_level") != "high" {
				t.Errorf("risk_level: %q", r.URL.Query().Get("risk_level"))
			}
			json.NewEncoder(w).Encode(map[string]any{
				"items": []ADR{{ID: "ADR-0010", RiskLevel: "high"}},
				"count": 1,
			})
		})
	rows, err := c.ADRList(context.Background(), ADRListClientFilter{RiskLevel: "high"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rows) != 1 || rows[0].RiskLevel != "high" {
		t.Errorf("rows: %+v", rows)
	}
}

func TestADRList_HTTPError(t *testing.T) {
	c, _ := startADRTestServer(t, "GET /v1/adr/list",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"adapter offline"}`))
		})
	_, err := c.ADRList(context.Background(), ADRListClientFilter{})
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 500 {
		t.Errorf("err shape: %v", err)
	}
}

func TestADRGraph_OK(t *testing.T) {
	c, _ := startADRTestServer(t, "GET /v1/adr/graph",
		func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if q.Get("from") != "ADR-0042" {
				t.Errorf("from: %q", q.Get("from"))
			}
			if q.Get("depth") != "3" {
				t.Errorf("depth: %q", q.Get("depth"))
			}
			json.NewEncoder(w).Encode(ADRGraph{
				Nodes: []ADRGraphNode{{ID: "ADR-0042", Status: "accepted"}},
				Edges: []ADREdge{{From: "ADR-0042", To: "ADR-0043", Type: "supersedes"}},
			})
		})
	g, err := c.ADRGraph(context.Background(), "ADR-0042", 3)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(g.Nodes) != 1 {
		t.Errorf("nodes: %d", len(g.Nodes))
	}
	if len(g.Edges) != 1 {
		t.Errorf("edges: %d", len(g.Edges))
	}
	if g.Edges[0].Type != "supersedes" {
		t.Errorf("edge type: %q", g.Edges[0].Type)
	}
}

func TestADRGraph_DefaultDepth(t *testing.T) {
	c, _ := startADRTestServer(t, "GET /v1/adr/graph",
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("depth") != "1" {
				t.Errorf("default depth expected 1, got %q", r.URL.Query().Get("depth"))
			}
			json.NewEncoder(w).Encode(ADRGraph{
				Nodes: []ADRGraphNode{},
				Edges: []ADREdge{},
			})
		})
	_, err := c.ADRGraph(context.Background(), "ADR-0001", 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestADRGraph_HTTPError(t *testing.T) {
	c, _ := startADRTestServer(t, "GET /v1/adr/graph",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"from required"}`))
		})
	_, err := c.ADRGraph(context.Background(), "", 1)
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 400 {
		t.Errorf("err shape: %v", err)
	}
}

func TestADRHistory_OK(t *testing.T) {
	c, _ := startADRTestServer(t, "GET /v1/adr/history",
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("id") != "ADR-0042" {
				t.Errorf("id: %q", r.URL.Query().Get("id"))
			}
			json.NewEncoder(w).Encode(map[string]any{
				"items": []ADRTransition{
					{ID: "ADR-0042", Status: "accepted", At: 1000, Reason: "max-scope"},
				},
				"count": 1,
			})
		})
	rows, err := c.ADRHistory(context.Background(), "ADR-0042")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("rows: %d", len(rows))
	}
	if rows[0].Reason != "max-scope" {
		t.Errorf("reason: %q", rows[0].Reason)
	}
}

func TestADRHistory_NilItemsNormalized(t *testing.T) {
	c, _ := startADRTestServer(t, "GET /v1/adr/history",
		func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"items":null,"count":0}`))
		})
	rows, err := c.ADRHistory(context.Background(), "ADR-0001")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rows == nil {
		t.Error("nil items must be normalized to empty slice")
	}
}

func TestADRHistory_HTTPError(t *testing.T) {
	c, _ := startADRTestServer(t, "GET /v1/adr/history",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"db error"}`))
		})
	_, err := c.ADRHistory(context.Background(), "ADR-0001")
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 500 {
		t.Errorf("err shape: %v", err)
	}
}

func TestADRAccept_OK(t *testing.T) {
	c, _ := startADRTestServer(t, "POST /v1/adr/accept",
		func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "ADR-0042") {
				t.Errorf("body: %s", string(body))
			}
			if !strings.Contains(string(body), "plan 9 approved") {
				t.Errorf("reason missing: %s", string(body))
			}
			w.WriteHeader(http.StatusNoContent)
		})
	err := c.ADRAccept(context.Background(), "ADR-0042", "plan 9 approved")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestADRAccept_HTTPError(t *testing.T) {
	c, _ := startADRTestServer(t, "POST /v1/adr/accept",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"id and reason required (inv-zen-146)"}`))
		})
	err := c.ADRAccept(context.Background(), "", "")
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 400 {
		t.Errorf("err shape: %v", err)
	}
}

func TestADRReject_OK(t *testing.T) {
	c, _ := startADRTestServer(t, "POST /v1/adr/reject",
		func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "ADR-0043") {
				t.Errorf("body: %s", string(body))
			}
			w.WriteHeader(http.StatusNoContent)
		})
	err := c.ADRReject(context.Background(), "ADR-0043", "scope too narrow")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestADRReject_HTTPError(t *testing.T) {
	c, _ := startADRTestServer(t, "POST /v1/adr/reject",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"id and reason required (inv-zen-146)"}`))
		})
	err := c.ADRReject(context.Background(), "ADR-0043", "")
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 400 {
		t.Errorf("err shape: %v", err)
	}
}

func TestADRSupersede_OK(t *testing.T) {
	c, _ := startADRTestServer(t, "POST /v1/adr/supersede",
		func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			s := string(body)
			if !strings.Contains(s, "ADR-0001") || !strings.Contains(s, "ADR-0042") {
				t.Errorf("body: %s", s)
			}
			if !strings.Contains(s, "plan 9 replaces") {
				t.Errorf("reason missing: %s", s)
			}
			w.WriteHeader(http.StatusNoContent)
		})
	err := c.ADRSupersede(context.Background(), "ADR-0001", "ADR-0042", "plan 9 replaces")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestADRSupersede_HTTPError(t *testing.T) {
	c, _ := startADRTestServer(t, "POST /v1/adr/supersede",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"old_id, new_id and reason required (inv-zen-146)"}`))
		})
	err := c.ADRSupersede(context.Background(), "", "", "")
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 400 {
		t.Errorf("err shape: %v", err)
	}
}

func TestADRIndex_OK(t *testing.T) {
	c, _ := startADRTestServer(t, "POST /v1/adr/index",
		func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "false") && string(body) != "" {

			}
			json.NewEncoder(w).Encode(ADRManifest{
				GeneratedAt: 1000,
				ADRCount:    42,
				Manifest:    `{"adrs":[]}`,
				Graph:       `{"nodes":[],"edges":[]}`,
			})
		})
	m, err := c.ADRIndex(context.Background(), false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if m.ADRCount != 42 {
		t.Errorf("adr_count: %d", m.ADRCount)
	}
}

func TestADRIndex_CheckMode(t *testing.T) {
	c, _ := startADRTestServer(t, "POST /v1/adr/index",
		func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "true") {
				t.Errorf("check=true not in body: %s", string(body))
			}
			json.NewEncoder(w).Encode(ADRManifest{
				GeneratedAt: 1001,
				ADRCount:    10,
				Manifest:    `{}`,
				Graph:       `{}`,
			})
		})
	m, err := c.ADRIndex(context.Background(), true)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if m.GeneratedAt != 1001 {
		t.Errorf("generated_at: %d", m.GeneratedAt)
	}
}

func TestADRIndex_HTTPError(t *testing.T) {
	c, _ := startADRTestServer(t, "POST /v1/adr/index",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"adapter error"}`))
		})
	_, err := c.ADRIndex(context.Background(), false)
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 500 {
		t.Errorf("err shape: %v", err)
	}
}
