// sessions_test.go — Task C-12 CLI tests.
//
// Tests cover:
// - `zen sessions ls` renders all 5 columns (alias, sha8, status,
// last-attach, panes) including empty-state path
// - tabwriter alignment + RFC3339 last-attach formatting
// - never-attached row renders as "never"
// - HTTP path coverage: client → daemon GET /v1/sessions via httptest,
// including 500 → unrecoverable, 503 → unrecoverable
// - subcommand registration: `sessions` MUST have an `ls` subcommand
package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

type fakeSessionsClient struct {
	rows []SessionRow
	err  error
}

func (f *fakeSessionsClient) ListSessions(_ context.Context) ([]SessionRow, error) {
	return f.rows, f.err
}

func newSessionsCmdForTest(c SessionsClient) *cobra.Command {
	return NewSessionsCmd(func(_ *cobra.Command) SessionsClient { return c })
}

func TestSessionsLsRendersAllColumns(t *testing.T) {
	c := &fakeSessionsClient{
		rows: []SessionRow{
			{Alias: "internal-platform-x", Sha8: "deadbeef", Status: "active", LastAttach: time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC), PaneCount: 5},
			{Alias: "nexus", Sha8: "12345678", Status: "idle", LastAttach: time.Date(2026, 4, 30, 9, 0, 0, 0, time.UTC), PaneCount: 3},
		},
	}
	cmd := newSessionsCmdForTest(c)
	cmd.SetArgs([]string{"ls"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"internal-platform-x", "deadbeef", "active", "nexus", "12345678", "idle", "5", "3"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}

	for _, hdr := range []string{"ALIAS", "SHA8", "STATUS", "LAST-ATTACH", "PANES"} {
		if !strings.Contains(out, hdr) {
			t.Errorf("output missing header %q\n%s", hdr, out)
		}
	}
}

func TestSessionsLsRendersRFC3339LastAttach(t *testing.T) {
	c := &fakeSessionsClient{
		rows: []SessionRow{
			{Alias: "x", Sha8: "00000000", Status: "active", LastAttach: time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC), PaneCount: 1},
		},
	}
	cmd := newSessionsCmdForTest(c)
	cmd.SetArgs([]string{"ls"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !strings.Contains(buf.String(), "2026-05-01T14:00:00Z") {
		t.Errorf("expected RFC3339 last-attach in output; got %q", buf.String())
	}
}

func TestSessionsLsNeverAttachedRowRendersNever(t *testing.T) {
	c := &fakeSessionsClient{
		rows: []SessionRow{
			{Alias: "fresh", Sha8: "aabbccdd", Status: "active", LastAttach: time.Time{}, PaneCount: 0},
		},
	}
	cmd := newSessionsCmdForTest(c)
	cmd.SetArgs([]string{"ls"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !strings.Contains(buf.String(), "never") {
		t.Errorf("expected 'never' for zero LastAttach; got %q", buf.String())
	}

	if strings.Contains(buf.String(), "1970") {
		t.Errorf("rendered unix epoch instead of 'never'; got %q", buf.String())
	}
}

func TestSessionsLsEmpty(t *testing.T) {
	c := &fakeSessionsClient{rows: nil}
	cmd := newSessionsCmdForTest(c)
	cmd.SetArgs([]string{"ls"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "no active sessions") {
		t.Errorf("expected empty-state message; got: %s", buf.String())
	}
}

func TestSessionsLsClientError(t *testing.T) {
	c := &fakeSessionsClient{err: context.DeadlineExceeded}
	cmd := newSessionsCmdForTest(c)
	cmd.SetArgs([]string{"ls"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error from daemon timeout")
	}
	if IsRecoverable(err) {
		t.Errorf("transport timeout wrongly marked recoverable: %v", err)
	}
}

func TestSessionsLsRecoverableSentinelPropagates(t *testing.T) {
	c := &fakeSessionsClient{err: recoverableWrap(context.DeadlineExceeded, "sessions list unavailable")}
	cmd := newSessionsCmdForTest(c)
	cmd.SetArgs([]string{"ls"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsRecoverable(err) {
		t.Errorf("recoverable sentinel lost across cobra: %v", err)
	}
}

// TestSessionsCmdHasLsSubcommand — the `sessions` cobra root MUST
// register the `ls` leaf so `zen sessions ls` is a real path.
func TestSessionsCmdHasLsSubcommand(t *testing.T) {
	root := newSessionsCmdForTest(&fakeSessionsClient{})
	var found bool
	for _, sc := range root.Commands() {
		if sc.Name() == "ls" {
			found = true
			break
		}
	}
	if !found {
		t.Error("`sessions` cobra root missing `ls` subcommand")
	}
}

// TestSessionsCmdHTTPRoundTrip — the production sessions ls RunE MUST
// hit GET /v1/sessions and render the daemon-returned rows.
func TestSessionsCmdHTTPRoundTrip(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"sessions": [
				{
					"alias": "internal-platform-x",
					"sha8": "deadbeef",
					"status": "active",
					"last_attach": "2026-05-01T14:00:00Z",
					"pane_count": 7
				}
			]
		}`))
	}))
	defer srv.Close()
	resetSessionsClient(t, srv)

	cmd := NewSessionsCmdProd()
	cmd.SetArgs([]string{"ls"})
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method=%q want GET", gotMethod)
	}
	if gotPath != "/v1/sessions" {
		t.Errorf("path=%q want /v1/sessions", gotPath)
	}
	for _, want := range []string{"internal-platform-x", "deadbeef", "active", "7"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q; got %q", want, out.String())
		}
	}
}

func TestSessionsCmdHTTPEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sessions":[]}`))
	}))
	defer srv.Close()
	resetSessionsClient(t, srv)

	cmd := NewSessionsCmdProd()
	cmd.SetArgs([]string{"ls"})
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "no active sessions") {
		t.Errorf("expected empty-state; got %q", out.String())
	}
}

func TestSessionsCmdHTTPError500Unrecoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()
	resetSessionsClient(t, srv)

	cmd := NewSessionsCmdProd()
	cmd.SetArgs([]string{"ls"})
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

func TestSessionsCmdHTTPError503Unrecoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("phase I pending"))
	}))
	defer srv.Close()
	resetSessionsClient(t, srv)

	cmd := NewSessionsCmdProd()
	cmd.SetArgs([]string{"ls"})
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
