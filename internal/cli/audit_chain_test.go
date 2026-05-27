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

func invokeAuditChainCmd(t *testing.T, args []string, baseURL string) (string, string, error) {
	t.Helper()
	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(uds string) *client.Client {
		return client.NewWithBaseURL(baseURL)
	}
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	cmd := NewAuditChainCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestAuditChainCmd_RegistersAllSubcommands(t *testing.T) {
	cmd := NewAuditChainCmd()
	want := []string{
		"verify-chain", "history", "recover", "checkpoint",
		"cold-archive", "configure-s3", "witness",
	}
	have := map[string]bool{}
	for _, c := range cmd.Commands() {
		have[c.Name()] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("audit-chain subcommand %q not registered", w)
		}
	}
}

func TestAuditChainCmd_HelpListsAllSubcommands(t *testing.T) {
	cmd := NewAuditChainCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("--help: %v", err)
	}
	for _, leaf := range []string{"verify-chain", "history", "recover", "checkpoint", "cold-archive", "configure-s3", "witness"} {
		if !strings.Contains(out.String(), leaf) {
			t.Errorf("--help missing leaf %q", leaf)
		}
	}
}

func TestAuditChainCmd_UseAndShortPopulated(t *testing.T) {
	cmd := NewAuditChainCmd()
	if cmd.Use == "" {
		t.Error("parent Use is empty")
	}
	if cmd.Short == "" {
		t.Error("parent Short is empty")
	}
	for _, sub := range cmd.Commands() {
		if sub.Use == "" {
			t.Errorf("subcommand %q has empty Use", sub.Name())
		}
		if sub.Short == "" {
			t.Errorf("subcommand %q has empty Short", sub.Name())
		}
	}
}

func TestAuditChainCmd_ColdArchiveHasTwoSubs(t *testing.T) {
	cmd := NewAuditChainCmd()
	var coldArchive *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "cold-archive" {
			coldArchive = sub
			break
		}
	}
	if coldArchive == nil {
		t.Fatal("cold-archive not registered on parent")
	}
	wantSubs := []string{"ls", "restore"}
	haveSubs := map[string]bool{}
	for _, s := range coldArchive.Commands() {
		haveSubs[s.Name()] = true
	}
	for _, w := range wantSubs {
		if !haveSubs[w] {
			t.Errorf("cold-archive missing sub %q", w)
		}
	}
}

func TestAuditChainCmd_WitnessHasTwoSubs(t *testing.T) {
	cmd := NewAuditChainCmd()
	var witness *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "witness" {
			witness = sub
			break
		}
	}
	if witness == nil {
		t.Fatal("witness not registered on parent")
	}
	wantSubs := []string{"rotate", "pubkey"}
	haveSubs := map[string]bool{}
	for _, s := range witness.Commands() {
		haveSubs[s.Name()] = true
	}
	for _, w := range wantSubs {
		if !haveSubs[w] {
			t.Errorf("witness missing sub %q", w)
		}
	}
}

// TestPromptYN_BlankIsDefaultNo verifies spec §6.5 recovery prompt semantics:
// "Continue? [y/N]" with empty input MUST default to N (do not proceed).
// Doctrine privacy-by-default; never auto-confirm destructive flows.
func TestPromptYN_BlankIsDefaultNo(t *testing.T) {
	in := strings.NewReader("\n")
	got, err := promptYN(in, &bytes.Buffer{}, "Continue?")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got {
		t.Error("blank input should default NO; got true")
	}
}

func TestPromptYN_YIsAffirmative(t *testing.T) {
	for _, s := range []string{"y\n", "Y\n", "yes\n", "YES\n"} {
		in := strings.NewReader(s)
		got, err := promptYN(in, &bytes.Buffer{}, "Continue?")
		if err != nil {
			t.Fatalf("input %q: err %v", s, err)
		}
		if !got {
			t.Errorf("input %q: expected true", s)
		}
	}
}

func TestPromptYN_NIsNegative(t *testing.T) {
	for _, s := range []string{"n\n", "N\n", "no\n"} {
		in := strings.NewReader(s)
		got, err := promptYN(in, &bytes.Buffer{}, "Continue?")
		if err != nil {
			t.Fatalf("input %q: err %v", s, err)
		}
		if got {
			t.Errorf("input %q: expected false", s)
		}
	}
}

func TestPromptYN_WritesPromptToOut(t *testing.T) {
	in := strings.NewReader("y\n")
	var out bytes.Buffer
	_, err := promptYN(in, &out, "Proceed?")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out.String(), "Proceed?") {
		t.Errorf("prompt string not written to out; got %q", out.String())
	}
	if !strings.Contains(out.String(), "[y/N]") {
		t.Errorf("[y/N] indicator missing from out; got %q", out.String())
	}
}

func TestPromptString_ReturnsTrimedLine(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  string
	}{
		{"hello\n", "hello"},
		{"  spaced  \n", "spaced"},
		{"\n", ""},
	} {
		in := strings.NewReader(tc.input)
		got, err := promptString(in, &bytes.Buffer{}, "Name?")
		if err != nil {
			t.Fatalf("input %q: err %v", tc.input, err)
		}
		if got != tc.want {
			t.Errorf("input %q: got %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestPromptString_WritesPrompt(t *testing.T) {
	in := strings.NewReader("answer\n")
	var out bytes.Buffer
	_, err := promptString(in, &out, "Question")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out.String(), "Question") {
		t.Errorf("prompt not written; got %q", out.String())
	}
}

func mockAuditChainServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/audit-chain/verify-chain", func(w http.ResponseWriter, r *http.Request) {

		_ = json.NewEncoder(w).Encode(client.AuditVerifyResp{
			ProjectID:      "zen-swarm",
			RecordsValid:   847239,
			PartitionSeals: 12,
			WitnessChecks:  12,
			VerifiedAtUnix: 1762000000,
		})
	})

	mux.HandleFunc("/v1/audit-chain/history", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.AuditHistoryEntry{
				{
					ID:         "11111111-uuid",
					ProjectID:  "zen-swarm",
					Type:       "audit.event_emitted",
					EmittedAt:  1762000000,
					RecordHash: "sha256:aabbcc",
					PrevHash:   "sha256:112233",
				},
				{
					ID:         "22222222-uuid",
					ProjectID:  "zen-swarm",
					Type:       "audit.partition_sealed",
					EmittedAt:  1762000100,
					RecordHash: "sha256:ccddee",
					PrevHash:   "sha256:aabbcc",
				},
			},
			"count": 2,
		})
	})

	mux.HandleFunc("/v1/audit-chain/recover", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ProjectID string `json:"project_id"`
			FromTs    int64  `json:"from_ts"`
			Confirm   bool   `json:"confirm"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		plan := client.AuditRecoverPlan{
			ProjectID:           req.ProjectID,
			LitestreamSizeBytes: 1287654321,
			ColdArchivePartCnt:  3,
			VerifyStepCount:     847239,
			EstimatedDurationS:  120,
		}
		resp := map[string]any{"plan": plan}
		if req.Confirm {
			resp["result"] = &client.AuditRecoverResult{
				Recovered:          true,
				RecordsRestored:    847239,
				PartitionsRestored: 3,
				DurationSeconds:    118,
			}
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	return httptest.NewServer(mux)
}

func TestAuditChainVerify_TableFormat(t *testing.T) {
	srv := mockAuditChainServer(t)
	defer srv.Close()
	stdout, _, err := invokeAuditChainCmd(t, []string{"verify-chain", "--project", "zen-swarm"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	for _, want := range []string{"zen-swarm", "847239", "12"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output missing %q:\n%s", want, stdout)
		}
	}
}

func TestAuditChainVerify_RequiresProject(t *testing.T) {
	srv := mockAuditChainServer(t)
	defer srv.Close()
	_, _, err := invokeAuditChainCmd(t, []string{"verify-chain"}, srv.URL)
	if err == nil {
		t.Fatal("expected --project required error, got nil")
	}
}

func TestAuditChainHistory_DefaultRender(t *testing.T) {
	srv := mockAuditChainServer(t)
	defer srv.Close()
	stdout, _, err := invokeAuditChainCmd(t, []string{"history"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	for _, want := range []string{"audit.event_emitted", "audit.partition_sealed", "sha256"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output missing %q:\n%s", want, stdout)
		}
	}
}

func TestAuditChainHistory_JSONFormat(t *testing.T) {
	srv := mockAuditChainServer(t)
	defer srv.Close()
	stdout, _, err := invokeAuditChainCmd(t, []string{"history", "--json"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	var got []client.AuditHistoryEntry
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("--json output not valid JSON: %v\noutput=%s", err, stdout)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 entries, got %d", len(got))
	}
}

func TestAuditChainRecover_DryRun_DisplaysPlan(t *testing.T) {
	confirmCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Confirm bool `json:"confirm"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Confirm {
			confirmCalls++
		}
		plan := client.AuditRecoverPlan{
			ProjectID:           "zen-swarm",
			LitestreamSizeBytes: 1287654321,
			ColdArchivePartCnt:  3,
			VerifyStepCount:     847239,
			EstimatedDurationS:  120,
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"plan": plan})
	}))
	defer srv.Close()

	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(uds string) *client.Client { return client.NewWithBaseURL(srv.URL) }
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	cmd := NewAuditChainCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	cmd.SetIn(strings.NewReader("n\n"))
	cmd.SetArgs([]string{"recover", "--project", "zen-swarm", "--from", "2026-05-06T08:00:00Z"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("decline should not error; got %v\nstderr=%s", err, stderr.String())
	}
	if confirmCalls != 0 {
		t.Errorf("operator declined: confirm=true should NOT have been called; calls=%d", confirmCalls)
	}
	for _, want := range []string{"1.20 GB", "Recovery aborted"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("expected stdout to contain %q\nfull stdout:\n%s", want, stdout.String())
		}
	}
}

func TestAuditChainRecover_OperatorAccepts_Executes(t *testing.T) {
	srv := mockAuditChainServer(t)
	defer srv.Close()

	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(uds string) *client.Client { return client.NewWithBaseURL(srv.URL) }
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	cmd := NewAuditChainCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	cmd.SetIn(strings.NewReader("y\ny\n"))
	cmd.SetArgs([]string{"recover", "--project", "zen-swarm", "--from", "2026-05-06T08:00:00Z"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("CLI: %v\nstderr=%s", err, stderr.String())
	}
	for _, want := range []string{
		"Recovery plan",
		"Continue?",
		"847239",
		"Resume audit",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("expected stdout to contain %q\nfull stdout:\n%s", want, stdout.String())
		}
	}
}

func TestAuditChainRecover_OperatorRejects_AbortsClean(t *testing.T) {
	executeCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Confirm bool `json:"confirm"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		plan := client.AuditRecoverPlan{
			ProjectID:           "zen-swarm",
			LitestreamSizeBytes: 1287654321,
			ColdArchivePartCnt:  3,
			VerifyStepCount:     847239,
			EstimatedDurationS:  120,
		}
		resp := map[string]any{"plan": plan}
		if req.Confirm {
			executeCalled = true
			resp["result"] = &client.AuditRecoverResult{Recovered: true, RecordsRestored: 1}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(uds string) *client.Client { return client.NewWithBaseURL(srv.URL) }
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	cmd := NewAuditChainCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetIn(strings.NewReader("n\n"))
	cmd.SetArgs([]string{"recover", "--project", "zen-swarm", "--from", "2026-05-06T08:00:00Z"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("decline should not error; got %v", err)
	}
	if executeCalled {
		t.Error("confirm=true should NOT have been called when operator declined")
	}
	if !strings.Contains(stdout.String(), "Recovery aborted") {
		t.Errorf("expected 'Recovery aborted' in output; got:\n%s", stdout.String())
	}
}

func TestAuditChainCheckpoint_RequiresReason(t *testing.T) {
	srv := mockAuditChainServer(t)
	defer srv.Close()
	_, _, err := invokeAuditChainCmd(t, []string{"checkpoint"}, srv.URL)
	if err == nil {
		t.Fatal("expected --reason required error (inv-zen-146)")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "reason") {
		t.Errorf("error should mention reason flag; got %v", err)
	}
}

func TestAuditChainCheckpoint_RejectsEmptyReason(t *testing.T) {
	srv := mockAuditChainServer(t)
	defer srv.Close()
	_, _, err := invokeAuditChainCmd(t, []string{"checkpoint", "--reason", ""}, srv.URL)
	if err == nil {
		t.Fatal("expected non-empty --reason required error (inv-zen-146)")
	}
}

func TestAuditChainCheckpoint_HappyPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/audit-chain/checkpoint", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.AuditCheckpointResp{
			CheckpointID: "ckpt-uuid-1",
			TesseraSTH:   "sha256:checkpoint-root",
			AnchoredAt:   1762000000,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	stdout, _, err := invokeAuditChainCmd(t,
		[]string{"checkpoint", "--reason", "pre-merge anchor for v0.9.0"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	for _, want := range []string{"ckpt-uuid-1", "checkpoint-root"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output missing %q:\n%s", want, stdout)
		}
	}
}

func TestAuditChainColdArchive_Ls(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/audit-chain/cold-archive/list", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.AuditColdArchiveEntry{
				{PartitionID: "2026_03", ArchivedAt: 1759000000, SizeBytes: 1024, ContentHash: "sha256:aaa"},
				{PartitionID: "2026_04", ArchivedAt: 1761000000, SizeBytes: 2048, ContentHash: "sha256:bbb"},
			},
			"count": 2,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	stdout, _, err := invokeAuditChainCmd(t, []string{"cold-archive", "ls", "--project", "zen-swarm"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	for _, want := range []string{"2026_03", "2026_04"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output missing %q:\n%s", want, stdout)
		}
	}
}

func TestAuditChainColdArchive_RestoreDeclined(t *testing.T) {
	restoreCalled := false
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/audit-chain/cold-archive/restore", func(w http.ResponseWriter, r *http.Request) {
		restoreCalled = true
		t.Errorf("operator declined; restore endpoint MUST NOT be called")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(uds string) *client.Client {
		return client.NewWithBaseURL(srv.URL)
	}
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	cmd := NewAuditChainCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader("n\n"))
	cmd.SetArgs([]string{"cold-archive", "restore", "--partition", "2026_05", "--project", "zen-swarm"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("declining is not error path; got %v\nstderr=%s", err, stderr.String())
	}
	if restoreCalled {
		t.Error("restore endpoint was called despite operator declining")
	}
	if !strings.Contains(strings.ToLower(stdout.String()), "abort") {
		t.Errorf("missing abort message in output: %s", stdout.String())
	}
}

func TestAuditChainColdArchive_RestoreAccepted(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/audit-chain/cold-archive/restore", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.AuditRestoreResult{
			Restored:    true,
			BytesPulled: 4096,
			DurationSec: 12,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(uds string) *client.Client {
		return client.NewWithBaseURL(srv.URL)
	}
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	cmd := NewAuditChainCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader("y\n"))
	cmd.SetArgs([]string{"cold-archive", "restore", "--partition", "2026_05", "--project", "zen-swarm"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("CLI: %v\nstderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "2026_05") {
		t.Errorf("missing partition in output: %s", stdout.String())
	}
}

// ---- Task I-4 tests: configure-s3 + witness rotate + witness pubkey ----------
//
// Actual H-7 client signatures (deviates from plan-file; adapted to actuals):
// AuditConfigureS3(ctx, projectID string, creds AuditS3Credentials) error
// → returns nil on 204; creds has Endpoint, Bucket, Region, AccessKey, SecretKey, Prefix
// AuditWitnessRotate(ctx, reason string) (AuditRotateResult, error)
// → AuditRotateResult{NewKeyFingerprint, OldKeyFingerprint, RotatedAt}
// (NO OverlapWindowDays — not in H-7 AuditRotateResult)
// AuditWitnessPubkey(ctx) (AuditWitnessPubkey, error)
// → AuditWitnessPubkey{PubkeyPEM, Fingerprint, CreatedAt, RotationCount}
// (NOT HexPub/IssuedAt/NotAfterAt as plan-file proposed)
//
// Plan-file types AuditChainConfigureS3Resp, AuditChainWitnessRotateResp,
// AuditChainWitnessPubkeyResp do NOT exist; all tests use H-7 actuals.

func TestAuditChainConfigureS3_RequiresProject(t *testing.T) {
	srv := mockAuditChainServer(t)
	defer srv.Close()
	_, _, err := invokeAuditChainCmd(t, []string{"configure-s3"}, srv.URL)
	if err == nil {
		t.Fatal("expected --project required error")
	}
}

func TestAuditChainConfigureS3_HappyPath_NeverEchoesSecret(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/audit-chain/configure-s3", func(w http.ResponseWriter, r *http.Request) {

		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	prev := TestOnlyClientFactory
	TestOnlyClientFactory = func(uds string) *client.Client {
		return client.NewWithBaseURL(srv.URL)
	}
	t.Cleanup(func() { TestOnlyClientFactory = prev })

	cmd := NewAuditChainCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader(strings.Join([]string{
		"s3.us-east-2.amazonaws.com",
		"zen-swarm-audit-prod",
		"us-east-2",
		"AKIAEXAMPLEACCESS",
		"sk-very-secret-do-not-log",
	}, "\n") + "\n"))
	cmd.SetArgs([]string{"configure-s3", "--project", "zen-swarm"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("CLI: %v\nstderr=%s", err, stderr.String())
	}
	got := stdout.String() + stderr.String()
	if !strings.Contains(got, "zen-swarm-audit-prod") {
		t.Errorf("expected bucket in confirmation; got %s", got)
	}
	if strings.Contains(got, "sk-very-secret-do-not-log") {
		t.Error("SECURITY: secret key leaked into stdout/stderr")
	}
	if strings.Contains(got, "AKIAEXAMPLEACCESS") {
		t.Error("SECURITY: access key leaked into stdout/stderr")
	}
}

func TestAuditChainWitness_Rotate_RequiresReason(t *testing.T) {
	srv := mockAuditChainServer(t)
	defer srv.Close()
	_, _, err := invokeAuditChainCmd(t, []string{"witness", "rotate"}, srv.URL)
	if err == nil {
		t.Fatal("expected --reason required (inv-zen-146)")
	}
}

func TestAuditChainWitness_Rotate_HappyPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/audit-chain/witness/rotate", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.AuditRotateResult{
			OldKeyFingerprint: "fp:old-1234",
			NewKeyFingerprint: "fp:new-5678",
			RotatedAt:         1762000000,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	stdout, _, err := invokeAuditChainCmd(t,
		[]string{"witness", "rotate", "--reason", "scheduled 90d rotation"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	for _, want := range []string{"fp:old-1234", "fp:new-5678"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q: %s", want, stdout)
		}
	}
}

func TestAuditChainWitness_Pubkey_HappyPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/audit-chain/witness/pubkey", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.AuditWitnessPubkey{
			PubkeyPEM:     "-----BEGIN PUBLIC KEY-----\nMFkwEwYH...\n-----END PUBLIC KEY-----",
			Fingerprint:   "fp:current-1234",
			CreatedAt:     1762000000,
			RotationCount: 3,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	stdout, _, err := invokeAuditChainCmd(t, []string{"witness", "pubkey"}, srv.URL)
	if err != nil {
		t.Fatalf("CLI: %v", err)
	}
	for _, want := range []string{"fp:current-1234", "BEGIN PUBLIC KEY"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q: %s", want, stdout)
		}
	}
}
