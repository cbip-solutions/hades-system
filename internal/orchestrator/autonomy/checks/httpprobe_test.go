package checks_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy/checks"
)

func TestHTTPProbe_RequestBuildError(t *testing.T) {
	c := checks.NewResearchMCPUp(checks.Deps{
		HTTP: stubHTTP{},
		URLs: checks.URLs{ResearchMCP: "http://example.com/\x7f"},
	})
	status, reason, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckFail {
		t.Fatalf("invalid URL: want fail; got %v", status)
	}
	if !strings.Contains(reason, "probe request build") {
		t.Fatalf("reason must cite request-build error; got %q", reason)
	}
}

type stubHTTP struct{}

func (stubHTTP) Do(*http.Request) (*http.Response, error) { return nil, errors.New("stub") }

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("body read boom") }
func (errReader) Close() error             { return nil }

type errReadHTTP struct{}

func (errReadHTTP) Do(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(errReader{})}, nil
}

func TestHTTPProbe_BodyReadError(t *testing.T) {
	c := checks.NewResearchMCPUp(checks.Deps{
		HTTP: errReadHTTP{},
		URLs: checks.URLs{ResearchMCP: "http://example.com/healthz"},
	})
	status, reason, _ := c.Run(context.Background(), autonomy.CheckEnv{})
	if status != autonomy.CheckFail {
		t.Fatalf("body read err: want fail; got %v", status)
	}
	if !strings.Contains(reason, "probe body read") {
		t.Fatalf("reason must cite body-read error; got %q", reason)
	}
}
