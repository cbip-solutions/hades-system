// layout_test.go — Plan 7 Phase C Task C-12 CLI tests.
//
// Tests cover:
//   - `zen layout repaint <alias>` propagates alias to client
//   - missing alias = usage error
//   - daemon error propagation + exit-code categorisation
//   - HTTP path coverage: client → daemon POST /v1/sessions/{alias}/layout/repaint
//     via httptest, including 404 → recoverable + 503 → unrecoverable
//   - subcommand registration: `layout` MUST have a `repaint` leaf
package cli

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

type fakeLayoutClient struct {
	calls []string
	err   error
}

func (f *fakeLayoutClient) RepaintLayout(_ context.Context, alias string) error {
	f.calls = append(f.calls, alias)
	return f.err
}

func newLayoutCmdForTest(c LayoutClient) *cobra.Command {
	return NewLayoutCmd(func(_ *cobra.Command) LayoutClient { return c })
}

func TestLayoutRepaintCallsClient(t *testing.T) {
	c := &fakeLayoutClient{}
	cmd := newLayoutCmdForTest(c)
	cmd.SetArgs([]string{"repaint", "internal-platform-x"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if len(c.calls) != 1 || c.calls[0] != "internal-platform-x" {
		t.Errorf("calls = %v, want [internal-platform-x]", c.calls)
	}
	if !strings.Contains(buf.String(), "ok") {
		t.Errorf("expected 'ok' confirmation; got %s", buf.String())
	}
}

func TestLayoutRepaintMissingAlias(t *testing.T) {
	c := &fakeLayoutClient{}
	cmd := newLayoutCmdForTest(c)
	cmd.SetArgs([]string{"repaint"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Error("expected usage error on missing alias")
	}
}

func TestLayoutRepaintTooManyArgs(t *testing.T) {
	c := &fakeLayoutClient{}
	cmd := newLayoutCmdForTest(c)
	cmd.SetArgs([]string{"repaint", "a", "extra"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Error("expected error on extra positional arg")
	}
}

func TestLayoutRepaintClientError(t *testing.T) {
	c := &fakeLayoutClient{err: errors.New("session not found")}
	cmd := newLayoutCmdForTest(c)
	cmd.SetArgs([]string{"repaint", "ghost"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error propagation")
	}
	if IsRecoverable(err) {
		t.Errorf("plain error wrongly marked recoverable: %v", err)
	}
}

func TestLayoutRepaintRecoverableSentinelPropagates(t *testing.T) {
	c := &fakeLayoutClient{err: recoverableWrap(errors.New("alias not found"), "alias not found")}
	cmd := newLayoutCmdForTest(c)
	cmd.SetArgs([]string{"repaint", "ghost"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsRecoverable(err) {
		t.Errorf("recoverable sentinel lost: %v", err)
	}
}

// TestLayoutCmdHasRepaintSubcommand — the `layout` cobra root MUST
// register the `repaint` leaf.
func TestLayoutCmdHasRepaintSubcommand(t *testing.T) {
	root := newLayoutCmdForTest(&fakeLayoutClient{})
	var found bool
	for _, sc := range root.Commands() {
		if sc.Name() == "repaint" {
			found = true
			break
		}
	}
	if !found {
		t.Error("`layout` cobra root missing `repaint` subcommand")
	}
}

// TestLayoutCmdSendsHTTPBody — the production repaint RunE MUST hit
// POST /v1/sessions/{alias}/layout/repaint.
func TestLayoutCmdSendsHTTPBody(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"windows_repainted":["orch","leads","workers","hra","logs"],"scratch_preserved":true}`))
	}))
	defer srv.Close()
	resetSessionsClient(t, srv)

	cmd := NewLayoutCmdProd()
	cmd.SetArgs([]string{"repaint", "internal-platform-x"})
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method=%q want POST", gotMethod)
	}
	if gotPath != "/v1/sessions/internal-platform-x/layout/repaint" {
		t.Errorf("path=%q want /v1/sessions/internal-platform-x/layout/repaint", gotPath)
	}
	if !strings.Contains(out.String(), "ok") {
		t.Errorf("output missing 'ok'; got %q", out.String())
	}
}

func TestLayoutCmdHTTPError404Recoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("alias not found"))
	}))
	defer srv.Close()
	resetSessionsClient(t, srv)

	cmd := NewLayoutCmdProd()
	cmd.SetArgs([]string{"repaint", "missing"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected 404 error")
	}
	if !IsRecoverable(err) {
		t.Errorf("404 not recoverable: %v", err)
	}
}

func TestLayoutCmdHTTPError500Unrecoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()
	resetSessionsClient(t, srv)

	cmd := NewLayoutCmdProd()
	cmd.SetArgs([]string{"repaint", "x"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected 500 error")
	}
	if IsRecoverable(err) {
		t.Errorf("500 wrongly marked recoverable: %v", err)
	}
}

func TestLayoutCmdHTTPError503Unrecoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("phase I pending"))
	}))
	defer srv.Close()
	resetSessionsClient(t, srv)

	cmd := NewLayoutCmdProd()
	cmd.SetArgs([]string{"repaint", "x"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected 503 error")
	}
	if IsRecoverable(err) {
		t.Errorf("503 wrongly marked recoverable: %v", err)
	}
}
