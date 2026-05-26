package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCaronteReindexSetsHeader(t *testing.T) {
	var gotHeader, gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Zen-Project-ID")
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"project_id":"X","files_indexed":3,"completed":true}`))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	resp, err := c.CaronteReindex(context.Background(), "zen-swarm-3572a35b")
	if err != nil {
		t.Fatalf("CaronteReindex: %v", err)
	}
	if gotHeader != "zen-swarm-3572a35b" {
		t.Errorf("X-Zen-Project-ID header = %q; want 'zen-swarm-3572a35b'", gotHeader)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q; want POST", gotMethod)
	}
	if gotPath != "/v1/caronte/reindex" {
		t.Errorf("path = %q; want /v1/caronte/reindex", gotPath)
	}
	if resp.FilesIndexed != 3 || !resp.Completed {
		t.Errorf("decoded resp = %+v; want FilesIndexed=3 Completed=true", resp)
	}
}

func TestCaronteReindexErrorStatuses(t *testing.T) {
	for _, code := range []int{http.StatusBadRequest, http.StatusNotFound, http.StatusInternalServerError, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(code)
				_, _ = w.Write([]byte("err body"))
			}))
			defer srv.Close()
			c := NewWithBaseURL(srv.URL)
			_, err := c.CaronteReindex(context.Background(), "x")
			if err == nil {
				t.Fatalf("status %d returned nil err; want HTTPError", code)
			}
			if !IsHTTPStatus(err, code) {
				t.Errorf("err = %v; not classified as HTTPStatus %d", err, code)
			}
		})
	}
}

func TestCaronteReindexTransportError(t *testing.T) {
	c := NewWithBaseURL("http://127.0.0.1:0")
	_, err := c.CaronteReindex(context.Background(), "x")
	if err == nil {
		t.Fatal("transport-failed CaronteReindex returned nil; want error")
	}
	if !strings.Contains(err.Error(), "POST /v1/caronte/reindex") {
		t.Errorf("err = %q; want substring 'POST /v1/caronte/reindex'", err.Error())
	}
}

func TestCaronteProjectsListProjects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"projects":[
				{"alias":"alpha","id_sha256":"aa"},
				{"alias":"beta","id_sha256":"bb"}
			]
		}`))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	resp, err := c.CaronteProjectsList(context.Background())
	if err != nil {
		t.Fatalf("CaronteProjectsList: %v", err)
	}
	if len(resp.Projects) != 2 {
		t.Fatalf("Projects len = %d; want 2", len(resp.Projects))
	}
	if resp.Projects[0].Alias != "alpha" || resp.Projects[1].Alias != "beta" {
		t.Errorf("Projects = %+v; want [alpha beta] aliases", resp.Projects)
	}
}

func TestCaronteProjectsListEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"projects":[]}`))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	resp, err := c.CaronteProjectsList(context.Background())
	if err != nil {
		t.Fatalf("CaronteProjectsList: %v", err)
	}
	if resp.Projects == nil {
		t.Error("Projects slice nil; want non-nil empty slice")
	}
	if len(resp.Projects) != 0 {
		t.Errorf("Projects len = %d; want 0", len(resp.Projects))
	}
}

func TestCaronteReindexResponseFieldNames(t *testing.T) {
	resp := CaronteReindexResponse{
		ProjectID:      "p",
		NodesCreated:   1,
		EdgesCreated:   2,
		FilesIndexed:   3,
		LanguageCounts: map[string]int{"go": 1},
		DurationMillis: 100,
		StartedAt:      time.Now(),
		Completed:      true,
	}

	src := `{"project_id":"p","nodes_created":1,"edges_created":2,"files_indexed":3,"language_counts":{"go":1},"duration_ms":100,"completed":true}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(src))
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	got, err := c.CaronteReindex(context.Background(), "p")
	if err != nil {
		t.Fatalf("CaronteReindex: %v", err)
	}
	if got.ProjectID != resp.ProjectID || got.NodesCreated != resp.NodesCreated {
		t.Errorf("decoded fields drift: got %+v, want %+v", got, resp)
	}
}
