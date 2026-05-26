// codegraph_daemon_errpath_test.go — CaronteCodeGraph daemon-error branches.
//
// CaronteCodeGraph.CodeGraph — a transport failure (doer.Do returns an error)
// and a malformed 200 response (JSON decode failure). Both are real failure
// modes (daemon down / wire-shape drift) that MUST surface as errors so the
// dispatcher's runBackend records a soft-fail, never a silent empty result.
package research

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeCodegraphDoer struct {
	resp *http.Response
	err  error
}

func (f *fakeCodegraphDoer) Do(*http.Request) (*http.Response, error) { return f.resp, f.err }
func (f *fakeCodegraphDoer) BaseURL() string                          { return "http://daemon.test" }

func TestCaronteCodeGraphTransportError(t *testing.T) {
	adapter := &CaronteCodeGraph{doer: &fakeCodegraphDoer{err: errors.New("connection refused")}}
	_, err := adapter.CodeGraph(context.Background(), "q", "proj")
	if err == nil {
		t.Fatal("CodeGraph with a transport error returned nil; want the wrapped POST error")
	}
	if !strings.Contains(err.Error(), "codegraph POST") {
		t.Errorf("err = %v; want it to wrap the POST failure", err)
	}
}

func TestCaronteCodeGraphDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{not valid json"))
	}))
	defer srv.Close()

	adapter := NewCaronteCodeGraph(makeCaronteClient(t, srv))
	_, err := adapter.CodeGraph(context.Background(), "q", "proj")
	if err == nil {
		t.Fatal("CodeGraph with a malformed response returned nil; want a decode error")
	}
	if !strings.Contains(err.Error(), "decode codegraph response") {
		t.Errorf("err = %v; want it to wrap the decode failure", err)
	}
}
