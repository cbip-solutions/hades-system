package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

type fakeCaronteReindexClient struct {
	calls    []string
	resp     *client.CaronteReindexResponse
	err      error
	listResp *client.CaronteProjectsListResponse
	listErr  error
	respFn   func(idOrAlias string) (*client.CaronteReindexResponse, error)
}

func (f *fakeCaronteReindexClient) CaronteReindex(_ context.Context, idOrAlias string) (*client.CaronteReindexResponse, error) {
	f.calls = append(f.calls, idOrAlias)
	if f.respFn != nil {
		return f.respFn(idOrAlias)
	}
	return f.resp, f.err
}

func (f *fakeCaronteReindexClient) CaronteProjectsList(_ context.Context) (*client.CaronteProjectsListResponse, error) {
	return f.listResp, f.listErr
}

func TestRunCaronteReindexExplicitProject(t *testing.T) {
	fake := &fakeCaronteReindexClient{
		resp: &client.CaronteReindexResponse{
			ProjectID:    "deadbeef0000000000000000000000000000000000000000000000000000beef",
			FilesIndexed: 5,
			NodesCreated: 42,
			Completed:    true,
		},
	}
	var out bytes.Buffer
	err := RunCaronteReindex(context.Background(), fake, CaronteReindexFlags{
		Project: "zen-swarm-3572a35b",
	}, &out)
	if err != nil {
		t.Fatalf("RunCaronteReindex: %v", err)
	}
	if len(fake.calls) != 1 || fake.calls[0] != "zen-swarm-3572a35b" {
		t.Errorf("client received calls=%v; want exactly 'zen-swarm-3572a35b'", fake.calls)
	}
	s := out.String()
	if !strings.Contains(s, "files_indexed:") {
		t.Errorf("output = %q; want substring 'files_indexed:'", s)
	}
	if !strings.Contains(s, "42") || !strings.Contains(s, "5") {
		t.Errorf("output = %q; want substrings '42' (nodes) and '5' (files)", s)
	}
}

func TestRunCaronteReindexJSONFormat(t *testing.T) {
	fake := &fakeCaronteReindexClient{
		resp: &client.CaronteReindexResponse{
			ProjectID:    "x",
			FilesIndexed: 1,
			NodesCreated: 1,
			Completed:    true,
		},
	}
	var out bytes.Buffer
	err := RunCaronteReindex(context.Background(), fake, CaronteReindexFlags{
		Project: "x",
		Format:  "json",
	}, &out)
	if err != nil {
		t.Fatalf("RunCaronteReindex: %v", err)
	}
	var decoded client.CaronteReindexResponse
	if jerr := json.Unmarshal(out.Bytes(), &decoded); jerr != nil {
		t.Fatalf("output not valid JSON: %v; got %q", jerr, out.String())
	}
	if decoded.FilesIndexed != 1 || !decoded.Completed {
		t.Errorf("decoded = %+v; want FilesIndexed=1 Completed=true", decoded)
	}
}

func TestRunCaronteReindexCwdResolution(t *testing.T) {
	want, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	fake := &fakeCaronteReindexClient{
		resp: &client.CaronteReindexResponse{ProjectID: want, Completed: true},
	}
	var out bytes.Buffer
	if err := RunCaronteReindex(context.Background(), fake, CaronteReindexFlags{}, &out); err != nil {
		t.Fatalf("RunCaronteReindex: %v", err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("expected 1 call; got %d (%v)", len(fake.calls), fake.calls)
	}

	resolved, _ := filepath.EvalSymlinks(want)
	if fake.calls[0] != want && fake.calls[0] != resolved {
		t.Errorf("client received %q; want cwd %q (or eval-symlinked %q)", fake.calls[0], want, resolved)
	}
}

func TestRunCaronteReindexAllFlag(t *testing.T) {
	fake := &fakeCaronteReindexClient{
		listResp: &client.CaronteProjectsListResponse{
			Projects: []client.CaronteProjectListItem{
				{Alias: "proj-a", IDSha256: "aaaa"},
				{Alias: "proj-b", IDSha256: "bbbb"},
			},
		},
		respFn: func(alias string) (*client.CaronteReindexResponse, error) {
			return &client.CaronteReindexResponse{
				ProjectID:    alias,
				FilesIndexed: 3,
				Completed:    true,
			}, nil
		},
	}
	var out bytes.Buffer
	err := RunCaronteReindex(context.Background(), fake, CaronteReindexFlags{All: true}, &out)
	if err != nil {
		t.Fatalf("RunCaronteReindex --all: %v", err)
	}
	if len(fake.calls) != 2 {
		t.Errorf("--all called Reindex %d times; want 2 (one per project)", len(fake.calls))
	}
	if fake.calls[0] != "proj-a" || fake.calls[1] != "proj-b" {
		t.Errorf("--all calls = %v; want [proj-a proj-b]", fake.calls)
	}
	s := out.String()
	if !strings.Contains(s, "proj-a") || !strings.Contains(s, "proj-b") {
		t.Errorf("--all output = %q; want both project aliases", s)
	}
}

func TestRunCaronteReindexAllFlagListError(t *testing.T) {
	fake := &fakeCaronteReindexClient{
		listErr: errors.New("daemon: list failed"),
	}
	var out bytes.Buffer
	err := RunCaronteReindex(context.Background(), fake, CaronteReindexFlags{All: true}, &out)
	if err == nil {
		t.Fatal("RunCaronteReindex --all returned nil; want list-err propagation")
	}
	if !strings.Contains(err.Error(), "list failed") {
		t.Errorf("err = %q; want substring 'list failed'", err.Error())
	}
}

func TestRunCaronteReindexAllAndProjectMutex(t *testing.T) {
	fake := &fakeCaronteReindexClient{}
	var out bytes.Buffer
	err := RunCaronteReindex(context.Background(), fake, CaronteReindexFlags{
		All:     true,
		Project: "p",
	}, &out)
	if err == nil {
		t.Fatal("RunCaronteReindex(--all + project) returned nil; want validation error")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("err = %v; want errors.Is(ErrRecoverable)", err)
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("err = %q; want 'mutually exclusive' wording", err.Error())
	}
}

func TestRunCaronteReindexHandlerError(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`project "x" not found`))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	prodC := &productionCaronteReindexClient{c: c}
	var out bytes.Buffer
	err := RunCaronteReindex(context.Background(), prodC, CaronteReindexFlags{Project: "x"}, &out)
	if err == nil {
		t.Fatal("RunCaronteReindex(404) returned nil; want error")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("404 err = %v; want errors.Is(ErrRecoverable)", err)
	}
}

func TestRunCaronteReindexFormatValidation(t *testing.T) {
	fake := &fakeCaronteReindexClient{}
	var out bytes.Buffer
	err := RunCaronteReindex(context.Background(), fake, CaronteReindexFlags{
		Project: "p",
		Format:  "yaml",
	}, &out)
	if err == nil {
		t.Fatal("RunCaronteReindex(--format yaml) returned nil; want validation error")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("format err = %v; want errors.Is(ErrRecoverable)", err)
	}
}

func TestNewCaronteReindexCmdHelp(t *testing.T) {
	cmd := NewCaronteReindexCmd(func(_ interface{}) CaronteReindexClient { return &fakeCaronteReindexClient{} })
	if cmd.Use == "" {
		t.Error("Use string empty")
	}
	if !strings.Contains(cmd.Use, "reindex") {
		t.Errorf("Use = %q; want substring 'reindex'", cmd.Use)
	}

	if f := cmd.Flag("all"); f == nil {
		t.Error("--all flag not registered")
	}
	if f := cmd.Flag("format"); f == nil {
		t.Error("--format flag not registered")
	}
}

func TestNewCaronteCmdRegistersReindex(t *testing.T) {
	root := NewCaronteCmd()
	if root.Use != "caronte" {
		t.Errorf("root.Use = %q; want 'caronte'", root.Use)
	}
	var foundReindex bool
	for _, sub := range root.Commands() {
		if sub.Name() == "reindex" {
			foundReindex = true
		}
	}
	if !foundReindex {
		t.Error("`zen caronte reindex` subcommand not registered under `zen caronte`")
	}
}

func TestRunCaronteReindexRenderEdges(t *testing.T) {
	fake := &fakeCaronteReindexClient{
		resp: &client.CaronteReindexResponse{
			ProjectID:      "p",
			FilesIndexed:   2,
			NodesCreated:   3,
			EdgesCreated:   4,
			LanguageCounts: map[string]int{"go": 1, "python": 1},
			DurationMillis: 250,
			Completed:      true,
		},
	}
	var out bytes.Buffer
	err := RunCaronteReindex(context.Background(), fake, CaronteReindexFlags{Project: "p"}, &out)
	if err != nil {
		t.Fatalf("RunCaronteReindex: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "edges_created:") {
		t.Errorf("output missing edges_created line; got %q", s)
	}
	if !strings.Contains(s, "languages:") {
		t.Errorf("output missing languages block; got %q", s)
	}
	if !strings.Contains(s, "go: 1 files") || !strings.Contains(s, "python: 1 files") {
		t.Errorf("output missing per-language counts; got %q", s)
	}
}

func TestRunCaronteReindexIncompleteStatus(t *testing.T) {
	fake := &fakeCaronteReindexClient{
		resp: &client.CaronteReindexResponse{
			ProjectID:    "p",
			FilesIndexed: 1,
			Completed:    false,
		},
	}
	var out bytes.Buffer
	err := RunCaronteReindex(context.Background(), fake, CaronteReindexFlags{Project: "p"}, &out)
	if err != nil {
		t.Fatalf("RunCaronteReindex: %v", err)
	}
	if !strings.Contains(out.String(), "INCOMPLETE") {
		t.Errorf("output = %q; want substring 'INCOMPLETE'", out.String())
	}
}

func TestRunCaronteReindexAllPerProjectError(t *testing.T) {
	failingErr := errors.New("walk failed")
	fake := &fakeCaronteReindexClient{
		listResp: &client.CaronteProjectsListResponse{
			Projects: []client.CaronteProjectListItem{
				{Alias: "good", IDSha256: "g"},
				{Alias: "bad", IDSha256: "b"},
			},
		},
		respFn: func(alias string) (*client.CaronteReindexResponse, error) {
			if alias == "bad" {
				return nil, failingErr
			}
			return &client.CaronteReindexResponse{ProjectID: alias, Completed: true}, nil
		},
	}
	var out bytes.Buffer
	err := RunCaronteReindex(context.Background(), fake, CaronteReindexFlags{All: true}, &out)
	if err != nil {
		t.Fatalf("RunCaronteReindex --all: %v (want nil on per-project failure)", err)
	}
	s := out.String()
	if !strings.Contains(s, "bad: ERROR") {
		t.Errorf("output missing 'bad: ERROR' line; got %q", s)
	}
	if !strings.Contains(s, "good") {
		t.Errorf("output missing successful 'good' project; got %q", s)
	}
}

func TestClassifyCaronteReindexError400(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`X-Zen-Project-ID header required`))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	prodC := &productionCaronteReindexClient{c: c}
	var out bytes.Buffer
	err := RunCaronteReindex(context.Background(), prodC, CaronteReindexFlags{Project: "x"}, &out)
	if err == nil {
		t.Fatal("RunCaronteReindex(400) returned nil")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("400 err = %v; want errors.Is(ErrRecoverable)", err)
	}
	if !strings.Contains(err.Error(), "bad request") {
		t.Errorf("err = %q; want 'bad request' wording", err.Error())
	}
}

func TestClassifyCaronteReindexError503(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`caronte engine not configured`))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	prodC := &productionCaronteReindexClient{c: c}
	var out bytes.Buffer
	err := RunCaronteReindex(context.Background(), prodC, CaronteReindexFlags{Project: "x"}, &out)
	if err == nil {
		t.Fatal("RunCaronteReindex(503) returned nil")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("503 err = %v; want errors.Is(ErrRecoverable)", err)
	}
	if !strings.Contains(err.Error(), "daemon engine not configured") {
		t.Errorf("err = %q; want 'engine not configured' wording", err.Error())
	}
}

func TestClassifyCaronteReindexError500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`reindex failed: db corrupted`))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	prodC := &productionCaronteReindexClient{c: c}
	var out bytes.Buffer
	err := RunCaronteReindex(context.Background(), prodC, CaronteReindexFlags{Project: "x"}, &out)
	if err == nil {
		t.Fatal("RunCaronteReindex(500) returned nil")
	}
	if errors.Is(err, ErrRecoverable) {
		t.Errorf("500 err = %v; want NOT errors.Is(ErrRecoverable) (exit 2)", err)
	}
}

func TestProductionCaronteReindexClient_ProjectsList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"projects":[{"alias":"p1","id_sha256":"aaaa"},{"alias":"p2","id_sha256":"bbbb"}]}`))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	prodC := &productionCaronteReindexClient{c: c}
	resp, err := prodC.CaronteProjectsList(context.Background())
	if err != nil {
		t.Fatalf("CaronteProjectsList: %v", err)
	}
	if len(resp.Projects) != 2 {
		t.Errorf("Projects len = %d; want 2", len(resp.Projects))
	}
}
