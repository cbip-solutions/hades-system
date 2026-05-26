package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestContractHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/mcpgateway/contract" {
			t.Errorf("path = %s, want /v1/mcpgateway/contract", r.URL.Path)
		}
		var req ContractRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req.Endpoint != "endpoint-1" {
			t.Errorf("Endpoint = %q", req.Endpoint)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ContractResponse{
			EndpointID:    req.Endpoint,
			Repo:          "repo-a",
			Kind:          "http",
			Method:        "GET",
			PathTemplate:  "/users/{id}",
			HandlerNodeID: "node-1",
			ExtractorID:   "oasdiff",
			ExtractedAt:   1700000000,
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.Contract(context.Background(), ContractRequest{Endpoint: "endpoint-1"})
	if err != nil {
		t.Fatalf("Contract: %v", err)
	}
	if resp.EndpointID != "endpoint-1" {
		t.Errorf("EndpointID = %q", resp.EndpointID)
	}
	if resp.Method != "GET" || resp.PathTemplate != "/users/{id}" {
		t.Errorf("Method/Path = %q %q", resp.Method, resp.PathTemplate)
	}
}

func TestContractErrorPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	if _, err := c.Contract(context.Background(), ContractRequest{Endpoint: "x"}); err == nil {
		t.Fatal("expected error on 500, got nil")
	}
}

func TestContractValidateHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcpgateway/contract/validate" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var req ContractValidateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req.Repo != "/tmp/repo" {
			t.Errorf("Repo = %q", req.Repo)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ContractValidateResponse{
			Valid:         true,
			SchemaVersion: 1,
			Services: []ContractValidateService{
				{BaseURLRef: "${BACKEND_URL}", TargetRepo: "repo-b"},
			},
		})
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	resp, err := c.ContractValidate(context.Background(), ContractValidateRequest{Repo: "/tmp/repo"})
	if err != nil {
		t.Fatalf("ContractValidate: %v", err)
	}
	if !resp.Valid || resp.SchemaVersion != 1 {
		t.Errorf("resp = %+v; want valid=true schemaVersion=1", resp)
	}
	if len(resp.Services) != 1 || resp.Services[0].TargetRepo != "repo-b" {
		t.Errorf("Services = %+v", resp.Services)
	}
}

func TestContractValidateInvalidReply(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ContractValidateResponse{
			Valid: false,
			Errors: []ContractValidateError{
				{Code: "schema_version_missing", Message: "schema_version field required"},
			},
		})
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	resp, err := c.ContractValidate(context.Background(), ContractValidateRequest{Repo: "x"})
	if err != nil {
		t.Fatalf("ContractValidate: %v", err)
	}
	if resp.Valid {
		t.Errorf("Valid = true; want false")
	}
	if len(resp.Errors) != 1 || resp.Errors[0].Code != "schema_version_missing" {
		t.Errorf("Errors = %+v", resp.Errors)
	}
}

func TestContractWhyHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcpgateway/contract/why" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var req ContractWhyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req.ChangeID != "chg-1" {
			t.Errorf("ChangeID = %q", req.ChangeID)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ContractWhyResponse{
			ChangeID:      req.ChangeID,
			WorkspaceID:   "ws-1",
			EndpointID:    "endpoint-1",
			EndpointRepo:  "repo-a",
			LoreAuthor:    "alice@example.com",
			LoreCommitSHA: "abc1234",
			LoreADRRefs:   []string{"ADR-0114"},
			DetectedAt:    1700000000,
		})
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	resp, err := c.ContractWhy(context.Background(), ContractWhyRequest{ChangeID: "chg-1"})
	if err != nil {
		t.Fatalf("ContractWhy: %v", err)
	}
	if resp.LoreAuthor != "alice@example.com" {
		t.Errorf("LoreAuthor = %q", resp.LoreAuthor)
	}
	if len(resp.LoreADRRefs) != 1 || resp.LoreADRRefs[0] != "ADR-0114" {
		t.Errorf("LoreADRRefs = %+v", resp.LoreADRRefs)
	}
}
