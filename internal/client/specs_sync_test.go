package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSpecsSyncSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/knowledge/ecosystem/specs-sync" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(SpecsSyncResponse{
			ChunksIndexed: 100,
			SpecsScanned:  10,
			ElapsedMs:     250,
			Message:       "ok",
		})
	}))
	defer ts.Close()

	c := NewWithBaseURL(ts.URL)
	resp, err := c.SpecsSync(context.Background(), SpecsSyncRequest{Full: true})
	if err != nil {
		t.Fatalf("SpecsSync: %v", err)
	}
	if resp.ChunksIndexed != 100 {
		t.Errorf("chunks: got %d want 100", resp.ChunksIndexed)
	}
	if resp.SpecsScanned != 10 {
		t.Errorf("specs: got %d want 10", resp.SpecsScanned)
	}
	if resp.ElapsedMs != 250 {
		t.Errorf("elapsed: got %d want 250", resp.ElapsedMs)
	}
	if resp.Message != "ok" {
		t.Errorf("message: got %s want ok", resp.Message)
	}
}

func TestSpecsSyncRequestShape(t *testing.T) {
	var got SpecsSyncRequest
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer ts.Close()

	c := NewWithBaseURL(ts.URL)
	_, err := c.SpecsSync(context.Background(), SpecsSyncRequest{
		Full:     true,
		SpecsDir: "/custom/specs",
	})
	if err != nil {
		t.Fatalf("SpecsSync: %v", err)
	}
	if !got.Full {
		t.Errorf("expected Full=true on wire, got %v", got.Full)
	}
	if got.SpecsDir != "/custom/specs" {
		t.Errorf("expected SpecsDir override on wire, got %q", got.SpecsDir)
	}
}

func TestSpecsSyncHTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"bad input"}`))
	}))
	defer ts.Close()

	c := NewWithBaseURL(ts.URL)
	_, err := c.SpecsSync(context.Background(), SpecsSyncRequest{})
	if err == nil {
		t.Fatal("expected error from 422")
	}
	if !IsHTTPStatus(err, http.StatusUnprocessableEntity) {
		t.Errorf("expected 422 HTTPError, got %v", err)
	}
}

func TestSpecsSyncEmptyRequest(t *testing.T) {

	var got SpecsSyncRequest
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer ts.Close()

	c := NewWithBaseURL(ts.URL)
	_, err := c.SpecsSync(context.Background(), SpecsSyncRequest{})
	if err != nil {
		t.Fatalf("SpecsSync: %v", err)
	}
	if got.Full {
		t.Errorf("expected Full=false default, got true")
	}
	if got.SpecsDir != "" {
		t.Errorf("expected empty SpecsDir default, got %q", got.SpecsDir)
	}
}
