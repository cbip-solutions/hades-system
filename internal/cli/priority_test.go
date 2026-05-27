package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func resetPriorityClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(_ string) *client.Client { return client.NewWithBaseURL(srv.URL) }
	t.Cleanup(func() { TestOnlyClientFactory = prev })
}

func TestPriorityCmdRequiresOneOfBoostResetLs(t *testing.T) {
	cmd := NewPriorityCmd()
	cmd.SetArgs([]string{})
	stderr := &bytes.Buffer{}
	cmd.SetErr(stderr)
	cmd.SetOut(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Error("Execute() with no flags: want error, got nil")
	}
	if !strings.Contains(stderr.String(), "exactly one of --boost / --reset / --ls") {
		t.Errorf("error message missing required hint; got %q", stderr.String())
	}
}

func TestPriorityCmdMutuallyExclusive(t *testing.T) {
	cases := [][]string{
		{"--boost", "a", "--reset", "a"},
		{"--boost", "a", "--ls"},
		{"--reset", "a", "--ls"},
		{"--boost", "a", "--reset", "b", "--ls"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			cmd := NewPriorityCmd()
			cmd.SetArgs(args)
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetOut(&bytes.Buffer{})
			if err := cmd.Execute(); err == nil {
				t.Errorf("Execute(%v): want error (mutually exclusive), got nil", args)
			}
		})
	}
}

func TestPriorityCmdBoostRequiresDurationAndReason(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"missing duration", []string{"--boost", "internal-platform-x", "--reason", "urgent"}},
		{"missing reason", []string{"--boost", "internal-platform-x", "--duration", "4h"}},
		{"empty alias", []string{"--boost", "", "--duration", "4h", "--reason", "u"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd := NewPriorityCmd()
			cmd.SetArgs(c.args)
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetOut(&bytes.Buffer{})
			if err := cmd.Execute(); err == nil {
				t.Errorf("Execute(%v): want error (%s), got nil", c.args, c.name)
			}
		})
	}
}

func TestPriorityCmdBoostInvalidDuration(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"unparseable", []string{"--boost", "a", "--duration", "not-a-duration", "--reason", "u"}},
		{"zero", []string{"--boost", "a", "--duration", "0s", "--reason", "u"}},
		{"negative", []string{"--boost", "a", "--duration", "-1h", "--reason", "u"}},
		{"excess", []string{"--boost", "a", "--duration", "169h", "--reason", "u"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd := NewPriorityCmd()
			cmd.SetArgs(c.args)
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetOut(&bytes.Buffer{})
			if err := cmd.Execute(); err == nil {
				t.Errorf("Execute(%v): want error (%s)", c.args, c.name)
			}
		})
	}
}

func TestPriorityCmdBoostMultiplierBounds(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	resetPriorityClient(t, srv)

	cases := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{"default ok", []string{"--boost", "a", "--duration", "1h", "--reason", "u"}, false},
		{"explicit 3", []string{"--boost", "a", "--duration", "1h", "--reason", "u", "--multiplier", "3"}, false},
		{"max 100", []string{"--boost", "a", "--duration", "1h", "--reason", "u", "--multiplier", "100"}, false},
		{"zero", []string{"--boost", "a", "--duration", "1h", "--reason", "u", "--multiplier", "0"}, true},
		{"negative", []string{"--boost", "a", "--duration", "1h", "--reason", "u", "--multiplier", "-2"}, true},
		{"excess", []string{"--boost", "a", "--duration", "1h", "--reason", "u", "--multiplier", "1000"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd := NewPriorityCmd()
			cmd.SetArgs(c.args)
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetOut(&bytes.Buffer{})
			err := cmd.Execute()
			if (err != nil) != c.wantErr {
				t.Errorf("Execute(%v) err=%v, wantErr=%v", c.args, err, c.wantErr)
			}
		})
	}
}

func TestPriorityCmdBoostRendersOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	resetPriorityClient(t, srv)

	cmd := NewPriorityCmd()
	cmd.SetArgs([]string{"--boost", "internal-platform-x", "--duration", "4h", "--reason", "investigation"})
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := out.String()
	for _, want := range []string{"internal-platform-x", "investigation", "4h0m0s", "multiplier:", "expires:"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q; got %q", want, got)
		}
	}
}

func TestPriorityCmdResetRendersOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	resetPriorityClient(t, srv)

	cmd := NewPriorityCmd()
	cmd.SetArgs([]string{"--reset", "internal-platform-x"})
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "internal-platform-x") {
		t.Errorf("reset output missing alias; got %q", out.String())
	}
	if !strings.Contains(out.String(), "removed") {
		t.Errorf("reset output missing 'removed'; got %q", out.String())
	}
}

func TestPriorityCmdLsRendersHeaderEvenWhenEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"overrides":[]}`))
	}))
	defer srv.Close()
	resetPriorityClient(t, srv)

	cmd := NewPriorityCmd()
	cmd.SetArgs([]string{"--ls"})
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"ALIAS", "MULT", "EXPIRES", "REASON", "CREATED"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("ls header missing %q; got %q", want, out.String())
		}
	}
}

func TestPriorityCmdRendersExpiryRFC3339(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	resetPriorityClient(t, srv)

	cmd := NewPriorityCmd()
	cmd.SetArgs([]string{"--boost", "x", "--duration", "1h", "--reason", "u"})
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := out.String()

	if !strings.Contains(got, "T") || !strings.Contains(got, "Z") {
		t.Errorf("expiry not RFC3339; got %q", got)
	}
}

func TestParseDurationRejectsZeroNegativeExcess(t *testing.T) {
	cases := []struct {
		s    string
		want bool
	}{
		{"0s", true},
		{"-1h", true},
		{"169h", true},
		{"1ns", true},
		{"1h", false},
		{"4h", false},
		{"168h", false},
		{"24h", false},
		{"30m", false},
	}
	for _, c := range cases {
		got := validatePriorityDuration(c.s)
		if (got != nil) != c.want {
			t.Errorf("validatePriorityDuration(%q) err=%v wantErr=%v", c.s, got, c.want)
		}
	}
}

func TestPriorityRendererStableFormat(t *testing.T) {
	out := &bytes.Buffer{}
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	expires := now.Add(4 * time.Hour)
	renderBoost(out, "internal-platform-x", 3.0, 4*time.Hour, "urgent", expires)
	got := out.String()
	wantLines := []string{
		"priority boost queued",
		"project:    internal-platform-x",
		"multiplier: 3",
		"duration:   4h0m0s",
		"expires:    2026-05-01T16:00:00Z",
		"reason:     urgent",
	}
	for _, w := range wantLines {
		if !strings.Contains(got, w) {
			t.Errorf("renderBoost output missing %q; got %q", w, got)
		}
	}
}

// TestPriorityCmdBoostSendsHTTPBody — the boost command MUST reach the
// daemon's POST /v1/priority/boost with the alias / multiplier /
// expires_at / reason fields populated.
func TestPriorityCmdBoostSendsHTTPBody(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	resetPriorityClient(t, srv)

	cmd := NewPriorityCmd()
	cmd.SetArgs([]string{"--boost", "internal-platform-x", "--duration", "2h", "--reason", "investigation", "--multiplier", "2.5"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method=%q want POST", gotMethod)
	}
	if gotPath != "/v1/priority/boost" {
		t.Errorf("path=%q want /v1/priority/boost", gotPath)
	}
	if gotBody["alias"] != "internal-platform-x" {
		t.Errorf("body.alias=%v want internal-platform-x", gotBody["alias"])
	}
	if gotBody["reason"] != "investigation" {
		t.Errorf("body.reason=%v want investigation", gotBody["reason"])
	}

	if m, _ := gotBody["multiplier"].(float64); m != 2.5 {
		t.Errorf("body.multiplier=%v want 2.5", gotBody["multiplier"])
	}
	if _, ok := gotBody["expires_at"].(string); !ok {
		t.Errorf("body.expires_at missing; got %v", gotBody["expires_at"])
	}
}

// TestPriorityCmdResetSendsHTTPBody — the reset command MUST reach the
// daemon's POST /v1/priority/reset with the alias.
func TestPriorityCmdResetSendsHTTPBody(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	resetPriorityClient(t, srv)

	cmd := NewPriorityCmd()
	cmd.SetArgs([]string{"--reset", "internal-platform-x"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/v1/priority/reset" {
		t.Errorf("path=%q want /v1/priority/reset", gotPath)
	}
	if gotBody["alias"] != "internal-platform-x" {
		t.Errorf("body.alias=%v want internal-platform-x", gotBody["alias"])
	}
}

// TestPriorityCmdLsRendersRows — the ls command MUST GET /v1/priority/list
// and render each returned row in the table.
func TestPriorityCmdLsRendersRows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method=%q want GET", r.Method)
		}
		if r.URL.Path != "/v1/priority/list" {
			t.Errorf("path=%q want /v1/priority/list", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"overrides": [
				{
					"alias": "internal-platform-x",
					"multiplier": 3.0,
					"expires_at": "2026-05-01T16:00:00Z",
					"reason": "urgent investigation",
					"created_at": "2026-05-01T12:00:00Z"
				},
				{
					"alias": "zen-swarm",
					"multiplier": 2.5,
					"expires_at": "2026-05-02T08:00:00Z",
					"reason": "morning sweep",
					"created_at": "2026-05-01T11:00:00Z"
				}
			]
		}`))
	}))
	defer srv.Close()
	resetPriorityClient(t, srv)

	cmd := NewPriorityCmd()
	cmd.SetArgs([]string{"--ls"})
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := out.String()
	for _, want := range []string{"internal-platform-x", "zen-swarm", "3", "2.5", "urgent investigation", "morning sweep"} {
		if !strings.Contains(got, want) {
			t.Errorf("ls output missing %q; got %q", want, got)
		}
	}
}

func TestPriorityCmdBoost404IsRecoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("alias not found"))
	}))
	defer srv.Close()
	resetPriorityClient(t, srv)

	cmd := NewPriorityCmd()
	cmd.SetArgs([]string{"--boost", "missing", "--duration", "1h", "--reason", "u"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error on 404; got nil")
	}
	if !IsRecoverable(err) {
		t.Errorf("404 error not recoverable: %v", err)
	}
}

func TestPriorityCmdReset404IsRecoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("alias not found"))
	}))
	defer srv.Close()
	resetPriorityClient(t, srv)

	cmd := NewPriorityCmd()
	cmd.SetArgs([]string{"--reset", "missing"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error on 404; got nil")
	}
	if !IsRecoverable(err) {
		t.Errorf("404 reset error not recoverable: %v", err)
	}
}

func TestPriorityCmdBoost500IsUnrecoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()
	resetPriorityClient(t, srv)

	cmd := NewPriorityCmd()
	cmd.SetArgs([]string{"--boost", "x", "--duration", "1h", "--reason", "u"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error on 500")
	}
	if IsRecoverable(err) {
		t.Errorf("500 error wrongly marked recoverable: %v", err)
	}
}

func TestPriorityCmdLs500IsUnrecoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()
	resetPriorityClient(t, srv)

	cmd := NewPriorityCmd()
	cmd.SetArgs([]string{"--ls"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error on 500")
	}
	if IsRecoverable(err) {
		t.Errorf("500 ls error wrongly marked recoverable: %v", err)
	}
}

func TestPriorityCmdReset500IsUnrecoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()
	resetPriorityClient(t, srv)

	cmd := NewPriorityCmd()
	cmd.SetArgs([]string{"--reset", "x"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error on 500")
	}
	if IsRecoverable(err) {
		t.Errorf("500 reset error wrongly marked recoverable: %v", err)
	}
}

func TestPriorityCmdBoost422IsRecoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte("invalid override"))
	}))
	defer srv.Close()
	resetPriorityClient(t, srv)

	cmd := NewPriorityCmd()
	cmd.SetArgs([]string{"--boost", "x", "--duration", "1h", "--reason", "u"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error on 422")
	}
	if !IsRecoverable(err) {
		t.Errorf("422 boost error not recoverable: %v", err)
	}
}

func TestRunPriorityBoostEmptyAliasGuard(t *testing.T) {
	c := client.NewWithBaseURL("http://unused")
	cmd := NewPriorityCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := runPriorityBoost(t.Context(), cmd, c, "  ", "1h", "u", 3.0)
	if err == nil {
		t.Fatal("expected error on empty alias")
	}
	if !IsRecoverable(err) {
		t.Errorf("empty-alias error not recoverable: %v", err)
	}
}

func TestRunPriorityResetEmptyAliasGuard(t *testing.T) {
	c := client.NewWithBaseURL("http://unused")
	cmd := NewPriorityCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := runPriorityReset(t.Context(), cmd, c, "  ")
	if err == nil {
		t.Fatal("expected error on empty alias")
	}
	if !IsRecoverable(err) {
		t.Errorf("empty-alias error not recoverable: %v", err)
	}
}

func TestRenderRelativeAgoBranches(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		then time.Time
		want string
	}{
		{"seconds", now.Add(-30 * time.Second), "30s ago"},
		{"minutes", now.Add(-10 * time.Minute), "10m ago"},
		{"hours", now.Add(-3 * time.Hour), "3h ago"},
		{"days", now.Add(-72 * time.Hour), "3d ago"},
		{"future", now.Add(time.Hour), "0s ago"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := renderRelativeAgo(now, c.then)
			if got != c.want {
				t.Errorf("renderRelativeAgo(%v) = %q, want %q", c.then, got, c.want)
			}
		})
	}
}

// TestPriorityCmdRegisteredOnProjectTree — `zen project priority` MUST
// be reachable via the project subcommand (replaces reserved
// not-implemented slot).
func TestPriorityCmdRegisteredOnProjectTree(t *testing.T) {
	root := NewProjectCmd()
	var found bool
	for _, sc := range root.Commands() {
		if sc.Name() == "priority" {
			found = true

			if sc.Flags().Lookup("boost") == nil {
				t.Error("priority subcommand missing --boost flag (still the Phase A stub?)")
			}
			if sc.Flags().Lookup("reset") == nil {
				t.Error("priority subcommand missing --reset flag (still the Phase A stub?)")
			}
			if sc.Flags().Lookup("ls") == nil {
				t.Error("priority subcommand missing --ls flag (still the Phase A stub?)")
			}
			break
		}
	}
	if !found {
		t.Error("priority subcommand not registered on `zen project`")
	}
}
