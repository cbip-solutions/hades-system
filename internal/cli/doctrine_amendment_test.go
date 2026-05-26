package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

type amendmentRec struct {
	ack    client.DoctrineDecision
	deny   client.DoctrineDecision
	revert client.DoctrineDecision
}

func newFakeDoctrineDaemon(t *testing.T, list client.DoctrineProposalList) (*httptest.Server, *amendmentRec) {
	t.Helper()
	rec := &amendmentRec{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/propose-list", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONP5(w, list)
	})
	mux.HandleFunc("/v1/doctrine/propose-show", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		for _, p := range list.Proposals {
			if p.ID == id {
				writeJSONP5(w, p)
				return
			}
		}
		http.Error(w, "not found", http.StatusNotFound)
	})
	mux.HandleFunc("/v1/doctrine/ack", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&rec.ack)
		writeJSONP5(w, map[string]string{"status": "applied"})
	})
	mux.HandleFunc("/v1/doctrine/deny", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&rec.deny)
		writeJSONP5(w, map[string]string{"status": "denied"})
	})
	mux.HandleFunc("/v1/doctrine/revert", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&rec.revert)
		writeJSONP5(w, map[string]string{"status": "reverted"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, rec
}

func runDoctrineSubcommand(t *testing.T, srvURL string, args ...string) (string, error) {
	t.Helper()
	root := NewDoctrineCmd()
	if err := root.PersistentFlags().Set(plan5DaemonURLFlag, srvURL); err != nil {
		t.Fatalf("set %s: %v", plan5DaemonURLFlag, err)
	}
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return buf.String(), err
}

func TestDoctrineProposeList(t *testing.T) {
	srv, _ := newFakeDoctrineDaemon(t, client.DoctrineProposalList{Proposals: []client.DoctrineProposal{
		{ID: "ADR-0020", Title: "Tighten worktree GC cadence", Status: "proposed", CooldownRemain: 1200},
		{ID: "ADR-0019", Title: "Reduce HRA L4 escalation threshold", Status: "applied"},
	}})
	out, err := runDoctrineSubcommand(t, srv.URL, "propose-list")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"ADR-0020", "ADR-0019", "Tighten", "applied"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestDoctrineProposeListEmpty(t *testing.T) {
	srv, _ := newFakeDoctrineDaemon(t, client.DoctrineProposalList{})
	out, err := runDoctrineSubcommand(t, srv.URL, "propose-list")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "no proposals") {
		t.Errorf("expected empty-state message: %q", out)
	}
}

func TestDoctrineProposeShow_Happy(t *testing.T) {
	srv, _ := newFakeDoctrineDaemon(t, client.DoctrineProposalList{Proposals: []client.DoctrineProposal{
		{
			ID: "ADR-0020", Title: "Tighten worktree GC cadence",
			Status: "proposed", BodyMarkdown: "## Context\nMotivation here\n",
			CooldownRemain: 600,
		},
	}})
	out, err := runDoctrineSubcommand(t, srv.URL, "propose-show", "ADR-0020")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"ADR-0020", "Tighten", "Status: proposed", "Motivation here", "600"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestDoctrineProposeShow_NotFound(t *testing.T) {
	srv, _ := newFakeDoctrineDaemon(t, client.DoctrineProposalList{})
	_, err := runDoctrineSubcommand(t, srv.URL, "propose-show", "ADR-9999")
	if err == nil {
		t.Fatal("expected error for missing ADR")
	}
}

func TestDoctrineAck_PassesIDToDaemon(t *testing.T) {
	srv, rec := newFakeDoctrineDaemon(t, client.DoctrineProposalList{})
	_, err := runDoctrineSubcommand(t, srv.URL, "ack", "ADR-0020")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rec.ack.ID != "ADR-0020" {
		t.Errorf("ack id not propagated: %+v", rec.ack)
	}
}

func TestDoctrineAck_RequiresID(t *testing.T) {
	srv, _ := newFakeDoctrineDaemon(t, client.DoctrineProposalList{})
	_, err := runDoctrineSubcommand(t, srv.URL, "ack")
	if err == nil {
		t.Fatal("expected error: missing id arg")
	}
}

func TestDoctrineDeny_RequiresReason(t *testing.T) {
	srv, _ := newFakeDoctrineDaemon(t, client.DoctrineProposalList{})
	_, err := runDoctrineSubcommand(t, srv.URL, "deny", "ADR-0020")
	if err == nil {
		t.Fatal("expected error: --reason required")
	}
}

func TestDoctrineDeny_RequiresID(t *testing.T) {
	srv, _ := newFakeDoctrineDaemon(t, client.DoctrineProposalList{})
	_, err := runDoctrineSubcommand(t, srv.URL, "deny", "--reason", "x")
	if err == nil {
		t.Fatal("expected error: missing id")
	}
}

func TestDoctrineDeny_Happy(t *testing.T) {
	srv, rec := newFakeDoctrineDaemon(t, client.DoctrineProposalList{})
	out, err := runDoctrineSubcommand(t, srv.URL, "deny", "ADR-0020", "--reason", "incompatible with G2 pin")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rec.deny.ID != "ADR-0020" || rec.deny.Reason != "incompatible with G2 pin" {
		t.Errorf("deny propagation broken: %+v", rec.deny)
	}
	if !strings.Contains(out, "denied") {
		t.Errorf("expected denied confirmation: %q", out)
	}
}

func TestDoctrineRevert_PassesIDToDaemon(t *testing.T) {
	srv, rec := newFakeDoctrineDaemon(t, client.DoctrineProposalList{})
	_, err := runDoctrineSubcommand(t, srv.URL, "revert", "ADR-0019")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rec.revert.ID != "ADR-0019" {
		t.Errorf("revert id not propagated: %+v", rec.revert)
	}
}

func TestDoctrineRevert_WithReason(t *testing.T) {
	srv, rec := newFakeDoctrineDaemon(t, client.DoctrineProposalList{})
	_, err := runDoctrineSubcommand(t, srv.URL, "revert", "ADR-0019", "--reason", "regression observed")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rec.revert.Reason != "regression observed" {
		t.Errorf("revert reason not propagated: %+v", rec.revert)
	}
}

func TestDoctrineRevert_RequiresID(t *testing.T) {
	srv, _ := newFakeDoctrineDaemon(t, client.DoctrineProposalList{})
	_, err := runDoctrineSubcommand(t, srv.URL, "revert")
	if err == nil {
		t.Fatal("expected error: missing id")
	}
}
