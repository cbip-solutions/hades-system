package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cli "github.com/cbip-solutions/hades-system/internal/doctrine/cli"
)

func invokeDebug(t *testing.T, args []string, baseURL string) (string, string, error) {
	t.Helper()
	prev := cli.TestOnlyClientFactory
	cli.TestOnlyClientFactory = func() *cli.Client { return cli.NewClient(baseURL) }
	t.Cleanup(func() { cli.TestOnlyClientFactory = prev })
	root := cli.NewRoot()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func mockDaemonForReinforce(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/reinforce", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			TaskKind     string `json:"task_kind"`
			ProjectAlias string `json:"project_alias"`
			Stage        string `json:"stage"`
			Phase        string `json:"phase"`
			PlanID       string `json:"plan_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"rendered": "## Worker reinforcement block\n\nTaskKind: " + body.TaskKind +
				"\nProjectAlias: " + body.ProjectAlias +
				"\nStage: " + body.Stage +
				"\nPhase: " + body.Phase +
				"\nPlanID: " + body.PlanID +
				"\n\nMax-scope axioms apply.",
		})
	})
	return httptest.NewServer(mux)
}

func TestReinforce_Default_RendersTemplate(t *testing.T) {
	srv := mockDaemonForReinforce(t)
	defer srv.Close()
	stdout, _, err := invokeDebug(t, []string{"reinforce", "worker"}, srv.URL)
	if err != nil {
		t.Fatalf("reinforce: %v", err)
	}
	for _, want := range []string{"Worker reinforcement", "TaskKind: worker", "Max-scope"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in: %s", want, stdout)
		}
	}
}

func TestReinforce_AllFlags_PassedToDaemon(t *testing.T) {
	var captured struct {
		TaskKind, ProjectAlias, Stage, Phase, PlanID string
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/reinforce", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			TaskKind     string `json:"task_kind"`
			ProjectAlias string `json:"project_alias"`
			Stage        string `json:"stage"`
			Phase        string `json:"phase"`
			PlanID       string `json:"plan_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		captured.TaskKind = body.TaskKind
		captured.ProjectAlias = body.ProjectAlias
		captured.Stage = body.Stage
		captured.Phase = body.Phase
		captured.PlanID = body.PlanID
		_ = json.NewEncoder(w).Encode(map[string]any{"rendered": "ok"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	_, _, err := invokeDebug(t, []string{
		"reinforce", "team_lead",
		"--project=foo",
		"--stage=2",
		"--phase=B",
		"--plan-id=plan-8",
	}, srv.URL)
	if err != nil {
		t.Fatalf("reinforce all flags: %v", err)
	}
	want := struct {
		TaskKind, ProjectAlias, Stage, Phase, PlanID string
	}{"team_lead", "foo", "2", "B", "plan-8"}
	if captured != want {
		t.Errorf("captured %+v, want %+v", captured, want)
	}
}

func TestReinforce_RequiresTaskKind(t *testing.T) {
	srv := mockDaemonForReinforce(t)
	defer srv.Close()
	_, _, err := invokeDebug(t, []string{"reinforce"}, srv.URL)
	if err == nil {
		t.Fatal("expected error: missing positional <task-kind>")
	}
}

func TestReinforce_JSONFormat_WrapsRenderedBody(t *testing.T) {
	srv := mockDaemonForReinforce(t)
	defer srv.Close()
	stdout, _, err := invokeDebug(t, []string{"reinforce", "worker", "--json"}, srv.URL)
	if err != nil {
		t.Fatalf("reinforce --json: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
	rendered, _ := got["rendered"].(string)
	if !strings.Contains(rendered, "Worker reinforcement") {
		t.Errorf("JSON should contain rendered body; got: %+v", got)
	}
}

func TestReinforce_GroupID_IsDebug(t *testing.T) {
	root := cli.NewRoot()
	for _, c := range root.Commands() {
		first := strings.SplitN(c.Use, " ", 2)[0]
		if first == "reinforce" {
			if c.GroupID != "debug" {
				t.Errorf("reinforce.GroupID = %q, want \"debug\"", c.GroupID)
			}
			return
		}
	}
	t.Fatal("reinforce command not registered on root")
}

func TestReinforce_RejectsUnknownTaskKind(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/reinforce", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "unknown task_kind: bogus_kind",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	_, _, err := invokeDebug(t, []string{"reinforce", "bogus_kind"}, srv.URL)
	if err == nil {
		t.Fatal("expected error: unknown task_kind")
	}
}

func TestReinforce_PrintsCommentHeader_NotInQuiet(t *testing.T) {
	srv := mockDaemonForReinforce(t)
	defer srv.Close()

	stdout, _, err := invokeDebug(t, []string{"reinforce", "worker", "--project=foo", "--stage=1"}, srv.URL)
	if err != nil {
		t.Fatalf("reinforce: %v", err)
	}
	if !strings.Contains(stdout, "# Refuerzo para task_kind=") {
		t.Errorf("expected comment header in output: %s", stdout)
	}

	stdoutQ, _, err := invokeDebug(t, []string{"reinforce", "worker", "--project=foo", "--quiet"}, srv.URL)
	if err != nil {
		t.Fatalf("reinforce --quiet: %v", err)
	}
	if strings.Contains(stdoutQ, "# Refuerzo para task_kind=") {
		t.Errorf("quiet mode should hide header: %s", stdoutQ)
	}
}
