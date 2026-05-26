package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func TestProjectDoctorRoundTrip(t *testing.T) {
	gotPath := ""
	gotBody := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"healthy": true,
			"alias": "internal-platform-x",
			"id_sha256": "9f3a1c2d8b4e5f60111122223333444455556666777788889999aaaabbbbccccdd",
			"canonical_path": "/path/to/projects/internal-platform-x",
			"path_history": [
				{"path": "/path/to/projects/internal-platform-x", "first_seen": 1700000000, "last_seen": 1700001000}
			]
		}`))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	resp, err := c.ProjectDoctor(context.Background(), "internal-platform-x", "", false)
	if err != nil {
		t.Fatalf("ProjectDoctor: %v", err)
	}
	if gotPath != "/v1/projects/doctor" {
		t.Errorf("path = %q, want /v1/projects/doctor", gotPath)
	}
	if gotBody["alias"] != "internal-platform-x" {
		t.Errorf("body alias = %v, want internal-platform-x", gotBody["alias"])
	}
	if !resp.Healthy {
		t.Errorf("Healthy = false; want true")
	}
	if resp.Alias != "internal-platform-x" {
		t.Errorf("Alias = %q, want internal-platform-x", resp.Alias)
	}
	if len(resp.PathHistory) != 1 {
		t.Errorf("PathHistory len = %d, want 1", len(resp.PathHistory))
	}
}

func TestProjectDoctorMvDetected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"healthy": false,
			"alias": "internal-platform-x",
			"id_sha256": "4ab8d172000000000000000000000000000000000000000000000000000000aa",
			"canonical_path": "/x",
			"path_history": [],
			"mv_detected": {
				"old_path": "/old",
				"new_path": "/new",
				"old_id_short": "9f3a1c2d",
				"new_id_short": "4ab8d172"
			},
			"hint": "rebind hint"
		}`))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	resp, err := c.ProjectDoctor(context.Background(), "internal-platform-x", "/x", false)
	if err != nil {
		t.Fatalf("ProjectDoctor: %v", err)
	}
	if resp.MvDetected == nil {
		t.Fatal("MvDetected is nil")
	}
	if resp.MvDetected.OldIDShort != "9f3a1c2d" {
		t.Errorf("OldIDShort = %q, want 9f3a1c2d", resp.MvDetected.OldIDShort)
	}
	if resp.Hint != "rebind hint" {
		t.Errorf("Hint = %q, want 'rebind hint'", resp.Hint)
	}
}

func TestProjectDoctorErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	_, err := c.ProjectDoctor(context.Background(), "x", "", false)
	if err == nil {
		t.Fatal("expected error; got nil")
	}
}

func TestProjectArchiveSendsAlias(t *testing.T) {
	gotBody := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	if err := c.ProjectArchive(context.Background(), "internal-platform-x"); err != nil {
		t.Fatalf("ProjectArchive: %v", err)
	}
	if gotBody["alias"] != "internal-platform-x" {
		t.Errorf("body alias = %v, want internal-platform-x", gotBody["alias"])
	}
}

func TestProjectArchiveErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	err := c.ProjectArchive(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error on 404; got nil")
	}
	if !client.IsHTTPStatus(err, http.StatusNotFound) {
		t.Errorf("expected 404 error, got %v", err)
	}
}

func TestProjectRemoveSendsAlias(t *testing.T) {
	gotBody := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	if err := c.ProjectRemove(context.Background(), "internal-platform-x"); err != nil {
		t.Fatalf("ProjectRemove: %v", err)
	}
	if gotBody["alias"] != "internal-platform-x" {
		t.Errorf("body alias = %v, want internal-platform-x", gotBody["alias"])
	}
}

func TestProjectRemoveErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	err := c.ProjectRemove(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error; got nil")
	}
}
