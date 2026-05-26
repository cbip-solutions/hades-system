package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func TestDoctrineState(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/state", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":    "max-scope",
			"version": 1,
			"research": map[string]any{
				"depth": "deep",
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	state, err := c.DoctrineStateCall(context.Background())
	if err != nil {
		t.Fatalf("DoctrineStateCall: %v", err)
	}
	if state["name"] != "max-scope" {
		t.Errorf("got %+v", state)
	}
}

func TestDoctrineValidate_OK(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/validate", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.DoctrineValidateResp{Valid: true, Errors: []string{}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	resp, err := c.DoctrineValidateCall(context.Background(), client.DoctrineValidateReq{TOMLContent: "name = \"x\""})
	if err != nil {
		t.Fatalf("DoctrineValidateCall: %v", err)
	}
	if !resp.Valid {
		t.Errorf("got %+v", resp)
	}
}

func TestDoctrineValidate_Errors(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/validate", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(client.DoctrineValidateResp{
			Valid: false, Errors: []string{"unknown key research.bogus"},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	_, err := c.DoctrineValidateCall(context.Background(), client.DoctrineValidateReq{TOMLContent: "x = 1"})
	if err == nil {
		t.Fatal("expected 422 error")
	}
}

func TestDoctrineReload(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/reload", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.DoctrineReloadResp{
			Reloaded: true,
			State:    client.DoctrineState{"name": "default"},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	resp, err := c.DoctrineReloadCall(context.Background())
	if err != nil {
		t.Fatalf("DoctrineReloadCall: %v", err)
	}
	if !resp.Reloaded || resp.State["name"] != "default" {
		t.Errorf("got %+v", resp)
	}
}

func TestDoctrineState_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/state", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	if _, err := c.DoctrineStateCall(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestDoctrineReload_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/reload", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	if _, err := c.DoctrineReloadCall(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}
