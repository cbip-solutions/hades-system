// projects_test.go — Task L-1 CLI tests for `zen projects ls`.
//
// Tests cover:
// - `zen projects ls` renders all 5 columns (alias, sha8, path,
// last-active, state) including the empty-state path.
// - tabwriter alignment + canonical relative-time formatting.
// - never-activated row renders as "never".
// - archived rows render with state=archived.
// - render path tolerates short / malformed ids without panic.
// - error propagation: client-side err → wrapped "projects ls:..."
// so the operator sees the namespace prefix even on a deep failure.
// - subcommand registration: `projects` MUST have an `ls` subcommand
// and `ls --help` must surface a Usage line (drift from
// newSessionsLsCmd / newProjectsLsCmd would surface here).
// - HTTP path coverage via httptest: client → daemon GET /v1/projects.
package cli

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
)

type fakeProjectsClient struct {
	rows []ProjectRow
	err  error
}

func (f *fakeProjectsClient) ListProjects(_ context.Context) ([]ProjectRow, error) {
	return f.rows, f.err
}

func newProjectsCmdForTest(c ProjectsClient) *cobra.Command {
	return NewProjectsCmd(func(_ *cobra.Command) ProjectsClient { return c })
}

func runCobra(t *testing.T, cmd *cobra.Command, args []string) (string, string, error) {
	t.Helper()
	cmd.SetArgs(args)
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

// TestProjectsCmdHasLsSubcommand — `projects` MUST have an `ls`
// subcommand. If a future refactor flag-counts the design (e.g.,
// `projects --ls`) this test surfaces it.
func TestProjectsCmdHasLsSubcommand(t *testing.T) {
	root := NewProjectsCmd(func(_ *cobra.Command) ProjectsClient { return &fakeProjectsClient{} })
	if findCobraChild(root.Commands(), "ls") == nil {
		t.Fatal("`projects` registered without `ls` subcommand")
	}
}

func TestProjectsLsHelpHasUsage(t *testing.T) {
	cmd := newProjectsCmdForTest(&fakeProjectsClient{})
	out, errOut, err := runCobra(t, cmd, []string{"ls", "--help"})
	if err != nil {
		t.Fatalf("Execute --help: %v", err)
	}
	combined := out + errOut
	if !strings.Contains(combined, "Usage:") {
		t.Errorf("help output missing 'Usage:' line; got %q", combined)
	}
	if !strings.Contains(combined, "ls") {
		t.Errorf("help output missing 'ls' subcommand reference; got %q", combined)
	}
}

func TestProjectsLsRendersAllColumns(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	c := &fakeProjectsClient{
		rows: []ProjectRow{
			{
				ID:              "9f3a1c2d4e5f6789aaabbbcccdddeeefffaaabbbcccdddeeefff111122223333",
				Alias:           "internal-platform-x",
				Path:            "/path/to/projects/internal-platform-x",
				LastActivatedAt: now.Add(-2 * time.Hour),
				AutonomousState: "active",
			},
			{
				ID:              "b8e1c4d61234567890abcdef1234567890abcdef1234567890abcdef12345678",
				Alias:           "zen-swarm",
				Path:            "/path/to/projects/zen-swarm-p7",
				LastActivatedAt: now.Add(-8 * time.Minute),
				AutonomousState: "active",
			},
		},
	}
	cmd := newProjectsCmdForTest(c)

	out, _, err := runCobra(t, cmd, []string{"ls"})
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}

	for _, hdr := range []string{"ALIAS", "SHA8", "PATH", "LAST-ACTIVE", "STATE"} {
		if !strings.Contains(out, hdr) {
			t.Errorf("output missing header %q\n%s", hdr, out)
		}
	}

	for _, want := range []string{"internal-platform-x", "9f3a1c2d", "/path/to/projects/internal-platform-x",
		"zen-swarm", "b8e1c4d6", "/path/to/projects/zen-swarm-p7", "active"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
}

func TestProjectsLsEmptyStateMessage(t *testing.T) {
	cmd := newProjectsCmdForTest(&fakeProjectsClient{rows: nil})
	out, _, err := runCobra(t, cmd, []string{"ls"})
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !strings.Contains(out, "no projects registered") {
		t.Errorf("expected 'no projects registered'; got %q", out)
	}

	if strings.Contains(out, "ALIAS") {
		t.Errorf("empty state must not print column headers; got %q", out)
	}
}

func TestProjectsLsRendersArchivedState(t *testing.T) {
	now := time.Now()
	archivedAt := now.Add(-100 * time.Hour)
	rows := []ProjectRow{
		{
			ID:              "aaaaaaaa11111111",
			Alias:           "active-one",
			Path:            "/p/a",
			AutonomousState: "active",
		},
		{
			ID:              "bbbbbbbb22222222",
			Alias:           "archived-one",
			Path:            "/p/b",
			AutonomousState: "complete",
			LastActivatedAt: archivedAt,
		},
	}
	var buf bytes.Buffer
	renderProjectsList(&buf, rows, now)
	out := buf.String()

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (header+2 rows); got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[1], "active-one") || !strings.HasSuffix(strings.TrimSpace(lines[1]), "active") {
		t.Errorf("row 1 expected to end in state 'active'; got %q", lines[1])
	}
	if !strings.Contains(lines[2], "archived-one") || !strings.HasSuffix(strings.TrimSpace(lines[2]), "archived") {
		t.Errorf("row 2 expected to end in state 'archived'; got %q", lines[2])
	}
}

func TestProjectsLsNeverActivatedRowRendersNever(t *testing.T) {
	rows := []ProjectRow{
		{ID: "cccccccc", Alias: "fresh", Path: "/p/c", AutonomousState: "active"},
	}
	var buf bytes.Buffer
	renderProjectsList(&buf, rows, time.Now())
	if !strings.Contains(buf.String(), "never") {
		t.Errorf("expected 'never' for zero-time row; got %q", buf.String())
	}
}

// TestProjectsLsRendersShortIDDefensively — id shorter than 8 chars
// (malformed daemon response) renders the whole id without panic. The
// CLI MUST tolerate daemon-side wire drift gracefully.
func TestProjectsLsRendersShortIDDefensively(t *testing.T) {
	rows := []ProjectRow{
		{ID: "abc", Alias: "tiny", Path: "/p/d", AutonomousState: "active"},
	}
	var buf bytes.Buffer
	renderProjectsList(&buf, rows, time.Now())
	if !strings.Contains(buf.String(), "abc") {
		t.Errorf("expected short id 'abc' rendered as-is; got %q", buf.String())
	}

}

func TestProjectsLsRelativeTimeBuckets(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		ts   time.Time
		want string
	}{
		{"just-now", now.Add(-30 * time.Second), "just now"},
		{"future-skew", now.Add(10 * time.Second), "just now"},
		{"minutes", now.Add(-15 * time.Minute), "15m ago"},
		{"hours", now.Add(-3 * time.Hour), "3h ago"},
		{"days", now.Add(-5 * 24 * time.Hour), "5d ago"},
		{"stale-rfc3339", now.Add(-90 * 24 * time.Hour), "2026-02-06"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := humanizeRelative(now, tc.ts)
			if got != tc.want {
				t.Errorf("humanizeRelative(%v -> %v): want %q, got %q", now, tc.ts, tc.want, got)
			}
		})
	}
}

func TestProjectsLsErrorWrapsNamespacePrefix(t *testing.T) {
	c := &fakeProjectsClient{err: errors.New("daemon ate the request")}
	cmd := newProjectsCmdForTest(c)
	_, errOut, err := runCobra(t, cmd, []string{"ls"})
	if err == nil {
		t.Fatal("expected non-nil err on client failure")
	}
	if !strings.Contains(err.Error(), "projects ls:") {
		t.Errorf("expected 'projects ls:' prefix in error; got %v", err)
	}
	if !strings.Contains(err.Error(), "daemon ate the request") {
		t.Errorf("expected wrapped inner error visible; got %v (stderr=%q)", err, errOut)
	}
}

func TestProjectsLsViaHTTPRoundTrips(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/projects" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "wrong route", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"projects": [
				{
					"id": "9f3a1c2d4e5f6789aaabbbcccdddeeefffaaabbbcccdddeeefff111122223333",
					"alias": "internal-platform-x",
					"path": "/p/internal-platform-x",
					"autonomous_state": "active"
				}
			]
		}`))
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	pc := &productionProjectsClient{c: c}
	rows, err := pc.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d: %+v", len(rows), rows)
	}
	if rows[0].Alias != "internal-platform-x" {
		t.Errorf("expected alias=internal-platform-x, got %s", rows[0].Alias)
	}
	if rows[0].IsArchived() {
		t.Errorf("expected non-archived (autonomous_state=active); got archived")
	}
}

func TestNewProjectsCmdProdResolvesFactoryAtRunE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"projects":[]}`))
	}))
	defer srv.Close()
	prev := TestOnlyClientFactory
	t.Cleanup(func() { TestOnlyClientFactory = prev })
	TestOnlyClientFactory = func(_ string) *client.Client {
		return client.NewWithBaseURL(srv.URL)
	}
	root := NewRootCmd()
	out, errOut, err := runCobra(t, root, []string{"projects", "ls"})
	if err != nil {
		t.Fatalf("Execute: %v stderr=%q", err, errOut)
	}
	if !strings.Contains(out, "no projects registered") {
		t.Errorf("expected empty-state line; got %q", out)
	}
}

func TestProjectsLsViaHTTP501Surfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"plan":7,"feature":"Multi-project + tmux + scheduling"}`, http.StatusNotImplemented)
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	pc := &productionProjectsClient{c: c}
	_, err := pc.ListProjects(context.Background())
	if err == nil {
		t.Fatal("expected error on 501 response; got nil")
	}
}
