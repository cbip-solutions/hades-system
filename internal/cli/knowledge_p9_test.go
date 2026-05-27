package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func invokeKnowledge9Cmd(t *testing.T, args []string, baseURL string) (string, string, error) {
	t.Helper()
	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(_ string) *client.Client {
		return client.NewWithBaseURL(baseURL)
	}
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	cmd := NewKnowledge9Cmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func mockKnowledge9Server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/knowledge/query", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.KnowledgeResult{
				{
					NoteID:    "internal-platform-x/M0-pattern-vault-format",
					ProjectID: "internal-platform-x",
					Snippet:   "M0 Pattern vault format",
					Score:     0.92,
				},
			},
			"count": 1,
		})
	})

	mux.HandleFunc("/v1/knowledge/promote", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/v1/knowledge/unpromote", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/v1/knowledge/list", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.KnowledgeNote{
				{NoteID: "zen-swarm/methodology-section-4", ProjectID: "zen-swarm", Path: "docs/METHODOLOGY.md", Pinned: false},
				{NoteID: "internal-platform-x/M0-pattern-vault-format", ProjectID: "internal-platform-x", Path: "docs/M0.md", Pinned: true},
			},
			"count": 2,
		})
	})

	mux.HandleFunc("/v1/knowledge/rebuild", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(client.KnowledgeRebuildResp{
			JobID:     "rebuild-zen-swarm-1",
			StartedAt: 1762000000,
		})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestKnowledge9_RegistersAllSubcommands(t *testing.T) {
	cmd := NewKnowledge9Cmd()
	want := []string{"query", "promote", "unpromote", "ls", "rebuild"}
	have := map[string]bool{}
	for _, c := range cmd.Commands() {
		have[c.Name()] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("knowledge subcommand %q not registered", w)
		}
	}
}

func TestKnowledge9Query_HappyPath(t *testing.T) {
	srv := mockKnowledge9Server(t)
	stdout, _, err := invokeKnowledge9Cmd(t, []string{"query", "vault format"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "internal-platform-x") {
		t.Errorf("expected internal-platform-x in output; got: %s", stdout)
	}
}

func TestKnowledge9Query_GlobalAndProjectMutuallyExclusive(t *testing.T) {
	srv := mockKnowledge9Server(t)
	_, _, err := invokeKnowledge9Cmd(t, []string{"query", "x", "--global", "--project", "y"}, srv.URL)
	if err == nil {
		t.Fatal("expected --global and --project mutually exclusive error; got nil")
	}
}

func TestKnowledge9Query_AuditChainFlagPropagates(t *testing.T) {
	srv := mockKnowledge9Server(t)
	stdout, _, err := invokeKnowledge9Cmd(t, []string{"query", "vault format", "--audit-chain"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "internal-platform-x") {
		t.Errorf("expected internal-platform-x hit; got: %s", stdout)
	}
}

func TestKnowledge9Promote_RequiresReason(t *testing.T) {
	srv := mockKnowledge9Server(t)
	_, _, err := invokeKnowledge9Cmd(t, []string{"promote", "internal-platform-x/x"}, srv.URL)
	if err == nil {
		t.Fatal("expected --reason required error (inv-zen-146); got nil")
	}
}

func TestKnowledge9Promote_RejectsEmptyReason(t *testing.T) {
	srv := mockKnowledge9Server(t)
	_, _, err := invokeKnowledge9Cmd(t, []string{"promote", "internal-platform-x/x", "--reason", "   "}, srv.URL)
	if err == nil {
		t.Fatal("expected non-empty --reason error; got nil")
	}
}

func TestKnowledge9Promote_HappyPath(t *testing.T) {
	srv := mockKnowledge9Server(t)
	stdout, _, err := invokeKnowledge9Cmd(t,
		[]string{"promote", "internal-platform-x/M0-pattern-vault-format", "--reason", "applies to all max-scope projects"},
		srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "promoted") {
		t.Errorf("expected 'promoted' in output; got: %s", stdout)
	}
}

func TestKnowledge9Promote_ForwardsProjectFlag(t *testing.T) {
	var gotProject string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/knowledge/promote", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ProjectID string `json:"project_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		gotProject = body.ProjectID
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	_, _, err := invokeKnowledge9Cmd(t,
		[]string{"promote", "M0-pattern-vault-format", "--project", "internal-platform-x", "--reason", "applies"},
		srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if gotProject != "internal-platform-x" {
		t.Fatalf("project_id = %q, want internal-platform-x", gotProject)
	}
}

func TestKnowledge9Unpromote_RequiresReason(t *testing.T) {
	srv := mockKnowledge9Server(t)
	_, _, err := invokeKnowledge9Cmd(t, []string{"unpromote", "x"}, srv.URL)
	if err == nil {
		t.Fatal("expected --reason required error (inv-zen-146); got nil")
	}
}

func TestKnowledge9Unpromote_HappyPath(t *testing.T) {
	srv := mockKnowledge9Server(t)
	stdout, _, err := invokeKnowledge9Cmd(t,
		[]string{"unpromote", "internal-platform-x/old-pattern", "--reason", "superseded by ADR-0072"},
		srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "unpromoted") {
		t.Errorf("expected 'unpromoted' in output; got: %s", stdout)
	}
}

func TestKnowledge9Unpromote_ForwardsProjectFlag(t *testing.T) {
	var gotProject string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/knowledge/unpromote", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ProjectID string `json:"project_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		gotProject = body.ProjectID
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	_, _, err := invokeKnowledge9Cmd(t,
		[]string{"unpromote", "M0-pattern-vault-format", "--project", "internal-platform-x", "--reason", "superseded"},
		srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if gotProject != "internal-platform-x" {
		t.Fatalf("project_id = %q, want internal-platform-x", gotProject)
	}
}

func TestKnowledge9Ls_Default(t *testing.T) {
	srv := mockKnowledge9Server(t)
	stdout, _, err := invokeKnowledge9Cmd(t, []string{"ls"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	for _, want := range []string{"zen-swarm/methodology-section-4", "internal-platform-x/M0-pattern-vault-format"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in ls output: %s", want, stdout)
		}
	}
}

func TestKnowledge9Rebuild_RequiresProject(t *testing.T) {
	srv := mockKnowledge9Server(t)
	_, _, err := invokeKnowledge9Cmd(t, []string{"rebuild"}, srv.URL)
	if err == nil {
		t.Fatal("expected --project required error; got nil")
	}
}

func TestKnowledge9Rebuild_HappyPath(t *testing.T) {
	srv := mockKnowledge9Server(t)
	stdout, _, err := invokeKnowledge9Cmd(t, []string{"rebuild", "--project", "zen-swarm"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "rebuild-zen-swarm-1") {
		t.Errorf("expected job_id in output; got: %s", stdout)
	}
}

func TestKnowledge9Query_PinnedOnly(t *testing.T) {
	srv := mockKnowledge9Server(t)
	stdout, _, err := invokeKnowledge9Cmd(t, []string{"query", "doctrine", "--pinned-only"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "internal-platform-x") {
		t.Errorf("expected internal-platform-x hit; got: %s", stdout)
	}
}

func TestKnowledge9Query_WithProject(t *testing.T) {
	srv := mockKnowledge9Server(t)
	stdout, _, err := invokeKnowledge9Cmd(t, []string{"query", "max scope", "--project", "internal-platform-x"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "internal-platform-x") {
		t.Errorf("expected internal-platform-x hit; got: %s", stdout)
	}
}

func TestKnowledge9Query_WithLimit(t *testing.T) {
	srv := mockKnowledge9Server(t)

	stdout, _, err := invokeKnowledge9Cmd(t, []string{"query", "vault format", "--limit", "5"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "internal-platform-x") {
		t.Errorf("expected internal-platform-x hit; got: %s", stdout)
	}
}

func TestKnowledge9Query_EmptyResult(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/knowledge/query", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []client.KnowledgeResult{}, "count": 0})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	stdout, _, err := invokeKnowledge9Cmd(t, []string{"query", "nothing here"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "no results") {
		t.Errorf("expected '(no results)'; got: %s", stdout)
	}
}

func TestKnowledge9Ls_PinnedOnly(t *testing.T) {
	srv := mockKnowledge9Server(t)
	stdout, _, err := invokeKnowledge9Cmd(t, []string{"ls", "--pinned-only"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "internal-platform-x/M0-pattern-vault-format") {
		t.Errorf("expected pinned note in output; got: %s", stdout)
	}
}

func TestKnowledge9Query_LongSnippetTruncated(t *testing.T) {

	longSnippet := strings.Repeat("a", 80)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/knowledge/query", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.KnowledgeResult{
				{NoteID: "proj/note1", ProjectID: "proj", Snippet: longSnippet, Score: 0.9},
			},
			"count": 1,
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	stdout, _, err := invokeKnowledge9Cmd(t, []string{"query", "something"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}

	if !strings.Contains(stdout, "…") {
		t.Errorf("expected ellipsis in truncated output; got: %s", stdout)
	}
}
