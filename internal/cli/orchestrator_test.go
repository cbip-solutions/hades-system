package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func newTestClient(baseURL string) *client.Client {
	return client.NewWithBaseURL(baseURL)
}

func orchestratorPinDirect(c *client.Client, scope, project, session, tier, provider, ttl, reason string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.OrchestratorPin(ctx, client.OrchestratorPinReq{
		Scope: scope, Project: project, Session: session,
		Tier: tier, Provider: provider, TTL: ttl, Reason: reason,
	})
}

func ptrTime(t time.Time) *time.Time { return &t }

func TestPrintPin(t *testing.T) {
	cases := []struct {
		name string
		pin  client.OrchestratorPinSummary
		want []string // substrings that MUST appear in output
	}{
		{
			name: "permanent global pin no reason",
			pin: client.OrchestratorPinSummary{
				Scope: "global", ScopeID: "", Tier: "in-house",
				SetAt: time.Unix(1700000000, 0),
			},
			want: []string{"global", "in-house", "permanent"},
		},
		{
			name: "TTL session pin with reason",
			pin: client.OrchestratorPinSummary{
				Scope: "session", ScopeID: "sess-1", Tier: "openclaude",
				SetAt:     time.Unix(1700000000, 0),
				ExpiresAt: ptrTime(time.Now().Add(time.Hour)),
				Reason:    "smoke test",
			},
			want: []string{"session/sess-1", "openclaude", "expires_in=", "smoke test"},
		},
		{
			name: "project pin permanent with reason",
			pin: client.OrchestratorPinSummary{
				Scope: "project", ScopeID: "internal-platform-x", Tier: "in-house",
				SetAt:  time.Unix(1700000000, 0),
				Reason: "release smoke",
			},
			want: []string{"project/internal-platform-x", "in-house", "permanent", "release smoke"},
		},
		{
			name: "TTL session pin no reason",
			pin: client.OrchestratorPinSummary{
				Scope: "session", ScopeID: "s2", Tier: "openclaude",
				SetAt:     time.Unix(1700000000, 0),
				ExpiresAt: ptrTime(time.Now().Add(time.Hour)),
			},
			want: []string{"session/s2", "expires_in="},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			printPin(&buf, tc.pin)
			out := buf.String()
			for _, sub := range tc.want {
				if !strings.Contains(out, sub) {
					t.Errorf("output %q missing substring %q", out, sub)
				}
			}
		})
	}
}

func TestOrchestratorSubcommandsHelp(t *testing.T) {
	want := []string{
		"status", "pin", "unpin", "pins", "probe", "history",
		"state", "depth", "pool", "capture", "replay",
	}
	root := NewOrchestratorCmd()
	have := map[string]bool{}
	for _, c := range root.Commands() {
		have[c.Name()] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("missing subcommand: orchestrator %s", w)
		}
	}
	if len(root.Commands()) != len(want) {
		t.Errorf("unexpected subcommand count = %d, want %d (got: %v)",
			len(root.Commands()), len(want), have)
	}
}

func TestOrchestratorPinFlags(t *testing.T) {
	root := NewOrchestratorCmd()
	for _, c := range root.Commands() {
		if c.Name() != "pin" {
			continue
		}
		for _, want := range []string{"scope", "project", "session", "tier", "provider", "for", "reason"} {
			if c.Flags().Lookup(want) == nil {
				t.Errorf("pin flag missing: --%s", want)
			}
		}
		return
	}
	t.Fatal("pin subcommand not found")
}

func TestOrchestratorUnpinFlags(t *testing.T) {
	root := NewOrchestratorCmd()
	for _, c := range root.Commands() {
		if c.Name() != "unpin" {
			continue
		}
		for _, want := range []string{"scope", "project", "session", "all"} {
			if c.Flags().Lookup(want) == nil {
				t.Errorf("unpin flag missing: --%s", want)
			}
		}
		return
	}
	t.Fatal("unpin subcommand not found")
}

func TestValidatePinFlags(t *testing.T) {
	cases := []struct {
		name, scope, project, session, tier string
		wantErr                             string
	}{
		{"global ok", "global", "", "", "in-house", ""},
		{"project ok", "project", "p1", "", "in-house", ""},
		{"session ok", "session", "", "s1", "in-house", ""},

		{"missing scope", "", "", "", "in-house", "--scope is required"},
		{"missing tier", "global", "", "", "", "--tier is required"},
		{"global with project", "global", "p", "", "in-house", "must NOT specify"},
		{"global with session", "global", "", "s", "in-house", "must NOT specify"},
		{"project missing id", "project", "", "", "in-house", "--scope=project requires --project"},
		{"project with session", "project", "p", "s", "in-house", "must NOT specify --session"},
		{"session missing id", "session", "", "", "in-house", "--scope=session requires --session"},
		{"session with project", "session", "p", "s", "in-house", "must NOT specify --project"},
		{"unknown scope", "weird", "", "", "in-house", "must be one of"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validatePinFlags(c.scope, c.project, c.session, c.tier)
			if c.wantErr == "" {
				if err != nil {
					t.Errorf("got %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error containing %q, got nil", c.wantErr)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("err = %q, want substring %q", err.Error(), c.wantErr)
			}
		})
	}
}

func TestValidateUnpinFlags(t *testing.T) {
	cases := []struct {
		name                    string
		scope, project, session string
		all                     bool
		wantErr                 string
	}{
		{"all ok", "", "", "", true, ""},
		{"global ok", "global", "", "", false, ""},
		{"project ok", "project", "p1", "", false, ""},
		{"session ok", "session", "", "s1", false, ""},

		{"all and scope", "global", "", "", true, "mutually exclusive"},
		{"all and project", "", "p1", "", true, "mutually exclusive"},
		{"neither", "", "", "", false, "either --all OR --scope"},
		{"global with project", "global", "p", "", false, "must NOT specify"},
		{"project missing id", "project", "", "", false, "requires --project"},
		{"session missing id", "session", "", "", false, "requires --session"},
		{"unknown scope", "weird", "", "", false, "must be one of"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateUnpinFlags(c.scope, c.project, c.session, c.all)
			if c.wantErr == "" {
				if err != nil {
					t.Errorf("got %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error containing %q, got nil", c.wantErr)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("err = %q, want substring %q", err.Error(), c.wantErr)
			}
		})
	}
}

func TestOrchestratorPinReturnsErrorOnBadFlags(t *testing.T) {
	root := NewRootCmd()
	root.SetArgs([]string{"orchestrator", "pin", "--scope", "project"})
	var stderr bytes.Buffer
	root.SetErr(&stderr)
	root.SetOut(&stderr)
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error from missing --tier; got nil")
	}
}

func TestOrchestratorClientPinWireShape(t *testing.T) {
	var got struct {
		Method, Path string
		Body         json.RawMessage
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/orchestrator/pin", func(w http.ResponseWriter, r *http.Request) {
		got.Method = r.Method
		got.Path = r.URL.Path
		buf := bytes.Buffer{}
		_, _ = buf.ReadFrom(r.Body)
		got.Body = buf.Bytes()
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := newTestClient(srv.URL)
	if err := orchestratorPinDirect(c, "project", "myproj", "", "openclaude", "", "30m", "operator override for incident X"); err != nil {
		t.Fatalf("Pin: %v", err)
	}
	if got.Method != "POST" || got.Path != "/v1/orchestrator/pin" {
		t.Errorf("got method=%q path=%q", got.Method, got.Path)
	}
	var pinPayload struct {
		Scope, Project, Tier, TTL, Reason string
	}
	if err := json.Unmarshal(got.Body, &pinPayload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if pinPayload.Scope != "project" || pinPayload.Project != "myproj" ||
		pinPayload.Tier != "openclaude" || pinPayload.TTL != "30m" ||
		pinPayload.Reason != "operator override for incident X" {
		t.Errorf("payload mismatch: %+v", pinPayload)
	}
}

func TestOrchestratorClientUnpinWireShape(t *testing.T) {
	var got json.RawMessage
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/orchestrator/unpin", func(w http.ResponseWriter, r *http.Request) {
		buf := bytes.Buffer{}
		_, _ = buf.ReadFrom(r.Body)
		got = buf.Bytes()
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := newTestClient(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.OrchestratorUnpin(ctx, client.OrchestratorUnpinReq{All: true}); err != nil {
		t.Fatalf("Unpin: %v", err)
	}
	var payload struct {
		All bool `json:"all"`
	}
	if err := json.Unmarshal(got, &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !payload.All {
		t.Errorf("expected all=true, got: %s", string(got))
	}
}

func TestOrchestratorClientPinsWireShape(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/orchestrator/pins", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pins":[{"id":1,"scope":"global","tier":"openclaude","set_at":"2026-04-30T00:00:00Z"}]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := newTestClient(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := c.OrchestratorPins(ctx)
	if err != nil {
		t.Fatalf("Pins: %v", err)
	}
	if len(r.Pins) != 1 || r.Pins[0].Tier != "openclaude" {
		t.Errorf("pins shape: %+v", r.Pins)
	}
}

func TestOrchestratorClientProbeWireShape(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/orchestrator/probe", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tiers":[{"tier":"in-house","state":"closed"}]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := newTestClient(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := c.OrchestratorProbe(ctx)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if len(r.Tiers) != 1 {
		t.Errorf("tiers shape: %+v", r.Tiers)
	}
}

func TestOrchestratorClientHistoryWireShape(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/orchestrator/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tiers":[{"tier":"in-house","state":"closed"}],"note":"post-rescope: ..."}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := newTestClient(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := c.OrchestratorHistory(ctx)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(r.Tiers) != 1 || !strings.Contains(r.Note, "rescope") {
		t.Errorf("history shape: %+v", r)
	}
}

func TestOrchestratorClientStatusWireShape(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/orchestrator/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"tiers": [{"tier":"in-house","state":"closed"},{"tier":"openclaude","state":"suspect"}],
			"pins": [{"id":1,"scope":"global","tier":"openclaude","set_at":"2026-04-30T00:00:00Z"}],
			"costs": [{"tier":"in-house","total_usd_30d":0.0,"window":"30d"}]
		}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := newTestClient(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := c.OrchestratorStatus(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(r.Tiers) != 2 || r.Tiers[1].State != "suspect" {
		t.Errorf("tiers shape: %+v", r.Tiers)
	}
	if len(r.Pins) != 1 || r.Pins[0].Tier != "openclaude" {
		t.Errorf("pins shape: %+v", r.Pins)
	}
	if len(r.Costs) != 1 {
		t.Errorf("costs shape: %+v", r.Costs)
	}
}
