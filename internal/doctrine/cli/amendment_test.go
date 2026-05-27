package cli_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cli "github.com/cbip-solutions/hades-system/internal/doctrine/cli"
)

func invokeAmendment(t *testing.T, args []string, baseURL string) (string, string, error) {
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

func proposeListBody(t *testing.T, proposals ...map[string]any) []byte {
	t.Helper()
	buf, err := json.Marshal(map[string]any{"proposals": proposals})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return buf
}

func fixtureProposed3() []map[string]any {
	return []map[string]any{
		{
			"id":                      "ADR-0050",
			"title":                   "Reduce amendment cooldown to 12h",
			"status":                  "proposed",
			"proposed_at":             1714737600,
			"body_markdown":           "# ADR-0050\nReduce cooldown.\n",
			"operator_reason":         "",
			"cooldown_remain_seconds": 0,
		},
		{
			"id":                      "ADR-0051",
			"title":                   "Tighten amendment threshold",
			"status":                  "proposed",
			"proposed_at":             1714742400,
			"body_markdown":           "# ADR-0051\nTighten threshold.\n",
			"operator_reason":         "",
			"cooldown_remain_seconds": 0,
		},
		{
			"id":                      "ADR-0052",
			"title":                   "Adjust review weight",
			"status":                  "proposed",
			"proposed_at":             1714748400,
			"body_markdown":           "# ADR-0052\nAdjust weight.\n",
			"operator_reason":         "",
			"cooldown_remain_seconds": 7200,
		},
	}
}

func buildProposeListServer(t *testing.T, proposals []map[string]any, capturedQuery *string) *httptest.Server {
	t.Helper()
	body := proposeListBody(t, proposals...)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/doctrine/propose-list" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if capturedQuery != nil {
			*capturedQuery = r.URL.RawQuery
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
}

func TestProposeList_HappyPath_3Proposed(t *testing.T) {
	var capturedQuery string
	srv := buildProposeListServer(t, fixtureProposed3(), &capturedQuery)
	defer srv.Close()

	stdout, stderr, err := invokeAmendment(t, []string{"propose-list"}, srv.URL)
	if err != nil {
		t.Fatalf("propose-list: %v stderr=%q", err, stderr)
	}

	for _, want := range []string{"ADR_ID", "ESTADO", "TÍTULO", "ENFRIAMIENTO", "PROPUESTA_EN"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing table header %q in stdout: %s", want, stdout)
		}
	}
	for _, want := range []string{"ADR-0050", "ADR-0051", "ADR-0052", "proposed"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing row content %q in stdout: %s", want, stdout)
		}
	}
	if capturedQuery != "" {
		t.Errorf("expected empty query string when no flags passed; got %q", capturedQuery)
	}
}

func TestProposeList_EmptyResult_FriendlyMessage(t *testing.T) {
	srv := buildProposeListServer(t, nil, nil)
	defer srv.Close()

	stdout, stderr, err := invokeAmendment(t, []string{"propose-list"}, srv.URL)
	if err != nil {
		t.Fatalf("propose-list (empty): %v stderr=%q", err, stderr)
	}
	if !strings.Contains(stdout, "No hay enmiendas") {
		t.Errorf("expected Spanish friendly message; got: %s", stdout)
	}

	if strings.Contains(stdout, "ADR_ID") {
		t.Errorf("empty result should not render table headers; got: %s", stdout)
	}
}

func TestProposeList_StatusFilter_FiltersClientSide(t *testing.T) {

	mixed := []map[string]any{
		{"id": "ADR-0050", "title": "x", "status": "proposed", "proposed_at": 1714737600},
		{"id": "ADR-0051", "title": "y", "status": "applied", "proposed_at": 1714737600},
		{"id": "ADR-0052", "title": "z", "status": "denied", "proposed_at": 1714737600},
	}
	srv := buildProposeListServer(t, mixed, nil)
	defer srv.Close()

	stdout, stderr, err := invokeAmendment(t, []string{"propose-list", "--status=proposed"}, srv.URL)
	if err != nil {
		t.Fatalf("propose-list --status=proposed: %v stderr=%q", err, stderr)
	}
	if !strings.Contains(stdout, "ADR-0050") {
		t.Errorf("expected ADR-0050 (status=proposed) in output; got: %s", stdout)
	}
	if strings.Contains(stdout, "ADR-0051") || strings.Contains(stdout, "ADR-0052") {
		t.Errorf("expected non-proposed ADRs filtered out; got: %s", stdout)
	}
}

func TestProposeList_InvalidStatus_RejectsClientSide(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("server should NOT be contacted when --status is invalid")
	}))
	defer srv.Close()

	_, stderr, err := invokeAmendment(t, []string{"propose-list", "--status=garbage"}, srv.URL)
	if err == nil {
		t.Fatal("expected error for invalid --status")
	}
	combined := stderr + err.Error()
	if !strings.Contains(combined, "estado inválido") {
		t.Errorf("expected Spanish 'estado inválido' in error; got stderr=%q err=%v", stderr, err)
	}
	for _, want := range []string{"proposed", "applied", "denied", "reverted"} {
		if !strings.Contains(combined, want) {
			t.Errorf("expected allowed value %q in error; got %q", want, combined)
		}
	}
}

func TestProposeList_NetworkError_FriendlyExit(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	_, _, err := invokeAmendment(t, []string{"propose-list"}, srv.URL)
	if err == nil {
		t.Fatal("expected network error")
	}
	if !strings.Contains(err.Error(), "petición HTTP") && !strings.Contains(err.Error(), "daemon") {
		t.Errorf("expected friendly network error; got: %v", err)
	}
}

func TestProposeList_DaemonError5xx_PreservesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "database unavailable")
	}))
	defer srv.Close()

	_, _, err := invokeAmendment(t, []string{"propose-list"}, srv.URL)
	if err == nil {
		t.Fatal("expected error from 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status 500; got: %v", err)
	}
	if !strings.Contains(err.Error(), "database unavailable") {
		t.Errorf("error should preserve body; got: %v", err)
	}
}

func TestProposeList_JSONOutput_BypassesTable(t *testing.T) {
	srv := buildProposeListServer(t, fixtureProposed3(), nil)
	defer srv.Close()

	stdout, stderr, err := invokeAmendment(t, []string{"propose-list", "--json"}, srv.URL)
	if err != nil {
		t.Fatalf("--json: %v stderr=%q", err, stderr)
	}
	if strings.Contains(stdout, "ADR_ID") || strings.Contains(stdout, "ESTADO") {
		t.Errorf("table headers must NOT appear in --json mode; got: %s", stdout)
	}
	var decoded any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &decoded); err != nil {
		t.Errorf("stdout must be valid JSON in --json mode: %v", err)
	}
}

func TestProposeList_SinceFilter_ParsesDuration(t *testing.T) {

	now := int64(1714748400)
	old := []map[string]any{
		{"id": "ADR-0050", "title": "fresh", "status": "proposed", "proposed_at": now - 100},
		{"id": "ADR-0051", "title": "ancient", "status": "proposed", "proposed_at": now - 100*24*3600},
	}
	srv := buildProposeListServer(t, old, nil)
	defer srv.Close()

	stdout, stderr, err := invokeAmendment(t, []string{"propose-list", "--since=24h"}, srv.URL)
	if err != nil {
		t.Fatalf("--since=24h: %v stderr=%q", err, stderr)
	}

	_ = stdout
}

func TestProposeList_SinceFilter_InvalidFormat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("server should NOT be contacted on invalid --since")
	}))
	defer srv.Close()

	_, stderr, err := invokeAmendment(t, []string{"propose-list", "--since=garbage"}, srv.URL)
	if err == nil {
		t.Fatal("expected error from invalid --since")
	}
	if !strings.Contains(stderr+err.Error(), "since") && !strings.Contains(stderr+err.Error(), "duración") {
		t.Errorf("expected since/duración error; got stderr=%q err=%v", stderr, err)
	}
}

func TestProposeList_SinceFilter_DaysSuffix(t *testing.T) {
	srv := buildProposeListServer(t, fixtureProposed3(), nil)
	defer srv.Close()

	_, stderr, err := invokeAmendment(t, []string{"propose-list", "--since=7d"}, srv.URL)
	if err != nil {
		t.Fatalf("--since=7d: %v stderr=%q", err, stderr)
	}
}

func TestProposeList_SinceFilter_NegativeDays(t *testing.T) {
	_, stderr, err := invokeAmendment(t, []string{"propose-list", "--since=-7d"}, "http://localhost:1")
	if err == nil {
		t.Fatal("expected error from negative --since")
	}
	combined := stderr + err.Error()
	if !strings.Contains(combined, "negativa") && !strings.Contains(combined, "inválido") {
		t.Errorf("expected negative-duration error; got: %s", combined)
	}
}

func TestProposeList_SinceFilter_EmptyDuration(t *testing.T) {
	srv := buildProposeListServer(t, fixtureProposed3(), nil)
	defer srv.Close()

	_, stderr, err := invokeAmendment(t, []string{"propose-list", "--since=   "}, srv.URL)
	if err == nil {
		t.Fatal("expected error from whitespace --since")
	}
	if !strings.Contains(stderr+err.Error(), "since") && !strings.Contains(stderr+err.Error(), "vacía") {
		t.Errorf("expected vacía/since error; got: %s err=%v", stderr, err)
	}
}

func TestProposeList_LongTitle_Truncated(t *testing.T) {
	long := strings.Repeat("X", 200)
	proposals := []map[string]any{
		{"id": "ADR-0050", "title": long, "status": "proposed", "proposed_at": int64(1714737600)},
	}
	srv := buildProposeListServer(t, proposals, nil)
	defer srv.Close()

	stdout, _, err := invokeAmendment(t, []string{"propose-list"}, srv.URL)
	if err != nil {
		t.Fatalf("propose-list: %v", err)
	}
	if !strings.Contains(stdout, "...") {
		t.Errorf("long title should be truncated with '...'; got: %s", stdout)
	}
	// The full 200-char title MUST NOT appear.
	if strings.Contains(stdout, long) {
		t.Errorf("full long title leaked into output; got: %s", stdout)
	}
}

func TestProposeList_ZeroProposedAt_RendersDash(t *testing.T) {
	proposals := []map[string]any{
		{"id": "ADR-0050", "title": "x", "status": "proposed", "proposed_at": int64(0), "cooldown_remain_seconds": int64(0)},
	}
	srv := buildProposeListServer(t, proposals, nil)
	defer srv.Close()

	stdout, _, err := invokeAmendment(t, []string{"propose-list"}, srv.URL)
	if err != nil {
		t.Fatalf("propose-list: %v", err)
	}
	if !strings.Contains(stdout, "—") {
		t.Errorf("expected em-dash placeholder for missing timestamps; got: %s", stdout)
	}
}

func buildAckServer(t *testing.T, statusCode int, body string, capturedBody *[]byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/doctrine/ack" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if capturedBody != nil {
			*capturedBody, _ = io.ReadAll(r.Body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = io.WriteString(w, body)
	}))
}

func buildDenyServer(t *testing.T, statusCode int, body string, capturedBody *[]byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/doctrine/deny" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if capturedBody != nil {
			*capturedBody, _ = io.ReadAll(r.Body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = io.WriteString(w, body)
	}))
}

func TestAck_HappyPath_NoReason(t *testing.T) {
	var captured []byte
	srv := buildAckServer(t, http.StatusOK, `{"status":"applied"}`, &captured)
	defer srv.Close()

	stdout, stderr, err := invokeAmendment(t, []string{"ack", "ADR-0050"}, srv.URL)
	if err != nil {
		t.Fatalf("ack: %v stderr=%q", err, stderr)
	}
	if !strings.Contains(stdout, "ADR-0050") || !strings.Contains(stdout, "aplicada") {
		t.Errorf("expected Spanish 'aplicada' confirmation; got: %s", stdout)
	}
	var req cli.AmendmentDecision
	if err := json.Unmarshal(captured, &req); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	if req.ID != "ADR-0050" {
		t.Errorf("request body ID = %q, want ADR-0050", req.ID)
	}
	if req.Reason != "" {
		t.Errorf("no --reason passed → empty Reason in body; got %q", req.Reason)
	}
}

func TestAck_HappyPath_WithReason(t *testing.T) {
	var captured []byte
	srv := buildAckServer(t, http.StatusOK, `{"status":"applied"}`, &captured)
	defer srv.Close()

	_, _, err := invokeAmendment(t, []string{"ack", "ADR-0050", "--reason", "operator approved after telemetry review"}, srv.URL)
	if err != nil {
		t.Fatalf("ack --reason: %v", err)
	}
	var req cli.AmendmentDecision
	if err := json.Unmarshal(captured, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.Reason != "operator approved after telemetry review" {
		t.Errorf("request body Reason = %q", req.Reason)
	}
}

func TestAck_MissingADRID_RejectsClientSide(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("server should NOT be contacted on missing ADR-ID")
	}))
	defer srv.Close()

	_, stderr, err := invokeAmendment(t, []string{"ack"}, srv.URL)
	if err == nil {
		t.Fatal("expected error from missing ADR ID")
	}
	if !strings.Contains(stderr+err.Error(), "se requiere el identificador") {
		t.Errorf("expected Spanish 'se requiere el identificador' error; got stderr=%q err=%v", stderr, err)
	}
}

func TestAck_InvalidADRIDFormat_RejectsClientSide(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("server should NOT be contacted on invalid ADR-ID")
	}))
	defer srv.Close()

	_, stderr, err := invokeAmendment(t, []string{"ack", "garbage-id"}, srv.URL)
	if err == nil {
		t.Fatal("expected error from invalid ADR-ID format")
	}
	combined := stderr + err.Error()
	if !strings.Contains(combined, "formato de ADR inválido") {
		t.Errorf("expected Spanish 'formato de ADR inválido'; got: %s", combined)
	}
	if !strings.Contains(combined, "ADR-NNNN") {
		t.Errorf("error should hint at expected format; got: %s", combined)
	}
}

func TestAck_ValidationFailure_409_RendersTightenViolation(t *testing.T) {

	body := `{"error":"tighten_violation","detail":{"rule_path":"amendment.cooldown_hours","current_value":"24","proposed_value":"12","reason":"proposed value loosens cooldown by 50%; tighten-only invariant inv-zen-136 forbids loosening per-project override knobs","validator_message":"ValidateTighten failed: amendment.cooldown_hours new=12 old=24 (decrease violates tighten-only direction)","invariant":"inv-zen-140"}}`
	srv := buildAckServer(t, http.StatusConflict, body, nil)
	defer srv.Close()

	_, _, err := invokeAmendment(t, []string{"ack", "ADR-0050"}, srv.URL)
	if err == nil {
		t.Fatal("expected error from 409 tighten violation")
	}
	for _, want := range []string{"rechazada por el validador", "amendment.cooldown_hours", "loosens", "inv-zen-140"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected %q in error; got: %v", want, err)
		}
	}
}

func TestAck_409_NonStructuredBody_FallsBackGeneric(t *testing.T) {
	srv := buildAckServer(t, http.StatusConflict, `"opaque legacy error string"`, nil)
	defer srv.Close()

	_, _, err := invokeAmendment(t, []string{"ack", "ADR-0050"}, srv.URL)
	if err == nil {
		t.Fatal("expected error from 409")
	}
	if !strings.Contains(err.Error(), "rechazada por el validador") {
		t.Errorf("expected fallback 'rechazada por el validador'; got: %v", err)
	}
	if !strings.Contains(err.Error(), "opaque legacy error string") {
		t.Errorf("opaque body should be surfaced for triage; got: %v", err)
	}
}

func TestAck_NetworkError_FriendlyExit(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	_, _, err := invokeAmendment(t, []string{"ack", "ADR-0050"}, srv.URL)
	if err == nil {
		t.Fatal("expected network error")
	}
	if !strings.Contains(err.Error(), "petición HTTP") && !strings.Contains(err.Error(), "daemon") {
		t.Errorf("expected friendly network error; got: %v", err)
	}
}

func TestAck_Server500_PreservesBody(t *testing.T) {
	srv := buildAckServer(t, http.StatusInternalServerError,
		`{"error":"applier panic","detail":"git commit failed"}`, nil)
	defer srv.Close()

	_, _, err := invokeAmendment(t, []string{"ack", "ADR-0050"}, srv.URL)
	if err == nil {
		t.Fatal("expected 500 error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status 500; got: %v", err)
	}
	if !strings.Contains(err.Error(), "git commit failed") {
		t.Errorf("error should preserve body; got: %v", err)
	}
}

func TestDeny_HappyPath_RequiresReason(t *testing.T) {
	var captured []byte
	srv := buildDenyServer(t, http.StatusOK, `{"status":"denied"}`, &captured)
	defer srv.Close()

	stdout, stderr, err := invokeAmendment(t, []string{"deny", "ADR-0050", "--reason", "proposal too aggressive for current traffic profile"}, srv.URL)
	if err != nil {
		t.Fatalf("deny: %v stderr=%q", err, stderr)
	}
	if !strings.Contains(stdout, "ADR-0050") || !strings.Contains(stdout, "rechazada") {
		t.Errorf("expected Spanish 'rechazada' confirmation; got: %s", stdout)
	}
	var req cli.AmendmentDecision
	if err := json.Unmarshal(captured, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.ID != "ADR-0050" || req.Reason != "proposal too aggressive for current traffic profile" {
		t.Errorf("request body = %+v", req)
	}
}

func TestDeny_MissingReason_RejectsClientSide(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("server should NOT be contacted when --reason is missing")
	}))
	defer srv.Close()

	_, stderr, err := invokeAmendment(t, []string{"deny", "ADR-0050"}, srv.URL)
	if err == nil {
		t.Fatal("expected error for missing --reason")
	}
	if !strings.Contains(stderr+err.Error(), "--reason es obligatorio") {
		t.Errorf("expected '--reason es obligatorio' error; got stderr=%q err=%v", stderr, err)
	}
}

func TestDeny_EmptyReason_RejectsClientSide(t *testing.T) {
	_, stderr, err := invokeAmendment(t, []string{"deny", "ADR-0050", "--reason", "   "}, "http://localhost:1")
	if err == nil {
		t.Fatal("expected error for whitespace --reason")
	}
	if !strings.Contains(stderr+err.Error(), "razón no puede ser vacía") {
		t.Errorf("expected 'razón no puede ser vacía'; got: %s err=%v", stderr, err)
	}
}

func TestDeny_InvalidADRIDFormat_RejectsClientSide(t *testing.T) {
	_, stderr, err := invokeAmendment(t, []string{"deny", "not-an-adr", "--reason", "x"}, "http://localhost:1")
	if err == nil {
		t.Fatal("expected error for invalid ADR-ID format")
	}
	if !strings.Contains(stderr+err.Error(), "formato de ADR inválido") {
		t.Errorf("expected 'formato de ADR inválido'; got: %s err=%v", stderr, err)
	}
}

func TestDeny_404_RendersAsGenericError(t *testing.T) {
	srv := buildDenyServer(t, http.StatusNotFound, `ADR-9999 does not exist`, nil)
	defer srv.Close()

	_, _, err := invokeAmendment(t, []string{"deny", "ADR-9999", "--reason", "test"}, srv.URL)
	if err == nil {
		t.Fatal("expected 404 error")
	}
	if !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "ADR-9999") {
		t.Errorf("error should mention 404 + body; got: %v", err)
	}
}

func TestDeny_NetworkError_FriendlyExit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	_, _, err := invokeAmendment(t, []string{"deny", "ADR-0050", "--reason", "x"}, srv.URL)
	if err == nil {
		t.Fatal("expected network error")
	}
	if !strings.Contains(err.Error(), "petición HTTP") && !strings.Contains(err.Error(), "daemon") {
		t.Errorf("expected friendly network error; got: %v", err)
	}
}

func buildRevertServer(t *testing.T, statusCode int, body string, capturedBody *[]byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/doctrine/revert" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if capturedBody != nil {
			*capturedBody, _ = io.ReadAll(r.Body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = io.WriteString(w, body)
	}))
}

func TestRevert_HappyPath_NoReason(t *testing.T) {
	var captured []byte
	srv := buildRevertServer(t, http.StatusOK, `{"status":"reverted"}`, &captured)
	defer srv.Close()

	stdout, stderr, err := invokeAmendment(t, []string{"revert", "ADR-0050"}, srv.URL)
	if err != nil {
		t.Fatalf("revert: %v stderr=%q", err, stderr)
	}
	if !strings.Contains(stdout, "ADR-0050") || !strings.Contains(stdout, "revertida") {
		t.Errorf("expected Spanish 'revertida' confirmation; got: %s", stdout)
	}
	// Operator-manual revert MUST surface DoctrineAmendmentReverted in the
	// audit trail per invariant — distinguish from telemetry-driven
	// DoctrineAutonomousReverted.
	if !strings.Contains(stdout, "DoctrineAmendmentReverted") {
		t.Errorf("expected DoctrineAmendmentReverted event reference; got: %s", stdout)
	}
	var req cli.AmendmentDecision
	if err := json.Unmarshal(captured, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.ID != "ADR-0050" || req.Reason != "" {
		t.Errorf("request body = %+v", req)
	}
}

func TestRevert_HappyPath_WithReason(t *testing.T) {
	var captured []byte
	srv := buildRevertServer(t, http.StatusOK, `{"status":"reverted"}`, &captured)
	defer srv.Close()

	_, _, err := invokeAmendment(t, []string{"revert", "ADR-0050", "--reason", "amendment introduced regression in cost-degradation telemetry"}, srv.URL)
	if err != nil {
		t.Fatalf("revert --reason: %v", err)
	}
	var req cli.AmendmentDecision
	if err := json.Unmarshal(captured, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.Reason != "amendment introduced regression in cost-degradation telemetry" {
		t.Errorf("request body Reason = %q", req.Reason)
	}
}

func TestRevert_MissingADRID_RejectsClientSide(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("server should NOT be contacted on missing ADR-ID")
	}))
	defer srv.Close()

	_, stderr, err := invokeAmendment(t, []string{"revert"}, srv.URL)
	if err == nil {
		t.Fatal("expected error from missing ADR-ID")
	}
	if !strings.Contains(stderr+err.Error(), "se requiere el identificador") {
		t.Errorf("expected Spanish error; got stderr=%q err=%v", stderr, err)
	}
}

func TestRevert_InvalidADRIDFormat_RejectsClientSide(t *testing.T) {
	_, stderr, err := invokeAmendment(t, []string{"revert", "ADR-x"}, "http://localhost:1")
	if err == nil {
		t.Fatal("expected error from invalid ADR-ID")
	}
	if !strings.Contains(stderr+err.Error(), "formato de ADR inválido") {
		t.Errorf("expected 'formato de ADR inválido'; got: %s err=%v", stderr, err)
	}
}

func TestRevert_NotInAcceptedState_409Surface(t *testing.T) {

	body := `{"error":"invalid_state","detail":"ADR-0050 is in state 'reverted'; only 'accepted' ADRs may be reverted","invariant":"inv-zen-141"}`
	srv := buildRevertServer(t, http.StatusConflict, body, nil)
	defer srv.Close()

	_, _, err := invokeAmendment(t, []string{"revert", "ADR-0050"}, srv.URL)
	if err == nil {
		t.Fatal("expected 409 error")
	}
	if !strings.Contains(err.Error(), "409") {
		t.Errorf("error should mention 409; got: %v", err)
	}
	if !strings.Contains(err.Error(), "only 'accepted' ADRs may be reverted") {
		t.Errorf("error should preserve body for triage; got: %v", err)
	}
	if !strings.Contains(err.Error(), "inv-zen-141") {
		t.Errorf("error should preserve invariant tag; got: %v", err)
	}
}

func TestRevert_NetworkError_FriendlyExit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	_, _, err := invokeAmendment(t, []string{"revert", "ADR-0050"}, srv.URL)
	if err == nil {
		t.Fatal("expected network error")
	}
	if !strings.Contains(err.Error(), "petición HTTP") && !strings.Contains(err.Error(), "daemon") {
		t.Errorf("expected friendly network error; got: %v", err)
	}
}

func TestRevert_Server500_PreservesBody(t *testing.T) {
	srv := buildRevertServer(t, http.StatusInternalServerError,
		`{"error":"reverter_panic","detail":"git revert exit 128: nothing to revert"}`, nil)
	defer srv.Close()

	_, _, err := invokeAmendment(t, []string{"revert", "ADR-0050"}, srv.URL)
	if err == nil {
		t.Fatal("expected 500 error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status 500; got: %v", err)
	}
	if !strings.Contains(err.Error(), "git revert exit 128") {
		t.Errorf("error should preserve body; got: %v", err)
	}
}

func buildProposeServer(t *testing.T, statusCode int, body string, capturedBody *[]byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/doctrine/propose" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if capturedBody != nil {
			*capturedBody, _ = io.ReadAll(r.Body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = io.WriteString(w, body)
	}))
}

func TestPropose_HappyPath_AllRequiredFlags(t *testing.T) {
	var captured []byte
	body := `{"id":"ADR-0050","status":"proposed","rule_path":"amendment.cooldown_hours","new_value":"12","category":"merge","proposed_at":1714737600,"proposer":"operator","adr_markdown_path":"docs/decisions/proposed/0050-amendment-cooldown.md"}`
	srv := buildProposeServer(t, http.StatusOK, body, &captured)
	defer srv.Close()

	stdout, stderr, err := invokeAmendment(t, []string{
		"propose",
		"amendment.cooldown_hours", "12",
		"--justify", "max-scope cooldown of 24h is too long for hot-fix iteration cadence; 12h matches observed P50 operator-ack latency",
		"--category", "merge",
	}, srv.URL)
	if err != nil {
		t.Fatalf("propose: %v stderr=%q", err, stderr)
	}
	for _, want := range []string{"ADR-0050", "propuesta creada", "amendment.cooldown_hours", "merge"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in stdout: %s", want, stdout)
		}
	}
	var req cli.AmendmentProposeRequest
	if err := json.Unmarshal(captured, &req); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	if req.RulePath != "amendment.cooldown_hours" || req.NewValue != "12" || req.Category != "merge" {
		t.Errorf("request body fields = %+v", req)
	}
	if !strings.Contains(req.Justification, "P50 operator-ack latency") {
		t.Errorf("Justification not propagated: %q", req.Justification)
	}
	if req.CooldownOverride {
		t.Errorf("CooldownOverride should default false")
	}
}

func TestPropose_HappyPath_WithCooldownOverride(t *testing.T) {
	var captured []byte
	body := `{"id":"ADR-0051","status":"proposed","rule_path":"amendment.threshold_pct","new_value":"0.04","category":"cost","proposed_at":1714737600}`
	srv := buildProposeServer(t, http.StatusOK, body, &captured)
	defer srv.Close()

	_, _, err := invokeAmendment(t, []string{
		"propose",
		"amendment.threshold_pct", "0.04",
		"--justify", "tighten preemptively",
		"--category", "cost",
		"--cooldown-override",
	}, srv.URL)
	if err != nil {
		t.Fatalf("propose --cooldown-override: %v", err)
	}
	var req cli.AmendmentProposeRequest
	if err := json.Unmarshal(captured, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !req.CooldownOverride {
		t.Errorf("CooldownOverride should be true")
	}
}

func TestPropose_MissingPositionalArgs_Errors(t *testing.T) {
	_, stderr, err := invokeAmendment(t, []string{"propose"}, "http://localhost:1")
	if err == nil {
		t.Fatal("expected error from missing positional args")
	}
	if !strings.Contains(stderr+err.Error(), "se requieren") {
		t.Errorf("expected 'se requieren <rule_path> y <new_value>'; got: %s err=%v", stderr, err)
	}
}

func TestPropose_MissingNewValue_Errors(t *testing.T) {
	_, stderr, err := invokeAmendment(t, []string{"propose", "amendment.cooldown_hours"}, "http://localhost:1")
	if err == nil {
		t.Fatal("expected error from missing new_value")
	}
	if !strings.Contains(stderr+err.Error(), "se requieren") {
		t.Errorf("expected 'se requieren'; got: %s err=%v", stderr, err)
	}
}

func TestPropose_MissingJustify_Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("server should NOT be contacted on missing --justify")
	}))
	defer srv.Close()

	_, stderr, err := invokeAmendment(t, []string{"propose", "x.y", "z", "--category", "merge"}, srv.URL)
	if err == nil {
		t.Fatal("expected error from missing --justify")
	}
	if !strings.Contains(stderr+err.Error(), "--justify es obligatorio") {
		t.Errorf("expected '--justify es obligatorio'; got: %s err=%v", stderr, err)
	}
}

func TestPropose_EmptyJustify_Errors(t *testing.T) {
	_, stderr, err := invokeAmendment(t, []string{"propose", "x.y", "z", "--justify", "   ", "--category", "cost"}, "http://localhost:1")
	if err == nil {
		t.Fatal("expected error from whitespace --justify")
	}
	if !strings.Contains(stderr+err.Error(), "justificación no puede ser vacía") {
		t.Errorf("expected 'justificación no puede ser vacía'; got: %s err=%v", stderr, err)
	}
}

func TestPropose_MissingCategory_Errors(t *testing.T) {
	_, stderr, err := invokeAmendment(t, []string{"propose", "x.y", "z", "--justify", "x"}, "http://localhost:1")
	if err == nil {
		t.Fatal("expected error from missing --category")
	}
	if !strings.Contains(stderr+err.Error(), "--category es obligatorio") {
		t.Errorf("expected '--category es obligatorio'; got: %s err=%v", stderr, err)
	}
}

func TestPropose_InvalidCategory_RejectsClientSide(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("server should NOT be contacted on invalid --category")
	}))
	defer srv.Close()

	_, stderr, err := invokeAmendment(t, []string{"propose", "x.y", "z", "--justify", "x", "--category", "garbage"}, srv.URL)
	if err == nil {
		t.Fatal("expected error from invalid --category")
	}
	combined := stderr + err.Error()
	if !strings.Contains(combined, "categoría inválida") {
		t.Errorf("expected 'categoría inválida'; got: %s", combined)
	}
	for _, want := range []string{"cost", "merge", "recovery"} {
		if !strings.Contains(combined, want) {
			t.Errorf("expected allowed category %q in error; got: %s", want, combined)
		}
	}
}

func TestPropose_RuleStillInCooldown_429RendersFriendly(t *testing.T) {
	body := `{"error":"rule_in_cooldown","detail":{"rule_path":"amendment.cooldown_hours","cooldown_remaining_seconds":28800,"cooldown_remaining_human":"8h","last_amendment_adr":"ADR-0049","last_amendment_at":"2026-05-03T01:00:00Z","doctrine_cooldown_hours":24,"override_available":true}}`
	srv := buildProposeServer(t, http.StatusTooManyRequests, body, nil)
	defer srv.Close()

	_, _, err := invokeAmendment(t, []string{"propose", "amendment.cooldown_hours", "12", "--justify", "x", "--category", "merge"}, srv.URL)
	if err == nil {
		t.Fatal("expected 429 cooldown error")
	}
	for _, want := range []string{"regla en cooldown", "8h", "--cooldown-override"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected %q in error; got: %v", want, err)
		}
	}
}

func TestPropose_CooldownOpaqueBody_FallsBack(t *testing.T) {
	srv := buildProposeServer(t, http.StatusTooManyRequests, "rate limited", nil)
	defer srv.Close()

	_, _, err := invokeAmendment(t, []string{"propose", "x.y", "z", "--justify", "j", "--category", "cost"}, srv.URL)
	if err == nil {
		t.Fatal("expected 429 error")
	}
	if !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("expected fallback 429 error with body; got: %v", err)
	}
}

func TestPropose_InvalidRulePath_400Surface(t *testing.T) {
	srv := buildProposeServer(t, http.StatusBadRequest,
		`unknown rule path 'nonexistent.path'`, nil)
	defer srv.Close()

	_, _, err := invokeAmendment(t, []string{"propose", "nonexistent.path", "x", "--justify", "x", "--category", "cost"}, srv.URL)
	if err == nil {
		t.Fatal("expected 400 error")
	}
	if !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "unknown rule path") {
		t.Errorf("expected 400 error with body; got: %v", err)
	}
}

func TestPropose_NetworkError_FriendlyExit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	_, _, err := invokeAmendment(t, []string{"propose", "x.y", "z", "--justify", "x", "--category", "cost"}, srv.URL)
	if err == nil {
		t.Fatal("expected network error")
	}
	if !strings.Contains(err.Error(), "petición HTTP") && !strings.Contains(err.Error(), "daemon") {
		t.Errorf("expected friendly network error; got: %v", err)
	}
}

func TestPropose_Server500_PreservesBody(t *testing.T) {
	srv := buildProposeServer(t, http.StatusInternalServerError,
		`{"error":"proposer_panic","detail":"L4 reviewer subagent failed"}`, nil)
	defer srv.Close()

	_, _, err := invokeAmendment(t, []string{"propose", "x.y", "z", "--justify", "x", "--category", "cost"}, srv.URL)
	if err == nil {
		t.Fatal("expected 500 error")
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "L4 reviewer subagent failed") {
		t.Errorf("error should preserve 500 body; got: %v", err)
	}
}

func TestAmendmentGroup_AllFiveCommandsRegistered(t *testing.T) {
	root := cli.NewRoot()
	want := map[string]bool{
		"propose-list": false,
		"ack":          false,
		"deny":         false,
		"revert":       false,
		"propose":      false,
	}
	for _, sub := range root.Commands() {
		if sub.GroupID != "amendment" {
			continue
		}
		leaf := strings.Fields(sub.Use)[0]
		if _, ok := want[leaf]; !ok {
			t.Errorf("unexpected amendment leaf %q", leaf)
			continue
		}
		want[leaf] = true
	}
	for leaf, found := range want {
		if !found {
			t.Errorf("missing amendment leaf %q (Phase K final surface)", leaf)
		}
	}
}
