package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/client"
)

func invokeWorkforceCmd(t *testing.T, args []string, baseURL string) (string, string, error) {
	t.Helper()
	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(uds string) *client.Client {
		return client.NewWithBaseURL(baseURL)
	}
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	cmd := NewWorkforceCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func mockWorkforceServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/workforce/specs", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.WorkforceSpec{
				{ID: "spec_a", Variant: "worker", TaskTier: "medium", ModelClass: "sonnet"},
				{ID: "spec_b", Variant: "teamlead", TaskTier: "complex", ModelClass: "opus"},
			},
			"count": 2,
		})
	})
	mux.HandleFunc("/v1/workforce/workers", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.WorkforceWorker{
				{ID: "w_1", SpecID: "spec_a", Status: "in_progress", TaskID: "task_42"},
				{ID: "w_2", SpecID: "spec_b", Status: "review", TaskID: "task_43"},
			},
			"count": 2,
		})
	})
	mux.HandleFunc("/v1/workforce/checkpoints", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.WorkforceCheckpoint{
				{ID: "cp_1", TaskID: "task_42", ThreadID: "th_1", CreatedAt: 1759320000},
			},
		})
	})
	mux.HandleFunc("/v1/workforce/fix_prompts", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.WorkforceFixPrompt{
				{ID: "fp_1", TaskID: "task_42", FromLayer: "l3", Consumed: false, CreatedAt: 1759320000},
			},
		})
	})
	mux.HandleFunc("/v1/workforce/gate/state", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.GateStateResp{State: "running", CanPause: true, CanResume: false})
	})
	mux.HandleFunc("/v1/workforce/gate/pause", func(w http.ResponseWriter, r *http.Request) {
		var req client.GatePauseReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		_ = json.NewEncoder(w).Encode(client.GatePauseResp{State: "paused_descriptive", Paused: true})
	})
	mux.HandleFunc("/v1/workforce/gate/resume", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.GateResumeResp{State: "running", Running: true})
	})
	return httptest.NewServer(mux)
}

func TestWorkforceStatus_Table(t *testing.T) {
	srv := mockWorkforceServer(t)
	defer srv.Close()
	stdout, _, err := invokeWorkforceCmd(t, []string{"status"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	for _, want := range []string{"GateState", "running", "WorkersTotal", "SpecsLoaded"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in: %s", want, stdout)
		}
	}
}

func TestWorkforceStatus_JSON(t *testing.T) {
	srv := mockWorkforceServer(t)
	defer srv.Close()
	stdout, _, err := invokeWorkforceCmd(t, []string{"status", "--json"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(stdout), &arr); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
	if len(arr) == 0 || arr[0]["gate_state"] != "running" {
		t.Errorf("unexpected: %+v", arr)
	}
}

func TestWorkforceGateState(t *testing.T) {
	srv := mockWorkforceServer(t)
	defer srv.Close()
	stdout, _, err := invokeWorkforceCmd(t, []string{"gate", "state"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "running") {
		t.Errorf("missing running, got %s", stdout)
	}
}

func TestWorkforceGateState_YAML(t *testing.T) {
	srv := mockWorkforceServer(t)
	defer srv.Close()
	stdout, _, err := invokeWorkforceCmd(t, []string{"gate", "state", "--format=yaml"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "state: running") {
		t.Errorf("expected yaml; got %s", stdout)
	}
}

func TestWorkforceGatePause_RequiresMode(t *testing.T) {
	srv := mockWorkforceServer(t)
	defer srv.Close()
	_, _, err := invokeWorkforceCmd(t, []string{"gate", "pause"}, srv.URL)
	if err == nil {
		t.Fatal("expected error when --mode missing")
	}
	if !strings.Contains(err.Error(), "--mode") {
		t.Errorf("error should mention --mode, got %v", err)
	}
}

func TestWorkforceGatePause_RequiresYes(t *testing.T) {
	srv := mockWorkforceServer(t)
	defer srv.Close()
	_, _, err := invokeWorkforceCmd(t, []string{"gate", "pause", "--mode=paused_descriptive"}, srv.URL)
	if err == nil {
		t.Fatal("expected error without --yes")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("error should mention --yes, got %v", err)
	}
}

func TestWorkforceGatePause_HappyPath(t *testing.T) {
	srv := mockWorkforceServer(t)
	defer srv.Close()
	stdout, _, err := invokeWorkforceCmd(t, []string{"gate", "pause", "--mode=paused_descriptive", "--reason=test", "--yes"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "paused_descriptive") {
		t.Errorf("expected paused state, got %s", stdout)
	}
}

func TestWorkforceGateResume_RequiresYes(t *testing.T) {
	srv := mockWorkforceServer(t)
	defer srv.Close()
	_, _, err := invokeWorkforceCmd(t, []string{"gate", "resume"}, srv.URL)
	if err == nil {
		t.Fatal("expected --yes error")
	}
}

func TestWorkforceGateResume_HappyPath(t *testing.T) {
	srv := mockWorkforceServer(t)
	defer srv.Close()
	stdout, _, err := invokeWorkforceCmd(t, []string{"gate", "resume", "--yes"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "running") {
		t.Errorf("got %s", stdout)
	}
}

func TestWorkforceWorkers(t *testing.T) {
	srv := mockWorkforceServer(t)
	defer srv.Close()
	stdout, _, err := invokeWorkforceCmd(t, []string{"workers"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	for _, want := range []string{"w_1", "spec_a", "in_progress", "task_42"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in %s", want, stdout)
		}
	}
}

func TestWorkforceCheckpoints(t *testing.T) {
	srv := mockWorkforceServer(t)
	defer srv.Close()
	stdout, _, err := invokeWorkforceCmd(t, []string{"checkpoints", "--task=task_42"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "cp_1") || !strings.Contains(stdout, "task_42") {
		t.Errorf("got %s", stdout)
	}
}

func TestWorkforceSpecsList(t *testing.T) {
	srv := mockWorkforceServer(t)
	defer srv.Close()
	stdout, _, err := invokeWorkforceCmd(t, []string{"specs", "list"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "spec_a") || !strings.Contains(stdout, "spec_b") {
		t.Errorf("got %s", stdout)
	}
}

func TestWorkforceSpecsShow_Found(t *testing.T) {
	srv := mockWorkforceServer(t)
	defer srv.Close()
	stdout, _, err := invokeWorkforceCmd(t, []string{"specs", "show", "spec_a"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "spec_a") || !strings.Contains(stdout, "sonnet") {
		t.Errorf("got %s", stdout)
	}
}

func TestWorkforceSpecsShow_NotFound(t *testing.T) {
	srv := mockWorkforceServer(t)
	defer srv.Close()
	_, _, err := invokeWorkforceCmd(t, []string{"specs", "show", "nope"}, srv.URL)
	if err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestWorkforceSpecsShow_JSON(t *testing.T) {
	srv := mockWorkforceServer(t)
	defer srv.Close()
	stdout, _, err := invokeWorkforceCmd(t, []string{"specs", "show", "spec_a", "--json"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(stdout), &arr); err != nil {
		t.Fatalf("json: %v\n%s", err, stdout)
	}
	if len(arr) != 1 || arr[0]["id"] != "spec_a" {
		t.Errorf("got %+v", arr)
	}
}

func TestWorkforceFixPrompts(t *testing.T) {
	srv := mockWorkforceServer(t)
	defer srv.Close()
	stdout, _, err := invokeWorkforceCmd(t, []string{"fix-prompts"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "fp_1") || !strings.Contains(stdout, "l3") {
		t.Errorf("got %s", stdout)
	}
}

func TestWorkforceSubcommandsRegistered(t *testing.T) {
	root := NewWorkforceCmd()
	want := []string{"status", "gate", "workers", "checkpoints", "specs", "fix-prompts"}
	have := map[string]bool{}
	for _, c := range root.Commands() {
		have[c.Name()] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("missing subcommand: workforce %s", w)
		}
	}
}

func TestWorkforceQuietVerboseConflict(t *testing.T) {
	srv := mockWorkforceServer(t)
	defer srv.Close()
	_, _, err := invokeWorkforceCmd(t, []string{"status", "--quiet", "--verbose"}, srv.URL)
	if err == nil {
		t.Fatal("expected conflict error")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("got %v", err)
	}
}

func TestWorkforceWorkers_Filter(t *testing.T) {
	srv := mockWorkforceServer(t)
	defer srv.Close()
	stdout, _, err := invokeWorkforceCmd(t, []string{"workers", "--filter", "status=review"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stdout, "w_2") {
		t.Errorf("expected w_2 in filtered output: %s", stdout)
	}
	if strings.Contains(stdout, "w_1") {
		t.Errorf("--filter status=review should exclude w_1: %s", stdout)
	}
}

func TestWorkforceWorkers_FilterRegex(t *testing.T) {
	srv := mockWorkforceServer(t)
	defer srv.Close()
	stdout, _, err := invokeWorkforceCmd(t, []string{"workers", "--filter", `id~^w_[12]$`}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	for _, want := range []string{"w_1", "w_2"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("regex filter dropped expected match %q: %s", want, stdout)
		}
	}
}

func TestWorkforceWorkers_Verbose(t *testing.T) {
	srv := mockWorkforceServer(t)
	defer srv.Close()
	_, stderr, err := invokeWorkforceCmd(t, []string{"workers", "--verbose"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	if !strings.Contains(stderr, "GET /v1/workforce/workers") {
		t.Errorf("--verbose should log the request URL on stderr: %s", stderr)
	}

	if !strings.Contains(stderr, "(") || !strings.Contains(stderr, ")") {
		t.Errorf("--verbose should include latency: %s", stderr)
	}
}

func TestIsVerboseSet_NoFlagReturnsFalse(t *testing.T) {
	bare := &cobra.Command{Use: "bare"}
	if isVerboseSet(bare) {
		t.Error("command without --verbose should return false")
	}
}

func TestIsVerboseSet_FromPersistentFlags(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	cmd.PersistentFlags().Bool("verbose", true, "")

	_ = cmd.Flags().Set("verbose", "true")
	if !isVerboseSet(cmd) {
		t.Error("command with --verbose=true should report true")
	}
}
