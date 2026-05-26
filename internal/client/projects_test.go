package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProjectsListAllRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/projects" {
			t.Errorf("got %s %s, want GET /v1/projects", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"projects":[
			{"id":"abc","alias":"internal-platform-x","path":"/p","autonomous_state":"active"},
			{"id":"def","alias":"old","path":"/q","autonomous_state":"complete"}
		]}`))
	}))
	t.Cleanup(srv.Close)
	c := NewWithBaseURL(srv.URL)
	projs, err := c.ProjectsListAll(context.Background())
	if err != nil {
		t.Fatalf("ProjectsListAll: %v", err)
	}
	if len(projs) != 2 {
		t.Fatalf("got %d projects, want 2", len(projs))
	}
	if projs[0].Alias != "internal-platform-x" {
		t.Errorf("projs[0].Alias = %q, want internal-platform-x", projs[0].Alias)
	}
	if projs[0].IsArchived() {
		t.Errorf("projs[0] (active) should not be archived")
	}
	if !projs[1].IsArchived() {
		t.Errorf("projs[1] (autonomous_state=complete) should be archived")
	}
}

func TestProjectsListAllErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(501)
		_, _ = w.Write([]byte(`{"error":"not implemented"}`))
	}))
	t.Cleanup(srv.Close)
	c := NewWithBaseURL(srv.URL)
	_, err := c.ProjectsListAll(context.Background())
	if err == nil {
		t.Fatal("expected error on 501, got nil")
	}
	if !IsHTTPStatus(err, 501) {
		t.Errorf("expected HTTPError 501, got %v", err)
	}
}

func TestProjectDoctorReportRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("got method %s, want GET", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/v1/projects/internal-platform-x/doctor") {
			t.Errorf("got path %q, want suffix /v1/projects/internal-platform-x/doctor", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[
			{"aspect":"path.exists","status":"ok","message":"verified","detail":"","hint":""},
			{"aspect":"sessions.alive","status":"warn","message":"1 stale","detail":"detail-x","hint":"zen sessions prune"}
		]}`))
	}))
	t.Cleanup(srv.Close)
	c := NewWithBaseURL(srv.URL)
	resp, err := c.ProjectDoctorReport(context.Background(), "internal-platform-x")
	if err != nil {
		t.Fatalf("ProjectDoctorReport: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(resp.Items))
	}
	if resp.Items[1].Hint != "zen sessions prune" {
		t.Errorf("items[1].Hint = %q, want 'zen sessions prune'", resp.Items[1].Hint)
	}
}

func TestProjectDoctorReportErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	t.Cleanup(srv.Close)
	c := NewWithBaseURL(srv.URL)
	_, err := c.ProjectDoctorReport(context.Background(), "ghost")
	if err == nil {
		t.Fatal("expected error on 404, got nil")
	}
	if !IsHTTPStatus(err, 404) {
		t.Errorf("expected HTTPError 404, got %v", err)
	}
}

func TestMetaSnapshotGetRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/meta/snapshot" {
			t.Errorf("got %s %s, want GET /v1/meta/snapshot", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"panics_last_24h":2,"cost_utilization_pct":83}`))
	}))
	t.Cleanup(srv.Close)
	c := NewWithBaseURL(srv.URL)
	m, err := c.MetaSnapshotGet(context.Background())
	if err != nil {
		t.Fatalf("MetaSnapshotGet: %v", err)
	}
	if m.PanicsLast24h != 2 {
		t.Errorf("PanicsLast24h = %d, want 2", m.PanicsLast24h)
	}
	if m.CostUtilizationPct != 83 {
		t.Errorf("CostUtilizationPct = %d, want 83", m.CostUtilizationPct)
	}
}

func TestMetaSnapshotGetErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	t.Cleanup(srv.Close)
	c := NewWithBaseURL(srv.URL)
	_, err := c.MetaSnapshotGet(context.Background())
	if err == nil {
		t.Fatal("expected error on 503, got nil")
	}
	if !IsHTTPStatus(err, 503) {
		t.Errorf("expected HTTPError 503, got %v", err)
	}
}

func TestProjectIsArchived(t *testing.T) {
	tests := []struct {
		state string
		want  bool
	}{
		{"active", false},
		{"paused", false},
		{"idle", false},
		{"complete", true},
		{"", false},
	}
	for _, tt := range tests {
		p := Project{AutonomousState: tt.state}
		if got := p.IsArchived(); got != tt.want {
			t.Errorf("Project{state=%q}.IsArchived() = %v, want %v", tt.state, got, tt.want)
		}
	}
}
